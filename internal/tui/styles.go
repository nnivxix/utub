package tui

import "charm.land/lipgloss/v2"

// Lip Gloss v2 dropped AdaptiveColor: colours are resolved against the actual
// terminal background, which only arrives as a tea.BackgroundColorMsg after
// RequestBackgroundColor. The palette is therefore package state that
// applyTheme rebuilds. Bubble Tea runs Update and View on one goroutine, so
// reassigning these between frames is safe.
var (
	styleBrand    = lipgloss.NewStyle()
	styleMuted    = lipgloss.NewStyle()
	styleDanger   = lipgloss.NewStyle()
	styleGood     = lipgloss.NewStyle()
	styleKey      = lipgloss.NewStyle()
	styleHeading  = lipgloss.NewStyle()
	styleNotice   = lipgloss.NewStyle()
	styleField    = lipgloss.NewStyle()
	styleFocused  = lipgloss.NewStyle()
	styleSelected = lipgloss.NewStyle()
	styleBar      = lipgloss.NewStyle()
	styleBarEmpty = lipgloss.NewStyle()
)

// Dark-first: terminals without background-colour reporting keep this palette.
func init() { applyTheme(true) }

// applyTheme rebuilds every style for a light or dark background.
func applyTheme(dark bool) {
	pick := lipgloss.LightDark(dark)

	accent := pick(lipgloss.Color("#6C3FD1"), lipgloss.Color("#B79CFF"))
	muted := pick(lipgloss.Color("#6B6B6B"), lipgloss.Color("#8A8A8A"))
	danger := pick(lipgloss.Color("#B3261E"), lipgloss.Color("#FF8A80"))
	good := pick(lipgloss.Color("#1B6E3C"), lipgloss.Color("#7FE0A5"))

	styleBrand = lipgloss.NewStyle().Bold(true).Foreground(accent)
	styleMuted = lipgloss.NewStyle().Foreground(muted)
	styleDanger = lipgloss.NewStyle().Foreground(danger)
	styleGood = lipgloss.NewStyle().Foreground(good)
	styleKey = lipgloss.NewStyle().Foreground(accent)
	styleHeading = lipgloss.NewStyle().Bold(true)
	styleNotice = lipgloss.NewStyle().Foreground(accent)

	styleField = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(muted).
		Padding(0, 1)
	styleFocused = styleField.BorderForeground(accent)
	styleSelected = lipgloss.NewStyle().Foreground(accent).Bold(true)
	styleBar = lipgloss.NewStyle().Foreground(accent)
	styleBarEmpty = lipgloss.NewStyle().Foreground(muted)
}
