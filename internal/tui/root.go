package tui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// screen identifies the active view. The router lives in root; each screen only
// knows how to render and update itself.
type screen int

const (
	screenHome screen = iota
	screenFormats
	screenProgress
	screenSettings
)

func (s screen) String() string {
	switch s {
	case screenFormats:
		return "formats"
	case screenProgress:
		return "progress"
	case screenSettings:
		return "settings"
	default:
		return "home"
	}
}

// Deps is everything the TUI needs from the rest of the program. It is a plain
// struct rather than globals so a test can build one without touching the disk.
type Deps struct {
	Manager core.Manager  // engine seam; NoopManager until M1
	Config  config.Config // snapshot at launch
	DestDir string        // resolved, absolute download directory
	Version string        // build stamp
}

// root is the router: it owns the window chrome (header, footer, sizing) and
// decides which screen is visible.
type root struct {
	deps Deps
	keys keyMap

	screen screen
	home   homeModel
	ref    media.Ref // last link the user submitted

	width  int
	height int
}

func newRoot(deps Deps) root {
	keys := defaultKeyMap()
	return root{deps: deps, keys: keys, home: newHome(keys)}
}

func (r root) Init() tea.Cmd {
	// Ask for the terminal background so the palette can adapt; bubbletea
	// replies with tea.BackgroundColorMsg.
	return tea.Batch(r.home.Init(), tea.RequestBackgroundColor)
}

func (r root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		r.home, _ = r.home.Update(msg)
		return r, nil

	case navigateMsg:
		r.screen = msg.screen
		if msg.ref.ID != "" {
			r.ref = msg.ref
		}
		return r, nil

	case tea.BackgroundColorMsg:
		applyTheme(msg.IsDark())
		return r, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, r.keys.Quit):
			return r, tea.Quit

		case key.Matches(msg, r.keys.Back):
			if r.screen != screenHome {
				r.screen = screenHome
				return r, nil
			}
			// On home, esc clears the field first and only quits once there is
			// nothing left to clear.
			if r.home.empty() {
				return r, tea.Quit
			}
			r.home.clearInput()
			return r, nil
		}
	}

	if r.screen == screenHome {
		var cmd tea.Cmd
		r.home, cmd = r.home.Update(msg)
		return r, cmd
	}
	return r, nil
}

// View returns the frame. Alt-screen is a property of the view in Bubble Tea v2
// rather than a program option, so it is set on every frame.
func (r root) View() tea.View {
	view := tea.NewView(r.render())
	view.AltScreen = true
	return view
}

func (r root) render() string {
	body := r.body()
	footer := r.footer()

	content := lipgloss.JoinVertical(lipgloss.Left, r.header(), "", body)
	if r.height > 0 {
		// Pin the footer to the bottom without fighting lipgloss: count what we
		// have and pad the gap.
		if gap := r.height - lipgloss.Height(content) - lipgloss.Height(footer) - 1; gap > 0 {
			content += strings.Repeat("\n", gap)
		}
	}

	return content + "\n" + footer
}

// body renders the active screen. Formats, progress and settings are labelled
// placeholders until step 8; routing itself is real, which is what step 7 is
// for.
func (r root) body() string {
	switch r.screen {
	case screenFormats:
		return placeholder("Format picker", r.ref.URL)
	case screenProgress:
		return placeholder("Download progress", r.ref.URL)
	case screenSettings:
		return placeholder("Settings", "default directory: "+shortenHome(r.deps.DestDir))
	default:
		return r.home.View()
	}
}

func (r root) header() string {
	left := styleBrand.Render("utub")
	if r.deps.Version != "" {
		left += styleMuted.Render(" " + r.deps.Version)
	}
	right := styleMuted.Render(shortenHome(r.deps.DestDir))

	width := r.width
	if width <= 0 {
		width = 80
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (r root) footer() string {
	if r.screen == screenHome {
		return helpLine(r.keys.Submit, r.keys.Paste, r.keys.Clear, r.keys.Settings, r.keys.Back)
	}
	return helpLine(r.keys.Back, r.keys.Quit)
}

// placeholder renders a screen that exists in the router but is not built yet.
func placeholder(title, detail string) string {
	var b strings.Builder
	b.WriteString(styleHeading.Render(title))
	b.WriteString("\n\n")
	if detail != "" {
		b.WriteString(styleMuted.Render(detail))
		b.WriteString("\n\n")
	}
	b.WriteString(styleNotice.Render("arrives in step 8"))
	return b.String()
}

// shortenHome renders /Users/you/Movies/utub as ~/Movies/utub.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}
