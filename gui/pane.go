package gui

import (
	"math"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/theme"
)

// pane is a window of the layout in a view of its own, its text over its
// status bar: what the loop last sent for it, where the mouse is over its
// text and where its text scrolled. paneNative is the system's views.
type pane struct {
	w           *window
	id          int // the window's
	cells       [][]cell
	cursor      cursorAt
	pics        []pic
	left, right []client.Part
	current     bool
	offers      []string // which language server features of the right click's menu its buffer's servers offer
	read        reading  // its text, for assistive apps
	gone        bool     // its window closed and its views went

	mouse   [2]int    // the cell of its text the mouse is over, -1s off it
	movedAt time.Time // when the mouse last moved over it, or went down
	hovered [3]int    // the row and the first and end columns of the word whose hover the mouse asked for, a row of -1 for none
	link    rule      // the word under the mouse shown as a link while linkKey is held, w 0 for none

	wheel            float64 // the lines a scroll moved short of one
	top, rows, lines int     // its window's first line shown, its rows and its text's lines
	want             int     // the first line the view asked its window for, ahead of the frames
	offset           float64 // how far a trackpad moved the text up past its first line, in points

	paneNative
}

// newPane returns a pane of the window with id, with no views yet.
func newPane(w *window, id int) *pane {
	return &pane{w: w, id: id, mouse: [2]int{-1, -1}, hovered: [3]int{-1}}
}

// box is a panel over a window's text, which shows the palette, the
// finder, a menu, a float or a toast. boxNative is the system's views.
type box struct {
	*boxed
	scroll float64 // how far its list scrolled, in points
	seen   int     // the item its list selected last, to tell when that moves
	hover  point   // where the mouse is over it, X -1 when it is not
	id     int     // a float's window, which the mouse goes to, or 0
	w      *window

	boxNative
}

// update shows in p what the loop sent for its window.
func (p *pane) update(sh shown, ground theme.RGB) {
	if len(p.cells) != sh.h || len(p.cells) > 0 && len(p.cells[0]) != sh.w {
		p.cells = make([][]cell, sh.h)
		for y := range p.cells {
			p.cells[y] = make([]cell, sh.w)
			for x := range p.cells[y] {
				p.cells[y][x] = cell{" ", 1, theme.Style{Bg: ground}}
			}
		}
	}
	for _, c := range sh.put {
		if c.y >= len(p.cells) || c.x >= len(p.cells[c.y]) {
			continue
		}
		p.cells[c.y][c.x] = c.cell
		// A wide cell covers the next.
		for i := 1; i < c.width && c.x+i < len(p.cells[c.y]); i++ {
			p.cells[c.y][c.x+i] = cell{style: c.style}
		}
	}
	p.cursor, p.left, p.right, p.current, p.pics, p.offers, p.read = sh.cursor, sh.left, sh.right, sh.current, sh.pics, sh.offers, sh.read
	moved := sh.scroll[0] != p.top
	// With no scroll asked for on the way, the view asks for where it is.
	if p.want == p.top {
		p.want = sh.scroll[0]
	}
	p.top, p.rows, p.lines = sh.scroll[0], sh.scroll[1], sh.scroll[2]
	p.showBar(moved)
	p.redraw()
}

// origin is where p's first cell draws. The text moves by a trackpad's
// points and by the lines asked for that no frame shows yet, so it moves
// at once and never jumps back.
func (p *pane) origin() point {
	return point{pad, pad - p.offset - float64(p.want-p.top)*cellH}
}

// cellAt is the cell of p's text at a point of its view.
func cellAt(at point) [2]int {
	return [2]int{int(math.Floor((at.X - pad) / cellW)), int(math.Floor((at.Y - pad) / cellH))}
}

