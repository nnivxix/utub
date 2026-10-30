// Package config loads, validates and stores utub's user configuration.
//
// The file lives at os.UserConfigDir()/utub/config.toml
// (macOS: ~/Library/Application Support/utub/config.toml).
// Writes are atomic: a temp file is written next to the target and renamed.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Video codec policy.
const (
	CodecH264 = "h264" // H.264/avc1 only — plays and imports everywhere (max 1080p)
	CodecAny  = "any"  // offer every codec, including VP9/AV1
)

// Downloader backends.
const (
	DownloaderAuto    = "auto"    // use aria2c when it is on PATH, else yt-dlp builtin
	DownloaderBuiltin = "builtin" // always use yt-dlp's builtin downloader
	DownloaderAria2c  = "aria2c"  // always use aria2c, fail if unavailable
)

// Audio output formats. "original" keeps the source stream untouched.
const (
	AudioOriginal = "original"
	AudioM4A      = "m4a"
	AudioMP3      = "mp3"
)

// DefaultFilenameTemplate keeps the video title and the YouTube ID, which makes
// files self-identifying and collisions unlikely. %(ext)s is mandatory.
const DefaultFilenameTemplate = "%(title)s [%(id)s].%(ext)s"

// DefaultConcurrentFragments is the number of DASH fragments fetched in
// parallel. 16 saturates most home connections without tripping YouTube's
// per-connection throttling.
const DefaultConcurrentFragments = 16

// EnvConfigPath overrides the config file location (used by tests and `--config`).
const EnvConfigPath = "UTUB_CONFIG"

var chunkSizeRE = regexp.MustCompile(`^[0-9]+[KMG]?$`)

// Config mirrors config.toml one-to-one.
type Config struct {
	DownloadDir         string `toml:"download_dir"`
	VideoCodec          string `toml:"video_codec"`
	ConcurrentFragments int    `toml:"concurrent_fragments"`
	HTTPChunkSize       string `toml:"http_chunk_size"`
	Downloader          string `toml:"downloader"`
	FilenameTemplate    string `toml:"filename_template"`
	DefaultAudioFormat  string `toml:"default_audio_format"`
}

// Default returns the configuration used when no file exists yet.
func Default() Config {
	return Config{
		DownloadDir:         DefaultDownloadDir(),
		VideoCodec:          CodecH264,
		ConcurrentFragments: DefaultConcurrentFragments,
		HTTPChunkSize:       "10M",
		Downloader:          DownloaderAuto,
		FilenameTemplate:    DefaultFilenameTemplate,
		DefaultAudioFormat:  AudioOriginal,
	}
}

