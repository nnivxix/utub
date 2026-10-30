// Package binmgr locates the external binaries utub shells out to — yt-dlp,
// ffmpeg and optionally aria2c — and reports whether they are usable.
//
// Nothing here needs to know which operating system it runs on: install hints
// list every supported package manager and the caller prints whichever lines
// apply to its audience. That keeps the "OS assumptions live in
// internal/platform" rule from docs/architecture.md §3 intact.
//
// Version probing is deliberately shallow. utub needs to know one thing: is the
// yt-dlp new enough for the --progress-template fields and the fragment flags we
// build in core. Everything else is reported, not enforced.
package binmgr

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Binary names as they appear on PATH.
const (
	YTDLP  = "yt-dlp"
	FFmpeg = "ffmpeg"
	Aria2c = "aria2c"
)

// MinYTDLPVersion is the oldest yt-dlp utub supports.
//
// This is a policy gate, not a claim about when a flag was introduced: it is
// the oldest release utub is tested against. Older builds may well work; they
// are simply not something we can promise, because the progress template and
// fragment flags are the engine's whole observability story.
const MinYTDLPVersion = "2024.01.01"

// probeTimeout bounds each version probe so --doctor cannot hang on a broken
// wrapper script.
const probeTimeout = 5 * time.Second

// Sentinel errors. Callers compare with errors.Is and show Tool.Hints.
var (
	// ErrNotFound means the binary is not on PATH.
	ErrNotFound = errors.New("binmgr: binary not found")

	// ErrTooOld means yt-dlp is below MinYTDLPVersion.
	ErrTooOld = errors.New("binmgr: version too old")

	// ErrMissingRequired means at least one required binary is unusable.
	ErrMissingRequired = errors.New("binmgr: missing required binaries")
)

// versionArgs is the flag each tool needs to print its version.
var versionArgs = map[string][]string{
	YTDLP:  {"--version"},
	FFmpeg: {"-version"},
	Aria2c: {"--version"},
}

// installHints lists every supported install path, per tool.
var installHints = map[string][]string{
	YTDLP: {
		"brew install yt-dlp",
		"pipx install yt-dlp",
		"sudo apt install yt-dlp",
		"winget install yt-dlp.yt-dlp",
	},
	FFmpeg: {
		"brew install ffmpeg",
		"sudo apt install ffmpeg",
		"winget install Gyan.FFmpeg",
	},
	Aria2c: {
		"brew install aria2",
		"sudo apt install aria2",
		"winget install aria2.aria2",
	},
}

// Hints returns the install commands for name, or nil for an unknown tool.
func Hints(name string) []string { return installHints[name] }

// Tool is one external binary and what we could learn about it.
type Tool struct {
	Name     string
	Path     string // absolute path when found, "" otherwise
	Version  string // parsed version, "" when unknown
	Required bool   // false for optional accelerators such as aria2c
	Hints    []string
	Err      error // why the tool is unusable; nil when Available
}

// Available reports whether the tool was found and is usable.
func (t Tool) Available() bool { return t.Path != "" && t.Err == nil }

// Set is the outcome of locating everything the engine needs.
type Set struct {
	YTDLP  Tool
	FFmpeg Tool
	Aria2c Tool
}

// All returns the tools in display order.
func (s Set) All() []Tool { return []Tool{s.YTDLP, s.FFmpeg, s.Aria2c} }

// Missing returns the required tools that are unusable.
func (s Set) Missing() []Tool {
	var missing []Tool
	for _, tool := range s.All() {
		if tool.Required && !tool.Available() {
			missing = append(missing, tool)
		}
	}
	return missing
}

// Ready returns nil when every required tool is usable, otherwise
// ErrMissingRequired naming the offenders. Hints are intentionally left to the
// caller so a TUI can render them differently from a CLI.
func (s Set) Ready() error {
	missing := s.Missing()
	if len(missing) == 0 {
		return nil
	}

	names := make([]string, 0, len(missing))
	for _, tool := range missing {
		names = append(names, tool.Name)
	}
	return fmt.Errorf("%w: %s", ErrMissingRequired, strings.Join(names, ", "))
}

// Locate finds all three binaries and applies the version gate to yt-dlp.
//
// A missing aria2c is not an error: it only upgrades the downloader from
// yt-dlp's builtin fragment concurrency to multi-connection ranges.
func Locate(ctx context.Context) Set {
	set := Set{
		YTDLP:  Lookup(ctx, YTDLP, true),
		FFmpeg: Lookup(ctx, FFmpeg, true),
		Aria2c: Lookup(ctx, Aria2c, false),
	}
	set.YTDLP = gateYTDLP(set.YTDLP)
	return set
}

// Lookup resolves name on PATH and probes its version.
func Lookup(ctx context.Context, name string, required bool) Tool {
	tool := Tool{
		Name:     name,
		Required: required,
		Hints:    Hints(name),
	}

	path, err := exec.LookPath(name)
	if err != nil {
		tool.Err = fmt.Errorf("%w: %s", ErrNotFound, name)
		return tool
	}
	tool.Path = path

	out, err := probe(ctx, path, versionArgs[name])
	if err != nil {
		tool.Err = err
		return tool
	}
	tool.Version = versionFromOutput(out)
	return tool
}

// gateYTDLP marks yt-dlp unusable when it is older than MinYTDLPVersion.
//
// An unparsable version is left alone on purpose: distro builds and git
// snapshots use version strings we cannot order, and refusing to run them would
// be worse than letting the download itself fail.
func gateYTDLP(tool Tool) Tool {
	if !tool.Available() || tool.Version == "" {
		return tool
	}
	ok, err := AtLeast(tool.Version, MinYTDLPVersion)
	if err != nil || ok {
		return tool
	}
	tool.Err = fmt.Errorf("%w: have %s, need >= %s", ErrTooOld, tool.Version, MinYTDLPVersion)
	return tool
}

// probe runs the version command and returns its stdout.
func probe(ctx context.Context, path string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
				return "", fmt.Errorf("binmgr: %s: %s", path, firstLine(msg))
			}
		}
		return "", fmt.Errorf("binmgr: %s: %w", path, err)
	}
	return string(out), nil
}

// firstLine keeps an error message on one line for multi-line tool output.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
