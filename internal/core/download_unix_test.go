//go:build !windows

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeYTDLP writes an executable shell script and returns its path plus an
// Executor that runs it instead of the real binary. This keeps the tests
// offline and makes yt-dlp's output shape explicit.
func fakeYTDLP(t *testing.T, body string) (string, Executor) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}

	return path, Executor{
		NewCmd: func(_ context.Context, _ string, args ...string) *exec.Cmd {
			// Forward the argv so tests can assert what yt-dlp would receive.
			return exec.Command(path, args...)
		},
	}
}

func TestRunStreamsStagesAndReportsPath(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "video [abc].mp4")

	script := fmt.Sprintf(`
printf 'DL 500 1000 NA 100 5 0 2\n'
printf '[download] Destination: %s\n'
printf 'DL 1000 1000 NA 100 0 2 2\n'
printf '[download] Destination: %s\n'
printf 'DL 250 250 NA 50 0 1 1\n'
printf '[Merger] Merging formats into "%s"\n'
printf '0123456789' > '%s'
`,
		filepath.Join(dir, "video [abc].f137.mp4"),
		filepath.Join(dir, "video [abc].f140.m4a"),
		final,
		final,
	)

	_, dl := fakeYTDLP(t, script)

	var events []Event
	res, err := dl.Run(t.Context(), videoRequest(t), func(ev Event) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}

	if res.Path != final {
		t.Errorf("Result.Path = %q, want %q", res.Path, final)
	}
	if res.Bytes != 10 {
		t.Errorf("Result.Bytes = %d, want 10", res.Bytes)
	}
	if res.Duration <= 0 {
		t.Error("Result.Duration was not measured")
	}

	stages := map[Stage]bool{}
	mergeAnnouncements := 0
	for _, ev := range events {
		stages[ev.Stage] = true
		if ev.Stage == StageMerge && ev.Percent == PercentUnknown {
			mergeAnnouncements++
		}
	}
	// Merger and the later MoveFiles both touch the merge stage; the UI should
	// be told once.
	if mergeAnnouncements != 1 {
		t.Errorf("merge stage announced %d times, want 1", mergeAnnouncements)
	}
	for _, want := range []Stage{StageVideo, StageAudio, StageMerge, StageDone} {
		if !stages[want] {
			t.Errorf("stage %v never emitted; got %v", want, stages)
		}
	}
	if !events[len(events)-1].Terminal() {
		t.Errorf("last event = %+v, want terminal", events[len(events)-1])
	}
}

func TestRunReportsProgressForVideo(t *testing.T) {
	script := `
printf 'DL 500 1000 NA 2048 7 0 2\n'
printf 'DL 1000 1000 NA 0 0 1 1\n'
printf '[download] Destination: /d/v.mp4\n'
`
	_, dl := fakeYTDLP(t, script)

	var samples []Event
	if _, err := dl.Run(t.Context(), videoRequest(t), func(ev Event) {
		if ev.Percent >= 0 {
			samples = append(samples, ev)
		}
	}); err != nil {
		t.Fatalf("Run error = %v", err)
	}

	if len(samples) == 0 {
		t.Fatal("no progress samples emitted")
	}
	first := samples[0]
	if first.Percent != 0.5 {
		t.Errorf("first percent = %v, want 0.5", first.Percent)
	}
	if first.Speed != 2048 {
		t.Errorf("first speed = %v, want 2048", first.Speed)
	}
	if first.ETA != 7*time.Second {
		t.Errorf("first eta = %v, want 7s", first.ETA)
	}
}

func TestRunClassifiesFailures(t *testing.T) {
	script := `
printf 'ERROR: [youtube] abc: Private video. Sign in if you have been granted access\n' >&2
exit 1
`
	_, dl := fakeYTDLP(t, script)

	_, err := dl.Run(t.Context(), videoRequest(t), nil)
	if err == nil {
		t.Fatal("Run returned nil error for a failing download")
	}
	if !errors.Is(err, ErrPrivate) {
		t.Errorf("error = %v, want it to wrap ErrPrivate", err)
	}
}

func TestRunRefusesToGuessOutputPath(t *testing.T) {
	_, dl := fakeYTDLP(t, "exit 0\n")

	_, err := dl.Run(t.Context(), videoRequest(t), nil)
	if !errors.Is(err, ErrNoOutputPath) {
		t.Fatalf("error = %v, want ErrNoOutputPath", err)
	}
}

