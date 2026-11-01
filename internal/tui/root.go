package tui

import (
	"context"
	"os"
	"strings"
	"time"

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
	screenProbing
	screenFormats
	screenProgress
	screenSettings
)

func (s screen) String() string {
	switch s {
	case screenProbing:
		return "probing"
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
	Manager core.Manager  // engine seam; core.Engine in the binary
	Config  config.Config // snapshot at launch, refreshed after a settings save
	DestDir string        // resolved, absolute download directory
	Version string        // build stamp

	// Demo keeps the TUI on canned media and the local progress ticker, so
	// layout work needs neither the network nor yt-dlp. Set by --demo.
	Demo bool

	// Reveal opens a finished file in the OS file manager. Nil means
	// platform.Reveal; tests inject a recorder.
	Reveal func(string) error
}

// root is the router: it owns the window chrome (header, footer, sizing), the
// global keys, and which screen is visible.
type root struct {
	deps Deps
	keys keyMap

	screen   screen
	home     homeModel
	probing  probingModel
	formats  formatsModel
	progress progressModel
	settings *settingsModel

	ref media.Ref // last link the user submitted

	// probeCancel stops the in-flight probe; probeSeq invalidates its result
	// when the user cancels or submits a different link.
	probeCancel context.CancelFunc
	probeSeq    int

	// shuttingDown means a quit is waiting for the running jobs to die; a
	// second ctrl+c must not start a second shutdown.
	shuttingDown bool

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
		r.resize(msg)
		return r, nil

	case navigateMsg:
		return r.navigate(msg)

	case probeDoneMsg:
		if r.screen != screenProbing || msg.seq != r.probeSeq {
			return r, nil
		}
		r.stopProbe()
		r.ref = msg.ref
		r.formats = newFormats(r.keys, msg.info, msg.ref, r.deps.Config)
		r.formats, _ = r.formats.Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
		r.screen = screenFormats
		return r, nil

	case probeErrMsg:
		if r.screen != screenProbing || msg.seq != r.probeSeq {
			return r, nil
		}
		r.stopProbe()
		r.screen = screenHome
		r.home = r.home.setError(humanProbeError(msg.err))
		return r, nil

	case configSavedMsg:
		r.deps.Config = msg.cfg
		r.deps.DestDir = msg.dir
		r.screen = screenHome
		r.home, _ = r.home.Update(noticeMsg{text: "settings saved — downloading to " + shortenHome(msg.dir)})
		return r, nil

	case jobStartedMsg, jobEventMsg, jobClosedMsg:
		// Job messages are handled even when the user has navigated away: a job
		// stopped in the background still has to reach its final state, and its
		// stream must keep draining.
		var cmd tea.Cmd
		r.progress, cmd = r.progress.Update(msg)
		return r, cmd

	case quitMsg:
		return r, tea.Quit

	case tea.BackgroundColorMsg:
		applyTheme(msg.IsDark())
		return r, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, r.keys.Quit):
			r.stopProbe()
			// Compute the command before returning: shutdown mutates r, and
			// return values are evaluated before the call.
			cmd := r.shutdown()
			return r, cmd
		case key.Matches(msg, r.keys.Back):
			return r.back()
		}
	}

	var cmd tea.Cmd
	switch r.screen {
	case screenProbing:
		r.probing, cmd = r.probing.Update(msg)
	case screenFormats:
		r.formats, cmd = r.formats.Update(msg)
	case screenProgress:
		r.progress, cmd = r.progress.Update(msg)
	case screenSettings:
		r.settings, cmd = r.settings.Update(msg)
	default:
		r.home, cmd = r.home.Update(msg)
	}
	return r, cmd
}

// resize keeps every screen's layout in sync with the terminal.
func (r *root) resize(msg tea.WindowSizeMsg) {
	r.width, r.height = msg.Width, msg.Height

	r.home, _ = r.home.Update(msg)
	r.probing, _ = r.probing.Update(msg)
	r.formats, _ = r.formats.Update(msg)
	r.progress, _ = r.progress.Update(msg)
	if r.settings != nil {
		r.settings, _ = r.settings.Update(msg)
	}
}

