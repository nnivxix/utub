package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/core"
	"github.com/nnivxix/utub/internal/media"
)

// ---------- formats ----------

func TestFormatsHidesNonH264(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{ID: "dQw4w9WgXcQ"}, config.Default())
	rows := m.rows()

	if len(rows) == 0 {
		t.Fatal("no video rows")
	}
	for _, row := range rows {
		if !row.IsH264() {
			t.Fatalf("row %s (%s) is not H.264 but was offered", row.ID, row.VCodec)
		}
		if row.Height > 1080 {
			t.Fatalf("row %s is %dp; H.264 policy should cap at 1080p", row.ID, row.Height)
		}
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5 (1080p60, 1080p, 720p, 480p, 360p)", len(rows))
	}
}

func TestFormatsPrefersVideoOnlyOverMuxed(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, config.Default())
	for _, row := range m.rows() {
		if row.ID == "18" {
			t.Fatal("muxed 360p (18) should lose its bucket to video-only 134")
		}
	}
}

func TestFormatsCursorClamps(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, config.Default())

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after up at the top", m.cursor)
	}

	for range 20 {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if want := len(m.rows()) - 1; m.cursor != want {
		t.Fatalf("cursor = %d, want %d (last row)", m.cursor, want)
	}
}

func TestFormatsTabSwitchesAndResetsCursor(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, config.Default())
	m.cursor = 3

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.tab != tabAudio {
		t.Fatalf("tab = %s, want audio", m.tab)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want reset to 0 after a tab switch", m.cursor)
	}
	// Two source streams (opus 160k, m4a 128k) plus an MP3 row per quality.
	if got := len(m.rows()); got != 4 {
		t.Fatalf("audio rows = %d, want 4 (opus, mp3, m4a, mp3)", got)
	}
}

func TestFormatsAudioOffersMP3Rows(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, config.Default())
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	rows := m.rows()
	var mp3 []media.Format
	for _, row := range rows {
		if row.TargetExt == config.AudioMP3 {
			mp3 = append(mp3, row)
		}
	}
	if len(mp3) != 2 {
		t.Fatalf("MP3 rows = %d, want one per source quality: %v", len(mp3), rows)
	}
	for _, row := range mp3 {
		if row.Kind() != media.KindAudioOnly {
			t.Errorf("MP3 row %s kind = %s, want audio-only", row.ID, row.Kind())
		}
		if row.Output() != "mp3" {
			t.Errorf("MP3 row %s output = %q, want mp3", row.ID, row.Output())
		}
	}
	if !strings.Contains(m.View(), "mp3") {
		t.Errorf("audio view does not show an mp3 row:\n%s", m.View())
	}
}

// TestFormatsAudioMP3DefaultHasNoDuplicateRows: when the configured default
// already converts, every source row is the MP3 row.
func TestFormatsAudioMP3DefaultHasNoDuplicateRows(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.DefaultAudioFormat = config.AudioMP3

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, cfg)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	rows := m.rows()
	if len(rows) != 2 {
		t.Fatalf("audio rows = %d, want the 2 source rows only: %v", len(rows), rows)
	}
	for _, row := range rows {
		if row.TargetExt != "" {
			t.Errorf("row %s carries an explicit target %q under an mp3 default",
				row.ID, row.TargetExt)
		}
	}
}

func TestFormatsEnterSelectsAndNavigates(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{ID: "dQw4w9WgXcQ"}, config.Default())

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}

	nav, ok := cmd().(navigateMsg)
	if !ok {
		t.Fatalf("command produced %T, want navigateMsg", cmd())
	}
	if nav.screen != screenProgress {
		t.Fatalf("screen = %s, want progress", nav.screen)
	}
	if nav.format.ID != "299" {
		t.Fatalf("format = %s, want the 1080p60 row (299)", nav.format.ID)
	}
	if nav.ref.ID != "dQw4w9WgXcQ" {
		t.Fatalf("ref = %q, want the submitted video", nav.ref.ID)
	}
}

func TestFormatsAudioEnterCarriesAudioFormat(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, config.Default())
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	nav := cmd().(navigateMsg)
	if nav.format.Kind() != media.KindAudioOnly {
		t.Fatalf("kind = %s, want audio-only", nav.format.Kind())
	}
}

