package binmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionFromOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		out  string
		want string
	}{
		{"yt-dlp", "2026.10.26\n", "2026.10.26"},
		{"ffmpeg", "ffmpeg version 9.0.1 Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with Apple clang\n", "9.0.1"},
		{"ffmpeg nightly", "ffmpeg version n7.1-12-g4c3d2 Copyright (c) 2000-2025\n", "n7.1-12-g4c3d2"},
		{"aria2c", "aria2c version 1.37.0\n", "1.37.0"},
		{"trailing newline blank", "\n\n2026.10.26\n\n", "2026.10.26"},
		{"empty", "", ""},
		{"comma suffix", "ffmpeg version 6.1.1, Copyright (c)\n", "6.1.1"},
	}

	for _, tc := range cases {
		if got := versionFromOutput(tc.out); got != tc.want {
			t.Errorf("%s: versionFromOutput(%q) = %q, want %q", tc.name, tc.out, got, tc.want)
		}
	}
}

func TestAtLeast(t *testing.T) {
	t.Parallel()

	cases := []struct {
		have, want string
		ok         bool
	}{
		{"2026.10.26", "2024.01.01", true},
		{"2024.01.01", "2024.01.01", true},
		{"2023.12.31", "2024.01.01", false},
		{"2024.1", "2024.1.0", true},
		{"2024.1", "2024.1.1", false},
		{"v1.37.0", "1.36.0", true},
		{"9.0.1-tarball", "9.0.1", true},
	}

	for _, tc := range cases {
		got, err := AtLeast(tc.have, tc.want)
		if err != nil {
			t.Errorf("AtLeast(%q, %q) error = %v", tc.have, tc.want, err)
			continue
		}
		if got != tc.ok {
			t.Errorf("AtLeast(%q, %q) = %v, want %v", tc.have, tc.want, got, tc.ok)
		}
	}
}

func TestAtLeastUnparsable(t *testing.T) {
	t.Parallel()

	if _, err := AtLeast("", "2024.01.01"); err == nil {
		t.Fatal("AtLeast(\"\", ...) error = nil, want error")
	}
	if _, err := AtLeast("2024.10.26", "not-a-version"); err == nil {
		t.Fatal("AtLeast(..., \"not-a-version\") error = nil, want error")
	}
}

func TestMissingAndReady(t *testing.T) {
	t.Parallel()

	// A zero Set has no Required flags set, so build the "nothing installed"
	// case the way Locate would report it.
	empty := Set{
		YTDLP:  Tool{Name: YTDLP, Required: true, Err: ErrNotFound},
		FFmpeg: Tool{Name: FFmpeg, Required: true, Err: ErrNotFound},
		Aria2c: Tool{Name: Aria2c, Required: false, Err: ErrNotFound},
	}
	if got := empty.Missing(); len(got) != 2 {
		t.Fatalf("Missing() = %d tools, want 2 (yt-dlp, ffmpeg)", len(got))
	}
	if err := empty.Ready(); !errors.Is(err, ErrMissingRequired) {
		t.Fatalf("Ready() error = %v, want ErrMissingRequired", err)
	} else if !strings.Contains(err.Error(), YTDLP) || !strings.Contains(err.Error(), FFmpeg) {
		t.Fatalf("Ready() error = %q, want it to name both binaries", err)
	}

	ok := Set{
		YTDLP:  Tool{Name: YTDLP, Path: "/usr/local/bin/yt-dlp", Version: "2026.10.26", Required: true},
		FFmpeg: Tool{Name: FFmpeg, Path: "/usr/local/bin/ffmpeg", Version: "9.0.1", Required: true},
		Aria2c: Tool{Name: Aria2c, Path: "", Required: false},
	}
	if got := ok.Missing(); len(got) != 0 {
		t.Fatalf("Missing() = %v, want none", got)
	}
	if err := ok.Ready(); err != nil {
		t.Fatalf("Ready() = %v, want nil: aria2c is optional", err)
	}
}

// TestLookupFindsFakeBinary puts a shell script on a private PATH so the probe
// path is exercised without depending on what the machine has installed.
func TestLookupFindsFakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}

	dir := t.TempDir()
	writeFake(t, dir, YTDLP, "2026.10.26")
	t.Setenv("PATH", dir)

	tool := Lookup(context.Background(), YTDLP, true)
	if !tool.Available() {
		t.Fatalf("Lookup(%s) = %+v, want available", YTDLP, tool)
	}
	if tool.Version != "2026.10.26" {
		t.Fatalf("Version = %q, want 2026.10.26", tool.Version)
	}
	if tool.Path != filepath.Join(dir, YTDLP) {
		t.Fatalf("Path = %q, want %q", tool.Path, filepath.Join(dir, YTDLP))
	}
	if len(tool.Hints) == 0 {
		t.Fatal("Hints is empty, want install commands")
	}
}

func TestLookupMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tool := Lookup(context.Background(), YTDLP, true)
	if tool.Available() {
		t.Fatalf("Lookup() = %+v, want unusable", tool)
	}
	if !errors.Is(tool.Err, ErrNotFound) {
		t.Fatalf("Err = %v, want ErrNotFound", tool.Err)
	}
}

func TestLocateGatesOldYTDLP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}

	dir := t.TempDir()
	writeFake(t, dir, YTDLP, "2023.01.01")
	writeFake(t, dir, FFmpeg, "ffmpeg version 9.0.1 Copyright (c) 2000-2026")
	t.Setenv("PATH", dir)

	set := Locate(context.Background())
	if set.FFmpeg.Available() != true {
		t.Fatalf("ffmpeg = %+v, want available", set.FFmpeg)
	}
	if !errors.Is(set.YTDLP.Err, ErrTooOld) {
		t.Fatalf("yt-dlp Err = %v, want ErrTooOld", set.YTDLP.Err)
	}
	if err := set.Ready(); !errors.Is(err, ErrMissingRequired) {
		t.Fatalf("Ready() error = %v, want ErrMissingRequired", err)
	}
	if set.Aria2c.Available() {
		t.Fatalf("aria2c = %+v, want unavailable", set.Aria2c)
	}
}

func TestLocateAcceptsCurrentYTDLP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}

	dir := t.TempDir()
	writeFake(t, dir, YTDLP, "2026.10.26")
	writeFake(t, dir, FFmpeg, "ffmpeg version 9.0.1")
	writeFake(t, dir, Aria2c, "aria2c version 1.37.0")
	t.Setenv("PATH", dir)

	set := Locate(context.Background())
	if err := set.Ready(); err != nil {
		t.Fatalf("Ready() = %v, want nil", err)
	}
	if !set.Aria2c.Available() {
		t.Fatal("aria2c should be reported available when present")
	}
}

// writeFake drops an executable shell script that prints output.
func writeFake(t *testing.T, dir, name, output string) {
	t.Helper()

	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q\n", output)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}
