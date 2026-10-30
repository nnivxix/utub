package core

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseProgressLineTemplate(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    progressSample
		wantOK  bool
		approxP float64 // expected percent, compared with a small epsilon
	}{
		{
			name: "full tick",
			line: "DL 1234567 89456789 89456789 6291456.0 48 12 46",
			want: progressSample{
				Bytes: 1234567,
				Total: 89456789,
				Speed: 6291456,
				ETA:   48 * time.Second,
			},
			wantOK:  true,
			approxP: 1234567.0 / 89456789.0,
		},
		{
			name: "no exact total, estimate wins",
			line: "DL 1000 NA 2000 500.0 10 NA NA",
			want: progressSample{
				Bytes: 1000,
				Total: 2000,
				Speed: 500,
				ETA:   10 * time.Second,
			},
			wantOK:  true,
			approxP: 0.5,
		},
		{
			name: "fragments when bytes are unknown",
			line: "DL NA NA NA NA NA 12 46",
			want: progressSample{
				Bytes: 0,
				Total: 0,
				Speed: 0,
				ETA:   0,
			},
			wantOK:  true,
			approxP: 12.0 / 46.0,
		},
		{
			name:   "fragment zero of many",
			line:   "DL NA NA NA 1000.0 NA 0 46",
			want:   progressSample{Speed: 1000},
			wantOK: true,
			// percent stays unknown until a fragment arrives
			approxP: PercentUnknown,
		},
		{
			name: "older yt-dlp, fields truncated",
			line: "DL 1000 2000",
			want: progressSample{
				Bytes: 1000,
				Total: 2000,
			},
			wantOK:  true,
			approxP: 0.5,
		},
		{
			name:    "percent clamped when estimate was low",
			line:    "DL 2000 1000 NA NA NA NA NA",
			want:    progressSample{Bytes: 2000, Total: 1000},
			wantOK:  true,
			approxP: 1,
		},
		{
			name:    "lowercase na is a placeholder too",
			line:    "DL 500 na 1000 na na na na",
			want:    progressSample{Bytes: 500, Total: 1000},
			wantOK:  true,
			approxP: 0.5,
		},
		{
			name:   "negative values are treated as unknown",
			line:   "DL -1 -2 NA -3 NA NA NA",
			want:   progressSample{},
			wantOK: false,
		},
		{
			name:   "nothing but placeholders",
			line:   "DL NA NA NA NA NA NA NA",
			want:   progressSample{},
			wantOK: false,
		},
		{
			name:   "empty payload",
			line:   "DL ",
			want:   progressSample{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseProgressLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("parseProgressLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.Bytes != tt.want.Bytes || got.Total != tt.want.Total {
				t.Errorf("bytes/total = %d/%d, want %d/%d", got.Bytes, got.Total, tt.want.Bytes, tt.want.Total)
			}
			if math.Abs(got.Speed-tt.want.Speed) > 0.001 {
				t.Errorf("speed = %v, want %v", got.Speed, tt.want.Speed)
			}
			if got.ETA != tt.want.ETA {
				t.Errorf("eta = %v, want %v", got.ETA, tt.want.ETA)
			}
			if math.Abs(got.Percent-tt.approxP) > 1e-6 {
				t.Errorf("percent = %v, want %v", got.Percent, tt.approxP)
			}
		})
	}
}

