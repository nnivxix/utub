package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/media"
)

// Fetch tuning. These are constants rather than settings because nobody has
// asked to change them and every knob is a support burden: retries cover the
// flaky mid-download 403s that are the single most common yt-dlp failure.
const (
	downloadRetries  = "10"
	fragmentRetries  = "10"
	killGracePeriod  = 3 * time.Second
	stderrKeepBytes  = 64 << 10
	scanLineMaxBytes = 1 << 20
)

// tempDirName is the scratch directory inside the download folder: separate
// fragment/merge files live here so a cancelled job never leaves half-muxed
// output next to finished videos.
const tempDirName = ".utub-tmp"

// Result is what one finished download produced.
type Result struct {
	Path     string        // final file, exactly as yt-dlp reported it
	Bytes    int64         // size of Path, 0 when it could not be stat'ed
	Duration time.Duration // wall clock, including ffmpeg merge
}

// Executor runs one download as a subprocess.
//
// The zero value is usable: Bin and Aria2c are resolved from PATH and NewCmd
// defaults to os/exec.
type Executor struct {
	Bin    string // path to yt-dlp; "" means "yt-dlp" from PATH
	Aria2c string // path to aria2c; "" means "not installed"

	// NewCmd builds the subprocess. Injected so tests can drive a shell script
	// instead of yt-dlp, the same seam Prober.Run uses for probing.
	NewCmd func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// Args builds the exact yt-dlp command line for one request.
//
// It is pure — no I/O, no PATH lookups — so the whole contract in
// docs/architecture.md §7 can be asserted in table tests.
//
// Ordering is deliberate and stable: format and post-processing first, then
// paths, then the machine-readable progress contract, then network tuning, then
// the optional acceleration and auth flags, and the URL last.
func (e Executor) Args(req Request) []string {
	selector, _ := Selector(req)
	audioOnly := req.Format.Kind() == media.KindAudioOnly

	args := make([]string, 0, 32)
	args = append(args, "-f", selector)

	// Audio extraction flags only make sense for an audio request; -x on a
	// video would produce an audio file.
	if audioOnly {
		args = append(args, AudioArgs(req.Config)...)
	}

	args = append(args,
		"-P", "home:"+req.OutDir,
		"-P", "temp:"+filepath.Join(req.OutDir, tempDirName),
		"-o", req.Config.FilenameTemplate,
		"--newline",
		"--no-colors",
		"--no-playlist",
		"--progress-template", ProgressTemplate,
		"--concurrent-fragments", strconv.Itoa(req.Config.ConcurrentFragments),
		"--http-chunk-size", req.Config.HTTPChunkSize,
		"--retries", downloadRetries,
		"--fragment-retries", fragmentRetries,
		"--continue",
	)

	// Merging two streams into MP4 is the whole point of the H.264 policy;
	// an audio request has nothing to merge.
	if !audioOnly {
		args = append(args, "--merge-output-format", "mp4")
	}

	if browser := strings.TrimSpace(req.Config.CookiesFromBrowser); browser != "" {
		args = append(args, "--cookies-from-browser", browser)
	}

	if e.useAria2c(req.Config) {
		// -x16 -s16: sixteen connections split over sixteen segments of 1 MiB.
		// The fragment concurrency above still applies to DASH streams.
		args = append(args, "--downloader", "aria2c",
			"--downloader-args", "aria2c:-x16 -s16 -k1M -j16")
	}

	return append(args, req.URL)
}

// useAria2c reports whether the request should be handed to aria2c.
//
// auto means "use it when it exists"; builtin never does; the explicit aria2c
// setting is honoured and fails loudly in Run when the binary is missing, so a
// misconfiguration is never silently downgraded.
func (e Executor) useAria2c(cfg config.Config) bool {
	switch cfg.Downloader {
	case config.DownloaderBuiltin:
		return false
	case config.DownloaderAria2c:
		return true
	default:
		return e.Aria2c != ""
	}
}

// Run downloads one request, reporting progress through emit, which may be nil.
//
// Emitted events carry no JobID: the manager owns identity and fills it in.
// The stage moves video → audio → merge as yt-dlp reports it, and a non-fatal
// selection warning is delivered as the first event.
//
// Cancellation is cooperative: the process is started without
// exec.CommandContext on purpose, because CommandContext kills only the direct
// child. yt-dlp spawns ffmpeg and aria2c, and those would survive as orphans.
// Instead a watcher sends SIGTERM to the whole process group and escalates to
// SIGKILL after killGracePeriod. .part files are left alone so --continue
// resumes them.
func (e Executor) Run(ctx context.Context, req Request, emit func(Event)) (Result, error) {
	if req.Config.Downloader == config.DownloaderAria2c && e.Aria2c == "" {
		return Result{}, ErrMissingAria2c
	}

	bin := e.Bin
	if bin == "" {
		bin = "yt-dlp"
	}

	if _, warn := Selector(req); warn != "" && emit != nil {
		emit(Event{Stage: StageVideo, Percent: PercentUnknown, Warn: warn})
	}

	cmd := e.command(ctx, bin, e.Args(req)...)
	configureProcess(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("core: download %s: stdout pipe: %w", req.URL, err)
	}
	stderr := &tailBuffer{max: stderrKeepBytes}
	cmd.Stderr = stderr

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("core: download %s: start %s: %w", req.URL, bin, err)
	}

	// Watch for cancellation while the process runs. The done channel stops the
	// watcher when the process exits on its own.
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			terminateProcess(cmd, false)
			select {
			case <-watchDone:
			case <-time.After(killGracePeriod):
				terminateProcess(cmd, true)
			}
		case <-watchDone:
		}
	}()

	tracker := &outputTracker{stage: StageVideo}
	scanErr := scanProgress(stdout, emit, tracker)

	waitErr := cmd.Wait()
	close(watchDone)

	res := Result{
		Path:     tracker.path,
		Duration: time.Since(started),
	}
	if res.Path != "" {
		if info, statErr := os.Stat(res.Path); statErr == nil {
			res.Bytes = info.Size()
		}
	}

	switch {
	case ctx.Err() != nil:
		return res, ctx.Err()
	case waitErr != nil:
		return res, wrapCLI("core: download", req.URL, stderr.Bytes(), waitErr)
	case scanErr != nil && !errors.Is(scanErr, io.EOF):
		return res, fmt.Errorf("core: download %s: read progress: %w", req.URL, scanErr)
	case res.Path == "":
		return res, fmt.Errorf("%w: %s", ErrNoOutputPath, req.URL)
	}

	if emit != nil {
		emit(Event{Stage: StageDone, Percent: 1})
	}
	return res, nil
}

