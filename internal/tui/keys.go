package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

// keyMap holds every binding the TUI understands.
//
// Global actions avoid plain letters on purpose: the home screen is a text
// field, so "s" or "q" must remain typeable. Anything that acts on the whole
// app uses a modifier or esc.
type keyMap struct {
	Quit     key.Binding
	Submit   key.Binding
	Paste    key.Binding
	Clear    key.Binding
	Settings key.Binding
	Back     key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit:     key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Submit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "formats")),
		Paste:    key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste link")),
		Clear:    key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "clear")),
		Settings: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "settings")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
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
