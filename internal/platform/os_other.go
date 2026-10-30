//go:build !darwin && !windows && !linux

package platform

// Reveal is not implemented on this operating system.
func Reveal(string) error { return ErrUnsupported }

// clipboardCommands returns no candidates: reading the clipboard is
// unsupported here, which reads as an empty clipboard to callers.
func clipboardCommands() [][]string { return nil }
