# utub — desktop

**Status: not started.** Planned at milestone **M6**. The Go engine, the HTTP API and the
Vue SPA are all built before this directory gets any code.

---

## What it is

A native desktop shell around the same engine and the same SPA as the web version, using
**Wails v3** (stable since 26 Oct 2026, requires Go ≥ 1.26; this repo targets Go 1.27).

Wails embeds the platform webview (WKWebView on macOS, WebView2 on Windows, WebKitGTK on
Linux) instead of bundling a browser. Result: a ~15–25 MB app with no open port, no localhost
server and native OS integration.

## Why this directory has its own `go.mod`

```
github.com/nnivxix/utub/desktop     ← separate module
```

Wails, GTK and WebKit are heavy, platform-specific and irrelevant to CLI/TUI users. Keeping
them in a separate module guarantees:

```bash
go install github.com/nnivxix/utub/cmd/utub@latest
```

never downloads Wails or requires GTK headers. The desktop app depends on the core module by
path (`replace github.com/nnivxix/utub => ../`), which works locally and is replaced by a real
version pin at release time.

## How it mounts the app

No listener is started. The same `http.Handler` that `utubd` serves is handed straight to the
Wails asset server:

```go
// desktop/main.go (sketch)
app := application.New(application.Options{
    Name: "utub",
    Assets: application.AssetOptions{
        Handler: buildHandler(mgr, spaFS), // mux: /api/* → internal/api, /* → web/dist
    },
    Services: []application.Service{...},  // native dialogs, reveal
})

app.Window.NewWithOptions(application.WebviewWindowOptions{
    Title:  "utub",
    Width:  980, Height: 720,
})
```

Flow stays identical to web: paste → probe → pick format → download. Only the plumbing under
`fetch` changes origin (`wails://` on macOS, `http://wails.localhost` on Windows).

## Events: Wails events, not SSE

Streaming responses through a custom asset scheme are unreliable across platforms, so progress
is delivered as native events and the SPA's `transport.ts` switches to the Wails implementation:

```ts
// src/lib/wails.ts
export const watch = (id, fn) =>
  Events.On(`job:${id}`, (e) => fn(e.data))
```

The Pinia reducer is shared with the web build — only the event source differs.

## Native features (the reason this exists)

| Feature | Web equivalent | Desktop |
|---|---|---|
| Choose folder | `/api/fs/browse` directory listing | real `NSSavePanel` / `IFileDialog` / GTK dialog |
| Reveal result | none | `open -R` / `explorer /select` / `xdg-open` |
| Tray | none | menu bar item: recent jobs, "open folder", quit |
| Clipboard | `navigator.clipboard` (permission-gated) | native, unprompted |
| Updates | manual | Wails updater against GitHub Releases (`M7`) |

## Build and package

```bash
wails3 doctor                     # verifies toolchain + signing status
make desktop                      # cd desktop && wails3 package
wails3 dev                        # hot reload against web/ dev server
```

Artifacts: `.app` (macOS), `.exe` + `.msi` (Windows), `.AppImage`/`.deb` (Linux).

## macOS signing

For distribution beyond your own machine:

- Developer ID Application certificate + hardened runtime.
- `com.apple.security.network.client` entitlement (it shells out to `yt-dlp`/`ffmpeg`).
- Notarization via `notarytool`; staple the ticket.
- For local-only builds, ad-hoc signing is fine — expect a Gatekeeper prompt on first launch.

Note that `yt-dlp` and `ffmpeg` must be on the user's `PATH` (Homebrew, or bundled binaries
later). The app detects them at startup via `internal/binmgr` and shows an actionable error
instead of failing mid-download.

## Milestone

**M6** — Wails v3 shell, `wails` transport, native folder picker, reveal, tray, packaging and
macOS signing. The SPA needs **zero** changes beyond `transport.ts`/`platform.ts` already
having the second implementation.
