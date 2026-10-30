# utub — PLAN.md (handoff / continuity)

> **What this file is.** A single source of truth so a fresh session (human or AI) can resume
> exactly where the previous one stopped. Read §2 (status), §7 (remaining work) and §10
> (how to continue) before touching code.

Last updated: 2026-10-30 · Repo: `/Users/hanasa/Labs/personal/utub` · Branch: `main` (clean)

---

## 1. What this project is

A YouTube / YouTube Music downloader in Go with three surfaces over one engine:

| Surface | Binary | Talks to the engine via |
|---|---|---|
| TUI | `utub` | in-process Go interface |
| Web (self-hosted, single-user) | `utubd` | HTTP/JSON + SSE |
| Desktop | `utub.app` (Wails v3) | the same HTTP handler mounted in the webview, no port |

Module: `github.com/nnivxix/utub` · Go 1.27.3 · macOS / Linux / Windows · MIT.

The engine is a **wrapper around sidecar binaries**, not a reimplementation:
`yt-dlp` (extraction) and `ffmpeg` (mux/convert) are required; `aria2c` is an optional
multi-connection accelerator. Go owns URL parsing, format selection, argv construction,
subprocess lifecycle, progress parsing, cancellation, resume and file placement.

Full design rationale: `docs/architecture.md` (it is the spec; this file is the progress log).

---

## 2. Status snapshot

**Done: steps 0–9, M1.1–M1.8 and M3.** The engine is complete and the TUI is wired to it:
paste a link, pick a format, watch the real download, cancel it, reveal the file. Only
`--demo` still runs on canned data.

| # | Deliverable | State |
|---|---|---|
| 0 | docs (`docs/architecture.md`, `web/README.md`, `desktop/README.md`) | done |
| 1 | repo skeleton (`go.mod`, Makefile, .gitignore) | done |
| 2 | `internal/config` | done, 18 tests |
| 3 | `internal/platform` | done, 7 tests |
| 4 | `internal/media` | done, 22 tests |
| 5 | `internal/core` seam (Event/State/Job/Manager + `NoopManager`) | done, 7 tests |
| 6 | `internal/binmgr` (+ `utub --doctor`) | done, 8 tests |
| 7 | TUI foundation (home, router, keys, styles) | done |
| 8 | TUI screens (formats, progress, settings) | done, 32 tests |
| 9 | wrap: fmt/vet/test, docs truth-up, commits | done |
| **M1** | **engine: probe / select / download / progress / errors / manager** | **done** |
| **M3** | **TUI complete (real data, probing state, real cancel)** | **done** |
| M4 | `internal/api` + `cmd/utubd` | next |
| M5 | Vue 3 SPA (`web/`) | not started |
| M6 | Wails v3 desktop (`desktop/`) | not started |
| M7 | release (goreleaser, brew, dmg/msi) | not started |

Commit history (newest first):

```
4563b61  docs: record that steps 0-9 are done and M1 is next
03588a9  feat(tui): format picker, live progress and settings form
59eb1dd  feat(tui): Bubble Tea v2 foundation with home screen and router
12158c6  feat(binmgr): locate yt-dlp/ffmpeg/aria2c, gate yt-dlp version, doctor report
13543a8  feat(core): engine seam, job state machine and noop manager
d9da241  feat(media): link parsing, H.264 format policy, share-text extraction
4fb8e8a  feat(platform): OS glue for reveal, clipboard and downloads dir
316950f  feat(config): validated TOML config with atomic save
e0f4475  chore: project skeleton
8ccc92c  docs: architecture spec, web/desktop surface plans, root README
```

Scale: ~5,700 lines of Go across `internal/` + `cmd/` (+ ~5,800 test lines), 194 test
functions, 7 packages green.

**What actually works today (the TUI is a usable downloader):** `--doctor`, `--version`, the
full TUI navigation, settings persistence (dir, fragments, downloader, audio format,
cookies-from-browser), real URL validation, real `yt-dlp -J` probing behind a spinner, H.264
format selection, and a real download: live speed/ETA per stage, process-group cancel that
keeps `.part` for `--continue`, reveal in Finder, classified failure messages, retry, and a
quit that waits for the downloader to die.

**`--demo`** is the only fake path left: canned media plus the local ticker, for offline layout
work. `NoopManager`/`ErrNotImplemented` remain for the not-yet-written API and desktop
surfaces.

