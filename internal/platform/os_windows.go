//go:build windows

package platform

import (
	"errors"
	"os/exec"
)

// Reveal opens Explorer with path selected.
//
// explorer.exe exits with status 1 even when it succeeds, so only a real
// launch failure is reported.
func Reveal(path string) error {
	cmd := exec.Command("explorer", "/select,"+path)
	err := cmd.Run()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	if err != nil {
		return wrapErr("reveal", path, err)
	}
	return nil
}

// clipboardCommands returns the clipboard reader candidates, best first.
// PowerShell ships with every supported Windows version.
func clipboardCommands() [][]string {
	return [][]string{
		{"powershell", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard -Raw"},
	}
}
