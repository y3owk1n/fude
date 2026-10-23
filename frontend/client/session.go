package client

import (
	"time"

	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/vim"
)

// Session is what a frontend draws and types into: a session of the fude
// the frontend runs in, or one of another fude it attached to (D39, D63).
// *editor.Session is the first.
type Session interface {
	// What the user does.
	Feed(k vim.Key)
	Paste(text string)
	SetSize(w, h int)
	SetCell(w, h int) // a cell's pixels, 0 when the frontend shows no images (B303)
	// Place puts a window's text where the frontend drew it, in cells of
	// its own grid, over the layout's place for it (B542).
	Place(window, x, y, w, h int)
	// Click, Drag, Release and Scroll are the mouse's, in cells of a
	// window's text as Place counts them. Scroll moves the view n lines
	// (B552, B557).
	Click(window, x, y int)
	Drag(window, x, y int)
	Scroll(window, n int)
	Release(window, x, y int)
	// Pick takes item n of the Items of the menu that shows (B556).
	Pick(n int)
	Focus()
	Timeout()

	// The loop. Queue holds work for the loop to run. Wait says how long
	// until a typed key times out, Done whether the session has ended, and
	// Suspend, once, that the user asked to suspend fude to the shell.
	Queue() <-chan func()
	Load()
	Wait() (time.Duration, bool)
	Done() bool
	Suspend() bool

	// What the client draws.
	Windows() []wire.Window
	Layout() wire.Split
	Pages() []wire.Page
	Buffers() []wire.Tab
	Menu() (wire.Menu, bool)
	Line() string
	LineCursor() int
	Mode() vim.Mode
	Pending() bool
	Message() string
	Confirm() string
	Selection() (vim.Selection, bool)
	Cursors() []int
	Recording() rune
	Substituting() bool
	Terminal() bool
	Shown() string
	Theme() wire.Theme
	Styles() (map[string]wire.Style, int)
	Title() string               // what the frontend's title shows, "" to leave it
	Clipboard() (string, bool)   // text to put on the frontend's clipboard, once
	Image(id int) ([]byte, bool) // the PNG an image's rows show (B303)
	Freed() []int                // the images dropped since it was last called (B306)
}