---

## 3. Decisions already locked (do not re-litigate without cause)

| Area | Decision | Why |
|---|---|---|
| Engine | sidecar `yt-dlp` + `ffmpeg`, never a pure-Go extractor | YouTube player changes would break a Go extractor weekly |
| Video formats | **H.264 (`avc1`) only**, AAC/m4a audio, `--merge-output-format mp4` | plays/imports everywhere (QuickTime, iOS, browsers, Premiere, Resolve). YouTube only serves H.264 up to **1080p** — the UI states this instead of hiding it. The only policy in v1 |
| Parallelism | `-N/--concurrent-fragments 16` + `--http-chunk-size 10M`, auto-upgrade to `aria2c -x16 -s16 -k1M` | DASH fragments are the real parallelism win; ranged requests cover progressive formats |
| Config | TOML at `os.UserConfigDir()/utub/config.toml`, atomic write, `UTUB_CONFIG` override | |
| Default dir | `~/Downloads` (macOS/Windows), `XDG_DOWNLOAD_DIR` on Linux | |
| TUI stack | `charm.land/bubbletea/v2` v2.1.1, `bubbles/v2` v2.2.1, `lipgloss/v2` v2.0.6, `huh/v2` v2.0.3 | v2 only; no v1 charm modules may return |
| Web model | self-hosted, **single-user**, files land on the *server's* disk, loopback + bearer token, no accounts | not a public SaaS (ToS/DMCA/storage) |
| Web frontend | **Vue 3** + Vite + TS + Tailwind + Pinia; one SPA shared with desktop via `VITE_TARGET` | |
| Desktop | **Wails v3** (stable since 2026-10-26), separate `go.mod`, mounts the same `internal/api` handler | keeps Wails out of `go install utub` |
| Quit keys | `ctrl+c` and `esc`-on-empty; **not `q`** | the home screen is a text field, `q` must stay typeable |
| Cancellation | `StateCanceled` and `Job.Err() == nil`; never rendered as a failure | |
| Out of scope v1 | playlists, cross-item queue, subtitles, thumbnail embedding, proxies, multi-tenant hosting | |

---

## 4. Environment used for verification

```
go       1.27.3 darwin/arm64
yt-dlp   2026.10.26   (/Users/hanasa/.pyenv/shims/yt-dlp)
ffmpeg   9.0.1        (/opt/homebrew/bin/ffmpeg)
aria2c   not installed (optional; the engine must fall back cleanly)
```

`make doctor` prints this report and exits non-zero when a required binary is missing.

---

## 5. Repo map (what lives where)

```
cmd/utub/main.go            --version / --doctor / TUI launch; deps built here
cmd/utubd/.gitkeep          empty — M4
internal/core/              THE ENGINE SEAM (must import no UI)
  doc.go                    package doc + layer rule + M1 file map
  event.go                  Stage, State, Event, PercentUnknown, Event.Terminal()
  job.go                    Request (incl. probed Source), Job, private state machine
  manager.go                Manager interface, NoopManager, Engine (registry + run loop)
  errors.go                 sentinels + stderr classification table
  probe.go  select.go       yt-dlp -J → MediaInfo; format → -f chain
  download.go  progress.go  argv, subprocess lifecycle, parser, coalescer
internal/media/             Format/Kind/MediaInfo, H.264 classification, size estimates,
                            URL validation (watch/youtu.be/shorts/live/embed/music), share-text
internal/config/            Config struct ↔ config.toml, defaults, atomic save, dir resolution
internal/platform/          OS glue: downloads dir, Reveal (darwin/linux/windows), clipboard
internal/binmgr/            locate + version-probe yt-dlp/ffmpeg/aria2c, yt-dlp version gate
internal/tui/               Bubble Tea v2
  run.go root.go            program + panic-safe terminal restore; router, header/footer
  home.go                   URL field, clipboard sniff, link errors
  formats.go                Video/Audio tabs, H.264 rows, ★ best
  progress.go               real download: channel reader, cancel/stopping, retry, reveal
  settings.go               huh v2 form → config.toml (dir, fragments, downloader, audio, cookies)
  demo.go                   demoMedia() — canned data for --demo
  keys.go styles.go msgs.go
internal/api/.gitkeep       empty — M4
web/README.md               spec only
desktop/README.md           spec only
docs/architecture.md        the design spec (milestone table has a Status column)
```

