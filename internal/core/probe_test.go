package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nnivxix/utub/internal/media"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// stubRunner returns a runner that always yields the given streams and records
// its invocations, so the command line can be asserted without a subprocess.
func stubRunner(stdout, stderr []byte, runErr error, calls *[]call) runner {
	return func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
		if calls != nil {
			*calls = append(*calls, call{name: name, args: args})
		}
		return stdout, stderr, runErr
	}
}

type call struct {
	name string
	args []string
}

func TestProbeMapsVideoFixture(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner(fixture(t, "video.json"), nil, nil, nil)}
	info, err := p.Probe(context.Background(), "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	if info.ID != "aqz-KE-bpKQ" {
		t.Errorf("ID = %q", info.ID)
	}
	if !strings.Contains(info.Title, "Big Buck Bunny") {
		t.Errorf("Title = %q, want the fixture's title", info.Title)
	}
	if info.Duration < 9*time.Minute || info.Duration > 12*time.Minute {
		t.Errorf("Duration = %s, want roughly ten minutes", info.Duration)
	}
	if info.ViewCount <= 0 {
		t.Errorf("ViewCount = %d, want the reported count", info.ViewCount)
	}
	if info.Source != media.SourceYouTube {
		t.Errorf("Source = %s, want youtube", info.Source)
	}
	if info.URL != "https://www.youtube.com/watch?v=aqz-KE-bpKQ" {
		t.Errorf("URL = %q, want the canonical watch URL", info.URL)
	}
	if len(info.Formats) < 20 {
		t.Fatalf("Formats = %d, want the full dump", len(info.Formats))
	}

	// The fixture is a real 4K upload, so this also proves the H.264 policy is
	// filtering rather than the source happening to be small.
	h264 := info.VideoFormats()
	if len(h264) == 0 {
		t.Fatal("VideoFormats() is empty for a real video")
	}
	for _, format := range h264 {
		if !format.IsH264() {
			t.Errorf("format %s (%s) leaked into the H.264 list", format.ID, format.VCodec)
		}
		if format.Height > media.MaxH264Height {
			t.Errorf("format %s is %dp, want the 1080p ceiling", format.ID, format.Height)
		}
	}
}

func TestProbeMapsMusicSource(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner(fixture(t, "music.json"), nil, nil, nil)}
	info, err := p.Probe(context.Background(), "https://music.youtube.com/watch?v=kJQP7kiw5Fk")
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	if info.Source != media.SourceYouTubeMusic {
		t.Fatalf("Source = %s, want youtube-music", info.Source)
	}
	if info.URL != "https://music.youtube.com/watch?v=kJQP7kiw5Fk" {
		t.Fatalf("URL = %q, want the music URL preserved", info.URL)
	}
	if len(info.AudioFormats()) == 0 {
		t.Fatal("AudioFormats() is empty for a music fixture")
	}
}

func TestProbeRejectsPlaylistDump(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner(fixture(t, "playlist.json"), nil, nil, nil)}
	_, err := p.Probe(context.Background(), "https://www.youtube.com/playlist?list=PLFgquLnL59alCl_2TQvOiD5Vgm1hCaGSI")

	if !errors.Is(err, media.ErrPlaylistOnly) {
		t.Fatalf("error = %v, want media.ErrPlaylistOnly", err)
	}
}

func TestProbeRejectsEmptyFormats(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner([]byte(`{"_type":"video","id":"abc","title":"x","formats":[]}`), nil, nil, nil)}
	_, err := p.Probe(context.Background(), "https://youtu.be/aqz-KE-bpKQ")

	if !errors.Is(err, ErrNoFormats) {
		t.Fatalf("error = %v, want ErrNoFormats", err)
	}
}

func TestProbeRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner([]byte(`{"_type":`), nil, nil, nil)}
	_, err := p.Probe(context.Background(), "https://youtu.be/aqz-KE-bpKQ")

	if !errors.Is(err, ErrBadProbe) {
		t.Fatalf("error = %v, want ErrBadProbe", err)
	}
}

