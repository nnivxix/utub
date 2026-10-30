package tui

import (
	"github.com/nnivxix/utub/internal/media"
)

// clipboardMsg carries a YouTube link found on the clipboard. An empty URL
// means nothing usable was there, which is not an error: the user can always
// paste by hand.
type clipboardMsg struct {
	url string
}

// navigateMsg asks the root model to switch screens. Sub-models never render or
// mutate each other; they only announce where the user wants to go.
type navigateMsg struct {
	screen screen
	ref    media.Ref
}

// noticeMsg is a transient line shown under the input field.
type noticeMsg struct {
	text string
}