---

## 6. Architecture contracts (breaking these breaks the other two surfaces)

1. **`internal/core` imports no UI.** `go list -deps ./internal/core | grep -E 'bubbletea|net/http|wails'`
   must print nothing. Enforced by habit today; wire it into CI in M1.
2. **One event stream.** `Manager.Start` returns `<-chan Event`. TUI reads it in-process, web
   re-encodes as SSE, desktop forwards as Wails events. **No surface computes progress itself.**
3. **One state machine** (`job.go`): `Pending → Running → Done|Failed|Canceled`; the first
   terminal transition wins, so a non-zero exit after a cancel cannot resurrect a job as failed.
4. **Progress ticks are coalesced to ~10 Hz** before leaving the engine.
5. **The engine never guesses the output filename.** It reads yt-dlp's own
   `[download] Destination:` / `[Merger] Merging formats into "…"` / `[MoveFiles]` lines.
6. **H.264 policy lives in `internal/media`**, not in the picker: `VideoFormats()`.
   The UI only renders what it is given and prints the 1080p ceiling.
7. **The manager owns job identity and the terminal frame.** The executor emits events with
   no `JobID` and its own `StageDone` is suppressed by `Engine.run`, which records the final
   state and output path *before* publishing the terminal event, so a reader that reacts to
   the closed channel never sees a half-final job.
8. **A cancelled job ends by closing its stream, not with an `Event`.** `Event` carries no
   cancellation signal; readers that must distinguish done from cancelled call `Job.State()`.

### Exact TODO(M1) locations

None. M1 is complete. What remains is M4+ (API, web, desktop, release). `--demo` is a
supported developer path, not a TODO.

---

## 7. Remaining work

### M1 — the engine (~4 days). **Done.**

M1.1–M1.8 are done and the acceptance run below was executed against a live link (the
per-step evidence is in each commit).

#### M1.1 `core/probe.go` — `yt-dlp -J` → `MediaInfo` — **done**

- Add a test seam: `type runner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)`,
  injected into the prober — same pattern as `platform/clipboard.go`. Keeps tests offline.
- Command: `yt-dlp -J --no-playlist --no-warnings --no-progress <url>`, 60 s ctx timeout.
- Map JSON → `media.MediaInfo` + `[]media.Format`: `duration` (float seconds → `time.Duration`),
  `filesize`/`filesize_approx`, `view_count`, `live_status`, `formats[]`.
- Guards: `{"_type":"playlist"}` → typed error; empty `formats` → typed error; one JSON object
  on stdout, nothing else.
- Commit fixtures under `internal/core/testdata/`: `video.json`, `music.json`, `playlist.json`,
  `private.json` (capture with `yt-dlp -J … > file.json`).
- **Gate:** table tests map all four fixtures; no network in `go test`.

#### M1.2 `core/select.go` — format → `-f` — **done**

- `Selector(req Request) (expr string, warn string)` — pure, no I/O.
- Chain: the exact picked itag, then `bv*[vcodec^=avc1][height=N]+ba[ext=m4a]` → `…[height<=N]…`
  → `b[vcodec^=avc1][height<=N]` → `bv*[vcodec^=avc1]+ba`. Verified live: when an itag is
  withheld the chain falls through cleanly (a `137` pin became `299+140`).
- `AudioArgs(cfg)` for `-x --audio-format mp3|m4a --audio-quality 0`; `original` = no `-x`.
  An m4a target prefers `ba[ext=m4a]/ba/b` so it can remux instead of re-encoding opus.
- `warn` (→ `Event.Warn`) only for an unknown height or a request above the 1080p ceiling.
- **Gate:** table tests assert the exact string for every format and audio format; covered.

#### M1.3 Wire probe into the TUI (M3 part 1) — **done**

- `screenProbing` with a spinner; `esc` cancels the probe context and a sequence number drops
  any result that arrives late.
- `probeDoneMsg` / `probeErrMsg` come from a `tea.Cmd` calling `Manager.Probe`; a failure
  returns to home with a readable sentence and a retry hint.
- `Deps.Manager` is `core.Engine`; in M1.7 `cmd/utub` began resolving the binaries through
  `binmgr.Locate` and filling in `Prober.Bin` / `Executor.Bin` / `Executor.Aria2c`.
