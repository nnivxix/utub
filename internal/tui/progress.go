package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// demoTickMsg drives the progress screen until M1 wires real core.Events.
type demoTickMsg struct {
	n int
}

const demoTickInterval = 120 * time.Millisecond

// progressModel shows one download. It renders whatever core.Event says; the
// demo ticker below feeds the same fields a real event would, so M1 only has to
// replace the producer.
type progressModel struct {
	keys keyMap

	ref     media.Ref
	format  media.Format
	info    media.MediaInfo
	destDir string

	state   core.State
	stage   core.Stage
	percent float64
	bytes   int64
	total   int64
	speed   float64
	eta     time.Duration
	warn    string
	errText string
	output  string

	demoTicks int
	width     int
	height    int
}

func newProgress(keys keyMap, ref media.Ref, format media.Format, info media.MediaInfo, destDir string) progressModel {
	stage := core.StageVideo
	if format.Kind() == media.KindAudioOnly {
		stage = core.StageAudio
	}
	return progressModel{
		keys:    keys,
		ref:     ref,
		format:  format,
		info:    info,
		destDir: destDir,
		state:   core.StateRunning,
		stage:   stage,
		percent: 0,
	}
}

// start kicks off the demo ticker. M1 replaces this with manager.Start(...).
func (m progressModel) start() (progressModel, tea.Cmd) { return m, demoTick(0) }

func demoTick(n int) tea.Cmd {
	return tea.Tick(demoTickInterval, func(time.Time) tea.Msg { return demoTickMsg{n: n} })
}

func (m progressModel) Update(msg tea.Msg) (progressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case demoTickMsg:
		return m.advance()

	case core.Event:
		return m.apply(msg), nil

	case tea.KeyPressMsg:
		// The router owns esc (cancel) and ctrl+c (quit). Enter only does
		// something once the job has stopped.
		if key.Matches(msg, m.keys.Submit) && m.state.IsTerminal() {
			return m, func() tea.Msg { return navigateMsg{screen: screenHome} }
		}
	}
	return m, nil
}

// advance moves the demo download forward one tick and stops at done.
func (m progressModel) advance() (progressModel, tea.Cmd) {
	if m.state != core.StateRunning {
		return m, nil
	}

	m.demoTicks++
	step := 0.06
	if m.percent < 0 { // merge phase: no byte progress to report
		m.percent = 0
	}
	m.percent += step

	if m.percent >= 1 {
		next, finished := m.nextStage()
		m.percent = 0
		m.stage = next
		if finished {
			m.percent = 1
			m.state = core.StateDone
			m.output = filepath.Join(m.destDir, demoOutputName(m.info, m.format))
			return m, nil
		}
		if next == core.StageMerge {
			m.percent = core.PercentUnknown
		}
	}

	if m.percent >= 0 {
		m.total = m.format.EstimateSize(m.info.Duration)
		m.bytes = int64(m.percent * float64(m.total))
		m.speed = 6.2 * 1024 * 1024
		if remaining := m.total - m.bytes; remaining > 0 {
			m.eta = time.Duration(float64(remaining)/m.speed) * time.Second
		} else {
			m.eta = 0
		}
	} else {
		m.speed, m.eta = 0, 0
	}

	return m, demoTick(m.demoTicks)
}

// nextStage reports the stage that follows the current one, and whether the
// job is finished. Audio-only requests never merge.
func (m progressModel) nextStage() (core.Stage, bool) {
	switch m.stage {
	case core.StageVideo:
		return core.StageAudio, false
	case core.StageAudio:
		if m.format.Kind() == media.KindAudioOnly {
			return core.StageDone, true
		}
		return core.StageMerge, false
	case core.StageMerge:
		return core.StageDone, true
	default:
		return core.StageDone, true
	}
}

// apply folds a real engine event into the view. Unused until M1, but written
// now so the wiring does not change shape later.
func (m progressModel) apply(event core.Event) progressModel {
	if event.Stage != core.StageDone {
		m.stage = event.Stage
	}
	if event.Percent >= 0 {
		m.percent = event.Percent
	} else {
		m.percent = core.PercentUnknown
	}
	m.bytes, m.total, m.speed, m.eta = event.Bytes, event.Total, event.Speed, event.ETA
	if event.Warn != "" {
		m.warn = event.Warn
	}

	switch {
	case event.Err != nil:
		m.state = core.StateFailed
		m.errText = event.Err.Error()
	case event.Stage == core.StageDone:
		m.state = core.StateDone
		m.stage = core.StageDone
		m.percent = 1
	}
	return m
}