func TestParseProgressLineLegacy(t *testing.T) {
	mib := float64(1 << 20)
	gib := float64(1 << 30)

	tests := []struct {
		name       string
		line       string
		wantOK     bool
		wantPct    float64
		wantTotal  int64
		wantSpeed  float64
		wantETA    time.Duration
		approxByte bool
	}{
		{
			name:      "with speed and eta",
			line:      "[download]  43.2% of 62.4MiB at 6.2MiB/s ETA 00:48",
			wantOK:    true,
			wantPct:   0.432,
			wantTotal: int64(62.4 * mib),
			wantSpeed: 6.2 * mib,
			wantETA:   48 * time.Second,
		},
		{
			name:      "final line with duration instead of eta",
			line:      "[download] 100% of 62.4MiB in 00:48",
			wantOK:    true,
			wantPct:   1,
			wantTotal: int64(62.4 * mib),
		},
		{
			name:      "approximate size marker",
			line:      "[download]  10.0% of ~1.2GiB at 3.0MiB/s ETA 01:02:03",
			wantOK:    true,
			wantPct:   0.1,
			wantTotal: int64(1.2 * gib),
			wantSpeed: 3 * mib,
			wantETA:   1*time.Hour + 2*time.Minute + 3*time.Second,
		},
		{
			name:      "unknown speed",
			line:      "[download]  12.5% of 1.2GiB at Unknown speed ETA Unknown",
			wantOK:    true,
			wantPct:   0.125,
			wantTotal: int64(1.2 * gib),
		},
		{
			name:   "destination line is not progress",
			line:   "[download] Destination: /Users/kai/Downloads/video [abc].f137.mp4",
			wantOK: false,
		},
		{
			name:   "merger line is not progress",
			line:   `[Merger] Merging formats into "/Users/kai/Downloads/video.mp4"`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseProgressLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("parseProgressLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if math.Abs(got.Percent-tt.wantPct) > 1e-6 {
				t.Errorf("percent = %v, want %v", got.Percent, tt.wantPct)
			}
			if got.Total != tt.wantTotal {
				t.Errorf("total = %d, want %d", got.Total, tt.wantTotal)
			}
			if math.Abs(got.Speed-tt.wantSpeed) > 1 {
				t.Errorf("speed = %v, want %v", got.Speed, tt.wantSpeed)
			}
			if got.ETA != tt.wantETA {
				t.Errorf("eta = %v, want %v", got.ETA, tt.wantETA)
			}
		})
	}
}

func TestParseProgressLineIgnoresNoise(t *testing.T) {
	noise := []string{
		"",
		"   ",
		"[youtube] Extracting URL: https://youtu.be/dQw4w9WgXcQ",
		"[info] dQw4w9WgXcQ: Downloading 1 format(s): 299+140",
		"WARNING: Falling back on generic information extractor",
		"DL",
		"DL not numbers at all",
	}

	for _, line := range noise {
		if _, ok := parseProgressLine(line); ok {
			t.Errorf("parseProgressLine(%q) reported progress, want ignored", line)
		}
	}
}

func TestParseProgressLineTrimsWhitespace(t *testing.T) {
	got, ok := parseProgressLine("  \t DL 1000 2000 NA NA NA NA NA  \r")
	if !ok {
		t.Fatal("padded progress line was not recognised")
	}
	if got.Percent != 0.5 {
		t.Errorf("percent = %v, want 0.5", got.Percent)
	}
}

func TestParseETA(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"48", 48 * time.Second},
		{"0", 0},
		{"00:48", 48 * time.Second},
		{"01:02:03", time.Hour + 2*time.Minute + 3*time.Second},
		{"70:00", 70 * time.Minute},
		{"NA", 0},
		{"", 0},
		{"notatime", 0},
		{"1:2:3:4", 0},
	}

	for _, tt := range tests {
		if got := parseETA(tt.in); got != tt.want {
			t.Errorf("parseETA(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestSizeMultiplier(t *testing.T) {
	tests := []struct {
		unit string
		want float64
	}{
		{"B", 1},
		{"KiB", 1 << 10},
		{"MiB", 1 << 20},
		{"GiB", 1 << 30},
		{"TiB", 1 << 40},
		{"MB", 1 << 20},
		{"kib", 1 << 10},
		{"", 1},
	}

	for _, tt := range tests {
		if got := sizeMultiplier(tt.unit); got != tt.want {
			t.Errorf("sizeMultiplier(%q) = %v, want %v", tt.unit, got, tt.want)
		}
	}
}

func TestClamp01(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{0.5, 0.5},
		{-0.2, 0},
		{1.7, 1},
		{0, 0},
		{math.NaN(), 0},
	}

	for _, tt := range tests {
		if got := clamp01(tt.in); got != tt.want {
			t.Errorf("clamp01(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// collect drains a coalesced channel, failing the test if it does not close in
// time. The deadline keeps a broken coalescer from hanging the suite.
func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()

	deadline := time.After(2 * time.Second)
	var got []Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("coalesce did not close; collected %d events", len(got))
		}
	}
}

func TestCoalesceKeepsFirstAndTerminal(t *testing.T) {
	// A one-hour window makes the timer irrelevant: only the leading edge and
	// the terminal event can be forwarded.
	in := make(chan Event)
	out := coalesce(context.Background(), in, time.Hour)

	go func() {
		for i := 0; i < 50; i++ {
			in <- Event{Percent: float64(i) / 50}
		}
		in <- Event{Stage: StageDone, Percent: 1}
	}()

	got := collect(t, out)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (leading edge + terminal): %+v", len(got), got)
	}
	if got[0].Percent != 0 {
		t.Errorf("first event percent = %v, want the leading-edge sample 0", got[0].Percent)
	}
	if !got[len(got)-1].Terminal() {
		t.Errorf("last event = %+v, want the terminal event", got[len(got)-1])
	}
}

func TestCoalesceForwardsTerminalError(t *testing.T) {
	in := make(chan Event, 1)
	out := coalesce(context.Background(), in, time.Hour)

	wantErr := errors.New("boom")
	in <- Event{Err: wantErr}
	close(in)

	got := collect(t, out)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if !errors.Is(got[0].Err, wantErr) {
		t.Errorf("event error = %v, want %v", got[0].Err, wantErr)
	}
}

func TestCoalesceFlushesLatestSampleOnClose(t *testing.T) {
	in := make(chan Event)
	out := coalesce(context.Background(), in, time.Hour)

	go func() {
		in <- Event{Percent: 0.1}
		in <- Event{Percent: 0.2}
		in <- Event{Percent: 0.9}
		close(in)
	}()

	got := collect(t, out)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (first + flushed latest): %+v", len(got), got)
	}
	if got[0].Percent != 0.1 {
		t.Errorf("first percent = %v, want 0.1", got[0].Percent)
	}
	if got[1].Percent != 0.9 {
		t.Errorf("flushed percent = %v, want the newest sample 0.9", got[1].Percent)
	}
}

