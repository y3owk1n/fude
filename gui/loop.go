package gui

import (
	"slices"
	"sync"
	"time"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/vim"
)

// window is a session in a native window. Its loop runs the session off
// the main thread, and native holds the system's objects, which only the
// main thread touches.
type window struct {
	s      client.Session
	keys   chan vim.Key
	do     chan func() // run on the loop, between keys, for tests
	pastes chan string
	mice   chan mouse
	ended  func() // runs on the main thread once the session ends

	mu      sync.Mutex
	pending []frame
	places  places        // the latest, for the loop
	placed  chan struct{} // tells the loop places changed
	cellPx  [2]int        // a cell's size in the screen's pixels

	// The main thread's.
	panes        map[int]*pane
	floats       map[int]*box
	panel, toast *box
	dragged      mouse     // the last click or drag, to send a drag once a cell
	typed        time.Time // when a key or the mouse last went, which shows the cursor
	caretOn      bool      // the cursor shows, as it blinks
	tabs         strip     // the tab strip, and the mode
	hits         []hit     // the tabs the strip shows
	hover        float64   // where the mouse is along the strip, -1 off it
	pressed      *hit      // the tab the mouse went down on
	pressX       float64   // where along the strip
	dragging     bool      // the tab pressed moves with the mouse
	dropX        float64   // where it is

	native
}

// cellW and cellH are a cell's size in points, in the text's font.
var cellW, cellH float64

// loop runs the session, as tui.Run does: it feeds keys and runs the
// session's work, and after each draws what changed.
func (w *window) loop(cols, rows int) {
	s := w.s
	var p painter
	question, asked := "", 0
	var spin <-chan time.Time
	draw := func() {
		f := p.paint(s, cols, rows)
		f.asked = asked
		if f.busy && spin == nil {
			spin = time.After(spinnerFrame)
		}
		w.mu.Lock()
		w.pending = append(w.pending, f)
		w.mu.Unlock()
		onMain(w.apply, false)
	}
	s.SetSize(cols, rows)
	s.SetCell(w.cellPx[0], w.cellPx[1])
	draw()
	s.Load()
	var timeout <-chan time.Time
	for !s.Done() {
		select {
		case k := <-w.keys:
			s.Feed(k)
		case f := <-s.Queue():
			f()
		case text := <-w.pastes:
			s.Paste(text)
		case m := <-w.mice:
			m.give(s)
		case <-w.placed:
			w.mu.Lock()
			pl := w.places
			w.mu.Unlock()
			cols, rows = pl.cols, pl.rows
			s.SetSize(pl.cols, pl.rows)
			s.SetCell(pl.cell[0], pl.cell[1])
			for _, at := range pl.wins {
				s.Place(at[0], at[1], at[2], at[3], at[4])
			}
		case f := <-w.do:
			f()
		case <-timeout:
			timeout = nil
			s.Timeout()
		case <-spin:
			spin = nil
		}
		if d, ok := s.Wait(); !ok {
			timeout = nil
		} else if timeout == nil {
			timeout = time.After(d)
		}
		// A question asked again after its answer, between frames, is new.
		if q := s.Confirm(); q != question {
			question = q
			asked++
		}
		if len(w.keys) > 0 {
			continue
		}
		draw()
	}
	onMain(w.close, false)
}

// places are where the panes put the windows' text, for the loop to give
// the session (B542).
type places struct {
	cols, rows int      // the area's, in cells
	cell       [2]int   // a cell's size in pixels
	wins       [][5]int // each window's id, x, y, w and h, in cells of the area
}

// leaves returns the ids of l's windows.
func leaves(l wire.Split) []int {
	if len(l.Kids) == 0 {
		return []int{l.Window}
	}
	var ids []int
	for _, k := range l.Kids {
		ids = append(ids, leaves(k)...)
	}
	return ids
}

// turning reports whether parts turn a spinner.
func turning(parts []client.Part) bool {
	return slices.ContainsFunc(parts, func(p client.Part) bool { return p.Role == "busy" })
}

// spinner is the frames of the spinner a busy part turns, one each
// spinnerFrame (B231).
var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

const spinnerFrame = 80 * time.Millisecond

// mouse is what the mouse did, for the loop to give the session: a click,
// a drag or a release at cell x, y of the window with the id, a scroll of
// y lines, or a pick of line y of a menu. A click's clicks count a double
// or triple click.
type mouse struct {
	kind             byte // c a click, g a click with Command, d a drag, u a release, s a scroll, p a pick and h a rest
	id, x, y, clicks int
}

// give gives the session what the mouse did, on the loop. A double click
// selects the word it hits and a triple click the line, as viw and V do
// (B552, B555).
func (m mouse) give(s client.Session) {
	switch m.kind {
	case 'c':
		// A double or triple click first ends the selection the click
		// before it made, so it selects anew and never grabs that one.
		if m.clicks > 1 && (s.Mode() == vim.Insert || s.Mode() == vim.Visual) {
			s.Feed("<Esc>")
		}
		s.Click(m.id, m.x, m.y)
		switch {
		case m.clicks == 2:
			for _, k := range vim.Keys("viw") {
				s.Feed(k)
			}
		case m.clicks > 2:
			s.Feed("V")
		}
	case 'g':
		// Insert mode ends first, so the click puts the cursor where <C-]>
		// reads it.
		if s.Mode() == vim.Insert {
			s.Feed("<Esc>")
		}
		s.Click(m.id, m.x, m.y)
		for _, k := range vim.Keys("<C-]>") {
			s.Feed(k)
		}
	case 'd':
		s.Drag(m.id, m.x, m.y)
	case 'u':
		s.Release(m.id, m.x, m.y)
	case 'p':
		s.Pick(m.y)
	case 'h':
		s.Hover(m.id, m.x, m.y)
	default:
		s.Scroll(m.id, m.y)
	}
}
