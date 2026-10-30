package media

import (
	"testing"
	"time"
)

func videoOnly(id string, height int, fps float64, vcodec string, tbr float64) Format {
	return Format{ID: id, Ext: "mp4", VCodec: vcodec, ACodec: "none",
		Height: height, FPS: fps, TBR: tbr}
}

func audioOnly(id, ext, acodec string, abr float64) Format {
	return Format{ID: id, Ext: ext, VCodec: "none", ACodec: acodec, ABR: abr}
}

func muxed(id string, height int, fps float64, tbr float64) Format {
	return Format{ID: id, Ext: "mp4", VCodec: "avc1.42001E", ACodec: "mp4a.40.2",
		Height: height, FPS: fps, TBR: tbr, ABR: 128}
}

func TestKind(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		want   Kind
	}{
		{"video only", videoOnly("137", 1080, 30, "avc1.640028", 4000), KindVideoOnly},
		{"audio only", audioOnly("140", "m4a", "mp4a.40.2", 128), KindAudioOnly},
		{"muxed", muxed("18", 360, 30, 700), KindMuxed},
		{"empty codecs", Format{ID: "x"}, KindUnknown},
		{"explicit none", Format{VCodec: "none", ACodec: "none"}, KindUnknown},
		{"uppercase NONE", Format{VCodec: "NONE", ACodec: "mp4a.40.2"}, KindAudioOnly},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.format.Kind(); got != tt.want {
				t.Errorf("Kind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodecDetection(t *testing.T) {
	tests := []struct {
		vcodec         string
		acodec         string
		h264, aac, hdr bool
	}{
		{"avc1.640028", "mp4a.40.2", true, true, false},
		{"avc1.4d401f", "none", true, false, false},
		{"h264", "none", true, false, false},
		{"vp9", "opus", false, false, false},
		{"av01.0.08M.08", "opus", false, false, false},
		{"", "", false, false, false},
		{"none", "none", false, false, false},
	}

	for _, tt := range tests {
		f := Format{VCodec: tt.vcodec, ACodec: tt.acodec}
		if got := f.IsH264(); got != tt.h264 {
			t.Errorf("IsH264(%q) = %v, want %v", tt.vcodec, got, tt.h264)
		}
		if got := f.IsAAC(); got != tt.aac {
			t.Errorf("IsAAC(%q) = %v, want %v", tt.acodec, got, tt.aac)
		}
	}

	if !(Format{DynamicRange: "HDR"}).IsHDR() {
		t.Error("IsHDR() = false for HDR dynamic range")
	}
	if (Format{DynamicRange: "SDR"}).IsHDR() {
		t.Error("IsHDR() = true for SDR dynamic range")
	}
}

func TestFormatLabel(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		want   string
	}{
		{"1080p30", videoOnly("137", 1080, 30, "avc1", 4000), "1080p"},
		{"1080p60", videoOnly("299", 1080, 60, "avc1", 6000), "1080p60"},
		{"59.94 rounds to 60", videoOnly("299", 1080, 59.94, "avc1", 6000), "1080p60"},
		{"23.976 stays silent", videoOnly("278", 720, 23.976, "avc1", 2000), "720p"},
		{"hdr", Format{Height: 2160, FPS: 60, VCodec: "av01", DynamicRange: "HDR"}, "2160p60 HDR"},
		{"audio with bitrate", audioOnly("140", "m4a", "mp4a.40.2", 129.5), "audio 130k"},
		{"audio without bitrate", Format{VCodec: "none", ACodec: "opus", Ext: "webm"}, "audio webm"},
		{"no height falls back to id", Format{ID: "137", VCodec: "avc1"}, "137"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.format.Label(); got != tt.want {
				t.Errorf("Label() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEstimateSize(t *testing.T) {
	oneMinute := time.Minute

	t.Run("exact filesize wins", func(t *testing.T) {
		f := Format{FileSize: 123456, TBR: 5000}
		if got := f.EstimateSize(oneMinute); got != 123456 {
			t.Errorf("EstimateSize() = %d, want 123456", got)
		}
	})

	t.Run("bitrate times duration", func(t *testing.T) {
		// 800 kbit/s for 60s = 6,000,000 bytes
		f := Format{TBR: 800}
		if got := f.EstimateSize(oneMinute); got != 6_000_000 {
			t.Errorf("EstimateSize() = %d, want 6000000", got)
		}
	})

	t.Run("falls back to audio bitrate", func(t *testing.T) {
		// 128 kbit/s for 60s = 960,000 bytes
		f := Format{ABR: 128}
		if got := f.EstimateSize(oneMinute); got != 960_000 {
			t.Errorf("EstimateSize() = %d, want 960000", got)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		if got := (Format{}).EstimateSize(oneMinute); got != 0 {
			t.Errorf("EstimateSize() = %d, want 0", got)
		}
		if got := (Format{TBR: 800}).EstimateSize(0); got != 0 {
			t.Errorf("EstimateSize(0 duration) = %d, want 0", got)
		}
	})
}

func TestVideoCandidatesH264Only(t *testing.T) {
	formats := []Format{
		videoOnly("337", 2160, 60, "av01.0.12M.10", 20000), // AV1, dropped
		videoOnly("313", 2160, 30, "vp9", 18000),           // VP9, dropped
		videoOnly("399", 1080, 60, "av01.0.08M.08", 9000),  // AV1, dropped
		videoOnly("299", 1080, 60, "avc1.64002a", 6000),    // kept
		videoOnly("137", 1080, 30, "avc1.640028", 4000),    // kept
		muxed("22", 720, 30, 2000),                         // kept (muxed)
		videoOnly("136", 720, 30, "avc1.4d401f", 1500),     // kept, beats muxed
		audioOnly("140", "m4a", "mp4a.40.2", 128),          // no video, dropped
	}

	// 1080p60, 1080p30 and 720p30 -> three buckets: the muxed 720p row loses
	// to the video-only 136 in its own bucket, so it never becomes its own row.
	got := VideoCandidates(formats, false)
	if len(got) != 3 {
		t.Fatalf("VideoCandidates returned %d rows, want 3: %v", len(got), ids(got))
	}
	for _, f := range got {
		if !f.IsH264() {
			t.Errorf("row %s is not H.264 (%s)", f.ID, f.VCodec)
		}
	}
	if got[0].ID != "299" {
		t.Errorf("first row = %s (%s), want 299 (1080p60)", got[0].ID, got[0].Label())
	}
	if got[2].ID != "136" {
		t.Errorf("720p row = %s, want the video-only 136 over muxed 22", got[2].ID)
	}
	if got[1].ID != "137" {
		t.Errorf("second row = %s, want 137 (1080p30)", got[1].ID)
	}
}

func TestVideoCandidatesAllowAny(t *testing.T) {
	formats := []Format{
		videoOnly("337", 2160, 60, "av01", 20000),
		videoOnly("299", 1080, 60, "avc1", 6000),
	}

	got := VideoCandidates(formats, true)
	if len(got) != 2 {
		t.Fatalf("VideoCandidates(allowAny) returned %d rows, want 2", len(got))
	}
	if got[0].Height != 2160 {
		t.Errorf("first row height = %d, want 2160", got[0].Height)
	}
}

func TestVideoCandidatesSortsByHeightThenFPS(t *testing.T) {
	formats := []Format{
		videoOnly("a", 720, 30, "avc1", 1000),
		videoOnly("b", 1080, 30, "avc1", 3000),
		videoOnly("c", 1080, 60, "avc1", 5000),
		videoOnly("d", 360, 30, "avc1", 500),
	}

	got := VideoCandidates(formats, false)
	wantOrder := []string{"c", "b", "a", "d"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("row %d = %s, want %s (full order %v)", i, got[i].ID, want, ids(got))
		}
	}
}

func TestVideoCandidatesEmpty(t *testing.T) {
	if got := VideoCandidates(nil, false); len(got) != 0 {
		t.Errorf("VideoCandidates(nil) = %v, want empty", got)
	}
	if got := VideoCandidates([]Format{audioOnly("140", "m4a", "mp4a", 128)}, false); len(got) != 0 {
		t.Errorf("VideoCandidates(audio only) = %v, want empty", got)
	}
}

func TestAudioCandidates(t *testing.T) {
	formats := []Format{
		audioOnly("251", "webm", "opus", 160),
		audioOnly("140", "m4a", "mp4a.40.2", 128),
		audioOnly("139", "m4a", "mp4a.40.2", 48),
		audioOnly("140-dup", "m4a", "mp4a.40.2", 129), // same bucket as 140
		muxed("18", 360, 30, 700),                     // has audio but is muxed
		videoOnly("137", 1080, 30, "avc1", 4000),      // no audio
	}

	got := AudioCandidates(formats)
	if len(got) != 3 {
		t.Fatalf("AudioCandidates returned %d rows, want 3: %v", len(got), ids(got))
	}
	if got[0].ID != "251" {
		t.Errorf("first row = %s, want 251 (highest bitrate)", got[0].ID)
	}
	for _, f := range got {
		if f.Kind() != KindAudioOnly {
			t.Errorf("row %s kind = %v, want audio-only", f.ID, f.Kind())
		}
	}
}

func TestAudioCandidatesDedupesWithinContainerOnly(t *testing.T) {
	// Bucketing is per container, so m4a and webm at the same bitrate are two
	// legitimate choices; the picker shows both and AAC sorts first.
	formats := []Format{
		audioOnly("webm", "webm", "opus", 128),
		audioOnly("m4a", "m4a", "mp4a.40.2", 128),
	}

	got := AudioCandidates(formats)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (one per container): %v", len(got), ids(got))
	}
	if got[0].ID != "m4a" {
		t.Errorf("first row = %s, want m4a (AAC sorts before opus on equal bitrate)", got[0].ID)
	}
}

func TestRecommend(t *testing.T) {
	t.Run("prefers highest at or below 1080p", func(t *testing.T) {
		video := VideoCandidates([]Format{
			videoOnly("2160", 2160, 60, "av01", 20000),
			videoOnly("1080", 1080, 60, "avc1", 6000),
			videoOnly("720", 720, 30, "avc1", 2000),
		}, true)

		got, ok := Recommend(video)
		if !ok {
			t.Fatal("Recommend returned ok = false")
		}
		if got.Height != 1080 {
			t.Errorf("recommended height = %d, want 1080", got.Height)
		}
	})

	t.Run("falls back to highest when everything is above 1080p", func(t *testing.T) {
		video := VideoCandidates([]Format{
			videoOnly("2160", 2160, 60, "av01", 20000),
		}, true)

		got, ok := Recommend(video)
		if !ok || got.Height != 2160 {
			t.Errorf("Recommend() = (%d, %v), want (2160, true)", got.Height, ok)
		}
	})

	t.Run("empty", func(t *testing.T) {
		if _, ok := Recommend(nil); ok {
			t.Error("Recommend(nil) returned ok = true")
		}
	})
}

func TestMediaInfoHelpers(t *testing.T) {
	info := MediaInfo{
		ID:       "dQw4w9WgXcQ",
		Title:    "Test",
		Duration: 3*time.Minute + 33*time.Second,
		Formats: []Format{
			videoOnly("299", 1080, 60, "avc1", 6000),
			videoOnly("337", 2160, 60, "av01", 20000),
			audioOnly("140", "m4a", "mp4a.40.2", 128),
		},
	}

	if got := info.VideoFormats(false); len(got) != 1 || got[0].Height != 1080 {
		t.Errorf("VideoFormats(false) = %v, want only the 1080p H.264 row", ids(got))
	}
	if got := info.VideoFormats(true); len(got) != 2 {
		t.Errorf("VideoFormats(true) returned %d rows, want 2", len(got))
	}
	if got := info.AudioFormats(); len(got) != 1 || got[0].ID != "140" {
		t.Errorf("AudioFormats() = %v, want the m4a row", ids(got))
	}
	if got, ok := info.Recommended(false); !ok || got.ID != "299" {
		t.Errorf("Recommended(false) = (%s, %v), want 299", got.ID, ok)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "~"},
		{-1, "~"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{10 << 20, "10.0 MB"},
		{62400000, "59.5 MB"},
		{1 << 30, "1.0 GB"},
		{3 << 40, "3.0 TB"},
	}

	for _, tt := range tests {
		if got := HumanBytes(tt.in); got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "--:--"},
		{-5 * time.Second, "--:--"},
		{252 * time.Second, "4:12"},
		{3725 * time.Second, "1:02:05"},
		{59*time.Second + 800*time.Millisecond, "1:00"},
	}

	for _, tt := range tests {
		if got := HumanDuration(tt.in); got != tt.want {
			t.Errorf("HumanDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func ids(formats []Format) []string {
	out := make([]string, len(formats))
	for i, f := range formats {
		out[i] = f.ID
	}
	return out
}
