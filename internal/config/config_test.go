package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func devConfig(t *testing.T) Config {
	t.Helper()
	cfg := Default()
	cfg.DownloadDir = t.TempDir()
	return cfg
}

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()

	if cfg.DownloadDir == "" {
		t.Fatal("Default().DownloadDir is empty")
	}
	if !filepath.IsAbs(cfg.DownloadDir) {
		t.Errorf("Default().DownloadDir = %q, want an absolute path", cfg.DownloadDir)
	}
	if cfg.VideoCodec != CodecH264 {
		t.Errorf("Default().VideoCodec = %q, want %q", cfg.VideoCodec, CodecH264)
	}
	if cfg.ConcurrentFragments != DefaultConcurrentFragments {
		t.Errorf("Default().ConcurrentFragments = %d, want %d", cfg.ConcurrentFragments, DefaultConcurrentFragments)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v, want nil", err)
	}
}

func TestLoadFromMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "config.toml")

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(%q) error = %v, want nil", path, err)
	}
	if want := Default(); got != want {
		t.Errorf("LoadFrom on missing file = %+v, want defaults %+v", got, want)
	}
}

func TestSaveToRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")

	want := devConfig(t)
	want.VideoCodec = CodecAny
	want.ConcurrentFragments = 8
	want.HTTPChunkSize = "4M"
	want.Downloader = DownloaderAria2c
	want.DefaultAudioFormat = AudioMP3

	if err := want.SaveTo(path); err != nil {
		t.Fatalf("SaveTo(%q) error = %v", path, err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(%q) error = %v", path, err)
	}
	if got != want {
		t.Errorf("round trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestSaveToLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := devConfig(t).SaveTo(path); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory contains %v, want only config.toml", names)
	}
}

func TestSaveToFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions do not apply on Windows")
	}
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := devConfig(t).SaveTo(path); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %o, want 600", perm)
	}
}

func TestLoadFromPartialFileMergesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`video_codec = "any"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom error = %v", err)
	}

	want := Default()
	want.VideoCodec = CodecAny
	if got != want {
		t.Errorf("partial file = %+v, want defaults with codec override %+v", got, want)
	}
}

func TestLoadFromMalformedTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("video_codec = \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFrom(path); err == nil {
		t.Fatal("LoadFrom on malformed TOML returned nil error")
	} else if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error = %q, want it to mention parsing", err)
	}
}

func TestLoadFromInvalidValueFailsValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`video_codec = "vp9"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Fatal("LoadFrom accepted video_codec = \"vp9\"")
	}
	if !strings.Contains(err.Error(), "video_codec") {
		t.Errorf("error = %q, want it to name video_codec", err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; "" means valid
	}{
		{"valid h264", func(*Config) {}, ""},
		{"valid any", func(c *Config) { c.VideoCodec = CodecAny }, ""},
		{"bad codec", func(c *Config) { c.VideoCodec = "vp9" }, "video_codec"},
		{"empty codec", func(c *Config) { c.VideoCodec = "" }, "video_codec"},
		{"bad downloader", func(c *Config) { c.Downloader = "curl" }, "downloader"},
		{"bad audio format", func(c *Config) { c.DefaultAudioFormat = "flac" }, "default_audio_format"},
		{"fragments zero", func(c *Config) { c.ConcurrentFragments = 0 }, "concurrent_fragments"},
		{"fragments too many", func(c *Config) { c.ConcurrentFragments = 65 }, "concurrent_fragments"},
		{"fragments max", func(c *Config) { c.ConcurrentFragments = 64 }, ""},
		{"bad chunk size", func(c *Config) { c.HTTPChunkSize = "10MB" }, "http_chunk_size"},
		{"empty download dir", func(c *Config) { c.DownloadDir = "  " }, "download_dir"},
		{"empty template", func(c *Config) { c.FilenameTemplate = "" }, "filename_template"},
		{"template without ext", func(c *Config) {
			c.FilenameTemplate = "%(title)s.%(ext)s"
			c.FilenameTemplate = "%(title)s.mp4"
		}, "filename_template"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestSaveToRejectsInvalidConfig(t *testing.T) {
	cfg := Default()
	cfg.DownloadDir = t.TempDir()
	cfg.VideoCodec = "vp9"

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := cfg.SaveTo(path); err == nil {
		t.Fatal("SaveTo accepted an invalid config")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("SaveTo wrote %s despite validation failure", path)
	}
}

func TestEnsureDownloadDirCreatesNestedPath(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "a", "b", "c")

	cfg := Default()
	cfg.DownloadDir = target

	got, err := cfg.EnsureDownloadDir()
	if err != nil {
		t.Fatalf("EnsureDownloadDir error = %v", err)
	}
	if got != target {
		t.Errorf("EnsureDownloadDir = %q, want %q", got, target)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatalf("download dir was not created: err=%v", err)
	}
}

func TestEnsureDownloadDirExpandsTilde(t *testing.T) {
	cfg := Default()
	cfg.DownloadDir = "~"

	got, err := cfg.EnsureDownloadDir()
	if err != nil {
		t.Fatalf("EnsureDownloadDir(~) error = %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != home {
		t.Errorf("EnsureDownloadDir(~) = %q, want %q", got, home)
	}
}

func TestEnsureDownloadDirUnwritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced this way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	base := t.TempDir()
	readOnly := filepath.Join(base, "readonly")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	cfg.DownloadDir = filepath.Join(readOnly, "child")

	_, err := cfg.EnsureDownloadDir()
	if err == nil {
		t.Fatal("EnsureDownloadDir succeeded inside a read-only directory")
	}
	if !strings.Contains(err.Error(), "not writable") && !strings.Contains(err.Error(), "create download_dir") {
		t.Errorf("error = %q, want a writability or creation failure", err)
	}
}

func TestPathHonoursEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom.toml")
	t.Setenv(EnvConfigPath, want)

	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathDefaultsUnderUserConfigDir(t *testing.T) {
	t.Setenv(EnvConfigPath, "")

	got := Path()
	if !strings.HasSuffix(got, filepath.Join("utub", "config.toml")) {
		t.Errorf("Path() = %q, want it to end with utub/config.toml", got)
	}
}

func TestParseChunkSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1024", 1024, false},
		{"512K", 512 << 10, false},
		{"10M", 10 << 20, false},
		{"2G", 2 << 30, false},
		{" 4M ", 4 << 20, false},
		{"", 0, true},
		{"M", 0, true},
		{"10MB", 0, true},
		{"10m", 0, true},
		{"-1M", 0, true},
		{"0", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseChunkSize(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseChunkSize(%q) = %d, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChunkSize(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseChunkSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in   string
		want string
	}{
		{"~", home},
		{"~/Movies", filepath.Join(home, "Movies")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~notahome", "~notahome"},
		{"", ""},
	}

	for _, tt := range tests {
		got, err := ExpandHome(tt.in)
		if err != nil {
			t.Fatalf("ExpandHome(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseUserDirs(t *testing.T) {
	const home = "/home/kai"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "standard",
			input: "XDG_DESKTOP_DIR=\"$HOME/Desktop\"\nXDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n",
			want:  "/home/kai/Downloads",
		},
		{
			name:  "braced home",
			input: "XDG_DOWNLOAD_DIR=\"${HOME}/Inbox\"\n",
			want:  "/home/kai/Inbox",
		},
		{
			name:  "absolute",
			input: "XDG_DOWNLOAD_DIR=\"/mnt/data/dl\"\n",
			want:  "/mnt/data/dl",
		},
		{
			name:  "commented out",
			input: "#XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n",
			want:  "",
		},
		{
			name:  "missing",
			input: "XDG_DESKTOP_DIR=\"$HOME/Desktop\"\n",
			want:  "",
		},
		{
			name:  "trailing slash cleaned",
			input: "XDG_DOWNLOAD_DIR=\"$HOME/\"\n",
			want:  "/home/kai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseUserDirs(strings.NewReader(tt.input), home); got != tt.want {
				t.Errorf("parseUserDirs() = %q, want %q", got, tt.want)
			}
		})
	}
}