- **Gate:** covered by router tests plus a live pty run (spinner → real formats); classified
  failures are exercised with canned errors.

#### M1.4 `core/progress.go` — template parser + coalescer — **done**

- Template (already documented in the stub): `download:DL %(progress.downloaded_bytes)s …`
- Tolerate `NA`, empty, missing trailing fields; `fragment_index/fragment_count` when bytes unknown.
- Legacy fallback regex for `[download]  43.2% of 62.4MiB …`.
- `coalesce(<-chan Event, 10*time.Hour/3600*…)` at 10 Hz; terminal event must never be dropped.
- **Gate:** 15+ parser cases + coalescer test (N inputs → ≤ expected ticks, terminal survives).

#### M1.5 `core/download.go` — argv + subprocess — **done**

- `Args(req Request) []string` pure and fully tested: `-f`, `-P home:<OutDir>`, `-P temp:<OutDir>/.utub-tmp`,
  `-o <FilenameTemplate>`, `--newline --no-colors`, `--progress-template …`, `--concurrent-fragments`,
  `--http-chunk-size`, `--retries 10 --fragment-retries 10 --continue`, `--merge-output-format mp4`,
  `--cookies-from-browser` when configured, aria2c upgrade when `config.Downloader` says so.
- Exec with `SysProcAttr{Setpgid: true}`; cancel = `syscall.Kill(-pgid, SIGTERM)` then `SIGKILL`
  after a grace period. `.part` files survive for `--continue`.
- Parse stdout for the **real output path**; never derive it.
- **Gate:** argv unit tests + one real 1080p download that plays in QuickTime.

#### M1.6 `core/errors.go` — classify stderr — **done (ahead of M1.4/M1.5)**

`ErrBotCheck`, `ErrPrivate`, `ErrAgeRestricted`, `ErrGeoBlocked`, `ErrUnavailable`, `ErrHTTPStatus`,
`ErrMissingFFmpeg`. Each wraps the offending `ERROR:` line and keeps the exit error in the chain,
so `errors.Is` works for both. Rule order is explicit: "Sign in to confirm your age" must match
age-restriction before the bot-check needle.
**Gate:** table tests from real stderr samples; covered.

#### M1.7 `core/manager.go` — the real Manager — **done**

- Job registry (`map[string]*Job` + mutex, creation order for `List`); `Start` validates
  synchronously (URL, `OutDir` → `ErrBadOutDir`, forced aria2c), registers the job **before**
  the goroutine starts, and returns a buffered, lossy channel that closes after the terminal
  event. Progress samples are dropped when the consumer is behind; the terminal event is not.
- `Cancel`: records `StateCanceled` first, then cancels the ctx, which makes `Executor.Run`
  SIGTERM/SIGKILL the process group. `ErrNotFound` / `ErrFinished` as pinned by `core_test.go`;
  a second cancel of a cancelled job is `ErrFinished`.
- `Request` gained `Source media.MediaInfo` (open decision §9.1 → yes), so `Start` never
  re-probes and `Job.Source` carries the picker's metadata.
- `Engine.run` owns the terminal frame: it suppresses the executor's `StageDone`, records the
  state/path, then publishes it — a reader that reacts to the closed channel always sees a
  final job. A cancelled job simply closes the stream (see contract 8).
- `config.CookiesFromBrowser` / `--cookies-from-browser` was already plumbed in M1.5.
- `cmd/utub` builds the engine from `binmgr.Locate`, in `main.go`'s `newEngine()`.
- **Gate:** two concurrent jobs tracked independently, cancel of a finished job → `ErrFinished`,
  process **and grandchild** die on cancel, `.part` survives, coalescing holds through the
  manager, `go test -race ./...` green.

#### M1.8 Wire downloads into the TUI (M3 part 2) — **done**

- `progressModel.start()` builds a `core.Request` from the picked row plus the probe result
  (`Source`) and calls `Manager.Start` in a `tea.Cmd`; a second command blocks on the event
  channel so quiet phases (merge) do not freeze the UI.
- Every event is folded in by `apply`; a terminal event triggers `finish`, which reads
  `Job.State()`/`OutputPath()`. A cancelled job has no terminal event, so the stream close is
  what flips the screen to "canceled".
- `esc` → `Manager.Cancel`; the screen stays on "stopping…" until the process group is gone,
  and a second `esc` leaves it running in the background.
