//go:build darwin

package platform

import "os/exec"

// Reveal shows path in Finder, selecting it when the parent folder opens.
func Reveal(path string) error {
	if err := exec.Command("open", "-R", path).Run(); err != nil {
		return wrapErr("reveal", path, err)
	}
	return nil
}

// clipboardCommands returns the clipboard reader candidates, best first.
func clipboardCommands() [][]string {
	return [][]string{{"pbpaste"}}
}
