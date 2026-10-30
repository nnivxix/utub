package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// fakeEngine is the engine seam for download tests: it records what the screen
// asked for and hands back a channel the test drives by hand.
type fakeEngine struct {
	mu       sync.Mutex
	info     media.MediaInfo
	startErr error
	starts   []core.Request
	cancels  []string
	job      *core.Job
	events   chan core.Event
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		job:    &core.Job{ID: "job-test"},
		events: make(chan core.Event, 8),
	}
}

func (f *fakeEngine) Probe(context.Context, string) (media.MediaInfo, error) {
	return f.info, nil
}

func (f *fakeEngine) Start(req core.Request) (*core.Job, <-chan core.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.starts = append(f.starts, req)
	if f.startErr != nil {
		return nil, nil, f.startErr
	}
	return f.job, f.events, nil
}

func (f *fakeEngine) Cancel(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cancels = append(f.cancels, id)
	return nil
}

func (f *fakeEngine) List() []*core.Job { return []*core.Job{f.job} }

func (f *fakeEngine) startRequests() []core.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]core.Request(nil), f.starts...)
}

func (f *fakeEngine) canceled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancels...)
}

// downloadDeps wires a fake engine into the usual test dependencies.
func downloadDeps(engine *fakeEngine) Deps {
	deps := testDeps()
	deps.Manager = engine
	deps.DestDir = "/tmp/utub-test-dest"
	return deps
}

// startDownload drives the router from home into a started job and returns the
// message the engine answered with.
func startDownload(t *testing.T, r *root, engine *fakeEngine) {
	t.Helper()

	ref := media.Ref{
		ID:     demoMedia().ID,
		URL:    "https://www.youtube.com/watch?v=" + demoMedia().ID,
		Source: media.SourceYouTube,
	}

	r.formats.info = demoMedia()

	model, cmd := r.Update(navigateMsg{screen: screenProgress, ref: ref, format: demoMedia().VideoFormats()[0]})
	*r = model.(root)
	if cmd == nil {
		t.Fatal("starting a download returned no command")
	}

	for _, msg := range runCmdMsgs(cmd) {
		model, _ := r.Update(msg)
		*r = model.(root)
	}
}

