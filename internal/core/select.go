package core

// TODO(M1): select — turn a chosen format into a yt-dlp -f expression.
//
// Contract, fixed by docs/architecture.md §8. Every selector is a chain with
// explicit fallbacks, because individual itags are sometimes withheld:
//
//	bv*[vcodec^=avc1][height=N]+ba[ext=m4a]
//	/bv*[vcodec^=avc1][height<=N]+ba[ext=m4a]
//	/b[vcodec^=avc1][height<=N]
//	/bv*[vcodec^=avc1]+ba
//
// Audio-only requests use ba/b plus -x and an --audio-format derived from
// Request.Config.DefaultAudioFormat.
//
// When the last alternative fires the job must set Event.Warn, so the UI can
// say the file may not be H.264/MP4 instead of silently producing something
// QuickTime cannot preview.
//
// The codec policy is fixed: H.264 (avc1) only. There is no configuration knob
// in v1 — every chain alternative carries the avc1 filter.
