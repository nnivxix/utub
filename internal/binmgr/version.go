package binmgr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// versionFromOutput extracts a version token from the first line of a tool's
// --version output.
//
//	yt-dlp   "2026.10.26"
//	ffmpeg   "ffmpeg version 9.0.1 Copyright (c) ..."
//	aria2c   "aria2c version 1.37.0"
//
// Everything after the version is ignored: ffmpeg's first line alone carries a
// copyright notice and a build configuration.
func versionFromOutput(out string) string {
	line := firstLine(strings.TrimSpace(out))
	if line == "" {
		return ""
	}

	if i := strings.Index(line, "version "); i >= 0 {
		if fields := strings.Fields(line[i+len("version "):]); len(fields) > 0 {
			return strings.Trim(fields[0], ",;")
		}
		return ""
	}

	if fields := strings.Fields(line); len(fields) > 0 {
		return strings.Trim(fields[0], ",;")
	}
	return ""
}

// parseVersion splits a dotted version into numeric components.
//
// A leading "v" is tolerated ("v1.37.0"), as are non-numeric suffixes on the
// last component ("9.0.1-tarball"). Anything else is an error, which callers
// treat as "cannot order this version" rather than "too old".
func parseVersion(v string) ([]int, error) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return nil, errors.New("binmgr: empty version")
	}

	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for i, part := range parts {
		// Strip a suffix only on the last component: 9.0.1-rc2 → 1.
		if i == len(parts)-1 {
			if j := strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }); j > 0 {
				part = part[:j]
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("binmgr: cannot parse version %q: %w", v, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// AtLeast reports whether have >= want, comparing component by component.
//
// Missing trailing components count as zero, so "2024.1" equals "2024.1.0" and
// is older than "2024.1.1".
func AtLeast(have, want string) (bool, error) {
	h, err := parseVersion(have)
	if err != nil {
		return false, err
	}
	w, err := parseVersion(want)
	if err != nil {
		return false, err
	}

	for i := 0; i < len(h) || i < len(w); i++ {
		hi, wi := 0, 0
		if i < len(h) {
			hi = h[i]
		}
		if i < len(w) {
			wi = w[i]
		}
		switch {
		case hi > wi:
			return true, nil
		case hi < wi:
			return false, nil
		}
	}
	return true, nil
}
