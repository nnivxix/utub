package core

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/media"
)

// videoRequest is a 1080p H.264 request with a stable output directory, so the
// argv can be compared as literals.
func videoRequest(t *testing.T) Request {
	t.Helper()

	cfg := config.Default()
	cfg.DownloadDir = "/Users/kai/Downloads"

	return Request{
		URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Format: media.Format{
			ID:     "137",
			Ext:    "mp4",
			VCodec: "avc1.640028",
			ACodec: "none",
			Height: 1080,
			FPS:    30,
		},
		OutDir: "/Users/kai/Downloads",
		Config: cfg,
	}
}

const wantVideoSelector = "137+ba[ext=m4a]" +
	"/bv*[vcodec^=avc1][height=1080]+ba[ext=m4a]" +
	"/bv*[vcodec^=avc1][height<=1080]+ba[ext=m4a]" +
	"/b[vcodec^=avc1][height<=1080]" +
	"/bv*[vcodec^=avc1]+ba"

func TestExecutorArgsVideo(t *testing.T) {
	req := videoRequest(t)

	want := []string{
		"-f", wantVideoSelector,
		"-P", "home:/Users/kai/Downloads",
		"-P", "temp:/Users/kai/Downloads/.utub-tmp",
		"-o", "%(title)s [%(id)s].%(ext)s",
		"--newline",
		"--no-colors",
		"--no-playlist",
		"--progress-template", ProgressTemplate,
		"--concurrent-fragments", "16",
		"--http-chunk-size", "10M",
		"--retries", "10",
		"--fragment-retries", "10",
		"--continue",
		"--merge-output-format", "mp4",
		req.URL,
	}

	got := Executor{}.Args(req)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestExecutorArgsAudio(t *testing.T) {
	req := videoRequest(t)
	req.Format = media.Format{ID: "140", Ext: "m4a", ACodec: "mp4a.40.2", ABR: 128}
	req.Config.DefaultAudioFormat = config.AudioMP3

	got := Executor{}.Args(req)

	if !containsSequence(got, "-x", "--audio-format", "mp3", "--audio-quality", "0") {
		t.Errorf("mp3 extraction flags missing from %q", got)
	}
	if containsSequence(got, "--merge-output-format", "mp4") {
		t.Errorf("audio request should not merge into mp4: %q", got)
	}
	if !containsSequence(got, "-f", "140/ba/b") {
		t.Errorf("audio selector missing from %q", got)
	}
	if !containsSequence(got, "--embed-thumbnail") {
		t.Errorf("cover-art flag missing from %q", got)
	}
	if got[len(got)-1] != req.URL {
		t.Errorf("URL must be last, got %q", got)
	}
}

func TestExecutorArgsAudioOriginalSkipsExtraction(t *testing.T) {
	req := videoRequest(t)
	req.Format = media.Format{ID: "251", Ext: "webm", ACodec: "opus", ABR: 160}
	req.Config.DefaultAudioFormat = config.AudioOriginal

	got := Executor{}.Args(req)
	if containsSequence(got, "-x") {
		t.Errorf("original audio must not pass -x: %q", got)
	}
	if containsSequence(got, "--audio-format") {
		t.Errorf("original audio must not pass --audio-format: %q", got)
	}
	if containsSequence(got, "--embed-thumbnail") {
		t.Errorf("original opus cannot carry cover art: %q", got)
	}
}

func TestExecutorArgsAria2c(t *testing.T) {
	tests := []struct {
		name       string
		downloader string
		path       string
		want       bool
	}{
		{"auto with binary", config.DownloaderAuto, "/opt/homebrew/bin/aria2c", true},
		{"auto without binary", config.DownloaderAuto, "", false},
		{"builtin wins over binary", config.DownloaderBuiltin, "/opt/homebrew/bin/aria2c", false},
		{"explicit aria2c", config.DownloaderAria2c, "/opt/homebrew/bin/aria2c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := videoRequest(t)
			req.Config.Downloader = tt.downloader

			got := Executor{Aria2c: tt.path}.Args(req)

			has := containsSequence(got, "--downloader", "aria2c")
			if has != tt.want {
				t.Errorf("aria2c in argv = %v, want %v (argv %q)", has, tt.want, got)
			}
			if tt.want && !containsSequence(got, "--downloader-args", "aria2c:-x16 -s16 -k1M -j16") {
				t.Errorf("aria2c tuning flags missing from %q", got)
			}
		})
	}
}

func TestExecutorArgsCookies(t *testing.T) {
	req := videoRequest(t)
	req.Config.CookiesFromBrowser = "safari"

	got := Executor{}.Args(req)
	if !containsSequence(got, "--cookies-from-browser", "safari") {
		t.Errorf("cookies flag missing from %q", got)
	}
}