func TestProgressStartsWithTheProbeSnapshot(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	r := newRoot(downloadDeps(engine))

	ref := media.Ref{ID: "dQw4w9WgXcQ", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
	r.formats.info = demoMedia()
	r.formats.info.ID = "dQw4w9WgXcQ"

	model, cmd := r.Update(navigateMsg{screen: screenProgress, ref: ref, format: demoMedia().VideoFormats()[0]})
	r = model.(root)

	if r.screen != screenProgress {
		t.Fatalf("screen = %s, want progress", r.screen)
	}
	if r.progress.state != core.StatePending {
		t.Fatalf("state = %s, want pending while the engine starts", r.progress.state)
	}
	if cmd == nil {
		t.Fatal("no start command")
	}

	for _, msg := range runCmdMsgs(cmd) {
		model, _ := r.Update(msg)
		r = model.(root)
	}

	if r.progress.state != core.StateRunning {
		t.Fatalf("state = %s, want running", r.progress.state)
	}

	reqs := engine.startRequests()
	if len(reqs) != 1 {
		t.Fatalf("engine received %d requests, want 1", len(reqs))
	}
	req := reqs[0]
	if req.URL != ref.URL {
		t.Errorf("URL = %q, want %q", req.URL, ref.URL)
	}
	if req.OutDir != "/tmp/utub-test-dest" {
		t.Errorf("OutDir = %q, want the resolved directory", req.OutDir)
	}
	if req.Source.ID != "dQw4w9WgXcQ" {
		t.Errorf("Source.ID = %q, want the probed media info to travel with the request", req.Source.ID)
	}
	if req.Format.ID != demoMedia().VideoFormats()[0].ID {
		t.Errorf("Format = %+v, want the picked row", req.Format)
	}
	if req.Config.ConcurrentFragments != testDeps().Config.ConcurrentFragments {
		t.Errorf("Config.ConcurrentFragments = %d, want the snapshot", req.Config.ConcurrentFragments)
	}
}

func TestProgressAppliesEventsUntilDone(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	r := newRoot(downloadDeps(engine))
	startDownload(t, &r, engine)

	model, cmd := r.Update(jobEventMsg{jobID: "job-test", event: core.Event{
		Stage: core.StageAudio, Percent: 0.5, Bytes: 512, Total: 1024, Speed: 2048,
	}})
	r = model.(root)

	if r.progress.state != core.StateRunning {
		t.Fatalf("state = %s, want running", r.progress.state)
	}
	if r.progress.stage != core.StageAudio || r.progress.percent != 0.5 {
		t.Fatalf("event not applied: stage=%s percent=%v", r.progress.stage, r.progress.percent)
	}
	if cmd == nil {
		t.Fatal("no follow-up read command after an event")
	}

	model, _ = r.Update(jobEventMsg{jobID: "job-test", event: core.Event{Stage: core.StageDone, Percent: 1}})
	r = model.(root)
	if r.progress.state != core.StateDone {
		t.Fatalf("state = %s, want done", r.progress.state)
	}

	model, cmd = r.Update(jobClosedMsg{jobID: "job-test"})
	r = model.(root)
	if cmd != nil {
		t.Fatal("a closed stream should not schedule another read")
	}
	if r.progress.state != core.StateDone {
		t.Fatalf("state after close = %s, want done", r.progress.state)
	}
}

func TestProgressShowsClassifiedFailure(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	r := newRoot(downloadDeps(engine))
	startDownload(t, &r, engine)

	model, _ := r.Update(jobEventMsg{jobID: "job-test", event: core.Event{
		Stage: core.StageVideo, Percent: core.PercentUnknown, Err: core.ErrPrivate,
	}})
	r = model.(root)

	if r.progress.state != core.StateFailed {
		t.Fatalf("state = %s, want failed", r.progress.state)
	}
	if !strings.Contains(r.progress.errText, "private") {
		t.Fatalf("errText = %q, want the classified sentence", r.progress.errText)
	}
	if !strings.Contains(r.View().Content, "retry") {
		t.Errorf("footer does not offer a retry: %q", r.footer())
	}
}

func TestProgressFailedStartExplainsTheCause(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	engine.startErr = core.ErrBadOutDir

	r := newRoot(downloadDeps(engine))
	startDownload(t, &r, engine)

	if r.progress.state != core.StateFailed {
		t.Fatalf("state = %s, want failed", r.progress.state)
	}
	if !strings.Contains(r.progress.errText, "settings") {
		t.Fatalf("errText = %q, want it to point at settings", r.progress.errText)
	}
}

func TestProgressRetryStartsANewJob(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	engine.startErr = core.ErrBadOutDir

	r := newRoot(downloadDeps(engine))
	startDownload(t, &r, engine)

	// The first attempt was refused, so the fake has to succeed now.
	engine.mu.Lock()
	engine.startErr = nil
	engine.mu.Unlock()

	model, cmd := r.Update(runeKey("r"))
	r = model.(root)
	if cmd == nil {
		t.Fatal("retry returned no command")
	}
	if r.progress.state != core.StatePending {
		t.Fatalf("state = %s, want pending while retrying", r.progress.state)
	}
	if r.progress.errText != "" {
		t.Fatalf("errText = %q, want it cleared", r.progress.errText)
	}

	for _, msg := range runCmdMsgs(cmd) {
		model, _ := r.Update(msg)
		r = model.(root)
	}

	if got := len(engine.startRequests()); got != 2 {
		t.Fatalf("engine received %d starts, want 2", got)
	}
	if r.progress.state != core.StateRunning {
		t.Fatalf("state = %s, want running after a successful retry", r.progress.state)
	}
}

func TestRootEscapeCancelsARunningJobAndStays(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	r := newRoot(downloadDeps(engine))
	startDownload(t, &r, engine)

	model, cmd := r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)

	if cmd != nil {
		t.Fatal("esc should not schedule anything; the stream close does")
	}
	if r.screen != screenProgress {
		t.Fatalf("screen = %s, want progress until the process is gone", r.screen)
	}
	if !r.progress.stopping {
		t.Fatal("stopping = false, want the screen to say it is stopping")
	}
	if got := engine.canceled(); len(got) != 1 || got[0] != "job-test" {
		t.Fatalf("Cancel calls = %v, want the job id", got)
	}
	if !strings.Contains(r.View().Content, "stopping") {
		t.Fatalf("view = %q, want a stopping status", r.View().Content)
	}

	// A second esc leaves the screen; the engine keeps killing in the background.
	model, _ = r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)
	if r.screen != screenHome {
		t.Fatalf("screen = %s, want home on the second esc", r.screen)
	}
}

