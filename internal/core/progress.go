package core

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ProgressTemplate is the --progress-template value the engine passes to yt-dlp.
//
// It lives next to the parser on purpose: the argument and the code that reads
// it cannot drift apart if they are edited in the same file.
//
// yt-dlp prints the part after "download:" once per progress tick, so a line
// looks like:
//
//	DL 1234567 89456789 89456789 6291456.0 48 12 46
const ProgressTemplate = "download:DL " +
	"%(progress.downloaded_bytes)s " +
	"%(progress.total_bytes)s " +
	"%(progress.total_bytes_estimate)s " +
	"%(progress.speed)s " +
	"%(progress.eta)s " +
	"%(progress.fragment_index)s " +
	"%(progress.fragment_count)s"

// progressPrefix is the literal the template above makes yt-dlp emit before the
// numbers.
const progressPrefix = "DL "

// progressInterval is the coalescing window: events leaving the engine are
// capped at roughly 10 Hz so a fast connection cannot flood a terminal or an
// SSE stream. docs/architecture.md §5 fixes this rate.
const progressInterval = 100 * time.Millisecond

// progressSample is one parsed progress line: numbers only, no job identity.
// Callers attach JobID and Stage, which the parser deliberately does not know.
type progressSample struct {
	Percent float64 // 0..1, or PercentUnknown
	Bytes   int64   // downloaded bytes of the current stage
	Total   int64   // expected bytes, 0 when unknown
	Speed   float64 // bytes per second, 0 when unknown
	ETA     time.Duration
}

// parseProgressLine reads one line of yt-dlp output and reports whether it was
// a progress line at all.
//
// Two formats are accepted: the machine-readable template above, and the legacy
// human line ("[download]  43.2% of 62.4MiB at 6.2MiB/s ETA 00:48") as a
// fallback in case a future yt-dlp renames the template fields. Lines that are
// not progress — Destination:, [Merger], warnings — return false and are
// ignored by the caller.
func parseProgressLine(line string) (progressSample, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return progressSample{}, false
	}

	if rest, ok := strings.CutPrefix(line, progressPrefix); ok {
		if s, ok := parseTemplateProgress(rest); ok {
			return s, true
		}
	}

	return parseLegacyProgress(line)
}

// parseTemplateProgress parses the field list after "DL ".
//
// Every field may be "NA" or absent: yt-dlp does not know the size of a DASH
// stream before it starts, and merged post-processing has no bytes at all. A
// line with no usable number is rejected so stray output cannot reset progress.
func parseTemplateProgress(rest string) (progressSample, bool) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return progressSample{}, false
	}

	field := func(i int) string {
		if i < len(fields) {
			return fields[i]
		}
		return ""
	}

	downloaded := parseByteCount(field(0))
	total := parseByteCount(field(1))
	estimate := parseByteCount(field(2))
	speed := parseRate(field(3))
	eta := parseETA(field(4))
	fragment := parseByteCount(field(5))
	fragmentCount := parseByteCount(field(6))

	// Nothing recognisable means this was not a progress tick.
	if downloaded == 0 && total == 0 && estimate == 0 && speed == 0 && fragment == 0 {
		return progressSample{}, false
	}

	// An estimate is only used when yt-dlp has no exact total; reporting both
	// would make the percentage jump around as the estimate is revised.
	expected := total
	if expected <= 0 {
		expected = estimate
	}

	percent := float64(PercentUnknown)
	switch {
	case expected > 0 && downloaded > 0:
		percent = clamp01(float64(downloaded) / float64(expected))
	case fragmentCount > 0 && fragment > 0:
		// Some live/DASH streams never publish a byte total; fragments are then
		// the only honest denominator.
		percent = clamp01(float64(fragment) / float64(fragmentCount))
	}

	return progressSample{
		Percent: percent,
		Bytes:   downloaded,
		Total:   expected,
		Speed:   speed,
		ETA:     eta,
	}, true
}

// legacyProgressRE matches the human-readable line yt-dlp has printed for years:
//
//	[download]  43.2% of 62.4MiB at 6.2MiB/s ETA 00:48
//	[download] 100% of 62.4MiB in 00:48
var legacyProgressRE = regexp.MustCompile(
	`^\[download\]\s+([0-9]+(?:\.[0-9]+)?)%\s+of\s+~?\s*([0-9.]+)\s*([KMGTP]?i?B)` +
		`(?:\s+(?:in\s+([0-9:]+)|at\s+(?:([0-9.]+)\s*([KMGTP]?i?B)/s|Unknown\s+speed)\s+ETA\s+(\S+)))?\s*$`,
)

