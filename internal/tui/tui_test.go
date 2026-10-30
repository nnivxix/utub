package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// fakeManager is the engine seam in tests: probing returns canned data and the
// download half reports ErrNotImplemented until M1.7.
type fakeManager struct {
	info media.MediaInfo
	err  error
}

func (f fakeManager) Probe(context.Context, string) (media.MediaInfo, error) {
	if f.err != nil {
		return media.MediaInfo{}, f.err
	}
	return f.info, nil
}

func (fakeManager) Start(core.Request) (*core.Job, <-chan core.Event, error) {
	return nil, nil, core.ErrNotImplemented
}

func (fakeManager) Cancel(string) error { return core.ErrNotImplemented }

func (fakeManager) List() []*core.Job { return nil }

func testDeps() Deps {
	return Deps{
		Manager: fakeManager{info: demoMedia()},
		Config:  config.Default(),
		DestDir: filepath.Join(os.TempDir(), "utub-test-downloads"),
		Version: "test",
	}
}

// runCmdMsgs executes a command and flattens one level of tea.BatchMsg, which is
// what the router returns when it starts a probe and the spinner together.
func runCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}

	msgs := make([]tea.Msg, 0, len(batch))
	for _, c := range batch {
		if c == nil {
			continue
		}
		msgs = append(msgs, c())
	}
	return msgs
}

func runeKey(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

func submit(t *testing.T, h homeModel) (homeModel, tea.Cmd) {
	t.Helper()
	return h.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestHomeRejectsNonYouTubeLink(t *testing.T) {
	t.Parallel()

	h := newHome(defaultKeyMap())
	h.input.SetValue("https://vimeo.com/12345")

	h, cmd := submit(t, h)

	if cmd != nil {
		t.Fatalf("submit returned a command %T, want nil for an invalid link", cmd())
	}
	if h.hasRef {
		t.Fatal("hasRef = true, want false")
	}
	if !strings.Contains(h.errText, "not a YouTube") {
		t.Fatalf("errText = %q, want it to explain the host is unsupported", h.errText)
	}
}

func TestHomeSubmitNavigatesToProbing(t *testing.T) {
	t.Parallel()

	h := newHome(defaultKeyMap())
	h.input.SetValue("https://youtu.be/dQw4w9WgXcQ")

	_, cmd := submit(t, h)
	if cmd == nil {
		t.Fatal("submit returned no command, want navigateMsg")
	}

	nav, ok := cmd().(navigateMsg)
	if !ok {
		t.Fatalf("command produced %T, want navigateMsg", cmd())
	}
	if nav.screen != screenProbing {
		t.Fatalf("screen = %s, want probing", nav.screen)
	}
	if nav.ref.ID != "dQw4w9WgXcQ" {
		t.Fatalf("id = %q, want dQw4w9WgXcQ", nav.ref.ID)
	}
	if nav.ref.URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("url = %q, want the canonical watch URL", nav.ref.URL)
	}
}

func TestHomeMusicLinkKeepsItsSource(t *testing.T) {
	t.Parallel()

	h := newHome(defaultKeyMap())
	h.input.SetValue("https://music.youtube.com/watch?v=dQw4w9WgXcQ")

	_, cmd := submit(t, h)
	if cmd == nil {
		t.Fatal("submit returned no command")
	}
	nav := cmd().(navigateMsg)
	if !nav.ref.IsMusic() {
		t.Fatalf("source = %s, want youtube-music", nav.ref.Source)
	}
}

func TestHomeClipboardOnlyFillsAnEmptyField(t *testing.T) {
	t.Parallel()

	h := newHome(defaultKeyMap())
	h, _ = h.Update(clipboardMsg{url: "https://youtu.be/dQw4w9WgXcQ"})
	if got := h.input.Value(); got != "https://youtu.be/dQw4w9WgXcQ" {
		t.Fatalf("value = %q, want the clipboard link", got)
	}
	if !strings.Contains(h.notice, "clipboard") {
		t.Fatalf("notice = %q, want it to mention the clipboard", h.notice)
	}

	// A field with content must not be overwritten by a clipboard read.
	h.input.SetValue("https://youtu.be/abcdefghijk")
	h, _ = h.Update(clipboardMsg{url: "https://youtu.be/zzzzzzzzzzz"})
	if got := h.input.Value(); got != "https://youtu.be/abcdefghijk" {
		t.Fatalf("value = %q, want the user's text preserved", got)
	}
}

func TestHomeIgnoresEmptyClipboard(t *testing.T) {
	t.Parallel()

	h := newHome(defaultKeyMap())
	h, cmd := h.Update(clipboardMsg{})
	if cmd != nil {
		t.Fatal("empty clipboard produced a command")
	}
	if h.input.Value() != "" || h.notice != "" {
		t.Fatalf("empty clipboard changed the model: value=%q notice=%q", h.input.Value(), h.notice)
	}
}

func TestHomeClearResetsState(t *testing.T) {
	t.Parallel()

	h := newHome(defaultKeyMap())
	h.input.SetValue("https://youtu.be/dQw4w9WgXcQ")
	h, _ = submit(t, h)
	h.errText = "stale"

	h, _ = h.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})

	if h.input.Value() != "" || h.errText != "" || h.hasRef || h.notice != "" {
		t.Fatalf("ctrl+u left state behind: %+v", h)
	}
	if !h.empty() {
		t.Fatal("empty() = false after clearing")
	}
}

