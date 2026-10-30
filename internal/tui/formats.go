package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/media"
)

// formatTab selects which half of the format list is visible.
type formatTab int

const (
	tabVideo formatTab = iota
	tabAudio
)

func (t formatTab) String() string {
	if t == tabAudio {
		return "Audio"
	}
	return "Video"
}

// formatsModel is the picker. It is built from a MediaInfo snapshot, so changing
// tabs or moving the cursor never re-probes.
type formatsModel struct {
	keys keyMap
	ref  media.Ref
	info media.MediaInfo

	tab    formatTab
	cursor int
	width  int
	height int
}

func newFormats(keys keyMap, info media.MediaInfo, ref media.Ref) formatsModel {
	return formatsModel{
		keys: keys,
		ref:  ref,
		info: info,
	}
}

// rows returns the visible rows for the active tab.
func (m formatsModel) rows() []media.Format {
	if m.tab == tabAudio {
		return m.info.AudioFormats()
	}
	return m.info.VideoFormats()
}

func (m formatsModel) selected() (media.Format, bool) {
	rows := m.rows()
	if len(rows) == 0 || m.cursor < 0 || m.cursor >= len(rows) {
		return media.Format{}, false
	}
	return rows[m.cursor], true
}

func (m formatsModel) Update(msg tea.Msg) (formatsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			m.cursor = max(0, m.cursor-1)
			return m, nil

		case key.Matches(msg, m.keys.Down):
			m.cursor = min(len(m.rows())-1, m.cursor+1)
			return m, nil

		case key.Matches(msg, m.keys.Tab, m.keys.ShiftTab):
			if m.tab == tabVideo {
				m.tab = tabAudio
			} else {
				m.tab = tabVideo
			}
			m.cursor = 0
			return m, nil

		case key.Matches(msg, m.keys.Dir):
			return m, func() tea.Msg { return navigateMsg{screen: screenSettings} }

		case key.Matches(msg, m.keys.Submit):
			format, ok := m.selected()
			if !ok {
				return m, nil
			}
			ref := m.ref
			return m, func() tea.Msg {
				return navigateMsg{screen: screenProgress, ref: ref, format: format}
			}
		}
	}
	return m, nil
}

func (m formatsModel) View() string {
	var b strings.Builder

	b.WriteString(styleHeading.Render(truncate(m.info.Title, contentWidth(m.width))))
	b.WriteString("\n")
	b.WriteString(styleMuted.Render(m.subtitle()))
	b.WriteString("\n\n")

	b.WriteString(m.tabs())
	b.WriteString("\n\n")

	rows := m.rows()
	switch {
	case len(rows) == 0:
		b.WriteString(styleDanger.Render("no formats reported for this video"))
		b.WriteString("\n")
	default:
		best, _ := media.Recommend(rows)
		for i, format := range rows {
			b.WriteString(m.row(format, i, best))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(styleMuted.Render(m.footnote()))

	return b.String()
}

func (m formatsModel) subtitle() string {
	parts := []string{m.info.Source.String()}
	if m.info.Duration > 0 {
		parts = append(parts, media.HumanDuration(m.info.Duration))
	}
	if m.info.ViewCount > 0 {
		parts = append(parts, humanCount(m.info.ViewCount)+" views")
	}
	if m.info.Channel != "" {
		parts = append(parts, m.info.Channel)
	}
	return strings.Join(parts, " · ")
}

func (m formatsModel) tabs() string {
	video := styleMuted.Render("Video")
	audio := styleMuted.Render("Audio")
	if m.tab == tabVideo {
		video = styleSelected.Render("Video")
	} else {
		audio = styleSelected.Render("Audio")
	}
	return " " + video + "   " + audio
}

// row renders one format line: cursor, quality label, container, frame rate,
// estimated size and the best-pick badge.
func (m formatsModel) row(format media.Format, index int, best media.Format) string {
	selected := index == m.cursor

	marker := "  "
	if selected {
		marker = styleSelected.Render("› ")
	}

	fps := ""
	if format.FPS > 0 && m.tab == tabVideo {
		fps = fmt.Sprintf("%.0ffps", format.FPS)
	}

	size := media.HumanBytes(format.EstimateSize(m.info.Duration))

	badge := ""
	if format.ID == best.ID {
		badge = styleGood.Render("★ best")
	}

	line := fmt.Sprintf("%s%-10s %-4s %-7s %9s  %s",
		marker, format.Label(), format.Ext, fps, size, badge)

	if selected {
		return styleSelected.Render(strings.TrimRight(line, " "))
	}
	return line
}

func (m formatsModel) footnote() string {
	return "H.264 · max 1080p (higher resolutions are VP9/AV1 only)"
}

// contentWidth is the usable text width inside the window chrome.
func contentWidth(width int) int {
	if width <= 0 {
		return 72
	}
	return max(width-8, 32)
}

// truncate shortens s to width runes, adding an ellipsis when it cuts.
func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return string(runes[:max(width, 0)])
	}
	return string(runes[:width-1]) + "…"
}

// humanCount renders 1284553 as 1.3M.
func humanCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