func TestExecutorArgsHonoursConfig(t *testing.T) {
	req := videoRequest(t)
	req.Config.ConcurrentFragments = 8
	req.Config.HTTPChunkSize = "4M"

	got := Executor{}.Args(req)
	if !containsSequence(got, "--concurrent-fragments", "8") {
		t.Errorf("fragment count not taken from config: %q", got)
	}
	if !containsSequence(got, "--http-chunk-size", "4M") {
		t.Errorf("chunk size not taken from config: %q", got)
	}
}

func TestOutputTracker(t *testing.T) {
	tracker := &outputTracker{stage: StageVideo}

	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantStage Stage
		wantPath  string
	}{
		{
			name:      "video stream destination",
			line:      "[download] Destination: /d/video [abc].f137.mp4",
			wantOK:    true,
			wantStage: StageVideo,
			wantPath:  "/d/video [abc].f137.mp4",
		},
		{
			name:      "audio stream destination",
			line:      "[download] Destination: /d/video [abc].f140.m4a",
			wantOK:    true,
			wantStage: StageAudio,
			wantPath:  "/d/video [abc].f140.m4a",
		},
		{
			name:      "merge renames the output",
			line:      `[Merger] Merging formats into "/d/video [abc].mp4"`,
			wantOK:    true,
			wantStage: StageMerge,
			wantPath:  "/d/video [abc].mp4",
		},
		{
			name:      "already downloaded",
			line:      "[download] /d/video [abc].mp4 has already been downloaded",
			wantOK:    true,
			wantStage: StageMerge,
			wantPath:  "/d/video [abc].mp4",
		},
		{
			name:      "final move out of the temp dir",
			line:      `[MoveFiles] Moving file "/d/.utub-tmp/video [abc].mp4" to "/d/video [abc].mp4"`,
			wantOK:    true,
			wantStage: StageMerge,
			wantPath:  "/d/video [abc].mp4",
		},
		{
			name:      "audio extraction",
			line:      "[ExtractAudio] Destination: /d/song [abc].mp3",
			wantOK:    true,
			wantStage: StagePost,
			wantPath:  "/d/song [abc].mp3",
		},
		{
			name:   "progress line is not a stage marker",
			line:   "DL 1 2 NA NA NA NA NA",
			wantOK: false,
		},
		{
			name:   "unrelated line",
			line:   "[youtube] Extracting URL: https://youtu.be/abc",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, ok := tracker.observe(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("observe(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if stage != tt.wantStage {
				t.Errorf("stage = %v, want %v", stage, tt.wantStage)
			}
			if tracker.path != tt.wantPath {
				t.Errorf("path = %q, want %q", tracker.path, tt.wantPath)
			}
		})
	}
}

func TestOutputTrackerMuxedHasOneStage(t *testing.T) {
	tracker := &outputTracker{stage: StageVideo}

	// A muxed format downloads one file: there is no audio stage.
	if stage, ok := tracker.observe("[download] Destination: /d/video [abc].18.mp4"); !ok || stage != StageVideo {
		t.Fatalf("muxed destination = %v, %v; want video stream", stage, ok)
	}
	if tracker.stage != StageVideo {
		t.Errorf("stage after a single stream = %v, want video", tracker.stage)
	}
}

func TestTailBufferKeepsTail(t *testing.T) {
	b := &tailBuffer{max: 8}

	for _, chunk := range []string{"aaaa", "bbbb", "cccc"} {
		if _, err := b.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}

	if got := string(b.Bytes()); got != "bbbbcccc" {
		t.Errorf("tail = %q, want %q", got, "bbbbcccc")
	}

	// A single write larger than the buffer keeps only its tail.
	if _, err := b.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if got := string(b.Bytes()); got != "23456789" {
		t.Errorf("tail after oversize write = %q, want %q", got, "23456789")
	}
}

func TestExecutorRejectsForcedAria2cWhenMissing(t *testing.T) {
	req := videoRequest(t)
	req.Config.Downloader = config.DownloaderAria2c

	_, err := Executor{}.Run(t.Context(), req, nil)
	if err == nil {
		t.Fatal("Run succeeded with downloader=aria2c and no binary")
	}
	if !strings.Contains(err.Error(), "aria2c") {
		t.Errorf("error = %q, want it to name aria2c", err)
	}
}

// containsSequence reports whether want appears as a contiguous run in got.
func containsSequence(got []string, want ...string) bool {
	if len(want) == 0 {
		return true
	}
	for i := 0; i+len(want) <= len(got); i++ {
		match := true
		for j := range want {
			if got[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
