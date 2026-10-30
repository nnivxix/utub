# utub — web (self-hosted)

**Status: not started.** Planned at milestones **M4** (`internal/api` + `utubd`) and **M5**
(Vue SPA). Naming, endpoints and the transport split below are the agreed spec; see
`../docs/architecture.md` for the engine.

---

## What it is

A single-user, self-hosted web UI for the same engine the TUI uses. You run `utubd` on a
machine you control (your Mac, a NAS, a VPS), open it in a browser, paste a link, pick a
format, watch it download.

It is **not** a public service. There are no accounts, no per-user quotas and no multi-tenancy.

## Where the files land — read this first

The web version downloads to **the server's disk**, not yours. That is the defining
difference from the TUI and the desktop app:

| Mode | Files land on | Status |
|---|---|---|
| Self-hosted / LAN | server disk | **this is what we build** |
| Public multi-user SaaS | server disk | out of scope — ToS, storage cost, abuse |
| "Save into my browser's folder" | your disk | optional progressive enhancement, Chrome/Edge only |

Two ways to get a finished file onto your own machine:

1. **Server-side (default):** the file stays in `download_dir` on the server; share the folder
   over the network (SMB/NFS/Syncthing) or pull it with `scp`/`rsync`.
2. **Browser download:** `POST /api/jobs/{id}/save` streams the finished file with
   `Content-Disposition: attachment`. Simple, works everywhere, but costs a second transfer
   over the wire and cannot resume.
3. **Progressive enhancement (M5, optional):** the File System Access API
   (`showDirectoryPicker()` + streamed response) writes straight into a directory you pick,
   with real progress. Chrome/Edge only; Safari/Firefox fall back to #2.

## Stack

| Concern | Choice |
|---|---|
| Framework | Vue 3.5, `<script setup>`, TypeScript |
| Build | Vite, two modes (`web`, `desktop`) |
| Styling | Tailwind CSS v4 (`@tailwindcss/vite`) |
| State | Pinia |
| Headless components | `reka-ui`, only where a dialog/popover is genuinely needed |
| Bundled size target | ≤ 80 KB gzipped |

No component framework: the format table needs custom keyboard navigation anyway, and
hand-rolled keeps the bundle small.

## One SPA, two builds

The desktop app reuses this exact SPA. The only thing that differs is how the app receives
progress events, because SSE does not stream reliably through Wails' custom asset scheme.

```ts
// src/lib/transport.ts
export interface Api {
  probe(url: string): Promise<MediaInfo>
  startJob(req: JobRequest): Promise<Job>
  cancel(id: string): Promise<void>
  watch(id: string, fn: (e: Event) => void): () => void
  getSettings(): Promise<Config>
  putSettings(c: Config): Promise<Config>
}

export const api: Api =
  import.meta.env.VITE_TARGET === 'desktop' ? wailsTransport : httpTransport
```

| Build | `VITE_TARGET` | Served by | Events |
|---|---|---|---|
| `vite build --mode web` | `web` | `utubd` on `:8080`, token-gated | `EventSource` (SSE) |
| `vite build --mode desktop` | `desktop` | Wails asset handler, no listening port | Wails events (`Events.On('job:<id>')`) |

Normal API calls use `fetch('/api/...')` in both builds — same-origin in the browser,
custom-scheme origin inside the webview. The Wails runtime is tree-shaken out of the web
build entirely.

## HTTP API

Mounted at `/api` by `internal/api`. Versioned implicitly; additive changes only.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/probe` | `{url}` → `MediaInfo` (metadata + H.264-only format list) |
| `POST` | `/api/jobs` | `{url, formatId, kind, outDir}` → `Job` |
| `GET` | `/api/jobs` | list jobs (all states) |
| `GET` | `/api/jobs/{id}/events` | **SSE** progress stream |
| `DELETE` | `/api/jobs/{id}` | cancel (children killed, `.part` kept for resume) |
| `POST` | `/api/jobs/{id}/save` | stream finished file to the browser |
| `POST` | `/api/jobs/{id}/reveal` | open in file manager (desktop only, 501 on web) |
| `GET`/`PUT` | `/api/settings` | read / update `config.toml` |
| `GET` | `/api/fs/browse?path=~` | directory listing (web has no native picker) |

SSE details: `text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, a
heartbeat comment every 15 s, and a client-side polling fallback after two missed beats
(reverse proxies sometimes buffer).

## Views

| View | Responsibility |
|---|---|
| `Home.vue` | paste field, clipboard hint, inline URL validation errors |
| `Formats.vue` | Video/Audio tabs, `FormatTable`, the `H.264 · max 1080p` note, size estimates |
| `Progress.vue` | per-stage bars (video → audio → merge → post), speed, ETA, cancel |
| `Jobs.vue` | history: active + finished, reveal/save actions |
| `Settings.vue` | download directory picker, codec, concurrency, downloader backend |

## Security

Non-negotiable:

- Bind `127.0.0.1` by default.
- Random bearer token generated on first run, stored in `config.toml`, sent as
  `Authorization: Bearer …` (or `?t=` for the initial page load).
- `Host`/`Origin` validated against the listener — defeats DNS rebinding.
- CORS off. Same-origin only.
- `--host 0.0.0.0` refuses to start unless `--token` is given explicitly, and logs a warning
  banner on boot.
- Never expose directly to the internet. If you must, put it behind a TLS reverse proxy with
  basic auth and keep the token.

## Ports

| Process | Address | Notes |
|---|---|---|
| `utubd` (server) | `127.0.0.1:8080` | production + embedded SPA; token required |
| Vite dev server | `127.0.0.1:5173` | development only, never shipped |

## Dev / build

```bash
make web            # vite build --mode web → web/dist  (embedded into utubd)
make server         # go build ./cmd/utubd
./bin/utubd --addr 127.0.0.1:8080

# dev loop (two terminals)
./bin/utubd --addr 127.0.0.1:8080 --cors-dev   # API on :8080, allows origin :5173
cd web && npm run dev                          # vite on :5173, proxy /api → :8080
```

In dev, everything except the SPA itself is still served by `:8080`; Vite proxies `/api` so
the browser sees a single origin and SSE is not blocked by a cross-origin preflight. The
`--cors-dev` flag exists only because Vite's HMR websocket and the `?t=` token handshake
occasionally need it — it is refused unless `--addr` is loopback.

## Milestones

**M4** — `internal/api`: router, JSON handlers, SSE, token + origin guard, `utubd`, `curl`-testable.
**M5** — this SPA: 5 views, `FormatTable`, Pinia stores, `http` transport, tests.
**M6** — no changes here; desktop consumes `web/dist` as-is.
