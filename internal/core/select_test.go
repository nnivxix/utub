package core

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/media"
)

func videoRow(id string, height int) media.Format {
	return media.Format{ID: id, Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: height}
}

func muxedRow(id string, height int) media.Format {
	return media.Format{ID: id, Ext: "mp4", VCodec: "avc1.42001E", ACodec: "mp4a.40.2", Height: height}
}

func audioRow(id, ext string) media.Format {
	return media.Format{ID: id, Ext: ext, VCodec: "none", ACodec: "mp4a.40.2", ABR: 128}
}

func requestFor(format media.Format, cfg config.Config) Request {
	return Request{
		URL:    "https://www.youtube.com/watch?v=aqz-KE-bpKQ",
		Format: format,
		OutDir: "/tmp",
		Config: cfg,
	}
}

// h264Chain is the fallback chain every video request must end with.
func h264Chain(height int) string {
	return fmt.Sprintf(
		"bv*[vcodec^=avc1][height=%d]+ba[ext=m4a]/"+
			"bv*[vcodec^=avc1][height<=%d]+ba[ext=m4a]/"+
			"b[vcodec^=avc1][height<=%d]/"+
			"bv*[vcodec^=avc1]+ba",
		height, height, height)
}

func TestSelectorVideoPinsThePickedItag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		format media.Format
	}{
		{"1080p60", videoRow("299", 1080)},
		{"1080p30", videoRow("137", 1080)},
		{"720p", videoRow("136", 720)},
		{"480p", videoRow("135", 480)},
		{"360p", videoRow("134", 360)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, warn := Selector(requestFor(tc.format, config.Default()))

			want := tc.format.ID + "+ba[ext=m4a]/" + h264Chain(tc.format.Height)
			if expr != want {
				t.Errorf("expr =\n  %s\nwant\n  %s", expr, want)
			}
			if warn != "" {
				t.Errorf("warn = %q, want empty", warn)
			}
		})
	}
}

func TestSelectorMuxedPickIsPinnedWithoutAudio(t *testing.T) {
	t.Parallel()

	format := muxedRow("18", 360)
	expr, warn := Selector(requestFor(format, config.Default()))

	want := "18/" + h264Chain(360)
	if expr != want {
		t.Errorf("expr =\n  %s\nwant\n  %s", expr, want)
	}
	if warn != "" {
		t.Errorf("warn = %q, want empty", warn)
	}
}

func TestSelectorWithoutItagUsesTheChain(t *testing.T) {
	t.Parallel()

	expr, warn := Selector(requestFor(videoRow("", 720), config.Default()))
	if want := h264Chain(720); expr != want {
		t.Errorf("expr =\n  %s\nwant\n  %s", expr, want)
	}
	if warn != "" {
		t.Errorf("warn = %q, want empty", warn)
	}
}

func TestSelectorUnknownHeightWarns(t *testing.T) {
	t.Parallel()

	expr, warn := Selector(requestFor(videoRow("137", 0), config.Default()))

	// Without a height there is no way to verify the itag is within the H.264
	// ceiling, so it is not pinned at all.
	if want := "bv*[vcodec^=avc1]+ba"; expr != want {
		t.Errorf("expr = %q, want %q", expr, want)
	}
	if !strings.Contains(warn, "no resolution") {
		t.Errorf("warn = %q, want a resolution warning", warn)
	}
}

func TestSelectorAboveH264CeilingWarns(t *testing.T) {
	t.Parallel()

	expr, warn := Selector(requestFor(videoRow("337", 2160), config.Default()))

	// The chain is capped at 1080p and the unpinnable 2160p itag is skipped.
	want := h264Chain(media.MaxH264Height)
	if expr != want {
		t.Errorf("expr =\n  %s\nwant\n  %s", expr, want)
	}
	if !strings.Contains(warn, "1080p") {
		t.Errorf("warn = %q, want the H.264 ceiling", warn)
	}
}

