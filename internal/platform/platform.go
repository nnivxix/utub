// Package platform holds every OS assumption in one place: the default
// download directory, revealing a file in the file manager, and reading the
// system clipboard.
//
// Everything else in utub is portable Go. If a feature needs to know what
// operating system it is running on, the implementation belongs here behind
// one of these functions.
package platform

import (
	"errors"

	"github.com/nnivxix/utub/internal/config"
)

// ErrUnsupported means the current GOOS has no implementation for the action.
var ErrUnsupported = errors.New("platform: unsupported on this operating system")

// DownloadsDir returns the OS default download directory.
//
// The resolution logic lives in config.DefaultDownloadDir because Config's
// defaults already depend on it; this wrapper exists so callers that only care
// about the OS can import platform alone.
func DownloadsDir() string { return config.DefaultDownloadDir() }
