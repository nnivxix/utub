package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/media"
	"github.com/nnivxix/utub/internal/platform"
)

// clipboardTimeout is deliberately short: clipboard helpers can block on a
// display server that does not exist (ssh, headless), and a missing clipboard
// must never delay the first frame.
const clipboardTimeout = 300 * time.Millisecond

// homeModel is the paste-a-link screen.
type homeModel struct {
	keys   keyMap
	input  textinput.Model
	width  int
	height int

	focusCmd tea.Cmd   // cursor blink, started by Init
	ref      media.Ref // last link that parsed
	hasRef   bool
	notice   string
	errText  string
}

func newHome(keys keyMap) homeModel {
	input := textinput.New()
	input.Placeholder = "youtube.com/watch?v=…  ·  youtu.be/…  ·  music.youtube.com/…"
	input.Prompt = "› "
	input.CharLimit = 2048
	input.SetWidth(60)
	focus := input.Focus() // v2: Focus returns the cursor-blink command

	return homeModel{keys: keys, input: input, focusCmd: focus}
}

// Init sniffs the clipboard once. A link found there is offered, never forced:
// the field is only filled when it is still empty.
func (h homeModel) Init() tea.Cmd { return tea.Batch(h.focusCmd, readClipboard) }

func readClipboard() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
	defer cancel()

	link := media.ExtractFirstURL(platform.ReadClipboardText(ctx))
	if link == "" || !media.LooksLikeYouTubeURL(link) {
		return clipboardMsg{}
	}
	return clipboardMsg{url: link}
}

func (h homeModel) Update(msg tea.Msg) (homeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
		h.input.SetWidth(h.fieldWidth() - 2)
		return h, nil

	case clipboardMsg:
		switch {
		case msg.url == "":
			return h, nil
		case h.input.Value() == "":
			h.input.SetValue(msg.url)
			h.notice = "pasted from clipboard"
		default:
			h.notice = "clipboard has a link — ctrl+u to clear, ctrl+v to use it"
		}
		return h, nil

	case noticeMsg:
		h.notice = msg.text
		return h, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, h.keys.Submit):
			return h.submit()

		case key.Matches(msg, h.keys.Paste):
			return h, readClipboard

		case key.Matches(msg, h.keys.Clear):
			h.clearInput()
			return h, nil

		case key.Matches(msg, h.keys.Settings):
			return h, func() tea.Msg { return navigateMsg{screen: screenSettings} }
		}
	}

	var cmd tea.Cmd
	h.input, cmd = h.input.Update(msg)

	// Any typed character invalidates the previous verdict; re-checking on
	// submit keeps the status line from contradicting what is on screen.
	if typed, ok := msg.(tea.KeyPressMsg); ok && typed.Text != "" {
		h.errText = ""
		h.hasRef = false
	}
	return h, cmd
}

// submit validates the field and, when the link is usable, asks the root model
// to move on to the format picker.
func (h homeModel) submit() (homeModel, tea.Cmd) {
	raw := strings.TrimSpace(h.input.Value())
	if raw == "" {
		h.errText = "paste a link first"
		return h, nil
	}

	ref, err := media.ParseURL(raw)
	if err != nil {
		h.errText = humanLinkError(err)
		h.hasRef = false
		return h, nil
	}

	h.ref, h.hasRef, h.errText, h.notice = ref, true, "", ""
	if ref.HasPlaylist {
		h.notice = "playlist link — only this video will be downloaded"
	}

	return h, func() tea.Msg { return navigateMsg{screen: screenProbing, ref: ref} }
}

func (h *homeModel) clearInput() {
	h.input.SetValue("")
	h.ref, h.hasRef, h.errText, h.notice = media.Ref{}, false, "", ""
}

func (h homeModel) empty() bool { return strings.TrimSpace(h.input.Value()) == "" }

// setError shows a failed probe on the status line. The URL stays in the field,
// so enter retries it and typing clears the message.
func (h homeModel) setError(text string) homeModel {
	h.errText, h.hasRef, h.notice = text, false, ""
	return h
}

func (h homeModel) fieldWidth() int {
	width := h.width
	if width <= 0 {
		width = 80
	}
	width = min(width-8, 72)
	return max(width, 24)
}

func (h homeModel) View() string {
	var b strings.Builder

	b.WriteString(styleMuted.Render("Paste a YouTube or YouTube Music link"))
	b.WriteString("\n\n")
	b.WriteString(h.renderField())
	b.WriteString("\n")

	if status := h.statusLine(); status != "" {
		b.WriteString(status)
		b.WriteString("\n")
	}

	return b.String()
}

func (h homeModel) renderField() string {
	style := styleField
	if h.input.Focused() {
		style = styleFocused
	}
	return style.Width(h.fieldWidth() - 2).Render(h.input.View())
}

// statusLine prioritises the most urgent message: an error beats a successful
// parse, which beats an informational notice.
func (h homeModel) statusLine() string {
	switch {
	case h.errText != "":
		return styleDanger.Render("✗ " + h.errText)
	case h.hasRef:
		return styleGood.Render("✓ " + describeRef(h.ref))
	case h.notice != "":
		return styleNotice.Render("· " + h.notice)
	default:
		return ""
	}
}

func describeRef(ref media.Ref) string {
	var b strings.Builder
	b.WriteString(ref.Source.String())
	b.WriteString(" · ")
	b.WriteString(ref.ID)
	if ref.StartSeconds > 0 {
		b.WriteString(" · starts at ")
		b.WriteString(media.HumanDuration(time.Duration(ref.StartSeconds) * time.Second))
	}
	return b.String()
}

// humanLinkError replaces media's terse sentinels with something a person can
// act on.
func humanLinkError(err error) string {
	switch {
	case errors.Is(err, media.ErrEmptyURL):
		return "paste a link first"
	case errors.Is(err, media.ErrUnsupportedURL):
		return "that is not a YouTube or YouTube Music link"
	case errors.Is(err, media.ErrPlaylistOnly):
		return "playlists are not supported yet — open a single video"
	case errors.Is(err, media.ErrNoVideoID):
		return "that link has no video ID"
	case errors.Is(err, media.ErrInvalidURL):
		return "that does not look like a URL"
	default:
		return err.Error()
	}
}
