package tui

import (
	"errors"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// probingModel is the wait between submitting a link and showing the format
// picker: yt-dlp resolves metadata in a few seconds, and a bare frozen frame
// reads as a hang.
type probingModel struct {
	ref   media.Ref
	spin  spinner.Model
	width int
}

func newProbing(ref media.Ref) probingModel {
	return probingModel{
		ref:  ref,
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

// Init starts the spinner. The probe itself is started by the router, which
// owns the cancelable context; this model only animates.
func (m probingModel) Init() tea.Cmd {
	return func() tea.Msg { return m.spin.Tick() }
}

func (m probingModel) Update(msg tea.Msg) (probingModel, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		return m, nil
	}

	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(msg)
	return m, cmd
}

func (m probingModel) View() string {
	var b strings.Builder

	b.WriteString(styleHeading.Render("Resolving formats"))
	b.WriteString("\n\n")
	b.WriteString(m.spin.View())
	b.WriteString(" ")
	b.WriteString(styleMuted.Render(describeRef(m.ref)))
	b.WriteString("\n\n")
	b.WriteString(styleMuted.Render("yt-dlp is fetching the format list — esc cancels"))

	return b.String()
}

// humanProbeError replaces a classified engine error with a sentence. The raw
// stderr line stays in the error chain for logs; nobody should have to read a
// traceback to learn a video is private.
func humanProbeError(err error) string {
	msg := humanEngineError(err)
	if msg == "" {
		msg = "could not read this video — " + firstLine(err.Error())
	}
	return msg + " (enter to retry)"
}

// humanEngineError translates core's classified sentinels, or returns "" when
// the error is not one of them. The two callers add their own fallback wording:
// a probe could not read the link, a download failed.
func humanEngineError(err error) string {
	switch {
	case errors.Is(err, core.ErrBotCheck):
		return "YouTube wants a sign-in check — try again in a minute"
	case errors.Is(err, core.ErrPrivate):
		return "that video is private"
	case errors.Is(err, core.ErrAgeRestricted):
		return "that video is age-restricted"
	case errors.Is(err, core.ErrGeoBlocked):
		return "that video is not available in your region"
	case errors.Is(err, core.ErrUnavailable):
		return "that video is unavailable"
	case errors.Is(err, core.ErrHTTPStatus):
		return "YouTube returned an HTTP error — try again"
	case errors.Is(err, core.ErrMissingFFmpeg):
		return "ffmpeg is missing — install it to mux downloads"
	default:
		return ""
	}
}

// firstLine keeps a multi-line engine message to the part that fits on one
// status line.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
