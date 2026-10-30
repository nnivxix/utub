package core

import (
	"sync"
	"time"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/media"
)

// Request is everything needed to run one download.
//
// Config is a snapshot taken when the job is created, not a reference to the
// live settings: changing the default directory or the fragment count while a
// job is running must not alter that job halfway through.
type Request struct {
	URL    string
	Format media.Format // the row the user picked
	OutDir string       // absolute path, already validated
	Config config.Config
}

// Job is one download. The exported fields are immutable after creation; all
// mutation goes through the unexported methods so readers (TUI, API, tests) can
// never observe a half-updated job.
//
// A Job contains a mutex and must not be copied; pass *Job.
type Job struct {
	ID      string
	Request Request
	Source  media.MediaInfo // probe result; zero value for a job started without one
	Created time.Time

	mu      sync.RWMutex
	state   State
	stage   Stage
	outPath string
	err     error
}

// State returns the job's lifecycle state.
func (j *Job) State() State {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.state
}

// Stage returns the pipeline phase the job is in.
func (j *Job) Stage() Stage {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.stage
}

// OutputPath returns the finished file, or "" until the job succeeds.
func (j *Job) OutputPath() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.outPath
}

// Err returns the terminal error, or nil.
//
// A canceled job has no error: StateCanceled is the signal, and a UI should not
// render a cancellation as a failure.
func (j *Job) Err() error {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.err
}

// start marks the job running. Called once the subprocess has been spawned.
func (j *Job) start() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == StatePending {
		j.state = StateRunning
	}
}

// setStage records the current phase, implying that the job has started.
func (j *Job) setStage(s Stage) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == StatePending {
		j.state = StateRunning
	}
	j.stage = s
}

// succeed records a finished job and the file it produced.
func (j *Job) succeed(outPath string) { j.setTerminal(StateDone, outPath, nil) }

// fail records a terminal error.
func (j *Job) fail(err error) { j.setTerminal(StateFailed, "", err) }

// cancel records a user cancellation, which is not an error.
func (j *Job) cancel() { j.setTerminal(StateCanceled, "", nil) }

// setTerminal applies the first terminal transition and ignores later ones, so
// a subprocess that exits non-zero after a cancellation cannot resurrect a
// finished job as failed.
func (j *Job) setTerminal(state State, outPath string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state.IsTerminal() {
		return
	}
	if state == StateDone {
		j.stage = StageDone
	}
	j.state = state
	j.outPath = outPath
	j.err = err
}