func TestFormatsAudioEnterCarriesMP3Target(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, config.Default())
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.cursor = 1 // the MP3 row right under the best source stream

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	nav := cmd().(navigateMsg)
	if nav.format.TargetExt != config.AudioMP3 {
		t.Fatalf("format target = %q, want %q", nav.format.TargetExt, config.AudioMP3)
	}
	if nav.format.Kind() != media.KindAudioOnly {
		t.Fatalf("kind = %s, want audio-only", nav.format.Kind())
	}
}

func TestFormatsViewStatesTheH264Limit(t *testing.T) {
	t.Parallel()

	m := newFormats(defaultKeyMap(), demoMedia(), media.Ref{}, config.Default())
	view := m.View()

	if !strings.Contains(view, "H.264 · max 1080p") {
		t.Fatalf("view = %q, want the H.264 ceiling footnote", view)
	}
	if !strings.Contains(view, "★ best") {
		t.Fatalf("view = %q, want a best-pick badge", view)
	}
}

// ---------- progress ----------

func videoFormat(t *testing.T) media.Format {
	t.Helper()
	rows := demoMedia().VideoFormats()
	if len(rows) == 0 {
		t.Fatal("demo media has no video format")
	}
	return rows[0]
}

func TestProgressDemoReachesAudioThenDone(t *testing.T) {
	t.Parallel()

	info := demoMedia()
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, info.VideoFormats()[0], info, "/tmp")

	if m.stage != core.StageVideo {
		t.Fatalf("stage = %s, want video", m.stage)
	}

	seen := map[core.Stage]bool{}
	for range 200 {
		m, _ = m.advance()
		seen[m.stage] = true
		if m.state == core.StateDone {
			break
		}
	}

	if m.state != core.StateDone {
		t.Fatalf("state = %s, want done after enough ticks", m.state)
	}
	if !seen[core.StageAudio] || !seen[core.StageMerge] {
		t.Fatalf("never passed through audio and merge: %v", seen)
	}
	if m.output == "" {
		t.Fatal("done job has no output path")
	}
	if !strings.HasSuffix(m.output, ".mp4") {
		t.Fatalf("output = %q, want an mp4", m.output)
	}
}

func TestProgressAudioOnlySkipsMerge(t *testing.T) {
	t.Parallel()

	info := demoMedia()
	audio := info.AudioFormats()[0]
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, audio, info, "/tmp")

	for range 200 {
		if m.stage == core.StageMerge {
			t.Fatal("audio-only download entered the merge stage")
		}
		m, _ = m.advance()
		if m.state == core.StateDone {
			return
		}
	}
	t.Fatal("audio-only download never finished")
}

func TestProgressDemoMP3RowOutputsMP3(t *testing.T) {
	t.Parallel()

	info := demoMedia()
	audio := media.AudioVariants(info.Formats)[1]
	if audio.TargetExt != config.AudioMP3 {
		t.Fatalf("row target = %q, want mp3", audio.TargetExt)
	}

	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, audio, info, "/tmp")
	for range 200 {
		m, _ = m.advance()
		if m.state == core.StateDone {
			if !strings.HasSuffix(m.output, ".mp3") {
				t.Fatalf("output = %q, want an mp3", m.output)
			}
			return
		}
	}
	t.Fatal("MP3 download never finished")
}

func TestProgressCancelIsSticky(t *testing.T) {
	t.Parallel()

	info := demoMedia()
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, info.VideoFormats()[0], info, "/tmp")
	m.demo = true

	m = m.requestCancel()
	if m.state != core.StateCanceled {
		t.Fatalf("state = %s, want canceled", m.state)
	}

	m, cmd := m.advance()
	if cmd != nil {
		t.Fatal("advance on a canceled job returned a command")
	}
	if m.state != core.StateCanceled {
		t.Fatalf("state = %s, want canceled to stay sticky", m.state)
	}
}

func TestProgressAppliesEngineEvents(t *testing.T) {
	t.Parallel()

	info := demoMedia()
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, info.VideoFormats()[0], info, "/tmp")

	m = m.apply(core.Event{Stage: core.StageVideo, Percent: 0.5, Bytes: 1024, Total: 2048, Speed: 512})
	if m.percent != 0.5 || m.bytes != 1024 || m.total != 2048 {
		t.Fatalf("event not applied: %+v", m)
	}

	m = m.apply(core.Event{Stage: core.StageDone})
	if m.state != core.StateDone || m.stage != core.StageDone {
		t.Fatalf("done event did not finish the job: state=%s stage=%s", m.state, m.stage)
	}
}

