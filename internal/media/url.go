// Package media models what a YouTube link points at and what can be
// downloaded from it.
//
// It contains no I/O: parsing links, classifying formats and estimating sizes
// are pure functions, so they are cheap to test and usable by every surface.
package media

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Source is the site a link belongs to. It only affects defaults: YouTube
// Music links open on the Audio tab and use a music.youtube.com canonical URL.
type Source int

const (
	SourceUnknown Source = iota
	SourceYouTube
	SourceYouTubeMusic
)

func (s Source) String() string {
	switch s {
	case SourceYouTube:
		return "youtube"
	case SourceYouTubeMusic:
		return "youtube-music"
	default:
		return "unknown"
	}
}

// Errors returned by ParseURL. Use errors.Is to distinguish them.
var (
	ErrEmptyURL       = errors.New("media: empty link")
	ErrInvalidURL     = errors.New("media: not a valid URL")
	ErrUnsupportedURL = errors.New("media: not a YouTube link")
	ErrNoVideoID      = errors.New("media: link has no video ID")
	ErrPlaylistOnly   = errors.New("media: playlist links are not supported")
)

var videoIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Ref is a validated YouTube link reduced to the parts we act on.
type Ref struct {
	ID           string
	Source       Source
	URL          string // canonical, tracking parameters removed
	PlaylistID   string
	HasPlaylist  bool // v1 ignores playlists; surfaces should warn
	StartSeconds int
}

// IsMusic reports whether the link came from YouTube Music.
func (r Ref) IsMusic() bool { return r.Source == SourceYouTubeMusic }

// ParseURL validates a pasted link and returns its canonical form.
//
// It accepts the shapes people actually paste: watch pages, youtu.be short
// links, shorts, live, embed, both www./m. hosts, YouTube Music, with or
// without a scheme, and with arbitrary tracking parameters.
func ParseURL(raw string) (Ref, error) {
	raw = strings.Trim(strings.TrimSpace(raw), `<>"'`)
	if raw == "" {
		return Ref{}, ErrEmptyURL
	}

	// People paste "youtu.be/xyz" without a scheme.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return Ref{}, fmt.Errorf("%w: unsupported scheme %q", ErrInvalidURL, u.Scheme)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return Ref{}, ErrInvalidURL
	}
	host = strings.TrimPrefix(host, "www.")

	var src Source
	switch host {
	case "youtube.com", "m.youtube.com":
		src = SourceYouTube
	case "music.youtube.com":
		src = SourceYouTubeMusic
	case "youtu.be":
		src = SourceYouTube
	default:
		return Ref{}, fmt.Errorf("%w: %s", ErrUnsupportedURL, host)
	}

	id := extractID(host, u)
	if !videoIDRE.MatchString(id) {
		// A shared playlist has no video ID. Say so plainly instead of
		// reporting a generic "no video ID" that reads like a bug.
		if u.Query().Get("list") != "" || strings.Contains(strings.Trim(u.Path, "/"), "playlist") {
			return Ref{}, ErrPlaylistOnly
		}
		return Ref{}, ErrNoVideoID
	}

	ref := Ref{ID: id, Source: src}

	if list := u.Query().Get("list"); list != "" {
		ref.PlaylistID = list
		ref.HasPlaylist = true
	}
	if t := u.Query().Get("t"); t != "" {
		if sec, ok := parseTimestamp(t); ok {
			ref.StartSeconds = sec
		}
	}

	if src == SourceYouTubeMusic {
		ref.URL = "https://music.youtube.com/watch?v=" + id
	} else {
		ref.URL = "https://www.youtube.com/watch?v=" + id
	}
	if ref.StartSeconds > 0 {
		ref.URL += "&t=" + strconv.Itoa(ref.StartSeconds)
	}

	return ref, nil
}

// extractID finds the 11-character video ID in any supported URL shape.
func extractID(host string, u *url.URL) string {
	path := strings.Trim(u.Path, "/")

	if host == "youtu.be" {
		return strings.SplitN(path, "/", 2)[0]
	}

	for _, prefix := range []string{"shorts/", "live/", "embed/", "v/"} {
		if rest, ok := strings.CutPrefix(path, prefix); ok {
			return strings.SplitN(rest, "/", 2)[0]
		}
	}

	return u.Query().Get("v")
}

// urlInTextRE finds a YouTube URL inside free text: share sheets and chat apps
// wrap links in prose, and clipboard content is often the whole message.
var urlInTextRE = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.|m\.|music\.)?(?:youtube\.com|youtu\.be)/\S+`)

// ExtractFirstURL returns the first YouTube URL found in free text, with
// trailing punctuation trimmed, or "" when there is none.
//
// Handles the shapes people actually paste:
//
//	"https://youtu.be/abc?si=xyz"
//	"Cek ini: https://youtu.be/abc?si=xyz"
//	"(youtu.be/abc)."
func ExtractFirstURL(s string) string {
	m := urlInTextRE.FindString(s)
	if m == "" {
		return ""
	}
	return strings.TrimRight(m, `.,;:!?)]}"'>`)
}

// LooksLikeYouTubeURL reports whether s is something worth offering to probe.
// Used for the clipboard hint; deliberately permissive.
func LooksLikeYouTubeURL(s string) bool {
	return ExtractFirstURL(s) != ""
}

// parseTimestamp accepts "90", "1:30", "1h2m3s" and "1h30m".
func parseTimestamp(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 {
			return 0, false
		}
		return n, true
	}

	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) > 3 {
			return 0, false
		}
		total := 0
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 {
				return 0, false
			}
			total = total*60 + n
		}
		return total, true
	}

	var total, current int
	var haveDigit bool
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			current = current*10 + int(r-'0')
			haveDigit = true
		case r == 'h' || r == 'm' || r == 's':
			if !haveDigit {
				return 0, false
			}
			switch r {
			case 'h':
				total += current * 3600
			case 'm':
				total += current * 60
			case 's':
				total += current
			}
			current, haveDigit = 0, false
		default:
			return 0, false
		}
	}
	if !haveDigit {
		return total, true
	}
	return total + current, true
}