func TestProgressCancelBeforeStartCancelsWhenTheJobArrives(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	r := newRoot(downloadDeps(engine))

	ref := media.Ref{ID: "dQw4w9WgXcQ", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
	model, startCmd := r.Update(navigateMsg{screen: screenProgress, ref: ref, format: demoMedia().VideoFormats()[0]})
	r = model.(root)

	// esc while Start is still in flight: there is no id to cancel yet.
	model, _ = r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)
	if !r.progress.stopping {
		t.Fatal("stopping = false, want the cancel to be remembered")
	}
	if got := engine.canceled(); len(got) != 0 {
		t.Fatalf("Cancel calls = %v, want none before a job id exists", got)
	}

	for _, msg := range runCmdMsgs(startCmd) {
		model, _ := r.Update(msg)
		r = model.(root)
	}

	if got := engine.canceled(); len(got) != 1 || got[0] != "job-test" {
		t.Fatalf("Cancel calls = %v, want the job cancelled as soon as it exists", got)
	}
}

func TestProgressRevealsTheFinishedFile(t *testing.T) {
	t.Parallel()

	var revealed string
	info := demoMedia()
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, info.VideoFormats()[0], info, "/tmp")
	m.state = core.StateDone
	m.output = "/tmp/video.mp4"
	m.reveal = func(path string) error {
		revealed = path
		return nil
	}

	_, cmd := m.Update(runeKey("o"))
	if cmd == nil {
		t.Fatal("o on a finished job returned no command")
	}
	cmd()

	if revealed != "/tmp/video.mp4" {
		t.Fatalf("revealed %q, want the output path", revealed)
	}
}

func TestProgressRevealNeedsAFinishedFile(t *testing.T) {
	t.Parallel()

	info := demoMedia()
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, info.VideoFormats()[0], info, "/tmp")

	if _, cmd := m.Update(runeKey("o")); cmd != nil {
		t.Fatal("o on a running job returned a command")
	}
}

func TestProgressRetryIgnoresAFinishedJob(t *testing.T) {
	t.Parallel()

	engine := newFakeEngine()
	info := demoMedia()
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, info.VideoFormats()[0], info, "/tmp")
	m.manager = engine
	m.state = core.StateDone
	m.output = "/tmp/video.mp4"

	if _, cmd := m.Update(runeKey("r")); cmd != nil {
		t.Fatal("r on a finished job started another download")
	}
	if got := len(engine.startRequests()); got != 0 {
		t.Fatalf("engine received %d starts, want 0", got)
	}
}

func TestRootCtrlCCancelsBeforeQuitting(t *testing.T) {
	oldGrace := shutdownGrace
	shutdownGrace = 50 * time.Millisecond
	t.Cleanup(func() { shutdownGrace = oldGrace })

	engine := newFakeEngine()
	r := newRoot(downloadDeps(engine))
	startDownload(t, &r, engine)

	model, cmd := r.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	r = model.(root)

	if cmd == nil {
		t.Fatal("ctrl+c while downloading produced no command")
	}
	if r.progress.stopping != true {
		t.Fatal("stopping = false, want the screen to show it is shutting down")
	}

	// The cancellation happens in the command, so the UI could render
	// "stopping" while the process group is being killed.
	msg := cmd()
	if got := engine.canceled(); len(got) != 1 || got[0] != "job-test" {
		t.Fatalf("Cancel calls = %v, want the running job stopped first", got)
	}
	if _, ok := msg.(quitMsg); !ok {
		t.Fatalf("shutdown produced %T, want quitMsg after the jobs stopped", msg)
	}

	model, quitCmd := r.Update(msg)
	r = model.(root)
	if quitCmd == nil {
		t.Fatal("quitMsg produced no command")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatal("quitMsg should quit the program")
	}
}

func TestActiveJobsSkipsFinishedOnes(t *testing.T) {
	t.Parallel()

	// A job built outside the engine is pending, which counts as live. The nil
	// entry stands for a malformed snapshot and must not panic.
	live := &core.Job{ID: "live"}
	jobs := activeJobs([]*core.Job{live, nil})

	if len(jobs) != 1 || jobs[0] != live {
		t.Fatalf("activeJobs = %v, want just the live job", len(jobs))
	}
	if got := activeJobs(core.NoopManager{}.List()); len(got) != 0 {
		t.Fatalf("activeJobs(NoopManager) = %d jobs, want 0", len(got))
	}
}