func TestProbeRejectsEmptyStdout(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner(nil, nil, nil, nil)}
	_, err := p.Probe(context.Background(), "https://youtu.be/aqz-KE-bpKQ")

	if !errors.Is(err, ErrBadProbe) {
		t.Fatalf("error = %v, want ErrBadProbe", err)
	}
}

func TestProbeRunsTheExpectedCommand(t *testing.T) {
	t.Parallel()

	var calls []call
	p := Prober{Run: stubRunner(fixture(t, "video.json"), nil, nil, &calls)}

	if _, err := p.Probe(context.Background(), "https://youtu.be/aqz-KE-bpKQ?t=42"); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("ran %d commands, want 1", len(calls))
	}
	got := calls[0]
	if got.name != "yt-dlp" {
		t.Errorf("binary = %q, want yt-dlp from PATH", got.name)
	}

	want := []string{"-J", "--no-playlist", "--no-warnings", "--no-progress",
		"https://www.youtube.com/watch?v=aqz-KE-bpKQ&t=42"}
	if strings.Join(got.args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %q, want %q", got.args, want)
	}
}

func TestProbeHonoursPinnedBinary(t *testing.T) {
	t.Parallel()

	var calls []call
	p := Prober{
		Bin: "/opt/homebrew/bin/yt-dlp",
		Run: stubRunner(fixture(t, "video.json"), nil, nil, &calls),
	}

	if _, err := p.Probe(context.Background(), "https://youtu.be/aqz-KE-bpKQ"); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got := calls[0].name; got != "/opt/homebrew/bin/yt-dlp" {
		t.Fatalf("binary = %q, want the pinned path", got)
	}
}

func TestProbeSurfacesStderrOnFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("exit status 1")
	p := Prober{Run: stubRunner(nil, []byte("\nERROR: [youtube] abc: Private video. Sign in if you've been granted access\nmore text\n"), boom, nil)}

	_, err := p.Probe(context.Background(), "https://youtu.be/aqz-KE-bpKQ")
	if err == nil {
		t.Fatal("Probe() error = nil, want a failure")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error %v does not wrap the exit error", err)
	}
	if !strings.Contains(err.Error(), "Private video") {
		t.Errorf("error = %q, want the first stderr line", err)
	}
	if strings.Contains(err.Error(), "more text") {
		t.Errorf("error = %q, want only the first stderr line", err)
	}
}

func TestProbeCancellationIsNotAFailure(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner(nil, nil, context.Canceled, nil)}
	_, err := p.Probe(context.Background(), "https://youtu.be/aqz-KE-bpKQ")

	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("error = %v, want a cancellation message", err)
	}
}

func TestProbeRejectsNonYouTubeURL(t *testing.T) {
	t.Parallel()

	p := Prober{Run: stubRunner(fixture(t, "video.json"), nil, nil, nil)}
	_, err := p.Probe(context.Background(), "https://vimeo.com/12345")

	if !errors.Is(err, media.ErrUnsupportedURL) {
		t.Fatalf("error = %v, want media.ErrUnsupportedURL", err)
	}
}

// TestProbeLive talks to YouTube for real. It is opt-in so `go test` stays
// offline and deterministic; use it to refresh the fixtures in testdata/.
func TestProbeLive(t *testing.T) {
	if os.Getenv("UTUB_LIVE") == "" {
		t.Skip("set UTUB_LIVE=1 to run against YouTube")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	info, err := (Prober{}).Probe(ctx, "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	t.Logf("%s · %s · %d formats · %d H.264 rows",
		info.ID, info.Duration, len(info.Formats), len(info.VideoFormats()))

	if info.Title == "" || len(info.VideoFormats()) == 0 {
		t.Fatalf("live probe returned nothing usable: %+v", info)
	}
}
