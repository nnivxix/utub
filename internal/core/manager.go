package core

import (
	"context"

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
// against a compiling engine before M1 lands real behaviour. Every method
// returns ErrNotImplemented.
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