// TestSelectorNeverPinsNonH264 enforces the policy for requests that bypass the
// picker: a VP9 row must not be selected by itag.
func TestSelectorNeverPinsNonH264(t *testing.T) {
	t.Parallel()

	vp9 := media.Format{ID: "315", Ext: "webm", VCodec: "vp9", ACodec: "none", Height: 2160}
	expr, warn := Selector(requestFor(vp9, config.Default()))

	if strings.HasPrefix(expr, "315") {
		t.Errorf("expr = %q, must not pin a VP9 itag", expr)
	}
	if want := h264Chain(media.MaxH264Height); expr != want {
		t.Errorf("expr =\n  %s\nwant\n  %s", expr, want)
	}
	if !strings.Contains(warn, "1080p") {
		t.Errorf("warn = %q, want the H.264 ceiling", warn)
	}
}

func TestSelectorAudio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		audio  string
		format media.Format
		want   string
	}{
		{"original", config.AudioOriginal, audioRow("251", "webm"), "ba/b"},
		{"m4a prefers an AAC source", config.AudioM4A, audioRow("140", "m4a"), "ba[ext=m4a]/ba/b"},
		{"mp3 re-encodes from the best source", config.AudioMP3, audioRow("140", "m4a"), "ba/b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.DefaultAudioFormat = tc.audio

			expr, warn := Selector(requestFor(tc.format, cfg))
			if expr != tc.want {
				t.Errorf("expr = %q, want %q", expr, tc.want)
			}
			if warn != "" {
				t.Errorf("warn = %q, want empty", warn)
			}
			if strings.Contains(expr, "vcodec") {
				t.Errorf("expr = %q, audio must not filter on video codec", expr)
			}
		})
	}
}

func TestAudioArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"original keeps the stream", config.AudioOriginal, nil},
		{"m4a extracts to AAC", config.AudioM4A, []string{"-x", "--audio-format", "m4a", "--audio-quality", "0"}},
		{"mp3 re-encodes", config.AudioMP3, []string{"-x", "--audio-format", "mp3", "--audio-quality", "0"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.DefaultAudioFormat = tc.in

			if got := AudioArgs(cfg); !slices.Equal(got, tc.want) {
				t.Errorf("AudioArgs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	if got := AudioArgs(config.Config{}); got != nil {
		t.Errorf("AudioArgs(zero config) = %q, want nil", got)
	}
}

func TestSelectorZeroConfigDefaultsToH264AndOriginalAudio(t *testing.T) {
	t.Parallel()

	expr, _ := Selector(requestFor(videoRow("137", 1080), config.Config{}))
	if want := "137+ba[ext=m4a]/" + h264Chain(1080); expr != want {
		t.Errorf("zero config expr =\n  %s\nwant\n  %s", expr, want)
	}

	if expr, _ := Selector(requestFor(audioRow("251", "webm"), config.Config{})); expr != "ba/b" {
		t.Errorf("zero config audio expr = %q, want ba/b", expr)
	}
}

// TestSelectorAlternativesAreWellFormed guards the string yt-dlp parses: no
// empty alternatives, the chain always ends on the documented fallback, and
// every video alternative filters for H.264.
func TestSelectorAlternativesAreWellFormed(t *testing.T) {
	t.Parallel()

	videoCases := []media.Format{
		videoRow("299", 1080),
		videoRow("136", 720),
		muxedRow("18", 360),
		videoRow("137", 0),
		videoRow("337", 2160),
	}

	for _, format := range videoCases {
		expr, _ := Selector(requestFor(format, config.Default()))
		alts := strings.Split(expr, "/")

		for i, alt := range alts {
			if strings.TrimSpace(alt) == "" {
				t.Errorf("%s: alternative %d is empty in %q", format.ID, i, expr)
			}
		}
		if got := alts[len(alts)-1]; got != "bv*[vcodec^=avc1]+ba" {
			t.Errorf("%s: last alternative = %q, want the documented fallback", format.ID, got)
		}

		// An unknown height legitimately collapses the chain to its fallback.
		wantFilters := 4
		if format.Height <= 0 {
			wantFilters = 1
		}
		if got := strings.Count(expr, "[vcodec^=avc1]"); got != wantFilters {
			t.Errorf("%s: expr has %d H.264 filters, want %d: %q", format.ID, got, wantFilters, expr)
		}
	}
}
