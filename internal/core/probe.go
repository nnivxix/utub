package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nnivxix/utub/internal/media"
)

// probeTimeout bounds one `yt-dlp -J`, which normally takes 2-4 seconds but can
// hang on a dead network.
const probeTimeout = 90 * time.Second

// runner executes a binary and keeps stdout and stderr apart, so yt-dlp's
// warnings can never contaminate the JSON on stdout. Injected so tests do not
// shell out — the same seam platform/clipboard.go uses.
type runner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// Prober resolves a URL into metadata and formats.
//
// The zero value is usable and looks up "yt-dlp" on PATH; the real manager
// fills Bin from binmgr so a pinned binary is honoured.
type Prober struct {
	Bin string // path to yt-dlp; "" means "yt-dlp" from PATH
	Run runner // nil means run the real process
}

// Probe runs `yt-dlp -J` and maps the result into a MediaInfo.
//
// The URL is re-validated here rather than trusted: probe is reachable from the
// API in M4, where the caller is HTTP and not the TUI's already-checked field.
func (p Prober) Probe(ctx context.Context, rawURL string) (media.MediaInfo, error) {
	ref, err := media.ParseURL(rawURL)
	if err != nil {
		return media.MediaInfo{}, err
	}

	run := p.Run
	if run == nil {
		run = execRunner
	}
	bin := p.Bin
	if bin == "" {
		bin = "yt-dlp"
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	stdout, stderr, err := run(ctx, bin, probeArgs(ref.URL)...)
	if err != nil {
		return media.MediaInfo{}, probeFailure(ref.URL, stderr, err)
	}

	return parseProbe(stdout, ref)
}

// probeArgs is the exact command line. --no-playlist keeps a watch URL with a
// list= parameter from expanding into a playlist.
func probeArgs(url string) []string {
	return []string{"-J", "--no-playlist", "--no-warnings", "--no-progress", url}
}

// probeJSON is the subset of yt-dlp's dump we consume. media.Format already
// carries the JSON tags for formats[].
type probeJSON struct {
	Type       string         `json:"_type"`
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	Channel    string         `json:"channel"`
	Uploader   string         `json:"uploader"`
	Duration   float64        `json:"duration"`
	ViewCount  int64          `json:"view_count"`
	LiveStatus string         `json:"live_status"`
	Formats    []media.Format `json:"formats"`
}

// parseProbe validates and converts one JSON dump.
func parseProbe(stdout []byte, ref media.Ref) (media.MediaInfo, error) {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return media.MediaInfo{}, fmt.Errorf("%w: empty stdout", ErrBadProbe)
	}

	var raw probeJSON
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return media.MediaInfo{}, fmt.Errorf("%w: %v", ErrBadProbe, err)
	}

	// A playlist dump is a different shape entirely; refuse it instead of
	// guessing which entry the user meant.
	if raw.Type == "playlist" {
		return media.MediaInfo{}, fmt.Errorf("%w: %s", media.ErrPlaylistOnly, ref.URL)
	}
	if len(raw.Formats) == 0 {
		return media.MediaInfo{}, fmt.Errorf("%w: %s", ErrNoFormats, ref.URL)
	}

	return media.MediaInfo{
		ID:        raw.ID,
		Title:     raw.Title,
		Channel:   raw.Channel,
		Uploader:  raw.Uploader,
		Duration:  time.Duration(raw.Duration * float64(time.Second)),
		ViewCount: raw.ViewCount,
		IsLive:    raw.LiveStatus == "is_live" || raw.LiveStatus == "is_upcoming",
		Source:    ref.Source,
		URL:       ref.URL,
		Formats:   raw.Formats,
	}, nil
}

// probeFailure turns a non-zero exit into something a person can read.
//
// TODO(M1.6): classify stderr into ErrBotCheck / ErrPrivate / ErrGeoBlocked /
// ErrAgeRestricted / ErrHTTPStatus and attach the hint each one needs.
func probeFailure(url string, stderr []byte, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("core: probe %s: timed out after %s", url, probeTimeout)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("core: probe %s: canceled", url)
	}
	if line := firstLineOf(stderr); line != "" {
		return fmt.Errorf("core: probe %s: %s: %w", url, line, err)
	}
	return fmt.Errorf("core: probe %s: %w", url, err)
}

// firstLineOf returns the first non-empty, trimmed line of a multi-line output.
func firstLineOf(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
