package tui

import (
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

func testDeps() Deps {
	return Deps{
		Manager: core.NoopManager{},
		Config:  config.Default(),
		DestDir: filepath.Join(os.TempDir(), "utub-test-downloads"),
		Version: "test",
	}
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

func TestHomeSubmitNavigatesToFormats(t *testing.T) {
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
	if nav.screen != screenFormats {
		t.Fatalf("screen = %s, want formats", nav.screen)
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

func TestViewRendersPlaceholder(t *testing.T) {
	t.Parallel()

	r := newRoot(testDeps())
	model, _ := r.Update(navigateMsg{screen: screenProgress})
	r = model.(root)

	view := r.View().Content
	if !strings.Contains(view, "Download progress") {
		t.Fatalf("view = %q, want the progress placeholder title", view)
	}
	if !strings.Contains(view, "arrives in step 8") {
		t.Fatalf("view = %q, want the step-8 marker", view)
	}
	if !strings.Contains(view, "esc") {
		t.Fatalf("view = %q, want the back/quit footer", view)
	}
}
