package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

func testRef() media.Ref {
	return media.Ref{ID: "dQw4w9WgXcQ", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
}

// startProbe drives the router into the probing screen and returns every
// message its commands produced, so a test can replay the real probe result.
func startProbe(t *testing.T, r root, ref media.Ref) (root, []tea.Msg) {
	t.Helper()

	model, cmd := r.Update(navigateMsg{screen: screenProbing, ref: ref})
	return model.(root), runCmdMsgs(cmd)
}

func TestProbeShowsSpinnerThenThePicker(t *testing.T) {
	t.Parallel()

	r, msgs := startProbe(t, newRoot(testDeps()), testRef())

	if r.screen != screenProbing {
		t.Fatalf("screen = %s, want probing", r.screen)
	}
	if got := r.View().Content; !strings.Contains(got, "esc cancels") {
		t.Fatalf("view = %q, want the probing hint", got)
	}

	var done bool
	for _, msg := range msgs {
		probe, ok := msg.(probeDoneMsg)
		if !ok {
			continue
		}
		done = true
		model, _ := r.Update(probe)
		r = model.(root)
	}
	if !done {
		t.Fatal("the probe command produced no probeDoneMsg")
	}
	if r.screen != screenFormats {
		t.Fatalf("screen = %s, want formats after a successful probe", r.screen)
	}
	if got := r.View().Content; !strings.Contains(got, "H.264 · max 1080p") {
		t.Fatalf("view = %q, want the real format picker", got)
	}
}

func TestProbeErrorReturnsHomeWithAReadableReason(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Manager = fakeManager{err: fmt.Errorf("core: probe x: %w", core.ErrPrivate)}

	r := newRoot(deps)
	r.home.input.SetValue(testRef().URL) // as home.submit leaves it
	r, msgs := startProbe(t, r, testRef())

	var failed bool
	for _, msg := range msgs {
		probe, ok := msg.(probeErrMsg)
		if !ok {
			continue
		}
		failed = true
		model, _ := r.Update(probe)
		r = model.(root)
	}
	if !failed {
		t.Fatal("the probe command produced no probeErrMsg")
	}

	if r.screen != screenHome {
		t.Fatalf("screen = %s, want home after a failed probe", r.screen)
	}
	if !strings.Contains(r.home.errText, "private") {
		t.Errorf("errText = %q, want it to say the video is private", r.home.errText)
	}
	if !strings.Contains(r.home.errText, "retry") {
		t.Errorf("errText = %q, want a retry hint", r.home.errText)
	}
	if strings.Contains(r.home.errText, "core:") {
		t.Errorf("errText = %q leaked engine detail", r.home.errText)
	}
	if got := r.home.input.Value(); got != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("input = %q, want the URL kept so enter can retry", got)
	}
}

func TestProbeEscapeCancelsAndIgnoresTheLateResult(t *testing.T) {
	t.Parallel()

	r, msgs := startProbe(t, newRoot(testDeps()), testRef())

	// Hold on to the result the in-flight probe would deliver after esc.
	var late probeDoneMsg
	for _, msg := range msgs {
		if done, ok := msg.(probeDoneMsg); ok {
			late = done
		}
	}

	model, _ := r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)

	if r.screen != screenHome {
		t.Fatalf("screen = %s, want home after canceling", r.screen)
	}
	if r.probeCancel != nil {
		t.Fatal("probe context was not released")
	}

	model, _ = r.Update(late)
	r = model.(root)
	if r.screen != screenHome {
		t.Fatalf("screen = %s, want the late probe result ignored", r.screen)
	}
}

func TestHumanProbeError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want string
	}{
		{core.ErrBotCheck, "sign-in"},
		{core.ErrPrivate, "private"},
		{core.ErrAgeRestricted, "age-restricted"},
		{core.ErrGeoBlocked, "region"},
		{core.ErrUnavailable, "unavailable"},
		{core.ErrHTTPStatus, "HTTP"},
		{core.ErrMissingFFmpeg, "ffmpeg"},
		{fmt.Errorf("wrapped: %w", core.ErrPrivate), "private"},
	}

	for _, tc := range cases {
		got := humanProbeError(tc.err)
		if !strings.Contains(got, tc.want) {
			t.Errorf("humanProbeError(%v) = %q, want it to contain %q", tc.err, got, tc.want)
		}
		if strings.Contains(got, "core:") {
			t.Errorf("humanProbeError(%v) = %q, want no engine prefixes", tc.err, got)
		}
		if !strings.Contains(got, "retry") {
			t.Errorf("humanProbeError(%v) = %q, want a retry hint", tc.err, got)
		}
	}
}