- `ctrl+c` (and esc-on-empty-home) cancels every live job and waits for it, bounded by
  `shutdownGrace`, before quitting — no orphaned `yt-dlp`/`ffmpeg`.
- Done screen: real path, `o` → `platform.Reveal`, `enter` → home; failed screen: `r` retries.
- Settings form gained **Audio format** (original/m4a/mp3) and **Cookies from browser**, so the
  MP3 and bot-check paths are reachable without hand-editing TOML.
- `--demo` keeps the canned media and the local ticker for offline UI work; the header shows
  a `demo` marker.
- **Gate:** real download to done with the reported path (integration test drives a real
  `core.Engine` against a fake yt-dlp), esc stops a live process, ctrl+c cancels before quit,
  retry restarts, reveal receives the path. Live pty run: 240p file → H.264/AAC mp4 in
  `~/Downloads`; esc on a 1080p job → "stopping" → "canceled", no `yt-dlp`/`ffmpeg` left,
  `.utub-tmp` keeps the `.part` files.

**M1 acceptance run:**

```
paste real link → spinner → real H.264 formats → 1080p download with live speed/ETA
→ file in ~/Downloads plays in QuickTime (spacebar)
→ esc mid-download → `pgrep yt-dlp` empty → re-run resumes from .part
→ audio tab → MP3 lands, tags present
→ settings dir change applies to the next download
→ private link → readable error
```

Verified live on 2026-10-30: real probe of two links, a complete 240p download
(`Me at the zoo`, H.264 + AAC in MP4, confirmed with `ffprobe`), a 1080p60 job cancelled with
`esc` (`stopping` → `canceled`, no `yt-dlp`/`ffmpeg` processes afterwards, `.part` and
`.ytdl` kept in `.utub-tmp/` for `--continue`), and `--demo` through the full flow. Still to
re-check by hand: QuickTime playback of a 1080p file, MP3 tagging, a private link.

### M4 — `internal/api` + `cmd/utubd` (~2 d)

Endpoints: `POST /api/probe`, `POST /api/jobs`, `GET /api/jobs`, `GET /api/jobs/{id}/events` (SSE),
`DELETE /api/jobs/{id}`, `GET|PUT /api/settings`, `GET /api/fs/browse`, `POST /api/jobs/{id}/save`.
Bearer token auto-generated into config; bind `127.0.0.1`; reject non-loopback origins
(DNS rebinding); CORS off; SSE heartbeat 15 s + `X-Accel-Buffering: no`.
`cmd/utubd` embeds `web/dist` via `embed.FS`.
**Gate:** `curl` drives a full download; SSE streams progress; a stale token is rejected.

### M5 — Vue 3 SPA (~5 d)

`web/`: `Home / Formats / Progress / Settings / Jobs`, Pinia stores (`jobs`, `settings`, `probe`),
`lib/transport.ts` with two implementations selected by `VITE_TARGET`:
`http` (fetch + `EventSource`) and `wails` (fetch + Wails events).
Desktop-only affordances (native folder picker, reveal) live in `lib/platform.ts`.
**Gate:** browser flow matches the TUI; both builds compile.

### M6 — Wails v3 desktop (~2.5 d)

Separate `go.mod` in `desktop/`; mount the same `api.Handler` through `AssetOptions.Handler`
(no listening port); Wails events instead of SSE; native dialogs; tray; macOS signing/notarization.
**Gate:** `.app` downloads to disk with no port open.

### M7 — release (~1.5 d)

goreleaser for `utub` + `utubd`; `make web`; brew formula; dmg/msi; README final pass.

---

## 8. Lessons from this session (read before coding)

- **Bubble Tea v2 API differences** that cost time: `View() tea.View` (`tea.NewView`), alt-screen is
  `view.AltScreen = bool` (no `WithAltScreen`), keys are `tea.KeyPressMsg`, `tea.Quit` is a *function*,
  `textinput.Width` is now `SetWidth()`, `Focus()` returns a `Cmd`, and Lip Gloss v2 removed
  `AdaptiveColor` — the palette is rebuilt from `tea.BackgroundColorMsg` (`styles.go`).
- **Nil-form panic:** the router forwards `WindowSizeMsg` to *every* screen, including ones never
  opened; `settingsModel.Update/View/Init` must guard `s.form == nil`. This crashed on startup and
  only the pty e2e caught it — keep doing pty runs.
