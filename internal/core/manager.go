package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nnivxix/utub/internal/media"
)

// Manager is the whole engine as the surfaces see it.
//
// Each surface is a thin consumer of the event channel returned by Start:
//
//	TUI      goroutine reads the channel  →  tea.Msg
//	Web      channel  →  SSE frames on GET /api/jobs/{id}/events
//	Desktop  channel  →  Wails event emit ("job:<id>")
//
// The channel is closed by the engine after the terminal Event. It is buffered
// and lossy: a reader that falls behind loses progress samples, never the
// terminal one.
//
// A cancelled job has no terminal Event, because Event carries no cancellation
// signal and StateCanceled is not an error. Its stream simply closes; a reader
// that needs to tell "done" from "cancelled" after the close reads Job.State().
type Manager interface {
	// Probe resolves a URL into metadata and the list of downloadable formats.
	Probe(ctx context.Context, rawURL string) (media.MediaInfo, error)

	// Start validates req and begins downloading. It returns immediately; the
	// caller ranges over the channel for progress.
	Start(req Request) (*Job, <-chan Event, error)

	// Cancel stops a running job, killing its process group. Canceling a
	// finished job returns ErrFinished; an unknown id returns ErrNotFound.
	Cancel(id string) error

	// List returns a snapshot of every job this process knows about, newest
	// first. The *Job values are live; read them through their accessors.
	List() []*Job
}

// NoopManager satisfies Manager without doing anything.
//
// It exists so the TUI, the API and the desktop shell can be written and tested
// against a compiling engine. Every method returns ErrNotImplemented.
type NoopManager struct{}

// Probe implements Manager.
func (NoopManager) Probe(context.Context, string) (media.MediaInfo, error) {
	return media.MediaInfo{}, ErrNotImplemented
}

// Start implements Manager.
func (NoopManager) Start(Request) (*Job, <-chan Event, error) {
	return nil, nil, ErrNotImplemented
}

// Cancel implements Manager.
func (NoopManager) Cancel(string) error { return ErrNotImplemented }

// List implements Manager.
func (NoopManager) List() []*Job { return nil }

var _ Manager = NoopManager{}

const (
	// eventBuffer bounds the samples waiting between the download goroutine and
	// the coalescer. It is deliberately small: a consumer that stops reading
	// should lose samples, not make the job allocate without limit.
	eventBuffer = 8

	// terminalSendTimeout bounds the (otherwise blocking) hand-off of the
	// terminal event, so a consumer that never drains its channel cannot leak
	// the job goroutine. The job's own state is already final by then.
	terminalSendTimeout = 10 * time.Second
)

// Engine is the concrete Manager: it probes with yt-dlp, runs one subprocess per
// job and streams coalesced events to the caller.
//
// The zero value is usable: binaries are looked up on PATH and every job gets a
// random id. Callers that ran binmgr should fill Prober.Bin and Executor so the
// resolved paths are honoured (and so aria2c auto-detection can work).
//
// Engine contains a mutex and must not be copied after first use.
type Engine struct {
	Prober   Prober
	Executor Executor

	// NewID overrides the job id generator; nil means "job-" plus random hex.
	NewID func() string
	// Now overrides the clock; nil means time.Now.
	Now func() time.Time

	mu      sync.Mutex
	jobs    map[string]*Job
	order   []string // ids in creation order; List walks it backwards
	cancels map[string]context.CancelFunc
}

// Probe implements Manager.
func (e *Engine) Probe(ctx context.Context, rawURL string) (media.MediaInfo, error) {
	return e.Prober.Probe(ctx, rawURL)
}

// Start implements Manager.
//
// Everything that can be checked without spawning a process — the URL, the
// destination directory, a forced-but-missing aria2c — is checked here and
// returned synchronously. Once Start succeeds the job is registered before the
// goroutine starts, so a Cancel with the returned id can never race a job that
// does not exist yet.
func (e *Engine) Start(req Request) (*Job, <-chan Event, error) {
	ref, err := media.ParseURL(req.URL)
	if err != nil {
		return nil, nil, err
	}
	// Store the canonical link, not whatever the caller pasted: the argv and
	// the job record then agree on one URL without tracking parameters.
	req.URL = ref.URL

	if err := checkOutDir(req.OutDir); err != nil {
		return nil, nil, err
	}
	if err := e.Executor.validate(req); err != nil {
		return nil, nil, err
	}

	job := &Job{
		ID:      e.newID(),
		Request: req,
		Source:  req.Source,
		Created: e.now(),
	}

	ctx, cancel := context.WithCancel(context.Background())

	e.mu.Lock()
	if e.jobs == nil {
		e.jobs = make(map[string]*Job)
	}
	if e.cancels == nil {
		e.cancels = make(map[string]context.CancelFunc)
	}
	e.jobs[job.ID] = job
	e.order = append(e.order, job.ID)
	e.cancels[job.ID] = cancel
	e.mu.Unlock()

	raw := make(chan Event, eventBuffer)
	go e.run(ctx, job, raw)

	// The coalescer runs on a background context: it must outlive the job's
	// own context so the last samples and the terminal event still reach a
	// cancelled-then-draining reader. It stops when raw is closed.
	return job, coalesce(context.Background(), raw, progressInterval), nil
}