func TestHumanLinkError(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"playlist":    "https://www.youtube.com/playlist?list=PL123",
		"no video id": "https://www.youtube.com/watch",
		"other host":  "https://example.com/watch?v=dQw4w9WgXcQ",
	}

	want := map[string]string{
		"playlist":    "playlists are not supported",
		"no video id": "no video ID",
		"other host":  "not a YouTube",
	}

	for name, raw := range cases {
		_, err := media.ParseURL(raw)
		if err == nil {
			t.Fatalf("%s: parseLink(%q) error = nil", name, raw)
		}
		if got := humanLinkError(err); !strings.Contains(got, want[name]) {
			t.Errorf("%s: humanLinkError() = %q, want it to contain %q", name, got, want[name])
		}
	}
}

func TestRootCtrlCQuits(t *testing.T) {
	t.Parallel()

	r := newRoot(testDeps())
	_, cmd := r.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c produced %T, want tea.QuitMsg", cmd())
	}
}

func TestRootEscapeClearsThenQuits(t *testing.T) {
	t.Parallel()

	r := newRoot(testDeps())
	r.home.input.SetValue("https://youtu.be/dQw4w9WgXcQ")

	model, cmd := r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)
	if cmd != nil {
		t.Fatal("esc with text produced a command, want it to only clear")
	}
	if !r.home.empty() {
		t.Fatalf("field still holds %q after esc", r.home.input.Value())
	}

	model, cmd = r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc on an empty field produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("esc on an empty field produced %T, want tea.QuitMsg", cmd())
	}
	_ = model
}

func TestRootEscapeLeavesAPlaceholderScreen(t *testing.T) {
	t.Parallel()

	r := newRoot(testDeps())
	model, _ := r.Update(navigateMsg{screen: screenSettings})
	r = model.(root)

	if r.screen != screenSettings {
		t.Fatalf("screen = %s, want settings", r.screen)
	}

	model, cmd := r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)
	if r.screen != screenHome {
		t.Fatalf("screen = %s, want home", r.screen)
	}
	if cmd != nil {
		t.Fatal("esc from a placeholder should not quit")
	}
}

func TestRootTypedLetterIsNotACommand(t *testing.T) {
	t.Parallel()

	r := newRoot(testDeps())
	model, cmd := r.Update(runeKey("q"))
	r = model.(root)

	if r.home.input.Value() != "q" {
		t.Fatalf("value = %q, want the letter to reach the input", r.home.input.Value())
	}
	// The input returns a cursor-blink command here; what matters is that the
	// router did not treat the letter as a binding.
	if r.screen != screenHome {
		t.Fatalf("screen = %s, want home", r.screen)
	}
	_ = cmd
}

func TestHeaderShowsShortenedDownloadDir(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	deps := testDeps()
	deps.DestDir = filepath.Join(home, "Downloads")
	r := newRoot(deps)
	r.width = 80

	header := r.header()
	if !strings.Contains(header, "~/Downloads") {
		t.Fatalf("header = %q, want it to shorten the download dir to ~/Downloads", header)
	}
	if width := lipgloss.Width(header); width != 80 {
		t.Fatalf("header width = %d, want it to fill the 80-column window", width)
	}
}

func TestViewRendersProgressScreen(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Demo = true
	r := newRoot(deps)
	model, _ := r.Update(navigateMsg{screen: screenProgress, format: demoMedia().VideoFormats()[0]})
	r = model.(root)

	view := r.View().Content
	for _, want := range []string{"video", "audio", "merge", "saving to", "esc"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want it to contain %q", view, want)
		}
	}
}