// command builds the subprocess, preferring the injected factory.
func (e Executor) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if e.NewCmd != nil {
		return e.NewCmd(ctx, name, args...)
	}
	// Deliberately not exec.CommandContext: cancellation is handled by killing
	// the process group, see Run.
	return exec.Command(name, args...)
}

// scanProgress reads yt-dlp's stdout line by line, turning progress rows into
// events and stage changes into stage events.
func scanProgress(stdout io.Reader, emit func(Event), tracker *outputTracker) error {
	scanner := bufio.NewScanner(stdout)
	// Destination lines carry the full output path, and titles can be long.
	scanner.Buffer(make([]byte, 0, 64<<10), scanLineMaxBytes)

	// Percentages are clamped monotonic within a stage. yt-dlp reports
	// total_bytes as "known so far" for fragmented streams, so a raw
	// downloaded/total ratio can go backwards; a progress bar that retreats
	// reads as a bug.
	lastPercent := float64(PercentUnknown)
	prevStage := tracker.stage
	stageStarted := false

	for scanner.Scan() {
		line := scanner.Text()

		if stage, ok := tracker.observe(line); ok {
			// Announce a stage once, when it begins. Later lines that only
			// refine the path (MoveFiles after Merger) must not re-announce it.
			if !stageStarted || stage != prevStage {
				stageStarted = true
				prevStage = stage
				lastPercent = PercentUnknown
				if emit != nil {
					emit(Event{Stage: stage, Percent: PercentUnknown})
				}
			}
			continue
		}

		sample, ok := parseProgressLine(line)
		if !ok {
			continue
		}
		if sample.Percent >= 0 && lastPercent >= 0 && sample.Percent < lastPercent {
			sample.Percent = lastPercent
		}
		if sample.Percent >= 0 {
			lastPercent = sample.Percent
		}
		if emit == nil {
			continue
		}
		emit(Event{
			Stage:   tracker.stage,
			Percent: sample.Percent,
			Bytes:   sample.Bytes,
			Total:   sample.Total,
			Speed:   sample.Speed,
			ETA:     sample.ETA,
		})
	}

	return scanner.Err()
}

