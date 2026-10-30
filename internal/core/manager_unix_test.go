//go:build !windows

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newEngineWith wires a fake yt-dlp script into a real Engine, so the manager's
// registry, cancellation and event plumbing can be exercised end to end without
// touching the network.
func newEngineWith(t *testing.T, script string) (*Engine, string) {
	t.Helper()

	path, dl := fakeYTDLP(t, script)
	return &Engine{Executor: dl}, path
}

func TestEngineStartStreamsToDone(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "video [abc].mp4")

	engine, _ := newEngineWith(t, fmt.Sprintf(`
printf '[download] Destination: %s\n'
printf 'DL 500 1000 NA 100 5 0 2\n'
printf '[Merger] Merging formats into "%s"\n'
printf '0123456789' > '%s'
`, filepath.Join(dir, "video [abc].f137.mp4"), final, final))

	job, events, err := engine.Start(jobRequest(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	got := collectEvents(t, events)

	if job.State() != StateDone {
		t.Fatalf("State() = %s, want done (err %v)", job.State(), job.Err())
	}
	if want := final; job.OutputPath() != want {
		t.Errorf("OutputPath() = %q, want %q", job.OutputPath(), want)
	}
	if job.Err() != nil {
		t.Errorf("Err() = %v, want nil", job.Err())
	}

	if len(got) == 0 {
		t.Fatal("no events emitted")
	}
	last := got[len(got)-1]
	if !last.Terminal() || last.Stage != StageDone {
		t.Errorf("last event = %+v, want a terminal done event", last)
	}
	for _, ev := range got {
		if ev.JobID != job.ID {
			t.Errorf("event JobID = %q, want %q", ev.JobID, job.ID)
		}
	}

	if jobs := engine.List(); len(jobs) != 1 || jobs[0] != job {
		t.Errorf("List() = %v, want the one job", jobs)
	}
}

func TestEngineStartClassifiesFailure(t *testing.T) {
	engine, _ := newEngineWith(t, `
printf 'ERROR: [youtube] abc: Private video. Sign in if you have been granted access\n' >&2
exit 1
`)

	job, events, err := engine.Start(jobRequest(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	got := collectEvents(t, events)

	if job.State() != StateFailed {
		t.Fatalf("State() = %s, want failed", job.State())
	}
	if !errors.Is(job.Err(), ErrPrivate) {
		t.Errorf("Err() = %v, want it to wrap ErrPrivate", job.Err())
	}

	last := got[len(got)-1]
	if !last.Terminal() || !errors.Is(last.Err, ErrPrivate) {
		t.Errorf("last event = %+v, want a terminal event wrapping ErrPrivate", last)
	}
}

func TestEngineTracksConcurrentJobsIndependently(t *testing.T) {
	started := t.TempDir()

	// One script for both jobs: it derives the file name from the requested
	// video id, so a single registry really does run two downloads at once.
	engine, _ := newEngineWith(t, fmt.Sprintf(`
url=""
for a in "$@"; do url="$a"; done
id="${url##*v=}"
printf 'running' > '%s'/"$id".started
sleep 0.3
final='%s'/"$id".mp4
printf '[download] Destination: %%s\n' "$final"
printf 'DL 1 1 NA NA NA NA NA\n'
printf 'x' > "$final"
`, started, started))

	firstReq := jobRequest(t)
	firstReq.URL = "https://www.youtube.com/watch?v=aaaaaaaaaaa"
	secondReq := jobRequest(t)
	secondReq.URL = "https://www.youtube.com/watch?v=bbbbbbbbbbb"

	first, firstEvents, err := engine.Start(firstReq)
	if err != nil {
		t.Fatalf("Start(first) error = %v", err)
	}
	second, secondEvents, err := engine.Start(secondReq)
	if err != nil {
		t.Fatalf("Start(second) error = %v", err)
	}

	// Both must be alive at the same time: neither has finished by the time the
	// second marker appears.
	for _, name := range []string{"aaaaaaaaaaa", "bbbbbbbbbbb"} {
		waitForFile(t, filepath.Join(started, name+".started"))
	}
	if first.State() != StateRunning || second.State() != StateRunning {
		t.Fatalf("states = %s, %s; want both running", first.State(), second.State())
	}

	gotFirst := collectEvents(t, firstEvents)
	gotSecond := collectEvents(t, secondEvents)

	for _, job := range []*Job{first, second} {
		waitForJobState(t, job, StateDone)
		if job.Err() != nil {
			t.Errorf("job %s error = %v", job.ID, job.Err())
		}
	}
	if first.ID == second.ID {
		t.Fatalf("both jobs share the id %q", first.ID)
	}

	for _, ev := range gotFirst {
		if ev.JobID != first.ID {
			t.Errorf("first stream carried JobID %q, want %q", ev.JobID, first.ID)
		}
	}
	for _, ev := range gotSecond {
		if ev.JobID != second.ID {
			t.Errorf("second stream carried JobID %q, want %q", ev.JobID, second.ID)
		}
	}

	jobs := engine.List()
	if len(jobs) != 2 {
		t.Fatalf("List() = %d jobs, want 2", len(jobs))
	}
	if jobs[0] != second || jobs[1] != first {
		t.Errorf("List() = [%s %s], want newest first [%s %s]", jobs[0].ID, jobs[1].ID, second.ID, first.ID)
	}
}
func TestEngineCancelKillsProcessGroupAndKeepsPartFiles(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	childPidFile := filepath.Join(dir, "child-pid")
	part := filepath.Join(dir, "video [abc].f137.mp4.part")

	engine, _ := newEngineWith(t, fmt.Sprintf(`
echo $$ > '%s'
sleep 30 &
echo $! > '%s'
printf '0123456789' > '%s'
wait
`, pidFile, childPidFile, part))

	job, events, err := engine.Start(jobRequest(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	pid := waitForPid(t, pidFile)
	childPid := waitForPid(t, childPidFile)
	waitForJobState(t, job, StateRunning)

	if err := engine.Cancel(job.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	// The stream closes without a terminal event: cancellation is not a failure.
	collectEvents(t, events)

	if job.State() != StateCanceled {
		t.Fatalf("State() = %s, want canceled (err %v)", job.State(), job.Err())
	}
	if job.Err() != nil {
		t.Errorf("Err() = %v, want nil: a cancellation is not a failure", job.Err())
	}
	if path := job.OutputPath(); path != "" {
		t.Errorf("OutputPath() = %q, want empty for a cancelled job", path)
	}

	waitForExit(t, pid)
	waitForExit(t, childPid)

	if _, err := os.Stat(part); err != nil {
		t.Errorf("partial file was removed, --continue cannot resume: %v", err)
	}

	// A cancelled job is terminal, so a second cancel is not a no-op.
	if err := engine.Cancel(job.ID); !errors.Is(err, ErrFinished) {
		t.Errorf("second Cancel() = %v, want ErrFinished", err)
	}
}

func TestEngineCoalescesRapidSamples(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "v.mp4")

	var script strings.Builder
	script.WriteString("printf '[download] Destination: " + filepath.Join(dir, "v.f137.mp4") + "\\n'\n")
	// Space the samples out so they span several coalescing windows; a burst
	// that ends inside one window would collapse to the terminal event alone.
	for i := range 20 {
		fmt.Fprintf(&script, "printf 'DL %d 20 NA NA NA NA NA\\n'\nsleep 0.02\n", i+1)
	}
	fmt.Fprintf(&script, "printf 'x' > '%s'\n", final)

	engine, _ := newEngineWith(t, script.String())

	job, events, err := engine.Start(jobRequest(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	got := collectEvents(t, events)
	waitForJobState(t, job, StateDone)

	samples := 0
	for _, ev := range got {
		if ev.Percent >= 0 && !ev.Terminal() {
			samples++
		}
	}
	if samples == 0 {
		t.Fatal("no progress samples reached the caller")
	}
	if samples > 8 {
		t.Errorf("%d samples for 20 lines: the stream was not coalesced", samples)
	}
	if last := got[len(got)-1]; !last.Terminal() {
		t.Errorf("last event = %+v, want terminal", last)
	}
}

// waitForFile polls until path exists.
func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}
