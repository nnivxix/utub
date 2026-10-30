package core

import (
	"fmt"
	"strings"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/media"
)

// audioQualityBest asks ffmpeg for the best quality: VBR for MP3, the highest
// bitrate for AAC.
const audioQualityBest = "0"

// Selector turns the chosen row into a yt-dlp -f expression and an optional
// warning to surface as Event.Warn. It is pure: no I/O, no subprocess.
//
// Every video selector is a chain with explicit fallbacks, because individual
// itags are sometimes withheld:
//
//	bv*[vcodec^=avc1][height=N]+ba[ext=m4a]
//	/bv*[vcodec^=avc1][height<=N]+ba[ext=m4a]
//	/b[vcodec^=avc1][height<=N]
//	/bv*[vcodec^=avc1]+ba
//
// The first alternative is the exact picked itag, so choosing 1080p30 does not
// silently download the 60fps stream; the chain follows as the fallback.
//
// Audio-only requests use ba/b plus -x and an --audio-format derived from
// Request.Config.DefaultAudioFormat.
//
// The codec policy is fixed: H.264 (avc1) only. There is no configuration knob
// in v1 — every video alternative carries the avc1 filter.
func Selector(req Request) (expr string, warn string) {
	if req.Format.Kind() == media.KindAudioOnly {
		return audioSelector(req.Config), ""
	}
	return videoSelector(req)
}

// audioSelector picks the audio stream. An m4a target prefers an AAC source so
// yt-dlp can remux instead of re-encoding opus to AAC; original and MP3 take
// whatever has the best bitrate.
func audioSelector(cfg config.Config) string {
	if cfg.DefaultAudioFormat == config.AudioM4A {
		return "ba[ext=m4a]/ba/b"
	}
	return "ba/b"
}

// videoSelector builds the video chain. A request with no known height can only
// use the last alternative, which is what the warning reports to the UI.
func videoSelector(req Request) (expr string, warn string) {
	height := req.Format.Height

	// H.264 is not served above 1080p. A hand-built request for 2160 (the picker
	// never offers one) is capped rather than silently trusted, and the cap is
	// what the warning describes.
	if height > media.MaxH264Height {
		warn = fmt.Sprintf("H.264 tops out at %dp — this download will be %dp or lower",
			media.MaxH264Height, media.MaxH264Height)
		height = media.MaxH264Height
	}

	alts := make([]string, 0, 5)

	// Honour the exact row the user picked before falling back to a filter.
	// Only H.264 itags within the ceiling are pinned: anything else would
	// circumvent the policy if a hand-built request reached the engine without
	// the picker's filter.
	videoPinnable := req.Format.ID != "" &&
		req.Format.IsH264() &&
		req.Format.Height > 0 &&
		req.Format.Height <= media.MaxH264Height
	switch req.Format.Kind() {
	case media.KindVideoOnly:
		if videoPinnable {
			alts = append(alts, req.Format.ID+"+ba[ext=m4a]")
		}
	case media.KindMuxed:
		if videoPinnable {
			alts = append(alts, req.Format.ID)
		}
	}

	if height <= 0 {
		alts = append(alts, "bv*[vcodec^=avc1]+ba")
		return strings.Join(alts, "/"), "no resolution reported — downloading the best available stream"
	}

	alts = append(alts,
		fmt.Sprintf("bv*[vcodec^=avc1][height=%d]+ba[ext=m4a]", height),
		fmt.Sprintf("bv*[vcodec^=avc1][height<=%d]+ba[ext=m4a]", height),
		fmt.Sprintf("b[vcodec^=avc1][height<=%d]", height),
		"bv*[vcodec^=avc1]+ba",
	)

	return strings.Join(alts, "/"), warn
}

// AudioArgs returns the post-processing flags for an audio-only request.
//
// original keeps the source stream untouched; m4a and mp3 extract the audio and
// re-encode it, so both need -x and an explicit format.
func AudioArgs(cfg config.Config) []string {
	switch cfg.DefaultAudioFormat {
	case config.AudioM4A, config.AudioMP3:
		return []string{"-x", "--audio-format", cfg.DefaultAudioFormat, "--audio-quality", audioQualityBest}
	default:
		return nil
	}
}
