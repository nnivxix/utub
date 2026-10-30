package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

// keyMap holds every binding the TUI understands.
//
// Global actions avoid plain letters on purpose: the home screen is a text
// field, so "s" or "q" must remain typeable. Anything that acts on the whole
// app uses a modifier or esc. Screen-local list navigation does use j/k, since
// those screens have no text input.
type keyMap struct {
	Quit     key.Binding
	Submit   key.Binding
	Paste    key.Binding
	Clear    key.Binding
	Settings key.Binding
	Save     key.Binding
	Back     key.Binding

	Up       key.Binding
	Down     key.Binding
	Tab      key.Binding
	ShiftTab key.Binding
	Dir      key.Binding

	// Download-screen actions. Cancel shares esc with Back; it exists so the
	// footer can say "cancel" while a job runs and "back" once it has stopped.
	Retry  key.Binding
	Reveal key.Binding
	Cancel key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit:     key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Submit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Paste:    key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste link")),
		Clear:    key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "clear")),
		Settings: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "settings")),
		Save:     key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),

		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move")),
		Tab:      key.NewBinding(key.WithKeys("tab", "l"), key.WithHelp("tab", "video/audio")),
		ShiftTab: key.NewBinding(key.WithKeys("shift+tab", "h"), key.WithHelp("shift+tab", "video/audio")),
		Dir:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "directory")),

		Retry:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
		Reveal: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "reveal")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// helpLine renders bindings as "key desc · key desc", so the footer and any
// future help overlay read from the same source of truth.
func helpLine(bindings ...key.Binding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		if help.Key == "" {
			continue
		}
		parts = append(parts, styleKey.Render(help.Key)+" "+styleMuted.Render(help.Desc))
	}
	return strings.Join(parts, styleMuted.Render(" · "))
}
