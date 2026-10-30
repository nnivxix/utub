package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

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

	// Classified yt-dlp failures. Each carries the offending stderr line and,
	// when there is one, the original *exec.ExitError.
	ErrBotCheck      = errors.New("core: youtube wants a sign-in check")
	ErrPrivate       = errors.New("core: video is private")
	ErrAgeRestricted = errors.New("core: video is age-restricted")
	ErrGeoBlocked    = errors.New("core: video is blocked in this region")
	ErrUnavailable   = errors.New("core: video is unavailable")
	ErrHTTPStatus    = errors.New("core: youtube returned an http error")
	ErrMissingFFmpeg = errors.New("core: ffmpeg not found")
)

// cliFailure is one classified yt-dlp failure: the sentinel a caller matches
// on, the line a person can read, and the process error underneath.
type cliFailure struct {
	kind error
	line string
	exit error
}

func (e *cliFailure) Error() string {
	if e.line == "" {
		return e.kind.Error()
	}
	return e.kind.Error() + ": " + e.line
}

// Unwrap exposes both the sentinel and the exit error to errors.Is/errors.As.
func (e *cliFailure) Unwrap() []error {
	if e.exit == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.exit}
}

// cliRule maps a fragment of yt-dlp's stderr to a sentinel. Order matters: the
// most specific cause wins, which is why the age check precedes the bot check
// ("Sign in to confirm your age" also contains "sign in to confirm").
var cliRules = []struct {
	kind   error
	needle string
}{
	{ErrAgeRestricted, "confirm your age"},
	{ErrAgeRestricted, "age-restricted"},
	{ErrBotCheck, "not a bot"},
	{ErrBotCheck, "sign in to confirm"},
	{ErrPrivate, "private video"},
	{ErrGeoBlocked, "available in your country"},
	{ErrGeoBlocked, "available in your region"},
	{ErrMissingFFmpeg, "ffmpeg not found"},
	{ErrMissingFFmpeg, "ffmpeg is not installed"},
	{ErrUnavailable, "video unavailable"},
	{ErrUnavailable, "this video is unavailable"},
	{ErrUnavailable, "has been removed"},
}

var httpStatusRE = regexp.MustCompile(`(?i)http error \d{3}`)

// classifyStderr turns yt-dlp's free text into a typed error. It returns nil
// when no rule matches, so the caller can keep the raw line instead.
func classifyStderr(stderr []byte, cause error) error {
	text := strings.ToLower(string(stderr))

	for _, rule := range cliRules {
		if strings.Contains(text, rule.needle) {
			return &cliFailure{kind: rule.kind, line: errorLine(stderr), exit: cause}
		}
	}
	if httpStatusRE.MatchString(text) {
		return &cliFailure{kind: ErrHTTPStatus, line: errorLine(stderr), exit: cause}
	}
	return nil
}

// errorLine returns the line worth showing: the first ERROR: line, or the first
// non-empty line when there is none. Warnings commonly precede the error.
func errorLine(stderr []byte) string {
	var fallback string

	for _, line := range strings.Split(string(stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if fallback == "" {
			fallback = line
		}
		if strings.HasPrefix(strings.ToUpper(line), "ERROR:") {
			return line
		}
	}
	return fallback
}

// wrapCLI turns a classified failure into a message that names the operation,
// preserving the sentinel and the exit error in the chain.
func wrapCLI(op, subject string, stderr []byte, cause error) error {
	if failure := classifyStderr(stderr, cause); failure != nil {
		return fmt.Errorf("%s %s: %w", op, subject, failure)
	}
	if line := errorLine(stderr); line != "" {
		return fmt.Errorf("%s %s: %s: %w", op, subject, line, cause)
	}
	return fmt.Errorf("%s %s: %w", op, subject, cause)
}
