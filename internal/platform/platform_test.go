package platform

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDownloadsDir(t *testing.T) {
	dir := DownloadsDir()
	if dir == "" {
		t.Fatal("DownloadsDir() returned an empty path")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("DownloadsDir() = %q, want an absolute path", dir)
	}
	if got, want := filepath.Base(dir), "Downloads"; got != want {
		// Linux may legitimately point elsewhere via XDG_DOWNLOAD_DIR.
		t.Logf("DownloadsDir() = %q (base %q, not %q) — allowed on XDG desktops", dir, got, want)
	}
}

func fakeRunner(results map[string]struct {
	out []byte
	err error
}, calls *[]string) runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		if calls != nil {
			*calls = append(*calls, name)
		}
		r, ok := results[name]
		if !ok {
			return nil, errors.New("no such command")
		}
		return r.out, r.err
	}
}

func TestReadClipboardFallsBackToLaterCandidate(t *testing.T) {
	var calls []string
	run := fakeRunner(map[string]struct {
		out []byte
		err error
	}{
		"wl-paste": {err: errors.New("no display")},
		"xclip":    {out: []byte("  https://youtu.be/dQw4w9WgXcQ  \n")},
		"xsel":     {out: []byte("should not be reached")},
	}, &calls)

	got := readClipboard(context.Background(),
		[][]string{{"wl-paste", "--no-newline"}, {"xclip", "-o"}, {"xsel", "--output"}}, run)

	if want := "https://youtu.be/dQw4w9WgXcQ"; got != want {
		t.Errorf("readClipboard() = %q, want %q", got, want)
	}
	if len(calls) != 2 || calls[1] != "xclip" {
		t.Errorf("commands called = %v, want [wl-paste xclip]", calls)
	}
}

func TestReadClipboardAllCandidatesFail(t *testing.T) {
	run := fakeRunner(map[string]struct {
		out []byte
		err error
	}{
		"a": {err: errors.New("missing")},
		"b": {err: errors.New("missing")},
	}, nil)

	if got := readClipboard(context.Background(), [][]string{{"a"}, {"b"}}, run); got != "" {
		t.Errorf("readClipboard() = %q, want empty", got)
	}
}

func TestReadClipboardSkipsEmptyOutput(t *testing.T) {
	run := fakeRunner(map[string]struct {
		out []byte
		err error
	}{
		"empty": {out: []byte("   \n\t ")},
		"real":  {out: []byte("https://music.youtube.com/watch?v=abc")},
	}, nil)

	got := readClipboard(context.Background(),
		[][]string{{"empty"}, {"real"}}, run)
	if want := "https://music.youtube.com/watch?v=abc"; got != want {
		t.Errorf("readClipboard() = %q, want %q", got, want)
	}
}

func TestReadClipboardNoCandidates(t *testing.T) {
	run := fakeRunner(nil, nil)

	if got := readClipboard(context.Background(), nil, run); got != "" {
		t.Errorf("readClipboard(nil) = %q, want empty", got)
	}
	if got := readClipboard(context.Background(), [][]string{{}}, run); got != "" {
		t.Errorf("readClipboard(empty cmd) = %q, want empty", got)
	}
}

func TestReadClipboardContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// execRunner honours ctx; the fake returns an error as a real tool would.
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return []byte("late"), nil
	}

	if got := readClipboard(ctx, [][]string{{"pbpaste"}}, run); got != "" {
		t.Errorf("readClipboard with cancelled context = %q, want empty", got)
	}
}

func TestClipboardCommandsNonEmptyOnSupportedOS(t *testing.T) {
	// Every platform we ship a CLI for must offer at least one reader.
	switch runtime.GOOS {
	case "darwin", "windows", "linux":
		if len(clipboardCommands()) == 0 {
			t.Fatalf("clipboardCommands() is empty on supported OS %q", runtime.GOOS)
		}
	}
}
