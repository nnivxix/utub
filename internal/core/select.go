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
// Audio-only requests pin the picked itag when the probe reported one, then
// fall back to ba/b, plus -x and an --audio-format derived from the row's
// target or Request.Config.DefaultAudioFormat.
//
// The codec policy is fixed: H.264 (avc1) only. There is no configuration knob
// in v1 — every video alternative carries the avc1 filter.
func Selector(req Request) (expr string, warn string) {
	if req.Format.Kind() == media.KindAudioOnly {
		return audioSelector(req), ""
	}
	return videoSelector(req)
}

// audioSelector picks the audio stream. The exact itag the user picked is
// pinned first so a row keeps its promised bitrate and container; the filter
// chain follows as the fallback. An m4a target prefers an AAC source so yt-dlp
// can remux instead of re-encoding opus to AAC; original and MP3 take whatever
// has the best bitrate.
func audioSelector(req Request) string {
	alts := make([]string, 0, 2)
	if req.Format.ID != "" {
		alts = append(alts, req.Format.ID)
	}
	if audioTarget(req) == config.AudioM4A {
		alts = append(alts, "ba[ext=m4a]/ba/b")
	} else {
		alts = append(alts, "ba/b")
	}
	return strings.Join(alts, "/")
}

// audioTarget is the container the finished file will have: the row's explicit
// target when the picker offered a conversion (an MP3 row), otherwise the
// configured default.
func audioTarget(req Request) string {
	if req.Format.TargetExt != "" {
		return req.Format.TargetExt
	}
	return req.Config.DefaultAudioFormat
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
// re-encode it, so both need -x and an explicit format. A row with its own
// target (the picker's MP3 rows) wins over the configured default.
func AudioArgs(req Request) []string {
	target := audioTarget(req)
	switch target {
	case config.AudioM4A, config.AudioMP3:
		return []string{"-x", "--audio-format", target, "--audio-quality", audioQualityBest}
	default:
		return nil
	}
}

// ThumbnailArgs returns the flags that embed the site's thumbnail as cover art.
//
// Cover art is only requested for containers that can carry it (MP3 and M4A);
// yt-dlp's post-processor rejects webm/opus, which is what an untouched
// "original" download of an Opus stream keeps. yt-dlp skips the step when the
// site reports no thumbnail, so "if available" needs no probing here.
func ThumbnailArgs(req Request) []string {
	if !req.Config.EmbedThumbnail {
		return nil
	}
	switch req.Format.OutputFor(req.Config.DefaultAudioFormat) {
	case config.AudioMP3, config.AudioM4A:
		return []string{"--embed-thumbnail"}
	default:
		return nil
	}
}
