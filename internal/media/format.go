package media

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Kind classifies a format by which streams it carries.
type Kind int

const (
	KindUnknown Kind = iota
	KindVideoOnly
	KindAudioOnly
	KindMuxed // progressive formats: video and audio in one file
)

func (k Kind) String() string {
	switch k {
	case KindVideoOnly:
		return "video-only"
	case KindAudioOnly:
		return "audio-only"
	case KindMuxed:
		return "muxed"
	default:
		return "unknown"
	}
}

// Format is one downloadable stream as reported by yt-dlp.
type Format struct {
	ID             string  `json:"format_id"`
	Ext            string  `json:"ext"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FPS            float64 `json:"fps"`
	TBR            float64 `json:"tbr"` // total bitrate, kbit/s
	ABR            float64 `json:"abr"` // audio bitrate, kbit/s
	VBR            float64 `json:"vbr"`
	AudioChannels  int     `json:"audio_channels"`
	FileSize       int64   `json:"filesize"`        // exact, when the server reports it
	FileSizeApprox int64   `json:"filesize_approx"` // estimate, when it does not
	DynamicRange   string  `json:"dynamic_range"`
	FormatNote     string  `json:"format_note"`
	Language       string  `json:"language"`
}

// Kind reports which streams the format carries.
func (f Format) Kind() Kind {
	v, a := f.hasVideo(), f.hasAudio()
	switch {
	case v && a:
		return KindMuxed
	case v:
		return KindVideoOnly
	case a:
		return KindAudioOnly
	default:
		return KindUnknown
	}
}

func (f Format) hasVideo() bool {
	switch strings.ToLower(strings.TrimSpace(f.VCodec)) {
	case "", "none":
		return false
	}
	return true
}

func (f Format) hasAudio() bool {
	switch strings.ToLower(strings.TrimSpace(f.ACodec)) {
	case "", "none":
		return false
	}
	return true
}

// IsH264 reports whether the video stream is H.264/AVC.
//
// yt-dlp names it "avc1.640028" (codec profile string) or, from some
// extractors, plain "h264".
func (f Format) IsH264() bool {
	c := strings.ToLower(f.VCodec)
	return strings.HasPrefix(c, "avc1") || c == "h264"
}

// IsAAC reports whether the audio stream is AAC, which pairs with H.264 in MP4
// without re-encoding.
func (f Format) IsAAC() bool {
	return strings.HasPrefix(strings.ToLower(f.ACodec), "mp4a")
}

// IsHDR reports whether the stream is high dynamic range.
func (f Format) IsHDR() bool {
	return strings.Contains(strings.ToUpper(f.DynamicRange), "HDR")
}

// Label renders the human name of the format: "1080p60", "720p", "audio 128k".
func (f Format) Label() string {
	if f.Kind() == KindAudioOnly {
		if f.ABR > 0 {
			return fmt.Sprintf("audio %dk", int(math.Round(f.ABR)))
		}
		if f.Ext != "" {
			return "audio " + f.Ext
		}
		return "audio"
	}

	if f.Height > 0 {
		label := fmt.Sprintf("%dp", f.Height)
		if f.FPS >= 48 {
			label += fmt.Sprintf("%.0f", f.FPS)
		}
		if f.IsHDR() {
			label += " HDR"
		}
		return label
	}
	return f.ID
}

// EstimateSize returns the best available size for the format over dur:
// the exact filesize when yt-dlp reported one, otherwise bitrate × duration.
// Returns 0 when neither is known.
func (f Format) EstimateSize(dur time.Duration) int64 {
	if f.FileSize > 0 {
		return f.FileSize
	}
	if f.FileSizeApprox > 0 {
		return f.FileSizeApprox
	}
	rate := f.TBR
	if rate <= 0 {
		rate = f.ABR
	}
	if rate <= 0 || dur <= 0 {
		return 0
	}
	return int64(rate * 1000 / 8 * dur.Seconds())
}

// MediaInfo is everything the UI needs about one video.
type MediaInfo struct {
	ID        string
	Title     string
	Channel   string
	Uploader  string
	Duration  time.Duration
	ViewCount int64
	IsLive    bool
	Source    Source
	URL       string
	Formats   []Format
}

// VideoFormats returns the selectable video rows: highest-quality stream per
// resolution (and frame rate), sorted best first.
//
// Only H.264 is offered because VP9/AV1 do not play in QuickTime, iOS or most
// editors. Note that YouTube only serves H.264 up to 1080p, so 1440p and above
// are deliberately absent.
func (m MediaInfo) VideoFormats() []Format {
	return VideoCandidates(m.Formats)
}

// AudioFormats returns the selectable audio rows, best first.
func (m MediaInfo) AudioFormats() []Format {
	return AudioCandidates(m.Formats)
}

// Recommended returns the row the picker should pre-select: the highest
// resolution at or below 1080p, else the highest available.
func (m MediaInfo) Recommended() (Format, bool) {
	return Recommend(m.VideoFormats())
}

// MaxH264Height is the highest resolution YouTube serves as H.264.
const MaxH264Height = 1080

// VideoCandidates filters, deduplicates and sorts video formats.
func VideoCandidates(formats []Format) []Format {
	best := make(map[string]Format)

	for _, f := range formats {
		if !f.hasVideo() || f.Height <= 0 {
			continue
		}
		if !f.IsH264() {
			continue
		}

		key := fmt.Sprintf("%d@%d", f.Height, int(math.Round(f.FPS/10))*10)
		if prev, ok := best[key]; !ok || betterVideo(f, prev) {
			best[key] = f
		}
	}

	out := make([]Format, 0, len(best))
	for _, f := range best {
		out = append(out, f)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Height != out[j].Height {
			return out[i].Height > out[j].Height
		}
		if out[i].FPS != out[j].FPS {
			return out[i].FPS > out[j].FPS
		}
		return out[i].TBR > out[j].TBR
	})
	return out
}

// betterVideo decides which stream wins for one resolution bucket: a
// video-only stream beats a muxed one, then higher bitrate wins.
func betterVideo(candidate, current Format) bool {
	candVO := candidate.Kind() == KindVideoOnly
	curVO := current.Kind() == KindVideoOnly
	if candVO != curVO {
		return candVO
	}
	return candidate.TBR > current.TBR
}

// AudioCandidates keeps audio-only formats, deduplicated by container and
// bitrate, sorted best first.
func AudioCandidates(formats []Format) []Format {
	best := make(map[string]Format)

	for _, f := range formats {
		if f.Kind() != KindAudioOnly {
			continue
		}
		key := fmt.Sprintf("%s@%d", f.Ext, int(math.Round(f.ABR/16)))
		if prev, ok := best[key]; !ok || audioBetter(f, prev) {
			best[key] = f
		}
	}

	out := make([]Format, 0, len(best))
	for _, f := range best {
		out = append(out, f)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ABR != out[j].ABR {
			return out[i].ABR > out[j].ABR
		}
		return extRank(out[i].Ext) < extRank(out[j].Ext)
	})
	return out
}

// audioBetter prefers AAC (pairs with MP4 without re-encoding), then bitrate.
func audioBetter(candidate, current Format) bool {
	if candidate.IsAAC() != current.IsAAC() {
		return candidate.IsAAC()
	}
	return candidate.ABR > current.ABR
}

// extRank orders containers for display when bitrates tie.
func extRank(ext string) int {
	switch ext {
	case "m4a":
		return 0
	case "opus", "webm":
		return 1
	default:
		return 2
	}
}

// Recommend picks the row to pre-select from already-sorted video candidates.
func Recommend(video []Format) (Format, bool) {
	if len(video) == 0 {
		return Format{}, false
	}
	for _, f := range video {
		if f.Height <= MaxH264Height {
			return f, true
		}
	}
	return video[0], true
}

// HumanBytes formats a byte count: 62.4 MB, 980 KB, 1.2 GB.
// Returns "~" for unknown sizes.
func HumanBytes(n int64) string {
	if n <= 0 {
		return "~"
	}

	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(n)
	i := -1
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

// HumanDuration formats a duration as 4:12 or 1:02:05.
func HumanDuration(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	total := int(d.Round(time.Second).Seconds())
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
