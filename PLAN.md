# utub — PLAN.md (handoff / continuity)

> **What this file is.** A single source of truth so a fresh session (human or AI) can resume
> exactly where the previous one stopped. Read §2 (status), §7 (remaining work) and §10
> (how to continue) before touching code.
>
> **Note on location.** Written while the agent was in Plan mode, which restricts writes to
> `~/.opencode/plan/`. When a build agent picks this up, copy it into the repo root as
> `PLAN.md` (`cp ~/.opencode/plan/PLAN.md ./PLAN.md`) so it travels with the code.

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

**Done: steps 0–9.** Skeleton, config, media, platform, binmgr, the `core` seam, and every
TUI screen. **The TUI works end-to-end but runs on demo data** — no bytes have ever been
downloaded by this codebase.

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
| **M1** | **engine: probe / select / download / progress / errors / manager** | **← NEXT, nothing started** |
| M3 | TUI complete (real data, probing state, real cancel) | blocked on M1 |
| M4 | `internal/api` + `cmd/utubd` | not started |
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

Scale: ~5,900 lines of Go across `internal/` + `cmd/`, 94 test functions, 7 packages green.

**What actually works today:** `--doctor`, `--version`, the full TUI navigation, real settings
persistence to `config.toml`, real URL validation (all YouTube URL shapes), H.264-only format
filtering over a realistic demo dataset, and a simulated progress screen.

**What is fake today:** `core.Probe`, `core.Start`, `core.Cancel`, format lists, byte progress.
Four `TODO(M1)` markers point at exactly these places (see §6).

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
  job.go                    Request, Job, private state machine (first terminal state wins)
  manager.go                Manager interface (Probe/Start/Cancel/List), NoopManager
  errors.go                 sentinels + TODO(M1) stderr classification table
  probe.go  select.go  download.go  progress.go     ← comment-only TODO(M1) checklists
internal/media/             Format/Kind/MediaInfo, H.264 classification, size estimates,
                            URL validation (watch/youtu.be/shorts/live/embed/music), share-text
internal/config/            Config struct ↔ config.toml, defaults, atomic save, dir resolution
internal/platform/          OS glue: downloads dir, Reveal (darwin/linux/windows), clipboard
internal/binmgr/            locate + version-probe yt-dlp/ffmpeg/aria2c, yt-dlp version gate
internal/tui/               Bubble Tea v2
  run.go root.go            program + panic-safe terminal restore; router, header/footer
  home.go                   URL field, clipboard sniff, link errors
  formats.go                Video/Audio tabs, H.264 rows, ★ best
  progress.go               stage bars, speed/ETA, apply(core.Event) already wired
  settings.go               huh v2 form → config.toml
  demo.go                   demoMedia() — the only fake data source, TODO(M1)
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
5. **The engine never guesses the output filename.** M1 must read yt-dlp's own
   `[download] Destination:` / `[Merger] Merging formats into "…"` lines.
6. **H.264 policy lives in `internal/media`**, not in the picker: `VideoFormats()`.
   The UI only renders what it is given and prints the 1080p ceiling.

### Exact TODO(M1) locations

| Marker | File | Replace with |
|---|---|---|
| demo formats | `internal/tui/demo.go` `demoMedia()` | `Manager.Probe(ref.URL)` |
| demo progress | `internal/tui/progress.go` `demoTick`/`advance` | `Manager.Start` + `<-chan Event` |
| `NoopManager` | `cmd/utub/main.go` `runTUI()` | real manager |
| core stubs | `internal/core/{probe,select,download,progress}.go` | implementations (see M1) |

---

## 7. Remaining work

### M1 — the engine (~4 days). **Everything else is blocked on this.**

Work in two reviewable halves. Do M1.1–M1.3 first (visible payoff: real data in the picker).

#### M1.1 `core/probe.go` — `yt-dlp -J` → `MediaInfo` (~0.5 d)

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

#### M1.2 `core/select.go` — format → `-f` (~0.5 d)

- `Selector(req Request) (expr string, warn string)` — pure, no I/O.
- Chain: `bv*[vcodec^=avc1][height=N]+ba[ext=m4a]` → `…[height<=N]…` → `b[vcodec^=avc1][height<=N]`
  → `bv*[vcodec^=avc1]+ba`, preceded by the exact picked itag so the fps choice is honoured.
- `AudioArgs(cfg)` for `-x --audio-format mp3|m4a --audio-quality …`; `original` = no `-x`.
- `warn` non-empty only when the final fallback is what will run → surfaces as `Event.Warn`.
- **Gate:** table test asserts the exact string for every (tab × format × codec policy × audio format).

#### M1.3 Wire probe into the TUI (M3 part 1, ~0.5 d)

- New `screenProbing` with spinner; `esc` aborts the probe ctx.
- `probeDoneMsg` / `probeErrMsg` produced by a `tea.Cmd` calling `Manager.Probe`; on error stay
  on home with a readable message (private / age-gated / geo / bot-check) and a retry hint.
- `Deps.Manager` becomes the real manager instead of `core.NoopManager{}`.
- **Gate:** real link → spinner → real formats; private video → readable error, no traceback.

