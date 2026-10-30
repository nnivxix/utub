// Package core is the download engine: it probes a URL, turns a chosen format
// into a yt-dlp invocation, runs the subprocess and streams progress as a
// channel of Event values.
//
// This package is the seam between the engine and every surface. The TUI reads
// the channel in-process, the web API re-encodes it as SSE frames, and the
// desktop shell forwards it as Wails events. None of them know that yt-dlp
// exists.
//
// # Layer rule
//
// This package imports no UI. Not bubbletea, not net/http, not wails. The
// guard from docs/architecture.md §4 is:
//
//	go list -deps ./internal/core | grep -E 'bubbletea|net/http|wails'
//
// and it must print nothing.
//
// # Status
//
// Step 5 provides the seam only — types, the Manager interface and a Noop
// implementation, so the TUI can be built against a stable contract. Real
// behaviour arrives in M1:
//
//	probe.go     yt-dlp -J  →  media.MediaInfo
//	select.go    chosen format  →  -f expression with fallback chain
//	download.go  argv build, subprocess lifecycle, process-group kill, resume
//	progress.go  --progress-template parser, coalesced to ~10 Hz
//	errors.go    stderr → typed, human-readable failures
package core
