package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
	"github.com/nnivxix/utub/internal/platform"
)

// demoTickMsg drives the progress screen in --demo mode, where nothing is
// downloaded. Real jobs are fed by the engine's event channel instead.
type demoTickMsg struct {
	n int
}

const demoTickInterval = 120 * time.Millisecond

// progressModel shows one download.
//
// It renders whatever core.Event says and never computes progress itself: with
// a manager, start() hands the request to the engine and the update loop folds
// the resulting events in. --demo keeps the local ticker so layout work does
// not need the network.
type progressModel struct {
	keys keyMap

	manager core.Manager       // nil or demo → offline ticker
	cfg     config.Config      // snapshot for this job
	demo    bool               // --demo
	reveal  func(string) error // nil → platform.Reveal

	ref     media.Ref
	format  media.Format
	info    media.MediaInfo
	destDir string

	job      *core.Job
	events   <-chan core.Event
	stopping bool // cancel requested, waiting for the stream to close

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
	return progressModel{
		keys:    keys,
		ref:     ref,
		format:  format,
		info:    info,
		destDir: destDir,
		state:   core.StateRunning,
		stage:   stageForFormat(format),
		percent: 0,
	}
}

// stageForFormat is the stage a fresh job starts in: an audio-only request has
// no video stream to download.
func stageForFormat(format media.Format) core.Stage {
	if format.Kind() == media.KindAudioOnly {
		return core.StageAudio
	}
	return core.StageVideo
}

// start hands the request to the engine. The manager validates synchronously,
// so a rejected request comes back as a failed job rather than a dead channel.
func (m progressModel) start() (progressModel, tea.Cmd) {
	if m.demo || m.manager == nil {
		m.state = core.StateRunning
		return m, demoTick(0)
	}

	manager := m.manager
	req := core.Request{
		URL:    m.ref.URL,
		Format: m.format,
		OutDir: m.destDir,
		Config: m.cfg,
		Source: m.info,
	}

	m.state = core.StatePending
	return m, func() tea.Msg {
		job, events, err := manager.Start(req)
		if err != nil {
			return jobStartedMsg{err: err}
		}
		return jobStartedMsg{job: job, events: events}
	}
}

// waitForEvent blocks in a tea.Cmd goroutine until the engine produces a sample
// or closes the stream, keeping the update loop responsive while yt-dlp is
// quiet (a merge can take minutes).
func waitForEvent(jobID string, events <-chan core.Event) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return jobClosedMsg{jobID: jobID}
		}
		return jobEventMsg{jobID: jobID, event: event}
	}
}

func demoTick(n int) tea.Cmd {
	return tea.Tick(demoTickInterval, func(time.Time) tea.Msg { return demoTickMsg{n: n} })
}

func (m progressModel) Update(msg tea.Msg) (progressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case demoTickMsg:
		if !m.demo {
			return m, nil
		}
		return m.advance()

	case jobStartedMsg:
		return m.started(msg)

	case jobEventMsg:
		if m.job == nil || msg.jobID != m.job.ID {
			return m, nil
		}
		m = m.apply(msg.event)
		if msg.event.Terminal() {
			// The engine records the final state before it publishes the
			// terminal event, so the output path is readable right now.
			m = m.finish()
		}
		return m, waitForEvent(msg.jobID, m.events)

	case jobClosedMsg:
		if m.job == nil || msg.jobID != m.job.ID {
			return m, nil
		}
		m.events = nil
		return m.finish(), nil

	case tea.KeyPressMsg:
		if !m.state.IsTerminal() {
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Submit):
			return m, func() tea.Msg { return navigateMsg{screen: screenHome} }
		case key.Matches(msg, m.keys.Retry):
			return m.retry()
		case key.Matches(msg, m.keys.Reveal):
			return m, m.revealOutput()
		}
	}
	return m, nil
}

// started adopts the engine's job and begins draining its stream. A cancel
// pressed while Start was still in flight is honoured here.
func (m progressModel) started(msg jobStartedMsg) (progressModel, tea.Cmd) {
	if msg.err != nil {
		m.state = core.StateFailed
		m.stopping = false
		m.errText = humanDownloadError(msg.err)
		return m, nil
	}

	m.job, m.events = msg.job, msg.events
	m.state = core.StateRunning
	m.errText = ""

	if m.stopping {
		// Cancelling is idempotent; ErrFinished just means the job beat us.
		if err := m.manager.Cancel(msg.job.ID); err != nil && !errors.Is(err, core.ErrFinished) {
			m.stopping = false
			m.errText = humanDownloadError(err)
		}
	}

	return m, waitForEvent(msg.job.ID, m.events)
}

