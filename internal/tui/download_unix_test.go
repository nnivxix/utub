//go:build !windows

package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// fakeYTDLPEngine is a real core.Engine whose yt-dlp is a shell script. The TUI
// is then exercised through the actual engine: registry, coalescer, process
// group, state machine and all.
func fakeYTDLPEngine(t *testing.T, script string) *core.Engine {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}

	return &core.Engine{Executor: core.Executor{
		NewCmd: func(_ context.Context, _ string, args ...string) *exec.Cmd {
			return exec.Command(path, args...)
		},
	}}
}

// drive runs the router's commands until the model stops producing them,
// mimicking what the Bubble Tea runtime does.
func drive(t *testing.T, r *root, cmd tea.Cmd) {
	t.Helper()

	for steps := 0; cmd != nil; steps++ {
		if steps > 500 {
			t.Fatal("the router kept producing commands")
		}
		model, next := r.Update(cmd())
		*r = model.(root)
		cmd = next
	}
}

func realDownloadRef(t *testing.T) media.Ref {
	t.Helper()
	info := demoMedia()
	return media.Ref{ID: info.ID, URL: "https://www.youtube.com/watch?v=" + info.ID}
}

func TestProgressRunsARealDownloadToDone(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "video [abc].mp4")

	engine := fakeYTDLPEngine(t, fmt.Sprintf(`
printf '[download] Destination: %s\n'
printf 'DL 500 1000 NA 100 5 0 2\n'
printf '[Merger] Merging formats into "%s"\n'
printf '0123456789' > '%s'
`, filepath.Join(dir, "video [abc].f137.mp4"), final, final))

	deps := testDeps()
	deps.Manager = engine
	deps.DestDir = dir

	r := newRoot(deps)
	r.formats.info = demoMedia()

	model, cmd := r.Update(navigateMsg{
		screen: screenProgress,
		ref:    realDownloadRef(t),
		format: demoMedia().VideoFormats()[0],
	})
	r = model.(root)

	drive(t, &r, cmd)

	if r.progress.state != core.StateDone {
		t.Fatalf("state = %s, want done (err %q)", r.progress.state, r.progress.errText)
	}
	if r.progress.output != final {
		t.Fatalf("output = %q, want the path yt-dlp reported (%q)", r.progress.output, final)
	}
	if view := r.View().Content; !strings.Contains(view, "saved to") {
		t.Fatalf("view = %q, want a saved line", view)
	}
	if !strings.Contains(r.footer(), "reveal") {
		t.Fatalf("footer = %q, want the reveal hint", r.footer())
	}
}

func TestProgressEscStopsARealDownload(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")

	engine := fakeYTDLPEngine(t, fmt.Sprintf(`
echo $$ > '%s'
sleep 30
`, pidFile))

	deps := testDeps()
	deps.Manager = engine
	deps.DestDir = dir

	r := newRoot(deps)
	r.formats.info = demoMedia()

	model, cmd := r.Update(navigateMsg{
		screen: screenProgress,
		ref:    realDownloadRef(t),
		format: demoMedia().VideoFormats()[0],
	})
	r = model.(root)

	// Run until the job is live, then stop driving so the pending command is
	// the one waiting on the event stream.
	for cmd != nil && r.progress.state != core.StateRunning {
		model, cmd = r.Update(cmd())
		r = model.(root)
	}
	if r.progress.state != core.StateRunning {
		t.Fatalf("state = %s, want running", r.progress.state)
	}

	pid := waitForPidFile(t, pidFile)

	model, _ = r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)
	if !r.progress.stopping {
		t.Fatal("stopping = false, want the screen to say it is stopping")
	}

	drive(t, &r, cmd)

	if r.progress.state != core.StateCanceled {
		t.Fatalf("state = %s, want canceled (err %q)", r.progress.state, r.progress.errText)
	}
	if r.progress.output != "" {
		t.Fatalf("output = %q, want empty for a cancelled job", r.progress.output)
	}
	if r.progress.stopping {
		t.Error("stopping still true after the stream closed")
	}

	waitForProcessExit(t, pid)
}

// waitForPidFile polls until path holds a process id.
func waitForPidFile(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no process id in %s", path)
	return 0
}

// waitForProcessExit polls kill(pid, 0) until the process is gone.
func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d survived the cancel", pid)
}