func TestRunUsesBuiltArgv(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")

	_, dl := fakeYTDLP(t, fmt.Sprintf("printf '%%s\\n' \"$@\" > '%s'\n", argsFile))

	req := videoRequest(t)
	if _, err := dl.Run(t.Context(), req, nil); !errors.Is(err, ErrNoOutputPath) {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	want := Executor{}.Args(req)
	if len(got) != len(want) {
		t.Fatalf("child received %d args, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunCancelKillsProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	childPidFile := filepath.Join(dir, "child-pid")

	// The script spawns a grandchild, mimicking yt-dlp -> ffmpeg. Both must die,
	// otherwise a cancelled download leaves a merge running in the background.
	script := fmt.Sprintf(`
echo $$ > '%s'
sleep 30 &
echo $! > '%s'
wait
`, pidFile, childPidFile)

	_, dl := fakeYTDLP(t, script)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := dl.Run(ctx, videoRequest(t), nil)
		done <- err
	}()

	pid := waitForPid(t, pidFile)
	childPid := waitForPid(t, childPidFile)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	waitForExit(t, pid)
	waitForExit(t, childPid)
}

// waitForPid polls a file until it contains a process id.
func waitForPid(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("process id never appeared in %s", path)
	return 0
}

// waitForExit polls kill(pid, 0) until the process is gone.
func waitForExit(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return // ESRCH: the process is gone
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("process %d survived cancellation", pid)
}

func TestRunLeavesPartFilesAlone(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "video [abc].f137.mp4.part")

	script := fmt.Sprintf(`
printf '0123456789' > '%s'
printf '[download] Destination: %s\n' '%s'
`, part, part, part)

	_, dl := fakeYTDLP(t, script)

	if _, err := dl.Run(t.Context(), videoRequest(t), nil); err != nil {
		t.Fatalf("Run error = %v", err)
	}

	if _, err := os.Stat(part); err != nil {
		t.Errorf("partial file was removed: %v", err)
	}
}

func TestRunPercentNeverGoesBackwardsWithinStage(t *testing.T) {
	// yt-dlp revises total_bytes upward as it learns more about a fragmented
	// stream, so a raw downloaded/total ratio dips. The engine clamps it.
	script := `
printf '[download] Destination: /d/v.mp4\n'
printf 'DL 900 1000 NA NA NA NA NA\n'
printf 'DL 100 1000 NA NA NA NA NA\n'
printf 'DL 1000 1000 NA NA NA NA NA\n'
`
	_, dl := fakeYTDLP(t, script)

	var percents []float64
	if _, err := dl.Run(t.Context(), videoRequest(t), func(ev Event) {
		// Terminal events are the job ending, not a progress sample.
		if ev.Percent >= 0 && !ev.Terminal() {
			percents = append(percents, ev.Percent)
		}
	}); err != nil {
		t.Fatalf("Run error = %v", err)
	}

	want := []float64{0.9, 0.9, 1.0}
	if len(percents) != len(want) {
		t.Fatalf("percents = %v, want %v", percents, want)
	}
	for i := range want {
		if percents[i] != want[i] {
			t.Errorf("percent[%d] = %v, want %v", i, percents[i], want[i])
		}
	}
}

func TestRunPercentResetsOnStageChange(t *testing.T) {
	script := `
printf 'DL 1000 1000 NA NA NA NA NA\n'
printf '[download] Destination: /d/v.mp4\n'
printf 'DL 1000 1000 NA NA NA NA NA\n'
printf '[download] Destination: /d/a.m4a\n'
printf 'DL 100 1000 NA NA NA NA NA\n'
`
	_, dl := fakeYTDLP(t, script)

	type sample struct {
		stage   Stage
		percent float64
	}
	var got []sample
	if _, err := dl.Run(t.Context(), videoRequest(t), func(ev Event) {
		if ev.Percent >= 0 {
			got = append(got, sample{ev.Stage, ev.Percent})
		}
	}); err != nil {
		t.Fatalf("Run error = %v", err)
	}

	var audio []sample
	for _, s := range got {
		if s.stage == StageAudio {
			audio = append(audio, s)
		}
	}
	if len(audio) != 1 || audio[0].percent != 0.1 {
		t.Fatalf("audio samples = %+v, want a single 0.1 sample (not clamped to the video stage)", audio)
	}
}