// finish reconciles the model with the job's final state after the stream
// closes. A cancelled job has no terminal event, so this is where the screen
// learns it was stopped rather than finished.
func (m progressModel) finish() progressModel {
	if m.job == nil {
		return m
	}

	switch m.job.State() {
	case core.StateDone:
		m.state = core.StateDone
		m.stage = core.StageDone
		m.percent = 1
		m.output = m.job.OutputPath()
	case core.StateCanceled:
		m.state = core.StateCanceled
	case core.StateFailed:
		m.state = core.StateFailed
		if err := m.job.Err(); err != nil {
			m.errText = humanDownloadError(err)
		}
	default:
		// The stream closed while the engine still calls the job pending or
		// running. That should not happen; treat it as a cancellation rather
		// than leaving the screen spinning forever.
		if !m.state.IsTerminal() {
			m.state = core.StateCanceled
		}
	}

	m.stopping = false
	return m
}

// requestCancel asks the engine to stop the job. The screen stays put and shows
// "stopping" until the process group is gone and the stream closes.
func (m progressModel) requestCancel() progressModel {
	if m.state.IsTerminal() || m.stopping {
		return m
	}

	if m.demo || m.manager == nil {
		m.state = core.StateCanceled
		return m
	}

	if m.job == nil {
		// Start is still in flight; started() cancels as soon as it has an id.
		m.stopping = true
		return m
	}

	m.stopping = true
	if err := m.manager.Cancel(m.job.ID); err != nil && !errors.Is(err, core.ErrFinished) {
		m.stopping = false
		m.errText = humanDownloadError(err)
	}
	return m
}

// retry starts a fresh job for the same request after a failure or a
// cancellation. A finished job has nothing to retry.
func (m progressModel) retry() (progressModel, tea.Cmd) {
	if m.demo || (m.state != core.StateFailed && m.state != core.StateCanceled) {
		return m, nil
	}

	m.job, m.events = nil, nil
	m.stopping = false
	m.state = core.StateRunning
	m.stage = stageForFormat(m.format)
	m.percent, m.bytes, m.total, m.speed, m.eta = 0, 0, 0, 0, 0
	m.warn, m.errText, m.output = "", "", ""

	return m.start()
}

// revealOutput opens the finished file in the OS file manager.
func (m progressModel) revealOutput() tea.Cmd {
	if m.state != core.StateDone || m.output == "" {
		return nil
	}

	path := m.output
	reveal := m.reveal
	return func() tea.Msg {
		if reveal == nil {
			reveal = platform.Reveal
		}
		_ = reveal(path)
		return nil
	}
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

// apply folds a real engine event into the view.
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
		m.errText = humanDownloadError(event.Err)
	case event.Stage == core.StageDone:
		m.state = core.StateDone
		m.stage = core.StageDone
		m.percent = 1
	}
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
	switch {
	case m.stopping:
		return styleDanger.Render("✗ stopping — waiting for yt-dlp to exit")
	case m.state == core.StateDone && m.output == "":
		return styleGood.Render("✓ finished")
	case m.state == core.StateDone:
		return styleGood.Render("✓ saved to " + shortenHome(m.output))
	case m.state == core.StateCanceled:
		return styleDanger.Render("✗ canceled — partial file kept for resume")
	case m.state == core.StateFailed:
		return styleDanger.Render("✗ failed")
	case m.job == nil && !m.demo:
		return styleMuted.Render("starting yt-dlp…")
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

// demoOutputName guesses the filename the demo would produce. Real jobs report
// the path from yt-dlp instead of guessing.
func demoOutputName(info media.MediaInfo, format media.Format) string {
	ext := format.Ext
	if ext == "" {
		ext = "mp4"
	}
	name := strings.NewReplacer("/", "-", ":", "-", "\\", "-").Replace(info.Title)
	return fmt.Sprintf("%s [%s].%s", name, info.ID, ext)
}

// humanDownloadError turns a start or download failure into one readable line.
// The raw engine text stays in the error chain for logs.
func humanDownloadError(err error) string {
	switch {
	case errors.Is(err, core.ErrBadOutDir):
		return "the download folder is missing or not a directory — fix it in settings (ctrl+s)"
	case errors.Is(err, core.ErrMissingAria2c):
		return "aria2c is not installed — set the downloader to auto or built-in in settings"
	case errors.Is(err, core.ErrNoOutputPath):
		return "yt-dlp finished but did not say where it saved the file"
	}
	if msg := humanEngineError(err); msg != "" {
		return msg
	}
	return "download failed — " + firstLine(err.Error())
}
