package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runner executes a command and returns its stdout. Injected so the clipboard
// probing loop can be tested without a real clipboard or display server.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner is the real implementation.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// ReadClipboardText returns the system clipboard's text, or "" when no
// clipboard tool is available (SSH session, headless box, xclip not installed).
//
// Callers should pass a short timeout: clipboard tools can block waiting on a
// display server that does not exist. An unavailable clipboard is never an
// error — the user can always paste manually.
func ReadClipboardText(ctx context.Context) string {
	return readClipboard(ctx, clipboardCommands(), execRunner)
}

// readClipboard tries each candidate command in order and returns the first
// non-empty result. Whitespace-only output counts as empty.
func readClipboard(ctx context.Context, cmds [][]string, run runner) string {
	for _, cmd := range cmds {
		if len(cmd) == 0 {
			continue
		}

		out, err := run(ctx, cmd[0], cmd[1:]...)
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(string(out)); text != "" {
			return text
		}
	}
	return ""
}

// wrapErr adds the operation and path to an exec error.
func wrapErr(op, path string, err error) error {
	return fmt.Errorf("platform: %s %s: %w", op, path, err)
}