// parseLegacyProgress is the fallback path for the human line.
func parseLegacyProgress(line string) (progressSample, bool) {
	m := legacyProgressRE.FindStringSubmatch(line)
	if m == nil {
		return progressSample{}, false
	}

	percent, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return progressSample{}, false
	}

	size, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return progressSample{}, false
	}
	total := int64(size * sizeMultiplier(m[3]))
	bytes := int64(float64(total) * percent / 100)

	var speed float64
	if m[5] != "" {
		if v, err := strconv.ParseFloat(m[5], 64); err == nil {
			speed = v * sizeMultiplier(m[6])
		}
	}

	var eta time.Duration
	if len(m) > 7 && m[7] != "" {
		eta = parseETA(m[7])
	}

	return progressSample{
		Percent: clamp01(percent / 100),
		Bytes:   bytes,
		Total:   total,
		Speed:   speed,
		ETA:     eta,
	}, true
}

// clamp01 keeps a percentage in [0,1]: yt-dlp can briefly report more bytes
// than the announced total when an estimate was too low.
func clamp01(v float64) float64 {
	switch {
	case v < 0 || v != v: // negative or NaN
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

// parseByteCount accepts yt-dlp's integer fields and treats "NA", "", "-" and
// negative values as unknown.
func parseByteCount(s string) int64 {
	v, ok := parseNumeric(s)
	if !ok || v <= 0 {
		return 0
	}
	return int64(v)
}

// parseRate accepts the float speed field, treating "NA" as unknown.
func parseRate(s string) float64 {
	v, ok := parseNumeric(s)
	if !ok || v <= 0 {
		return 0
	}
	return v
}

// parseNumeric is the shared "is this a number or a placeholder" check.
func parseNumeric(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	switch strings.ToUpper(s) {
	case "", "NA", "N/A", "-", "NONE", "UNKNOWN":
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseETA accepts the template's integer seconds and the legacy HH:MM:SS.
func parseETA(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	if seconds, ok := parseNumeric(s); ok {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds * float64(time.Second))
	}

	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}

	var total time.Duration
	units := []time.Duration{time.Second, time.Minute, time.Hour}
	// Parse right to left so both MM:SS and HH:MM:SS work.
	for i, unit := range units {
		idx := len(parts) - 1 - i
		if idx < 0 {
			break
		}
		v, err := strconv.ParseInt(parts[idx], 10, 64)
		if err != nil || v < 0 {
			return 0
		}
		total += time.Duration(v) * unit
	}
	return total
}

// sizeMultiplier maps a unit suffix to its byte multiplier. yt-dlp prints
// binary units (MiB, KiB); plain MB is accepted because some builds do.
func sizeMultiplier(unit string) float64 {
	u := strings.ToUpper(strings.TrimSpace(unit))
	u = strings.TrimSuffix(u, "IB")
	u = strings.TrimSuffix(u, "B")

	switch u {
	case "K":
		return 1 << 10
	case "M":
		return 1 << 20
	case "G":
		return 1 << 30
	case "T":
		return 1 << 40
	case "P":
		return 1 << 50
	default:
		return 1
	}
}

// coalesce caps an event stream at one event per interval.
//
// Shape of the guarantee: the first event is forwarded immediately (leading
// edge), later events inside the window are collapsed so only the newest
// survives, and a terminal event is never delayed or dropped — a UI must always
// see the end of a job, even if the channel closes right after it. The returned
// channel is closed when in is closed or ctx is canceled.
func coalesce(ctx context.Context, in <-chan Event, interval time.Duration) <-chan Event {
	const outBuffer = 8

	out := make(chan Event, outBuffer)

	go func() {
		defer close(out)

		var pending *Event

		timer := time.NewTimer(interval)
		if !timer.Stop() {
			<-timer.C
		}
		var window <-chan time.Time // nil disables the case until the window opens

		forward := func(ev Event) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}

		openWindow := func() {
			timer.Reset(interval)
			window = timer.C
		}

		for {
			select {
			case ev, ok := <-in:
				if !ok {
					if pending != nil && !forward(*pending) {
						return
					}
					return
				}

				if ev.Terminal() {
					// The terminal event supersedes any held sample: a stale
					// percentage is less useful than the end of the job.
					pending = nil
					forward(ev)
					return
				}

				if window == nil {
					if !forward(ev) {
						return
					}
					openWindow()
					continue
				}

				sample := ev
				pending = &sample

			case <-window:
				window = nil
				if pending == nil {
					continue
				}
				sample := *pending
				pending = nil
				if !forward(sample) {
					return
				}
				openWindow()

			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}
