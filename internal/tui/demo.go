package tui

import (
	"time"

	"github.com/nnivxix/utub/internal/media"
)

// TODO(M1): replace demoMedia with a real core.Manager.Probe result.
//
// The format list is deliberately realistic — including VP9 and AV1 rows that
// the H.264 policy must filter out, and a muxed 360p that must lose to the
// video-only 360p in the same resolution bucket. That way the picker is
// exercised against the same shapes yt-dlp actually reports.
func demoMedia() media.MediaInfo {
	const mb = 1 << 20

	return media.MediaInfo{
		ID:        "dQw4w9WgXcQ",
		Title:     "Demo video — probing lands in M1",
		Channel:   "utub",
		Uploader:  "utub",
		Duration:  4*time.Minute + 12*time.Second,
		ViewCount: 1_284_553,
		Source:    media.SourceYouTube,
		URL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Formats: []media.Format{
			// Refused by the H.264 policy.
			{ID: "315", Ext: "webm", VCodec: "vp9", ACodec: "none", Height: 2160, FPS: 60, TBR: 16000, FileSize: 500 * mb},
			{ID: "399", Ext: "mp4", VCodec: "av01.0.08M.08", ACodec: "none", Height: 1440, FPS: 60, TBR: 9000, FileSize: 280 * mb},

			// Offered.
			{ID: "299", Ext: "mp4", VCodec: "avc1.64002a", ACodec: "none", Height: 1080, FPS: 60, TBR: 6200, FileSize: 62 * mb},
			{ID: "137", Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, TBR: 5800, FileSize: 58 * mb},
			{ID: "136", Ext: "mp4", VCodec: "avc1.4d401f", ACodec: "none", Height: 720, FPS: 30, TBR: 3100, FileSize: 31 * mb},
			{ID: "135", Ext: "mp4", VCodec: "avc1.4d401e", ACodec: "none", Height: 480, FPS: 30, TBR: 1800, FileSize: 18 * mb},
			{ID: "134", Ext: "mp4", VCodec: "avc1.4d401e", ACodec: "none", Height: 360, FPS: 30, TBR: 1100, FileSize: 11 * mb},

			// Muxed fallback for the same 360p bucket; must lose to 134.
			{ID: "18", Ext: "mp4", VCodec: "avc1.42001E", ACodec: "mp4a.40.2", Height: 360, FPS: 30, TBR: 700, FileSize: 7 * mb},

			{ID: "140", Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128, FileSize: 3 * mb},
			{ID: "251", Ext: "webm", VCodec: "none", ACodec: "opus", ABR: 160, FileSize: 4 * mb},
		},
	}
}