func TestCoalesceWindowEmitsPendingSample(t *testing.T) {
	in := make(chan Event)
	out := coalesce(context.Background(), in, 20*time.Millisecond)

	go func() {
		in <- Event{Percent: 0.1}
		for i := 2; i <= 6; i++ {
			in <- Event{Percent: float64(i) / 10}
		}
		time.Sleep(60 * time.Millisecond) // let at least one window elapse
		in <- Event{Stage: StageDone, Percent: 1}
	}()

	got := collect(t, out)
	if len(got) < 2 || len(got) > 4 {
		t.Fatalf("got %d events, want between 2 and 4: %+v", len(got), got)
	}
	if !got[len(got)-1].Terminal() {
		t.Errorf("last event = %+v, want terminal", got[len(got)-1])
	}
}

func TestCoalesceCapsRate(t *testing.T) {
	in := make(chan Event)
	out := coalesce(context.Background(), in, 20*time.Millisecond)

	const total = 100
	go func() {
		for i := 0; i < total; i++ {
			in <- Event{Percent: float64(i) / total}
			time.Sleep(time.Millisecond)
		}
		close(in)
	}()

	got := collect(t, out)
	// ~100ms of input with a 20ms window: leading edge plus a handful of
	// windows plus the final flush. The bound is deliberately loose; the point
	// is that 100 samples do not become 100 events.
	if len(got) > 15 {
		t.Errorf("got %d events for %d samples, want a capped stream", len(got), total)
	}
	if len(got) == 0 {
		t.Fatal("got no events at all")
	}
}

func TestCoalesceStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan Event)
	out := coalesce(ctx, in, time.Hour)

	in <- Event{Percent: 0.5}
	cancel()

	got := collect(t, out)
	if len(got) != 1 {
		t.Errorf("got %d events before cancellation, want 1", len(got))
	}
}

func TestProgressTemplateMatchesParserPrefix(t *testing.T) {
	// The argv and the parser live apart in code paths but not in intent: if
	// someone edits one, this fails.
	if !strings.HasPrefix(ProgressTemplate, "download:"+progressPrefix) {
		t.Fatalf("ProgressTemplate = %q, want it to emit the %q prefix the parser expects",
			ProgressTemplate, progressPrefix)
	}
	if !strings.Contains(ProgressTemplate, "%(progress.downloaded_bytes)s") {
		t.Error("ProgressTemplate lost the downloaded_bytes field")
	}
	if !strings.Contains(ProgressTemplate, "%(progress.fragment_count)s") {
		t.Error("ProgressTemplate lost the fragment_count field")
	}
}