// navigate builds the destination screen from the current config and the
// message payload, then switches to it.
func (r root) navigate(msg navigateMsg) (tea.Model, tea.Cmd) {
	ref := msg.ref
	if ref.ID == "" {
		ref = r.ref
	}

	switch msg.screen {
	case screenProbing:
		r.ref = ref
		r.probing = newProbing(ref)
		probe := r.beginProbe(ref)
		r.screen = screenProbing
		return r, tea.Batch(probe, r.probing.Init())

	case screenProgress:
		r.ref = ref
		// One download at a time in v1: a job the user walked away from is
		// stopped before a new one is registered.
		if r.progress.job != nil && !r.progress.state.IsTerminal() && r.deps.Manager != nil {
			_ = r.deps.Manager.Cancel(r.progress.job.ID)
		}
		r.progress = r.newProgressModel(ref, msg.format)
		var cmd tea.Cmd
		r.progress, cmd = r.progress.start()
		r.screen = screenProgress
		return r, cmd

	case screenSettings:
		r.settings = newSettings(r.keys, r.deps.Config)
		r.settings, _ = r.settings.Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
		r.screen = screenSettings
		return r, r.settings.Init()

	default:
		r.screen = screenHome
		return r, nil
	}
}

// beginProbe starts Manager.Probe in the background. The context is cancelable
// so esc can abandon a hung yt-dlp, and seq keeps a late answer from replacing
// a newer request.
func (r *root) beginProbe(ref media.Ref) tea.Cmd {
	r.stopProbe()
	r.probeSeq++
	seq := r.probeSeq

	if r.deps.Demo {
		return func() tea.Msg {
			return probeDoneMsg{seq: seq, ref: ref, info: demoMedia()}
		}
	}

	if r.deps.Manager == nil {
		return func() tea.Msg {
			return probeErrMsg{seq: seq, ref: ref, err: core.ErrNotImplemented}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.probeCancel = cancel

	manager := r.deps.Manager
	return func() tea.Msg {
		info, err := manager.Probe(ctx, ref.URL)
		if err != nil {
			return probeErrMsg{seq: seq, ref: ref, err: err}
		}
		return probeDoneMsg{seq: seq, ref: ref, info: info}
	}
}

// stopProbe cancels the in-flight probe, if any.
func (r *root) stopProbe() {
	if r.probeCancel != nil {
		r.probeCancel()
		r.probeCancel = nil
	}
}

// newProgressModel snapshots everything a job needs. Config is copied on
// purpose: changing settings mid-download must not alter the running job.
func (r root) newProgressModel(ref media.Ref, format media.Format) progressModel {
	info := r.formats.info
	if r.deps.Demo {
		info = demoMedia()
	}

	m := newProgress(r.keys, ref, format, info, r.deps.DestDir)
	m.manager = r.deps.Manager
	m.cfg = r.deps.Config
	m.reveal = r.deps.Reveal
	m.demo = r.deps.Demo
	return m
}

// shutdownGrace bounds how long quitting waits for a cancelled process group to
// disappear. It matches the engine's SIGTERM→SIGKILL escalation plus slack.
var shutdownGrace = 8 * time.Second

// shutdown stops every live job before the program exits, so quitting never
// leaves yt-dlp or ffmpeg running as orphans. The wait runs in a command: the
// screen can render "stopping" instead of freezing for the kill grace period.
func (r *root) shutdown() tea.Cmd {
	if r.shuttingDown {
		return nil
	}

	manager := r.deps.Manager
	if manager == nil {
		return tea.Quit
	}

	jobs := activeJobs(manager.List())
	if len(jobs) == 0 {
		return tea.Quit
	}

	r.shuttingDown = true
	if r.screen == screenProgress {
		r.progress.stopping = true
	}

	return func() tea.Msg {
		for _, job := range jobs {
			_ = manager.Cancel(job.ID)
		}
		waitForTerminal(jobs, shutdownGrace)
		return quitMsg{}
	}
}

// activeJobs filters a snapshot down to the jobs still worth cancelling.
func activeJobs(jobs []*core.Job) []*core.Job {
	live := make([]*core.Job, 0, len(jobs))
	for _, job := range jobs {
		if job != nil && !job.State().IsTerminal() {
			live = append(live, job)
		}
	}
	return live
}

// waitForTerminal polls until every job has stopped or max elapses.
func waitForTerminal(jobs []*core.Job, max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if len(activeJobs(jobs)) == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// back handles esc per screen: cancel on progress or probing, clear-then-quit
// on home, plain return everywhere else.
func (r root) back() (tea.Model, tea.Cmd) {
	switch r.screen {
	case screenProbing:
		r.stopProbe()
		r.probeSeq++ // a result already in flight must not open the picker
		r.screen = screenHome
		r.home, _ = r.home.Update(noticeMsg{text: "probe canceled"})
		return r, nil

	case screenProgress:
		if r.progress.state.IsTerminal() {
			r.screen = screenHome
			return r, nil
		}
		if r.progress.demo {
			r.progress = r.progress.requestCancel()
			r.screen = screenHome
			r.home, _ = r.home.Update(noticeMsg{text: "download canceled"})
			return r, nil
		}
		if r.progress.stopping {
			// The engine keeps killing the process group in the background.
			r.screen = screenHome
			r.home, _ = r.home.Update(noticeMsg{text: "stopping the download in the background"})
			return r, nil
		}
		r.progress = r.progress.requestCancel()
		return r, nil

	case screenHome:
		if r.home.empty() {
			return r, r.shutdown()
		}
		r.home.clearInput()
		return r, nil

	default:
		r.screen = screenHome
		return r, nil
	}
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

func (r root) body() string {
	switch r.screen {
	case screenProbing:
		return r.probing.View()
	case screenFormats:
		return r.formats.View()
	case screenProgress:
		return r.progress.View()
	case screenSettings:
		return r.settings.View()
	default:
		return r.home.View()
	}
}

func (r root) header() string {
	left := styleBrand.Render("utub")
	if r.deps.Version != "" {
		left += styleMuted.Render(" " + r.deps.Version)
	}
	if r.deps.Demo {
		left += styleNotice.Render(" demo")
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
	switch r.screen {
	case screenProbing:
		return helpLine(r.keys.Back, r.keys.Quit)
	case screenFormats:
		return helpLine(r.keys.Up, r.keys.Down, r.keys.Tab, r.keys.Submit, r.keys.Dir, r.keys.Back)
	case screenProgress:
		return r.progressFooter()
	case screenSettings:
		return helpLine(r.keys.Save, r.keys.Back, r.keys.Quit)
	default:
		return helpLine(r.keys.Submit, r.keys.Paste, r.keys.Clear, r.keys.Settings, r.keys.Back)
	}
}

// progressFooter offers only the actions that make sense for the job's state:
// a running job can be cancelled, a failed one retried, a finished one revealed.
func (r root) progressFooter() string {
	switch {
	case r.shuttingDown:
		return helpLine(r.keys.Quit)
	case r.progress.stopping:
		return helpLine(r.keys.Quit)
	case r.progress.state == core.StateDone:
		return helpLine(r.keys.Reveal, r.keys.Submit, r.keys.Quit)
	case r.progress.state == core.StateFailed:
		return helpLine(r.keys.Retry, r.keys.Back, r.keys.Quit)
	case r.progress.state == core.StateCanceled:
		return helpLine(r.keys.Submit, r.keys.Back, r.keys.Quit)
	case r.progress.demo:
		return helpLine(r.keys.Back, r.keys.Quit)
	default:
		return helpLine(r.keys.Cancel, r.keys.Quit)
	}
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
