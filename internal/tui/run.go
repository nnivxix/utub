// Package tui is the terminal frontend: Bubble Tea models that render the
// engine's state and forward user intent back to it.
//
// It is one of three consumers of internal/core, and the only one that talks to
// the engine through a Go interface instead of HTTP. Nothing here shells out or
// parses yt-dlp output — that is core's job, and the layer rule in
// docs/architecture.md §4 keeps it that way.
package tui

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

// Run starts the TUI and blocks until the user quits.
//
// The deferred recover exists because alt-screen programs that panic leave the
// terminal in the alternate buffer with the cursor hidden: the user is dropped
// into an apparently broken shell. Restoring the screen before returning the
// error keeps a crash survivable.
func Run(deps Deps) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			restoreTerminal()
			err = fmt.Errorf("tui: %v", recovered)
		}
	}()

	// Alt-screen is declared by root.View, not by a program option.
	program := tea.NewProgram(newRoot(deps))
	if _, runErr := program.Run(); runErr != nil {
		return fmt.Errorf("tui: %w", runErr)
	}
	return nil
}

// restoreTerminal undoes the two things alt-screen mode changes: the alternate
// buffer and cursor visibility.
func restoreTerminal() {
	fmt.Fprint(os.Stderr, "\x1b[?25h\x1b[?1049l")
}
