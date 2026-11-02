package main

import (
	"runtime/debug"
	"strings"
)

const (
	// devVersion is the fallback when nothing else identifies the build: a
	// source tree compiled without flags, no VCS, no module version.
	devVersion = "dev"

	// revLen is the short revision length git itself prints.
	revLen = 7
)

// resolveVersion picks the most specific version available:
//
//  1. the -ldflags "-X main.version=..." stamp written by make and goreleaser,
//  2. VCS metadata for a local build: dev-abc1234 or dev-abc1234-dirty,
//  3. the module version Go records for `go install .../utub@v0.1.0`,
//  4. "dev".
//
// The stamp wins because releases set it deliberately. The VCS branch comes
// before the module version because a local `go build` stamps a long
// pseudo-version (v0.0.0-20261101224619-d170ceb2f200+dirty) that would
// overflow the TUI header; the short revision is the useful part.
func resolveVersion(stamped string, bi *debug.BuildInfo) string {
	if v := normalizeVersion(stamped); v != "" && v != devVersion {
		return v
	}
	if bi == nil {
		return devVersion
	}

	var rev string
	var dirty bool
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			if len(setting.Value) >= revLen {
				rev = setting.Value[:revLen]
			}
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if rev != "" {
		if dirty {
			return "dev-" + rev + "-dirty"
		}
		return "dev-" + rev
	}

	if v := normalizeVersion(bi.Main.Version); v != "" {
		return v
	}
	return devVersion
}

// normalizeVersion drops the v prefix and the "(devel)" placeholder so the
// header reads the same whether the value came from a tag or a build stamp.
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "(devel)" {
		return ""
	}
	return v
}

// buildInfo returns the module metadata Go embeds in the binary, or nil when
// the build embedded none.
func buildInfo() *debug.BuildInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return bi
}
