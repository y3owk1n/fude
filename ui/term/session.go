package term

import (
	"time"

	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Session is what the terminal client draws and types into: a session of
// the fude the client runs in, or one of another fude it attached to
// (D39). *editor.Session is the first.
type Session interface {
	// What the user does.
	Feed(k vim.Key)
	Paste(text string)
	SetSize(w, h int)
	Focus()
	Timeout()
	ShowHints()

	// The loop. Queue holds work for the loop to run. Wait says how long
	// until a typed key times out, and Done whether the session has ended.
	Queue() <-chan func()
	Load()
	Wait() (time.Duration, bool)
	HintsIn() (time.Duration, bool)
	Done() bool

	// What the client draws.
	Views() []wire.View
	Buffers() []wire.Tab
	Float() (wire.Float, bool)
	Signature() (wire.Float, bool)
	Line() string
	LineCursor() int
	Mode() vim.Mode
	Message() string
	Selection() (vim.Selection, bool)
	Cursors() []int
	Recording() rune
	Substituting() bool
	Terminal() bool
	Shown() string
	Theme() (string, *[16]int32, *wire.Colors)
	TerminalClipboard() (string, bool)
}