func TestProgressFailedEventStopsControls(t *testing.T) {
	t.Parallel()

	info := demoMedia()
	m := newProgress(defaultKeyMap(), media.Ref{ID: info.ID}, info.VideoFormats()[0], info, "/tmp")

	boom := errors.New("ffmpeg not found")
	m = m.apply(core.Event{Stage: core.StageMerge, Err: boom})

	if m.state != core.StateFailed {
		t.Fatalf("state = %s, want failed", m.state)
	}
	if !strings.Contains(m.errText, "ffmpeg") {
		t.Fatalf("errText = %q, want the engine error", m.errText)
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a failed job should offer a way back")
	}
	if nav, ok := cmd().(navigateMsg); !ok || nav.screen != screenHome {
		t.Fatalf("enter produced %v, want navigation home", cmd())
	}
}

// ---------- settings ----------

func settingsForTest(t *testing.T) *settingsModel {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(config.EnvConfigPath, path)

	return newSettings(defaultKeyMap(), config.Default())
}

func TestSettingsSaveWritesConfig(t *testing.T) {
	m := settingsForTest(t)
	dir := t.TempDir()

	m.dir = dir
	m.fragments = "8"
	m.cfg.EmbedThumbnail = false // the confirm toggle binds straight to the working copy

	m, cmd := m.save()
	if m.errText != "" {
		t.Fatalf("save error: %s", m.errText)
	}
	if !strings.Contains(m.notice, "saved") {
		t.Fatalf("notice = %q, want a saved confirmation", m.notice)
	}
	if cmd == nil {
		t.Fatal("save produced no configSavedMsg")
	}
	msg, ok := cmd().(configSavedMsg)
	if !ok {
		t.Fatalf("save produced %T, want configSavedMsg", cmd())
	}
	if msg.dir != dir {
		t.Fatalf("saved dir = %q, want %q", msg.dir, dir)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.DownloadDir != dir {
		t.Fatalf("persisted dir = %q, want %q", loaded.DownloadDir, dir)
	}
	if loaded.ConcurrentFragments != 8 {
		t.Fatalf("persisted fragments = %d, want 8", loaded.ConcurrentFragments)
	}
	if loaded.EmbedThumbnail {
		t.Fatal("persisted embed_thumbnail = true, want the toggle's false")
	}
}

func TestSettingsSaveRejectsNonNumericFragments(t *testing.T) {
	m := settingsForTest(t)
	m.dir = t.TempDir()
	m.fragments = "sixteen"

	m, cmd := m.save()
	if cmd != nil {
		t.Fatal("invalid fragments produced a save command")
	}
	if !strings.Contains(m.errText, "number") {
		t.Fatalf("errText = %q, want a numeric complaint", m.errText)
	}
}

func TestSettingsSaveRejectsUnwritableDirectory(t *testing.T) {
	m := settingsForTest(t)

	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m.dir = filepath.Join(file, "nested")
	m.fragments = "16"

	m, cmd := m.save()
	if cmd != nil {
		t.Fatal("unwritable directory produced a save command")
	}
	if m.errText == "" {
		t.Fatal("expected an error explaining the directory is unusable")
	}
}

func TestRootCancelFromProgressReturnsHome(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Demo = true
	r := newRoot(deps)
	model, _ := r.Update(navigateMsg{screen: screenProgress, format: videoFormat(t)})
	r = model.(root)

	model, cmd := r.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r = model.(root)

	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("esc on progress quit the program instead of canceling")
		}
	}
	if r.screen != screenHome {
		t.Fatalf("screen = %s, want home", r.screen)
	}
	if r.progress.state != core.StateCanceled {
		t.Fatalf("job state = %s, want canceled", r.progress.state)
	}
}

func TestRootSettingsSaveUpdatesHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(config.EnvConfigPath, path)

	r := newRoot(testDeps())
	model, _ := r.Update(navigateMsg{screen: screenSettings})
	r = model.(root)

	newDir := t.TempDir()
	r.settings.dir = newDir
	r.settings.fragments = "16"

	saved, cmd := r.settings.save()
	r.settings = saved
	if cmd == nil {
		t.Fatalf("save failed: %s", r.settings.errText)
	}

	updated, _ := r.Update(cmd())
	r = updated.(root)

	if r.screen != screenHome {
		t.Fatalf("screen = %s, want home after saving", r.screen)
	}
	if r.deps.DestDir != newDir {
		t.Fatalf("dest dir = %q, want %q", r.deps.DestDir, newDir)
	}
	if r.deps.Config.DownloadDir != newDir {
		t.Fatalf("config dir = %q, want %q", r.deps.Config.DownloadDir, newDir)
	}
}
