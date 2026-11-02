package main

import (
	"runtime/debug"
	"testing"
)

// info builds a BuildInfo with a module version and optional VCS settings.
func info(moduleVersion string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{
		Main:     debug.Module{Version: moduleVersion},
		Settings: settings,
	}
}

func TestResolveVersion(t *testing.T) {
	const fullRev = "d170ceb1234567890abcdef"

	vcs := func(revision, modified string) []debug.BuildSetting {
		settings := []debug.BuildSetting{}
		if revision != "" {
			settings = append(settings, debug.BuildSetting{Key: "vcs.revision", Value: revision})
		}
		if modified != "" {
			settings = append(settings, debug.BuildSetting{Key: "vcs.modified", Value: modified})
		}
		return settings
	}

	tests := []struct {
		name    string
		stamped string
		bi      *debug.BuildInfo
		want    string
	}{
		{
			name:    "ldflags stamp wins",
			stamped: "0.1.0",
			bi:      info("v9.9.9"),
			want:    "0.1.0",
		},
		{
			name:    "stamp loses the v prefix",
			stamped: "v0.1.0",
			bi:      nil,
			want:    "0.1.0",
		},
		{
			name:    "prerelease stamp is kept",
			stamped: "0.1.0-beta.1",
			bi:      nil,
			want:    "0.1.0-beta.1",
		},
		{
			name:    "stamp whitespace is trimmed",
			stamped: " 0.1.0 ",
			bi:      nil,
			want:    "0.1.0",
		},
		{
			name:    "go install falls back to the module version",
			stamped: devVersion,
			bi:      info("v0.2.3"),
			want:    "0.2.3",
		},
		{
			name:    "local build uses the clean revision",
			stamped: devVersion,
			bi:      info("(devel)", vcs(fullRev, "false")...),
			want:    "dev-d170ceb",
		},
		{
			name:    "local build marks a dirty tree",
			stamped: devVersion,
			bi:      info("(devel)", vcs(fullRev, "true")...),
			want:    "dev-d170ceb-dirty",
		},
		{
			name:    "local build shortens the stamped pseudo-version",
			stamped: devVersion,
			bi:      info("v0.0.0-20261101224619-d170ceb2f200+dirty", vcs(fullRev, "true")...),
			want:    "dev-d170ceb-dirty",
		},
		{
			name:    "revision shorter than seven characters is ignored",
			stamped: devVersion,
			bi:      info("(devel)", vcs("abc", "false")...),
			want:    devVersion,
		},
		{
			name:    "devel without vcs settings",
			stamped: devVersion,
			bi:      info("(devel)"),
			want:    devVersion,
		},
		{
			name:    "no build info at all",
			stamped: devVersion,
			bi:      nil,
			want:    devVersion,
		},
		{
			name:    "empty stamp with no build info",
			stamped: "",
			bi:      nil,
			want:    devVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.stamped, tt.bi); got != tt.want {
				t.Errorf("resolveVersion(%q, ...) = %q, want %q", tt.stamped, got, tt.want)
			}
		})
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0.1.0", "0.1.0"},
		{"v0.1.0", "0.1.0"},
		{" v0.1.0\n", "0.1.0"},
		{"(devel)", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.in); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