#### M1.4 `core/progress.go` — template parser + coalescer (~0.5 d)

- Template (already documented in the stub): `download:DL %(progress.downloaded_bytes)s …`
- Tolerate `NA`, empty, missing trailing fields; `fragment_index/fragment_count` when bytes unknown.
- Legacy fallback regex for `[download]  43.2% of 62.4MiB …`.
- `coalesce(<-chan Event, 10*time.Hour/3600*…)` at 10 Hz; terminal event must never be dropped.
- **Gate:** 15+ parser cases + coalescer test (N inputs → ≤ expected ticks, terminal survives).

#### M1.5 `core/download.go` — argv + subprocess (~1 d)

- `Args(req Request) []string` pure and fully tested: `-f`, `-P home:<OutDir>`, `-P temp:<OutDir>/.utub-tmp`,
  `-o <FilenameTemplate>`, `--newline --no-colors`, `--progress-template …`, `--concurrent-fragments`,
  `--http-chunk-size`, `--retries 10 --fragment-retries 10 --continue`, `--merge-output-format mp4`,
  `--cookies-from-browser` when configured, aria2c upgrade when `config.Downloader` says so.
- Exec with `SysProcAttr{Setpgid: true}`; cancel = `syscall.Kill(-pgid, SIGTERM)` then `SIGKILL`
  after a grace period. `.part` files survive for `--continue`.
- Parse stdout for the **real output path**; never derive it.
- **Gate:** argv unit tests + one real 1080p download that plays in QuickTime.

#### M1.6 `core/errors.go` — classify stderr (~0.25 d)

`ErrBotCheck` (hint: set `cookies_from_browser`), `ErrPrivate`, `ErrAgeRestricted`, `ErrGeoBlocked`,
`ErrUnavailable`, `ErrHTTPStatus`, `ErrMissingFFmpeg`; wrap the offending line, preserve `*exec.ExitError`.
**Gate:** table tests from real stderr samples.

#### M1.7 `core/manager.go` — the real Manager (~0.75 d)

- Job registry (`map[string]*Job` + mutex); `Start` spawns a goroutine and returns a buffered,
  lossy channel closed after the terminal event.
- `Cancel`: ctx cancel + process-group kill → `StateCanceled`. `ErrNotFound` / `ErrFinished` semantics
  already pinned by `core_test.go`.
- `Request` gains the probed `media.MediaInfo` so `Start` never re-probes (needs a small interface change — **confirm with the user**).
- Add `config.CookiesFromBrowser` + `--cookies-from-browser` plumbing.
- **Gate:** two concurrent jobs tracked independently; cancel of a finished job → `ErrFinished`; `go test -race`.

#### M1.8 Wire downloads into the TUI (M3 part 2, ~0.5 d)

- Replace `demoTick` with a goroutine reading `<-chan core.Event` → `tea.Msg`.
- `esc` → `Manager.Cancel(jobID)`; quit-while-downloading cancels all jobs first (no orphaned children).
- Done screen: real path + `o` → `platform.Reveal`.
- Keep the demo ticker behind `--demo` (offline UI work).

**M1 acceptance run (do this before declaring done):**

```
paste real link → spinner → real H.264 formats → 1080p download with live speed/ETA
→ file in ~/Downloads plays in QuickTime (spacebar)
→ esc mid-download → `pgrep yt-dlp` empty → re-run resumes from .part
→ audio tab → MP3 lands, tags present
→ settings dir change applies to the next download
→ private link → readable error
```

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

---

## 9. Open decisions (ask before assuming)

1. **`Request` carrying `MediaInfo`** (M1.7) — a deliberate interface change; get a yes.
2. **M1 granularity** — the user prefers one step per approval. Confirm whether to land M1.1–M1.3
   as one review, or each separately.
3. **Desktop before web?** — recommendation stands: web (M4+M5) first, because Wails then reuses the
   SPA and the API for free.

---

## 10. How to continue (playbook)

```bash
cd /Users/hanasa/Labs/personal/utub
git log --oneline -3          # expect 4563b61 at HEAD when resuming
git status --short            # expect clean
go build ./... && go vet ./... && go test ./...
./bin/utub --doctor           # or: make doctor
```

Then:

1. Read `docs/architecture.md` §5–§7 (seam, pipeline, download invocation) — it is the contract.
2. Start **M1.1** (`internal/core/probe.go`). Do not touch `internal/tui` until M1.3.
3. After each M1.x: `gofmt -l .`, `go vet ./...`, `go test ./...`, then commit with the existing
   `type(scope): summary` style.
4. Run the TUI through a pty (script pattern in the session log) after any TUI-visible change —
   unit tests cannot see layout or startup panics.
5. Update this file's §2 table and `docs/architecture.md`'s Status column when a milestone lands.

Definition of done for M1: the acceptance run above passes, `pgrep yt-dlp` is empty afterwards,
and `internal/core` still has zero UI imports.

---

## 11. Never in scope (v1)

Playlists · cross-item download queue · subtitle download · thumbnail embedding · proxy support ·
public multi-tenant hosting · reimplementing the extractor.
