# Architecture

Status: **design doc / spec**. Written before the code (step 0). Steps 0-9 are complete —
the skeleton, config, media, platform, binmgr and every TUI screen exist; `internal/core` still
returns `ErrNotImplemented` and the picker/progress run on demo data. **M1 is next.** Later
work is checked against this document; if a step contradicts it, the step stops and this file
is amended deliberately.

Module: `github.com/nnivxix/utub` · Go 1.27 · macOS/Linux/Windows

---

## 1. Goal

One download engine, three frontends:

| Surface | Binary | Talks to the engine via |
|---|---|---|
| TUI | `utub` | in-process Go interface |
| Web (self-hosted) | `utubd` | HTTP/JSON + SSE |
| Desktop | `utub.app` (Wails v3) | the same HTTP handler, mounted in the webview |

The engine is the hard part and it is written **once**. Everything a surface adds on top is
presentation.

## 2. The engine is a sidecar wrapper

`yt-dlp` and `ffmpeg` are external binaries. Go owns:

- URL parsing and validation
- format probing, filtering and selection
- building the `yt-dlp` argv
- spawning, streaming progress, cancellation, resume
- destination path resolution, collision handling, post-download reveal

Go does **not** implement YouTube's signature/PO-token handling or DASH assembly. Those
change constantly; `yt-dlp` absorbs that churn. A pure-Go extractor was considered and
rejected: it is weeks of work that breaks whenever YouTube ships a player change.

`aria2c` is optional. When present it is used for multi-connection ranged downloads; when
absent the engine falls back to `yt-dlp`'s built-in fragment concurrency.

## 3. Layer model

```
cmd/utub   → internal/tui  ─┐
                            ├→ internal/core → yt-dlp / ffmpeg
cmd/utubd  → internal/api  ─┘         ↑
desktop/   → internal/api (handler mounted in webview, no listening port)

web/ (Vue) → JSON over internal/api (SSE for events), or Wails events in desktop mode
```

Supporting packages, used by `core` and above, never by each other's siblings:

```
internal/media     Format/MediaInfo model, URL validation, H.264 classification
internal/config    TOML load/save, defaults, download directory resolution
internal/platform  OS glue: download dir, reveal-in-file-manager, clipboard
internal/binmgr    locating yt-dlp/ffmpeg/aria2c, version gate
```

## 4. Layer rules

1. **`internal/core` imports no UI.** Not `bubbletea`, not `net/http`, not `wails`.
   Enforced in CI:
   `go list -deps ./internal/core | grep -E 'bubbletea|net/http|wails'` must be empty.
2. **Everything under `internal/` is private.** Compiler-enforced: importable only from
   `github.com/nnivxix/utub/...`. No public API surface, no semver promises yet.
3. **`desktop/` has its own `go.mod`.** Wails, GTK and WebKit must never be pulled in by
   `go install github.com/nnivxix/utub/cmd/utub@latest`.
4. **`web/dist` is generated, never committed.** Both `utubd` and the desktop shell consume
   the built assets; `make web` produces them.

## 5. The seam: one event stream

The engine exposes a job manager whose progress is a channel, not a callback and not a poll:

```go
type Stage int

const (
    StageProbe    Stage = iota // resolving formats
    StageVideo                 // downloading video stream
    StageAudio                 // downloading audio stream
    StageMerge                 // ffmpeg mux
    StagePost                  // convert / tag
    StageDone
)

type Event struct {
    JobID   string
    Stage   Stage
    Percent float64       // 0..1 for the current stage; -1 when unknown
    Bytes   int64
    Total   int64         // 0 when unknown
    Speed   float64       // bytes/sec
    ETA     time.Duration
    Warn    string        // e.g. "no H.264 source above 720p"
    Err     error         // terminal for this job
}

type Manager interface {
    Probe(ctx context.Context, rawURL string) (media.MediaInfo, error)
    Start(req Request) (*Job, <-chan Event, error)
    Cancel(id string) error
    List() []*Job
}
```

Each surface is a thin consumer of that channel:

| Consumer | Mechanism |
|---|---|
| TUI | goroutine reads channel → `tea.Msg` |
| Web | channel → SSE frames on `GET /api/jobs/{id}/events` |
| Desktop | channel → Wails event emit (`job:<id>`) |

Progress ticks are coalesced to ~10 Hz before leaving the engine, so a fast download cannot
flood a terminal or a websocket.

**There is exactly one progress parser and one state machine.** Surfaces never compute
progress themselves.

## 6. Pipeline

```
            ┌─────────┐   ┌──────────┐   ┌──────────┐   ┌────────┐
  URL ────► │ validate│──►│  probe   │──►│  select  │──►│download│───► file
            │ (media) │   │ (yt-dlp) │   │ (media)  │   │(yt-dlp)│
            └─────────┘   └──────────┘   └──────────┘   └────────┘
                              -J JSON       -f expr      -N 16
```

- **validate** — host allowlist (`youtube.com`, `www.`, `m.`, `music.`, `youtu.be`), extract
  the 11-char ID, normalize `music.youtube.com` → `Source = YouTubeMusic`, strip `list=`
  with a visible warning (playlists are out of scope for v1).
- **probe** — `yt-dlp -J --no-playlist --no-warnings --no-progress <url>` → `MediaInfo`.
- **select** — chosen `Format` → a `-f` expression with an explicit fallback chain.
- **download** — see below.

