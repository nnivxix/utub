package tui

import (
	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// clipboardMsg carries a YouTube link found on the clipboard. An empty URL
// means nothing usable was there, which is not an error: the user can always
// paste by hand.
type clipboardMsg struct {
	url string
}

// navigateMsg asks the root model to switch screens. Sub-models never render or
// mutate each other; they only announce where the user wants to go.
type navigateMsg struct {
	screen screen
	ref    media.Ref
	format media.Format
}

// noticeMsg is a transient line shown under the home input.
type noticeMsg struct {
	text string
}

// configSavedMsg carries settings back to the router after a successful write,
// so the header's downloads path updates immediately.
type configSavedMsg struct {
	cfg config.Config
	dir string
}

// probeDoneMsg carries a successful probe back to the router. seq identifies
// the request that produced it, so a late result from a canceled probe cannot
// open the picker for a newer link.
type probeDoneMsg struct {
	seq  int
	ref  media.Ref
	info media.MediaInfo
}

// probeErrMsg carries a failed probe back to the router, which returns to the
// home screen with a readable reason.
type probeErrMsg struct {
	seq int
	ref media.Ref
	err error
}

// jobStartedMsg is the answer to Manager.Start: either a live job plus its
// event stream, or the validation error Start refused with.
type jobStartedMsg struct {
	job    *core.Job
	events <-chan core.Event
	err    error
}

// jobEventMsg is one sample from a job's stream. jobID lets the progress screen
// ignore a late event from a job it no longer owns.
type jobEventMsg struct {
	jobID string
	event core.Event
}

// jobClosedMsg means the job's stream is closed. For a cancelled job that is
// the only signal it has ended, so the handler reads Job.State().
type jobClosedMsg struct {
	jobID string
}

// quitMsg ends the program after every running job has been stopped. Quitting
// through Tea directly would leave yt-dlp and ffmpeg running as orphans.
type quitMsg struct{}