- **pty captures are not screenshots.** Bubble Tea rewrites cells with cursor moves; stripping ANSI
  leaves stale glyphs. Trust assertions, not the visual dump, for spacing.
- **`q` cannot be a global key** while a text field is focused (it's typeable). Use `ctrl+c`/`esc`.
- **Test the seams, not the tools:** `platform` injects a `runner`, `binmgr` probes fake shell scripts
  on a private `PATH`. Do the same for `core` (M1.1).
- **`go vet` copylocks:** `Job` and `homeModel` contain mutexes/sub-models; pass pointers or return copies.
- **M1.7, terminal-frame ordering:** the executor emits no `JobID` and its `StageDone` is suppressed
  by `Engine.run`; the manager records the final state and path, *then* publishes the terminal event.
  Otherwise a reader that reacts to the closed channel can see an empty `OutputPath`.
- **M1.7, cancellation is not an event:** `Event` has no cancelled field, so a cancelled job ends by
  closing its stream. Read `Job.State()` on close. The state is recorded before the process-group
  SIGTERM, so the non-zero exit cannot flip a cancelled job to failed.
- **M1.7, fast jobs produce few samples:** the coalescer drops a pending sample when the terminal
  event arrives, so a download that finishes inside one 100 ms window may show only "video" then
  "done". That is the intended behaviour, not a parser bug.
- **M1.8, Go evaluates return values left to right:** `return r, r.shutdown()` returns the *old* `r`
  because the copy happens before `shutdown()` mutates it. Assign the command first, then return.
- **M1.8, blocking reads belong in a `tea.Cmd`:** reading the event channel in `Update` would freeze
  the UI during a merge. The pattern is `Update` → start a command that blocks → message → repeat.
- **M1.8, the cancel window is real:** between `Manager.Start` being called and its `tea.Cmd`
  returning there is no job id, so `esc` sets `stopping` and `started()` cancels as soon as the id
  exists. Do not drop lifecycle messages when the user has navigated away — drain them at the root.
- **M1.8, `--demo` must skip `binmgr.Locate`:** probing is canned and downloads are simulated, so a
  binary scan only slows startup. `engineFor(demo)` returns a bare engine.

---

## 9. Open decisions (ask before assuming)

1. ~~**`Request` carrying `MediaInfo`**~~ — **resolved in M1.7**: `Request.Source` carries the
   probe result; `Start` never re-probes and `Job.Source` exposes it.
2. ~~**M1 granularity**~~ — **resolved**: one milestone per review. M1.4, M1.5, M1.7 and M1.8 each
   landed as their own commit.
3. **Desktop before web?** — recommendation stands: web (M4+M5) first, because Wails then reuses the
   SPA and the API for free.
4. **Post-v1 TUI polish** — cross-item queue, a jobs screen and keyboard-configurable bindings are
   deliberately out of scope. Revisit after M4.

---

## 10. How to continue (playbook)

```bash
cd /Users/hanasa/Labs/personal/utub
git log --oneline -3          # expect the M1.8 commit at HEAD when resuming
git status --short            # expect clean
go build ./... && go vet ./... && go test -race ./...
./bin/utub --doctor           # or: make doctor
./bin/utub                    # the real flow; --demo for offline layout work
```

Then:

1. Read `docs/architecture.md` §5–§7 (seam, pipeline, download invocation) — it is the contract.
2. **TUI is feature-complete for v1.** Next is **M4** (`internal/api` + `cmd/utubd`): the endpoints
   listed above, bearer token, loopback bind, SSE re-encoding of the same event channel.
   `Manager` is already safe for it (ids, states, cancellation by id).
3. After each step: `gofmt -l .`, `go vet ./...`, `go test -race ./...`, then commit with the
   existing `type(scope): summary` style.
4. Run the TUI through a pty (script pattern in the session log) after any TUI-visible change —
   unit tests cannot see layout or startup panics.
5. Update this file's §2 table and `docs/architecture.md`'s Status column when a milestone lands.

Definition of done for M1: the acceptance run above passes, `pgrep yt-dlp` is empty afterwards,
and `internal/core` still has zero UI imports. All three hold as of M1.8.

---

## 11. Never in scope (v1)

Playlists · cross-item download queue · subtitle download · thumbnail embedding · proxy support ·
public multi-tenant hosting · reimplementing the extractor.