// cancel stops the demo job. Partial files are kept on purpose so --continue
// can resume once M1 runs real downloads.
func (m progressModel) cancel() progressModel {
	if m.state.IsTerminal() {
		return m
	}
	m.state = core.StateCanceled
	return m
}

func (m progressModel) View() string {
	var b strings.Builder

	b.WriteString(styleHeading.Render(truncate(m.info.Title, contentWidth(m.width))))
	b.WriteString("\n")
	b.WriteString(styleMuted.Render(m.subtitle()))
	b.WriteString("\n\n")

	active := m.stage
	for _, stage := range m.stages() {
		b.WriteString(m.stageLine(stage, active))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.statusLine())

	if m.warn != "" {
		b.WriteString("\n")
		b.WriteString(styleDanger.Render("⚠ " + m.warn))
	}
	if m.errText != "" {
		b.WriteString("\n")
		b.WriteString(styleDanger.Render("✗ " + m.errText))
	}

	return b.String()
}

func (m progressModel) subtitle() string {
	parts := []string{m.info.Source.String()}
	if m.ref.ID != "" {
		parts = append(parts, m.ref.ID)
	}
	if m.format.ID != "" {
		parts = append(parts, m.format.Label())
	}
	return strings.Join(parts, " · ")
}

// stages lists only the phases this request actually goes through.
func (m progressModel) stages() []core.Stage {
	if m.format.Kind() == media.KindAudioOnly {
		return []core.Stage{core.StageAudio}
	}
	return []core.Stage{core.StageVideo, core.StageAudio, core.StageMerge}
}

const barWidth = 26

func (m progressModel) stageLine(stage core.Stage, active core.Stage) string {
	label := fmt.Sprintf("%-6s", stage.String())

	var line string
	switch {
	case m.state == core.StateDone || stageIndex(stage) < stageIndex(active):
		line = styleBar.Render(strings.Repeat("█", barWidth)) + "  100%"
	case stage != active:
		line = styleBarEmpty.Render(strings.Repeat("░", barWidth)) + "  " + styleMuted.Render("waiting")
	case m.percent < 0:
		// Indeterminate: merging has no byte progress to show.
		spin := []string{"◐", "◓", "◑", "◒"}[m.demoTicks%4]
		line = styleBarEmpty.Render(strings.Repeat("░", barWidth)) + "  " + styleNotice.Render(spin+" working")
	default:
		filled := int(m.percent * barWidth)
		bar := styleBar.Render(strings.Repeat("█", filled)) +
			styleBarEmpty.Render(strings.Repeat("░", barWidth-filled))
		line = fmt.Sprintf("%s %3.0f%%  %s  ETA %s",
			bar, m.percent*100, humanSpeed(m.speed), shortETA(m.eta))
	}

	if stage == active {
		return styleSelected.Render(label) + " " + line
	}
	return label + " " + line
}

func (m progressModel) statusLine() string {
	switch m.state {
	case core.StateDone:
		return styleGood.Render("✓ saved to " + shortenHome(m.output))
	case core.StateCanceled:
		return styleDanger.Render("✗ canceled — partial file kept for resume")
	case core.StateFailed:
		return styleDanger.Render("✗ failed")
	default:
		return styleMuted.Render("saving to " + shortenHome(m.destDir))
	}
}

func stageIndex(stage core.Stage) int {
	switch stage {
	case core.StageVideo:
		return 0
	case core.StageAudio:
		return 1
	case core.StageMerge:
		return 2
	default:
		return 3
	}
}

func humanSpeed(bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "--"
	}
	return media.HumanBytes(int64(bytesPerSecond)) + "/s"
}

func shortETA(eta time.Duration) string {
	if eta <= 0 {
		return "--:--"
	}
	return media.HumanDuration(eta)
}

// demoOutputName guesses the filename the demo would produce. M1 reports the
// real path from yt-dlp instead of guessing.
func demoOutputName(info media.MediaInfo, format media.Format) string {
	ext := format.Ext
	if ext == "" {
		ext = "mp4"
	}
	name := strings.NewReplacer("/", "-", ":", "-", "\\", "-").Replace(info.Title)
	return fmt.Sprintf("%s [%s].%s", name, info.ID, ext)
}
