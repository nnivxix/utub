# utub

A YouTube / YouTube Music downloader with a terminal UI, written in Go.

Paste a link, pick a format, download it — chunked, in parallel, straight to `~/Downloads`.
No browser, no ads, no Electron.

```
utub dev ────────────────────────────────────────────────── ~/Downloads
Paste a YouTube or YouTube Music link

╭──────────────────────────────────────────────────────────────────╮
│ › https://youtu.be/dQw4w9WgXcQ                                   │
╰──────────────────────────────────────────────────────────────────╯
✓ youtube · dQw4w9WgXcQ

enter select · ctrl+v paste link · ctrl+u clear · ctrl+s settings · esc back
```

> **⚠️ BETA - but usabale**
>
> The interface is built (home, format picker, progress, settings) and navigation works, but it
> runs on **demo data: nothing is downloaded**. The engine — probe, download, cancel — is the
> next milestone. There is no release and no stable behaviour; expect breaking changes.
>
> Progress and the full task list live in [PLAN.md](PLAN.md); the design contract is
> [docs/architecture.md](docs/architecture.md). See also [Roadmap](#roadmap).

---

## Why another one

Two opinions shape this project:

**1. Don't reimplement the extractor.** `yt-dlp` absorbs YouTube's endless player changes;
a hand-rolled extractor breaks weekly. `utub` is a wrapper with a good UI over known-good
binaries — `yt-dlp` for extraction and `ffmpeg` for muxing.

**2. Offer only formats that actually work.** The video list is **H.264 (`avc1`) + AAC in
MP4** and nothing else. That combination plays and imports everywhere: QuickTime and Finder
preview, iOS, every browser, Premiere, Resolve. VP9/AV1 files that only VLC can open are not
offered.

The trade-off is stated in the UI rather than hidden: YouTube only serves H.264 up to
**1080p**. If you need 4K you need VP9/AV1, and this tool won't pretend otherwise.

## Features

- Paste a YouTube or YouTube Music URL — `youtube.com`, `youtu.be`, `shorts`, `music.youtube.com`
- Format list with resolution, fps, codec, container and estimated size
- Video (H.264/MP4) and Audio tabs — source streams plus an MP3 row per quality
- Cover art embedded into MP3/M4A when the site has a thumbnail
- **Parallel chunked downloads**: 16 concurrent DASH fragments, ranged HTTP chunks, and
  automatic multi-connection upgrade when `aria2c` is installed
- Live progress: per-stage bars (video → audio → merge), speed, ETA
- Cancel that actually kills the children, and resume via `.part` files
- Configurable download directory, defaulting to the OS `Downloads` folder
- Same engine under a TUI, a self-hosted web UI and a desktop app

## Requirements

| | |
|---|---|
| Go | 1.27+ (build only) |
| [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) | required — extraction |
| [`ffmpeg`](https://ffmpeg.org) | required — muxing and audio conversion |
| `aria2c` | optional — multi-connection downloads |

```bash
brew install yt-dlp ffmpeg        # macOS
sudo apt install yt-dlp ffmpeg    # Debian/Ubuntu
pipx install yt-dlp               # any platform, keeps it current
```

Keep `yt-dlp` current (`yt-dlp -U`). When YouTube changes something, that release is what
fixes it — not this repo.

## Install

```bash
go install github.com/nnivxix/utub/cmd/utub@latest
```

Or from a clone:

```bash
git clone https://github.com/nnivxix/utub
cd utub
make tui && ./bin/utub
```

There is no runtime dependency on Node, Wails or GTK for the CLI/TUI binary.

## Usage

```bash
utub                 # interactive TUI
utub --doctor        # show detected yt-dlp / ffmpeg / aria2c versions
utub --dir ~/Movies  # override the download directory for this run
```

### Keys

| Key | Action |
|---|---|
| `enter` | probe the pasted URL / confirm selection |
| `tab` | switch Video / Audio |
| `j` `k` / arrows | move in the format list |
| `d` | change destination for this download |
| `s` | settings |
| `esc` | back / cancel the running download |
| `?` | help |
| `q` | quit |

## Configuration

`os.UserConfigDir()/utub/config.toml` — on macOS,
`~/Library/Application Support/utub/config.toml`.

```toml
download_dir         = "/Users/you/Downloads"
concurrent_fragments = 16
http_chunk_size      = "10M"
downloader           = "auto"        # auto | builtin | aria2c
filename_template    = "%(title)s [%(id)s].%(ext)s"
default_audio_format = "original"    # original | m4a | mp3
embed_thumbnail      = true          # cover art in MP3/M4A when the site has a thumbnail
cookies_from_browser  = ""           # "", safari, chrome, firefox, … (for bot checks)
```

Defaults: `~/Downloads` on macOS and Windows; on Linux `XDG_DOWNLOAD_DIR` is honoured, falling
back to `~/Downloads`. The directory is validated when you save it *and* again when a download
starts.

## Roadmap

| Surface | Status | How |
|---|---|---|
| **TUI** (`utub`) | screens done, engine pending (M1) | Bubble Tea v2 |
| **Web** (`utubd`) | specified → [web/README.md](web/README.md) | self-hosted, Vue 3 SPA, `127.0.0.1:8080`, bearer token |
| **Desktop** | specified → [desktop/README.md](desktop/README.md) | Wails v3, same SPA, no open port |

The web version is **single-user and self-hosted**: files land on the server's disk, not
yours. It is not, and won't become, a public multi-user service.

## Project layout

```
cmd/utub      TUI entrypoint
cmd/utubd     web server entrypoint (embedded SPA)
internal/core     engine: probe · select · download · progress · jobs  (no UI imports)
internal/tui      Bubble Tea screens
internal/api      HTTP + SSE adapter
internal/media    format model, URL validation, H.264 classification
internal/config   TOML config
web/              Vue 3 SPA (shared by web and desktop)
desktop/          Wails v3 shell (separate go.mod)
docs/             architecture notes
PLAN.md           status, remaining milestones, resume instructions
```

The rule that keeps three frontends honest: `internal/core` imports no UI package, and every
surface consumes the same `<-chan Event`. Details in
[docs/architecture.md](docs/architecture.md).

## Contributing

Issues and PRs welcome. Two constraints to respect: keep `internal/core` UI-free, and keep the
format list H.264-only by default.

## Disclaimer

For downloading content you have the right to download — your own uploads, Creative Commons
material, or anything you have permission for. Respect YouTube's Terms of Service and the
copyright of others. This project is not affiliated with, endorsed by, or connected to YouTube
or Google.

## License

MIT
