//go:build linux

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Reveal opens the file manager at path (or at path's parent when path is a
// file). Desktop selection APIs are not portable, so this opens the folder and
// lets the user find the file.
func Reveal(path string) error {
	target := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		target = filepath.Dir(path)
	}

	if err := exec.Command("xdg-open", target).Run(); err != nil {
		return wrapErr("reveal", path, err)
	}
	return nil
}

// clipboardCommands returns the clipboard reader candidates, best first:
// Wayland first, then X11 via xclip, then xsel.
func clipboardCommands() [][]string {
	return [][]string{
		{"wl-paste", "--no-newline"},
		{"xclip", "-o", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--output"},
	}
}