## 7. Download invocation

```
yt-dlp
  -f <selector>
  -P "home:<downloadDir>" -P "temp:<downloadDir>/.utub-tmp"
  -o "%(title)s [%(id)s].%(ext)s"
  --newline --no-colors
  --progress-template "download:DL %(progress.downloaded_bytes)s %(progress.total_bytes)s \
    %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s \
    %(progress.fragment_index)s %(progress.fragment_count)s"
  --concurrent-fragments 16
  --http-chunk-size 10M
  --retries 10 --fragment-retries 10 --continue
  --merge-output-format mp4
  <url>
```

Parallelism, stated precisely because "chunked parallel" means two different things:

- Every YouTube format at 1080p and above is **DASH/fragmented** → `-N 16` fetches 16
  fragments concurrently. This is the main parallelism win and needs no custom code.
- Progressive/legacy formats have no fragments → `--http-chunk-size` issues ranged requests.
- If `aria2c` is on `PATH`, upgrade to
  `--downloader aria2c --downloader-args "aria2c:-x16 -s16 -k1M -j16"` for true
  multi-connection ranges on a single file. Auto-detected, auto-fallback.

Cancellation: `context.Cancel` plus a kill of the child's process group, so `ffmpeg`/`aria2c`
children die too. `.part` files survive for `--continue`.

## 8. H.264-only decision

The video format list contains **only** `avc1` streams, paired with `mp4a` audio and
`--merge-output-format mp4`. Rationale: H.264 + AAC in MP4 is the only combination that
plays and imports everywhere (QuickTime/Finder preview, iOS, browsers, Premiere, Resolve).

The constraint has a real cost that the UI must state plainly:

> YouTube only serves `avc1` up to **1080p**. 1440p/2160p are VP9/AV1 only.

Therefore the picker prints `H.264 · max 1080p (higher resolutions are VP9/AV1 only)` and
never renders a resolution it cannot honour.

Fallback chain, mandatory because individual itags are sometimes withheld:

```
<id>+ba[ext=m4a]                     the picked itag, so the fps choice sticks
/bv*[vcodec^=avc1][height=N]+ba[ext=m4a]
/bv*[vcodec^=avc1][height<=N]+ba[ext=m4a]
/b[vcodec^=avc1][height<=N]
/bv*[vcodec^=avc1]+ba
```

The last alternative may yield a non-MP4 result. Selection is pure and cannot know which
alternative yt-dlp will actually use, so it warns when the request itself cannot be honoured
(unknown height, or above the 1080p ceiling) via `Event.Warn`; the download stage adds the
runtime check once yt-dlp reports the streams it picked.

Reversible only by changing the code: H.264 is the single video policy in v1.

## 9. Configuration

`os.UserConfigDir()/utub/config.toml` (macOS: `~/Library/Application Support/utub/`).

```toml
download_dir         = "/Users/hanasa/Downloads"
concurrent_fragments = 16
http_chunk_size      = "10M"
downloader           = "auto"        # auto | builtin | aria2c
filename_template    = "%(title)s [%(id)s].%(ext)s"
default_audio_format = "original"    # original | m4a | mp3
```

Download directory resolution order: `download_dir` → OS default (`~/Downloads`; Linux reads
`XDG_DOWNLOAD_DIR` from `~/.config/user-dirs.dirs`) → `~/Downloads`.

Writes are atomic (temp file + rename). The directory is validated at save time **and** again
at download time, so a deleted folder fails loudly rather than halfway through a job.

## 10. Security model (web only)

Single user, no accounts, no sessions.

- Bind `127.0.0.1` by default.
- Bearer token generated on first run, stored in config, required on every `/api` call.
- `Host`/`Origin` checked against the listener to defeat DNS rebinding.
- CORS disabled — same-origin only.
- Binding to a non-loopback address requires an explicit `--token` and prints a warning.
- The public-internet multi-tenant case is explicitly out of scope (ToS, storage cost, abuse).

Ports: `utubd` listens on `127.0.0.1:8080`; the Vite dev server runs on `127.0.0.1:5173` and
only ever proxies to it in development. `--cors-dev` (loopback only) exists for the HMR and
token handshake, and is never needed in production because the SPA is embedded and
same-origin.

## 11. Milestone map

| # | Deliverable | Status |
|---|---|---|
| 0 | docs (this file, `web/README.md`, `desktop/README.md`) | done |
| 1 | repo skeleton | done |
| 2 | `internal/config` | done |
| 3 | `internal/platform` | done |
| 4 | `internal/media` | done |
| 5 | `internal/core` (interfaces + stubs) | done |
| 6 | `internal/binmgr` | done |
| 7 | TUI foundation | done |
| 8 | TUI screens | done |
| 9 | wrap: fmt, vet, test, commit | done |
| M1 | `core` probe/select/download/progress — real behaviour | probe, select, errors done; download/progress/manager next |
| M3 | TUI complete: real data, probing state, cancel kills the process group | probing + real formats done; downloads pending M1.8 |
| M4 | `internal/api` + `utubd` | not started |
| M5 | Vue SPA | not started |
| M6 | Wails desktop | not started |
| M7 | release | not started |

## 12. Deliberately out of scope (v1)

Playlists, download queue across items, subtitles, thumbnail embedding, proxies,
public multi-user hosting.
