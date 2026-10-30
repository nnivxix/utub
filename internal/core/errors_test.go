package core

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyStderr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		stderr string
		want   error
		line   string
	}{
		{
			"private",
			"ERROR: [youtube] aqz-KE-bpKQ: Private video. Sign in if you've been granted access to this video\n",
			ErrPrivate,
			"Private video",
		},
		{
			"age restricted",
			"ERROR: [youtube] xyz: Sign in to confirm your age. This video may be inappropriate for some users.",
			ErrAgeRestricted,
			"confirm your age",
		},
		{
			"bot check",
			"ERROR: [youtube] xyz: Sign in to confirm you're not a bot. Use --cookies-from-browser or --cookies for the authentication.",
			ErrBotCheck,
			"not a bot",
		},
		{
			"geo blocked",
			"ERROR: [youtube] xyz: The uploader has not made this video available in your country",
			ErrGeoBlocked,
			"available in your country",
		},
		{
			"unavailable",
			"ERROR: [youtube] xyz: Video unavailable",
			ErrUnavailable,
			"Video unavailable",
		},
		{
			"http status",
			"ERROR: unable to download video data: HTTP Error 403: Forbidden",
			ErrHTTPStatus,
			"HTTP Error 403",
		},
		{
			"missing ffmpeg",
			"ERROR: ffmpeg not found. Please install or provide the path using --ffmpeg-location",
			ErrMissingFFmpeg,
			"ffmpeg not found",
		},
		{
			"warning precedes the error",
			"WARNING: [youtube] falling back to another client\nERROR: [youtube] xyz: Private video\n",
			ErrPrivate,
			"Private video",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyStderr([]byte(tc.stderr), nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("classifyStderr() = %v, want %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.line) {
				t.Errorf("error %q does not carry the offending line %q", err, tc.line)
			}
		})
	}
}

func TestClassifyStderrPreservesTheExitError(t *testing.T) {
	t.Parallel()

	exit := errors.New("exit status 1")
	err := classifyStderr([]byte("ERROR: [youtube] xyz: Private video"), exit)

	if !errors.Is(err, exit) {
		t.Errorf("error %v does not wrap the exit error", err)
	}
	if !errors.Is(err, ErrPrivate) {
		t.Errorf("error %v does not match ErrPrivate", err)
	}
}

func TestClassifyStderrUnknownReturnsNil(t *testing.T) {
	t.Parallel()

	if err := classifyStderr([]byte("ERROR: something nobody has seen before"), nil); err != nil {
		t.Fatalf("classifyStderr() = %v, want nil for an unknown failure", err)
	}
}

func TestErrorLinePrefersTheErrorLine(t *testing.T) {
	t.Parallel()

	stderr := []byte("\n  \nWARNING: a warning\nERROR: the real cause\nERROR: a second cause\n")

	if got, want := errorLine(stderr), "ERROR: the real cause"; got != want {
		t.Errorf("errorLine() = %q, want %q", got, want)
	}
	if got, want := errorLine([]byte("\n only a warning \n")), "only a warning"; got != want {
		t.Errorf("errorLine() without ERROR = %q, want %q", got, want)
	}
	if got := errorLine(nil); got != "" {
		t.Errorf("errorLine(nil) = %q, want empty", got)
	}
}

func TestWrapCLIFallsBackToTheRawLine(t *testing.T) {
	t.Parallel()

	boom := errors.New("exit status 1")
	err := wrapCLI("core: probe", "https://youtu.be/x", []byte("ERROR: who knows"), boom)

	if !strings.Contains(err.Error(), "who knows") {
		t.Errorf("error = %q, want the raw stderr line", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error %v does not wrap the exit error", err)
	}
}