// run owns one subprocess from spawn to terminal state, then closes the stream.
//
// Only the manager knows job identity, so it fills JobID. Only the manager
// knows the final state, so it also owns the terminal frame: the executor's own
// StageDone is suppressed and re-sent after succeed() records the output path.
// Without that ordering a reader that reacts to the closed channel could see an
// empty OutputPath.
func (e *Engine) run(ctx context.Context, job *Job, raw chan<- Event) {
	defer close(raw)
	defer e.forget(job.ID)

	emit := func(ev Event) {
		if ev.Stage == StageDone {
			return // the terminal frame is sent below, after state is final
		}
		ev.JobID = job.ID
		job.setStage(ev.Stage)
		sendEvent(raw, ev)
	}

	job.start()
	res, err := e.Executor.Run(ctx, job.Request, emit)

	switch {
	case ctx.Err() != nil:
		// Cancel already recorded StateCanceled; this call is a no-op then and
		// covers the case where the context died without going through Cancel.
		job.cancel()

	case err != nil:
		job.fail(err)
		sendEvent(raw, Event{JobID: job.ID, Stage: job.Stage(), Percent: PercentUnknown, Err: err})

	default:
		job.succeed(res.Path)
		sendEvent(raw, Event{JobID: job.ID, Stage: StageDone, Percent: 1})
	}
}

// Cancel implements Manager.
//
// StateCanceled is recorded before the process group is signalled, so the
// non-zero exit that a SIGTERM causes cannot turn a cancellation into a
// failure: the state machine's first terminal transition wins.
func (e *Engine) Cancel(id string) error {
	e.mu.Lock()
	job, ok := e.jobs[id]
	cancel := e.cancels[id]
	e.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if state := job.State(); state.IsTerminal() {
		return fmt.Errorf("%w: %s is %s", ErrFinished, id, state)
	}

	job.cancel()
	if cancel != nil {
		cancel()
	}
	return nil
}

// List implements Manager.
func (e *Engine) List() []*Job {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.order) == 0 {
		return nil
	}

	out := make([]*Job, 0, len(e.order))
	for i := len(e.order) - 1; i >= 0; i-- {
		out = append(out, e.jobs[e.order[i]])
	}
	return out
}

// forget drops the cancel func of a finished job. The job itself stays in the
// registry for List: a surface may still ask what happened to it.
func (e *Engine) forget(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.cancels, id)
}

func (e *Engine) newID() string {
	if e.NewID != nil {
		return e.NewID()
	}
	return defaultJobID()
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// jobCounter is the fallback id source when the entropy pool is unavailable.
var jobCounter atomic.Uint64

// defaultJobID returns "job-" plus eight random bytes as hex. Ids travel in URLs
// once M4 lands, so they are unguessable rather than sequential.
func defaultJobID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "job-" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("job-%d-%d", time.Now().UnixNano(), jobCounter.Add(1))
}

// sendEvent delivers one sample without ever blocking the download goroutine,
// except for a terminal event: a full buffer costs a progress sample, never the
// end of the job.
func sendEvent(ch chan<- Event, ev Event) {
	if ev.Terminal() {
		timer := time.NewTimer(terminalSendTimeout)
		defer timer.Stop()

		select {
		case ch <- ev:
		case <-timer.C:
		}
		return
	}

	select {
	case ch <- ev:
	default:
		// The consumer is behind. Dropping the newest sample is safe: the
		// coalescer already holds nothing newer, and the next tick carries the
		// same fields a moment later.
	}
}

// checkOutDir validates the destination once more at download time, because the
// folder can be deleted between changing settings and pressing download.
func checkOutDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("%w: no path given", ErrBadOutDir)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrBadOutDir, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrBadOutDir, dir)
	}
	return nil
}

var _ Manager = (*Engine)(nil)