// outputTracker follows the file yt-dlp says it is writing.
//
// The engine never derives the output name from the template: post-processors
// rename files (merge, extract, remux, "already downloaded"), and the template
// is user-configurable. Every path here comes from yt-dlp's own output.
type outputTracker struct {
	stage Stage
	path  string

	sawVideo bool
}

// observe inspects one line for a stage transition or a new destination path.
// It returns ok when the line was recognised as a stage marker.
func (t *outputTracker) observe(line string) (Stage, bool) {
	switch {
	case strings.HasPrefix(line, "[download] Destination: "):
		t.path = strings.TrimSpace(strings.TrimPrefix(line, "[download] Destination: "))

		// yt-dlp downloads the video stream first, then the audio stream, so
		// the second separate stream is the audio one. A muxed format has only
		// one.
		if t.stage == StageVideo && t.sawVideo {
			t.stage = StageAudio
		}
		t.sawVideo = true
		return t.stage, true

	case strings.HasPrefix(line, "[download] ") && strings.HasSuffix(line, " has already been downloaded"):
		// Resume/skip path: "…/[title] [id].mp4 has already been downloaded".
		inner := strings.TrimSuffix(strings.TrimPrefix(line, "[download] "), " has already been downloaded")
		t.path = strings.TrimSpace(inner)
		return t.stage, true

	case strings.HasPrefix(line, `[Merger] Merging formats into "`):
		t.path = strings.TrimSuffix(strings.TrimPrefix(line, `[Merger] Merging formats into "`), `"`)
		t.stage = StageMerge
		return t.stage, true

	case strings.HasPrefix(line, "[ExtractAudio] Destination: "):
		t.path = strings.TrimSpace(strings.TrimPrefix(line, "[ExtractAudio] Destination: "))
		t.stage = StagePost
		return t.stage, true

	case strings.HasPrefix(line, `[MoveFiles] Moving file "`):
		// yt-dlp downloads into -P temp: and only moves the finished file into
		// -P home: at the very end. This line is the authoritative final path;
		// everything before it points at the scratch directory.
		rest := strings.TrimPrefix(line, `[MoveFiles] Moving file "`)
		if i := strings.LastIndex(rest, `" to "`); i >= 0 {
			t.path = strings.TrimSuffix(rest[i+len(`" to "`):], `"`)
			return t.stage, true
		}

	case strings.HasPrefix(line, "[VideoRemuxer] Remuxing video from ") ||
		strings.HasPrefix(line, "[VideoConvertor] Converting video from "):
		t.stage = StagePost
		return t.stage, true
	}

	return t.stage, false
}

// tailBuffer keeps only the last max bytes of stderr.
//
// yt-dlp can emit megabytes of warnings before failing; the classified error is
// always at the end, so keeping the head would defeat the purpose.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n := len(p)
	if n >= b.max {
		b.buf = append(b.buf[:0], p[n-b.max:]...)
		return n, nil
	}
	if overflow := len(b.buf) + n - b.max; overflow > 0 {
		b.buf = b.buf[overflow:]
	}
	b.buf = append(b.buf, p...)
	return n, nil
}

func (b *tailBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
}