// Path returns the config file location, honouring $UTUB_CONFIG.
func Path() string {
	if p := strings.TrimSpace(os.Getenv(EnvConfigPath)); p != "" {
		return p
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		// Last resort: keep working rather than failing to start.
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "config.toml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "utub", "config.toml")
}

// Load reads Path(). A missing file is not an error: the defaults are returned.
func Load() (Config, error) { return LoadFrom(Path()) }

// LoadFrom reads path, merging it over the defaults so partial files are valid.
func LoadFrom(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes the config to Path().
func (c Config) Save() error { return c.SaveTo(Path()) }

// SaveTo validates, ensures the download directory is usable, then writes
// atomically (temp file + rename) so a crash cannot truncate the config.
func (c Config) SaveTo(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if _, err := c.EnsureDownloadDir(); err != nil {
		return err
	}

	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("config: temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("config: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: replace %s: %w", path, err)
	}
	return nil
}

// Validate checks every value without touching the filesystem.
func (c Config) Validate() error {
	switch c.VideoCodec {
	case CodecH264, CodecAny:
	default:
		return fmt.Errorf("config: video_codec must be %q or %q, got %q", CodecH264, CodecAny, c.VideoCodec)
	}

	switch c.Downloader {
	case DownloaderAuto, DownloaderBuiltin, DownloaderAria2c:
	default:
		return fmt.Errorf("config: downloader must be %q, %q or %q, got %q",
			DownloaderAuto, DownloaderBuiltin, DownloaderAria2c, c.Downloader)
	}

	switch c.DefaultAudioFormat {
	case AudioOriginal, AudioM4A, AudioMP3:
	default:
		return fmt.Errorf("config: default_audio_format must be %q, %q or %q, got %q",
			AudioOriginal, AudioM4A, AudioMP3, c.DefaultAudioFormat)
	}

	if c.ConcurrentFragments < 1 || c.ConcurrentFragments > 64 {
		return fmt.Errorf("config: concurrent_fragments must be between 1 and 64, got %d", c.ConcurrentFragments)
	}

	if !chunkSizeRE.MatchString(c.HTTPChunkSize) {
		return fmt.Errorf("config: http_chunk_size must look like 10M or 512K, got %q", c.HTTPChunkSize)
	}

	if strings.TrimSpace(c.DownloadDir) == "" {
		return errors.New("config: download_dir must not be empty")
	}

	if strings.TrimSpace(c.FilenameTemplate) == "" {
		return errors.New("config: filename_template must not be empty")
	}
	if !strings.Contains(c.FilenameTemplate, "%(ext)s") {
		return fmt.Errorf("config: filename_template must contain %%(ext)s, got %q", c.FilenameTemplate)
	}

	return nil
}

// EnsureDownloadDir expands ~, creates the download directory if needed, checks
// that it is writable, and returns its absolute path.
//
// Called both when settings are saved and when a download starts, so a folder
// deleted in between fails before any bytes are transferred.
func (c Config) EnsureDownloadDir() (string, error) {
	dir, err := ExpandHome(c.DownloadDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("config: download_dir must not be empty")
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("config: resolve download_dir %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("config: create download_dir %s: %w", abs, err)
	}

	probe, err := os.CreateTemp(abs, ".utub-write-test-*")
	if err != nil {
		return "", fmt.Errorf("config: download_dir %s is not writable: %w", abs, err)
	}
	probeName := probe.Name()
	probe.Close()
	os.Remove(probeName)

	return abs, nil
}

// ChunkSizeBytes converts http_chunk_size ("10M", "512K") to bytes.
func (c Config) ChunkSizeBytes() (int64, error) {
	return ParseChunkSize(c.HTTPChunkSize)
}

// ParseChunkSize parses a yt-dlp style size suffix: 1024, 512K, 10M, 2G.
func ParseChunkSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if !chunkSizeRE.MatchString(s) {
		return 0, fmt.Errorf("config: invalid size %q", s)
	}

	mult := int64(1)
	switch s[len(s)-1] {
	case 'K':
		mult, s = 1<<10, s[:len(s)-1]
	case 'M':
		mult, s = 1<<20, s[:len(s)-1]
	case 'G':
		mult, s = 1<<30, s[:len(s)-1]
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: invalid size %q: %w", s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("config: size must be positive, got %q", s)
	}
	return n * mult, nil
}

// ExpandHome turns a leading "~" into the user's home directory.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~"+string(os.PathSeparator)) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home for %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// DefaultDownloadDir returns the OS download folder:
// macOS and Windows have a fixed convention; Linux honours XDG_DOWNLOAD_DIR.
func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Downloads")
	case "windows":
		if profile := os.Getenv("USERPROFILE"); profile != "" {
			return filepath.Join(profile, "Downloads")
		}
		return filepath.Join(home, "Downloads")
	default:
		if dir := xdgDownloadDir(home); dir != "" {
			return dir
		}
		return filepath.Join(home, "Downloads")
	}
}

// xdgDownloadDir reads ~/.config/user-dirs.dirs and returns XDG_DOWNLOAD_DIR.
// Returns "" when the file is missing or does not define one.
func xdgDownloadDir(home string) string {
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "user-dirs.dirs")
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		path = filepath.Join(home, ".config", "user-dirs.dirs")
	}

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	return parseUserDirs(f, home)
}

// parseUserDirs extracts XDG_DOWNLOAD_DIR from the user-dirs.dirs format.
func parseUserDirs(r io.Reader, home string) string {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "XDG_DOWNLOAD_DIR" {
			continue
		}
		dir := strings.Trim(strings.TrimSpace(value), `"`)
		dir = strings.ReplaceAll(dir, "$HOME", home)
		dir = strings.ReplaceAll(dir, "${HOME}", home)
		if dir == "" {
			continue
		}
		return filepath.Clean(dir)
	}
	return ""
}
