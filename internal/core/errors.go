package core

import "errors"

// Sentinel errors. Surfaces compare with errors.Is; implementations wrap them
// with context so a caller can log detail without matching on strings.
var (
	// ErrNotImplemented is returned by the step-5 stubs and NoopManager.
	ErrNotImplemented = errors.New("core: not implemented yet")

	// ErrNotFound means the job id is unknown to this manager.
	ErrNotFound = errors.New("core: job not found")

	// ErrFinished means the job already reached a terminal state.
	ErrFinished = errors.New("core: job already finished")

	// ErrBadProbe means yt-dlp's output could not be understood: JSON that does
	// not parse, or a video object with no formats.
	ErrBadProbe = errors.New("core: could not read yt-dlp output")

	// ErrNoFormats means the video exists but offers nothing downloadable.
	ErrNoFormats = errors.New("core: yt-dlp reported no formats")
)

// TODO(M1): stderr classification lives here.
//
// yt-dlp reports failures as free text on stderr. The parser maps the common
// ones to typed errors so surfaces can show a sentence instead of a traceback:
//
//	"Sign in to confirm you're not a bot"   → ErrBotCheck  (suggest cookies_from_browser)
//	"Private video"                         → ErrPrivate
//	"Video unavailable"                     → ErrUnavailable
//	"This video is available in ... only"    → ErrGeoBlocked
//	"confirm your age" / "age-restricted"    → ErrAgeRestricted
//	HTTP Error 403 / 429                     → ErrHTTPStatus (retryable)
//	"ffmpeg not found"                       → ErrMissingFFmpeg
//
// Each carries the offending line and the hint the UI should print. The
// underlying *exec.ExitError is preserved in the chain so callers can inspect
// the exit code.
