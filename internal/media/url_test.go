package media

import (
	"errors"
	"testing"
)

func TestParseURLValid(t *testing.T) {
	const id = "dQw4w9WgXcQ"

	tests := []struct {
		name string
		in   string
		want Ref
	}{
		{
			name: "watch page",
			in:   "https://www.youtube.com/watch?v=" + id,
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "no scheme",
			in:   "youtu.be/" + id,
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "short link with tracking param",
			in:   "https://youtu.be/" + id + "?si=AbCdEf123",
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "youtube music",
			in:   "https://music.youtube.com/watch?v=" + id,
			want: Ref{ID: id, Source: SourceYouTubeMusic, URL: "https://music.youtube.com/watch?v=" + id},
		},
		{
			name: "shorts",
			in:   "https://www.youtube.com/shorts/" + id,
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "mobile host",
			in:   "https://m.youtube.com/watch?v=" + id,
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "live",
			in:   "https://www.youtube.com/live/" + id,
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "embed",
			in:   "https://www.youtube.com/embed/" + id,
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "timestamp seconds",
			in:   "https://www.youtube.com/watch?v=" + id + "&t=42",
			want: Ref{ID: id, Source: SourceYouTube, StartSeconds: 42,
				URL: "https://www.youtube.com/watch?v=" + id + "&t=42"},
		},
		{
			name: "playlist flag",
			in:   "https://www.youtube.com/watch?v=" + id + "&list=PLabc123",
			want: Ref{ID: id, Source: SourceYouTube, HasPlaylist: true, PlaylistID: "PLabc123",
				URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "surrounding whitespace and angle brackets",
			in:   "   <https://youtu.be/" + id + ">   ",
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
		{
			name: "uppercase host",
			in:   "https://WWW.YouTube.com/watch?v=" + id,
			want: Ref{ID: id, Source: SourceYouTube, URL: "https://www.youtube.com/watch?v=" + id},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURL(tt.in)
			if err != nil {
				t.Fatalf("ParseURL(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseURL(%q)\n got: %+v\nwant: %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseURLInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrEmptyURL},
		{"spaces only", "   ", ErrEmptyURL},
		{"not a url", "not a url", ErrInvalidURL},
		{"wrong host", "https://vimeo.com/12345", ErrUnsupportedURL},
		{"lookalike host", "https://youtube.com.evil.test/watch?v=dQw4w9WgXcQ", ErrUnsupportedURL},
		{"no id", "https://www.youtube.com/", ErrNoVideoID},
		{"short id", "https://www.youtube.com/watch?v=short", ErrNoVideoID},
		{"long id", "https://www.youtube.com/watch?v=dQw4w9WgXcQextra", ErrNoVideoID},
		{"empty short link", "https://youtu.be/", ErrNoVideoID},
		{"channel url", "https://www.youtube.com/channel/UC1234567890", ErrNoVideoID},
		{"playlist only", "https://www.youtube.com/playlist?list=PLabc123", ErrPlaylistOnly},
		{"music playlist only", "https://music.youtube.com/playlist?list=OLAK5uy_abc", ErrPlaylistOnly},
		{"channel playlists page", "https://www.youtube.com/@somechannel/playlists", ErrPlaylistOnly},
		{"bad scheme", "ftp://www.youtube.com/watch?v=dQw4w9WgXcQ", ErrInvalidURL},
		{"javascript scheme", "javascript:alert(1)", ErrInvalidURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseURL(tt.in)
			if err == nil {
				t.Fatalf("ParseURL(%q) = nil error, want %v", tt.in, tt.want)
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("ParseURL(%q) error = %v, want errors.Is(..., %v)", tt.in, err, tt.want)
			}
		})
	}
}

func TestParseURLIsCaseSensitiveForID(t *testing.T) {
	// IDs are case-sensitive: normalising them would break the lookup.
	got, err := ParseURL("https://youtu.be/Dqw4W9wgXCq")
	if err != nil {
		t.Fatalf("ParseURL error = %v", err)
	}
	if got.ID != "Dqw4W9wgXCq" {
		t.Errorf("ID = %q, want the original casing preserved", got.ID)
	}
}

func TestParseURLKeepsSingleVideoFromPlaylistURL(t *testing.T) {
	// A watch link that also carries list= is still a single video: probe it
	// and let the UI warn about the playlist, rather than refusing outright.
	got, err := ParseURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabc123&index=4")
	if err != nil {
		t.Fatalf("ParseURL error = %v", err)
	}
	if !got.HasPlaylist {
		t.Error("HasPlaylist = false, want true so the UI can warn")
	}
	if got.ID != "dQw4w9WgXcQ" {
		t.Errorf("ID = %q, want dQw4w9WgXcQ", got.ID)
	}
}

func TestExtractFirstURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bare share link",
			in:   "https://youtu.be/yzoyG_AX8sU?si=IJ1tljYXb78DcKf1",
			want: "https://youtu.be/yzoyG_AX8sU?si=IJ1tljYXb78DcKf1",
		},
		{
			name: "share sheet prose",
			in:   "Cek ini: https://youtu.be/yzoyG_AX8sU?si=IJ1tljYXb78DcKf1",
			want: "https://youtu.be/yzoyG_AX8sU?si=IJ1tljYXb78DcKf1",
		},
		{
			name: "trailing punctuation",
			in:   "watch this https://youtu.be/abc, it is great",
			want: "https://youtu.be/abc",
		},
		{
			name: "surrounded by brackets",
			in:   "(https://youtu.be/abc).",
			want: "https://youtu.be/abc",
		},
		{
			name: "scheme plus www",
			in:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			want: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		},
		{
			name: "bare host no scheme",
			in:   "youtu.be/dQw4w9WgXcQ",
			want: "youtu.be/dQw4w9WgXcQ",
		},
		{
			name: "music link in text",
			in:   "song: music.youtube.com/watch?v=dQw4w9WgXcQ",
			want: "music.youtube.com/watch?v=dQw4w9WgXcQ",
		},
		{
			name: "first of several wins",
			in:   "a https://youtu.be/aaaaaaaaaaa b https://youtu.be/bbbbbbbbbbb",
			want: "https://youtu.be/aaaaaaaaaaa",
		},
		{
			name: "no link",
			in:   "just a message with no link",
			want: "",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "other site",
			in:   "https://vimeo.com/12345",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractFirstURL(tt.in); got != tt.want {
				t.Errorf("ExtractFirstURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractFirstURLFeedsParseURL(t *testing.T) {
	// The clipboard path: share text in, canonical ref out.
	const shared = "Cek ini: https://youtu.be/yzoyG_AX8sU?si=IJ1tljYXb78DcKf1 — keren"

	extracted := ExtractFirstURL(shared)
	if extracted == "" {
		t.Fatal("ExtractFirstURL found nothing in share text")
	}

	ref, err := ParseURL(extracted)
	if err != nil {
		t.Fatalf("ParseURL(%q) error = %v", extracted, err)
	}
	if ref.ID != "yzoyG_AX8sU" {
		t.Errorf("ID = %q, want yzoyG_AX8sU", ref.ID)
	}
	if want := "https://www.youtube.com/watch?v=yzoyG_AX8sU"; ref.URL != want {
		t.Errorf("URL = %q, want %q", ref.URL, want)
	}
}

func TestLooksLikeYouTubeURL(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"youtu.be/dQw4w9WgXcQ", true},
		{"https://music.youtube.com/watch?v=abc", true},
		{"  HTTPS://YOUTU.BE/abc  ", true},
		{"share text https://youtu.be/abc?si=x", true},
		{"https://vimeo.com/12345", false},
		{"", false},
		{"just some text", false},
		{"https://youtube.com.evil.test/watch?v=dQw4w9WgXcQ", false},
	}

	for _, tt := range tests {
		if got := LooksLikeYouTubeURL(tt.in); got != tt.want {
			t.Errorf("LooksLikeYouTubeURL(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"90", 90, true},
		{"0", 0, true},
		{"1:30", 90, true},
		{"1:02:05", 3725, true},
		{"1h2m3s", 3723, true},
		{"1h30m", 5400, true},
		{"1m", 60, true},
		{"45s", 45, true},
		{"", 0, false},
		{"abc", 0, false},
		{"1:2:3:4", 0, false},
		{"-5", 0, false},
	}

	for _, tt := range tests {
		got, ok := parseTimestamp(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseTimestamp(%q) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
