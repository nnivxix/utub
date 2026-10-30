package core

import "time"

// PercentUnknown is the value of Event.Percent when the engine cannot compute a
// fraction yet (merging, post-processing, or a stream with no known size).
const PercentUnknown = -1

// Stage is the pipeline phase a job is in right now.
//
// Stage reports where the job is; State reports whether it is still alive.
// Keeping them separate means a UI can say "merging" without inventing a
// lifecycle state for it.
type Stage int

const (
	StageProbe Stage = iota // resolving formats
	StageVideo              // downloading the video stream
	StageAudio              // downloading the audio stream
	StageMerge              // ffmpeg mux
	StagePost               // convert / tag
	StageDone
)

// String returns the lowercase stage name used in logs and wire formats.
func (s Stage) String() string {
	switch s {
	case StageProbe:
		return "probe"
	case StageVideo:
		return "video"
	case StageAudio:
		return "audio"
	case StageMerge:
		return "merge"
	case StagePost:
		return "post"
	case StageDone:
		return "done"
	default:
		return "unknown"
	}
}

// State is the lifecycle of a job.
type State int

const (
	StatePending  State = iota // accepted, not started
	StateRunning               // the subprocess is alive
	StateDone                  // output file exists
	StateFailed                // terminal error, see Job.Err
	StateCanceled              // stopped by the user
)

// String returns the lowercase state name used in logs and wire formats.
func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateRunning:
		return "running"
	case StateDone:
		return "done"
	case StateFailed:
		return "failed"
	case StateCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// IsTerminal reports whether the job has stopped and will not change again.
func (s State) IsTerminal() bool {
	return s == StateDone || s == StateFailed || s == StateCanceled
}

// Event is one progress sample for one job.
//
// Events are advisory and lossy by design: the reader may drop samples, and the
// engine coalesces ticks to roughly 10 Hz so a fast connection cannot flood a
// terminal or a websocket. The terminal Event is the one carrying StageDone or
// a non-nil Err.
type Event struct {
	JobID   string
	Stage   Stage
	Percent float64 // 0..1 within the current stage; PercentUnknown when unknown
	Bytes   int64   // bytes of the current stage, 0 when unknown
	Total   int64   // expected bytes of the current stage, 0 when unknown
	Speed   float64 // bytes per second, 0 when unknown
	ETA     time.Duration
	Warn    string // non-fatal notice, e.g. "no H.264 source above 720p"
	Err     error  // set on the final event of a failed job
}

// Terminal reports whether this event ends the job's stream.
func (e Event) Terminal() bool { return e.Err != nil || e.Stage == StageDone }