// wordAt returns the columns of the word the pane shows at cell, from its
// first to past its last, the same twice for none.
func (p *pane) wordAt(cell [2]int) (int, int) {
	x, y := cell[0], cell[1]
	if y < 0 || y >= len(p.cells) || x < 0 || x >= len(p.cells[y]) {
		return 0, 0
	}
	row := p.cells[y]
	word := func(i int) bool {
		r, _ := utf8.DecodeRuneInString(row[i].text)
		return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	if !word(x) {
		return 0, 0
	}
	from, to := x, x+1
	for from > 0 && word(from-1) {
		from--
	}
	for to < len(row) && word(to) {
		to++
	}
	return from, to
}

// restFor is how long the mouse rests on a word before its hover shows.
const restFor = 500 * time.Millisecond

// pointAt marks the cell of p's text the mouse is over, -1s for none, with
// mods held. A move off the word whose hover it asked for closes that
// hover, and a rest on another word asks for its hover (B562).
func (p *pane) pointAt(cell [2]int, mods int) {
	// The link's key may go down or up between moves on one cell.
	same := cell == p.mouse
	p.mouse = cell
	if p.relink(mods); same {
		return
	}
	p.movedAt = time.Now()
	if h := p.hovered; h[0] >= 0 && (cell[1] != h[0] || cell[0] < h[1] || cell[0] >= h[2]) {
		p.hovered = [3]int{-1}
		p.w.mice <- mouse{'h', p.id, -1, -1, 0}
	}
	from, to := p.wordAt(cell)
	if from == to || p.hovered[0] >= 0 {
		return
	}
	at := p.movedAt
	time.AfterFunc(restFor, func() {
		onMain(func() {
			if p.movedAt == at && !p.gone {
				p.hovered = [3]int{cell[1], from, to}
				p.w.mice <- mouse{'h', p.id, cell[0], cell[1], 0}
			}
		}, false)
	})
}

// relink makes the word under the mouse a link while mods hold linkKey and
// the window's servers offer definitions. A link is underlined, and the
// pointer over it is the system's for a link (B563).
func (p *pane) relink(mods int) {
	var l rule
	if mods&linkKey != 0 && slices.Contains(p.offers, "definition") {
		if from, to := p.wordAt(p.mouse); from != to {
			l = rule{from, p.mouse[1], to - from}
		}
	}
	if l == p.link {
		return
	}
	p.link = l
	p.pointer(l.w > 0)
	p.redraw()
}

// press gives the session a click, a drag or a release at a point of p's
// text, or of b's when p is nil, with clicks counting a double or triple
// click and mods held. A click on a menu picks the item it hits. While the
// palette or the finder shows, a click off it closes it as <Esc> does. A
// drag goes once per cell (B552, B556, B563).
func (w *window) press(p *pane, b *box, at point, kind byte, clicks, mods int) {
	w.typed, w.caretOn = time.Now(), true
	id, o := 0, point{pad, pad}
	switch {
	case p != nil:
		id = p.id
	case b != nil && b.boxed != nil:
		id, o = b.id, point{inset, inset + b.head()}
	default:
		return
	}
	m := mouse{kind, id, int(math.Floor((at.X - o.X) / cellW)), int(math.Floor((at.Y - o.Y) / cellH)), 1}
	switch {
	case b != nil && b.list != nil:
		if i, ok := b.list.itemAt(at); kind == 'c' && ok {
			w.mice <- mouse{kind: 'p', y: i}
		}
		return
	case w.panel.boxed != nil:
		if kind == 'c' && b != w.panel {
			w.keys <- "<Esc>"
		}
		return
	case id <= 0:
		return
	}
	if kind == 'c' {
		m.clicks = clicks
		// A click closes hover, and a rest after it waits for a move.
		if p != nil {
			p.movedAt, p.hovered = time.Now(), [3]int{-1}
			// A click on a link goes to its definition (B563).
			p.mouse = [2]int{m.x, m.y}
			if p.relink(mods); p.link.w > 0 {
				m.kind = 'g'
			}
		}
	}
	// A drag counts once it reaches another cell, so a mouse that moves
	// within a double click's cell does not drag.
	if kind == 'd' && m.id == w.dragged.id && m.x == w.dragged.x && m.y == w.dragged.y {
		return
	}
	w.dragged = m
	w.mice <- m
}

// roll scrolls p's window a line for each line a wheel turns, keeping what
// is short of a line for the next turn.
func (p *pane) roll(lines float64) {
	p.wheel += lines
	if n := int(p.wheel); n != 0 {
		p.wheel -= float64(n)
		p.w.mice <- mouse{'s', p.id, 0, n, 0}
	}
}

// smooth moves p's text up by dy points, a trackpad's move, asking its
// window to scroll a line for each row's height it passes. At the ends of
// the text it stops. Once the fingers or the momentum let go, ended, the
// text settles on the nearest line (B561).
func (p *pane) smooth(dy float64, ended bool) {
	p.offset -= dy
	n := int(math.Floor(p.offset / cellH))
	n = min(max(n, -p.want), max(p.lines-1-p.want, 0))
	if n != 0 {
		p.w.mice <- mouse{'s', p.id, 0, n, 0}
		p.want += n
		p.offset -= float64(n) * cellH
	}
	if p.want == 0 && p.offset < 0 || p.want >= p.lines-1 && p.offset > 0 {
		p.offset = 0
	}
	if ended {
		if p.offset > cellH/2 && p.want < p.lines-1 {
			p.w.mice <- mouse{'s', p.id, 0, 1, 0}
			p.want++
		}
		p.offset = 0
	}
	p.offset = min(max(p.offset, 0), cellH)
	p.redraw()
}
