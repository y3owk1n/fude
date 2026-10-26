package editor

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// Click gives the window with the id the focus and puts its cursor at the
// cell at column x and row y of its text, gutter included, in cells as
// Place counts them (B552). A cell in the gutter means its row's first
// character, and one past a row's text its last. A row at or past the
// window's height, such as its status line's, only gives it the focus.
// A click ends visual mode and keeps insert mode. It closes the menu at
// the cursor, and the float there unless the click is on that float.
// While the command line, the finder or a question shows, it does nothing.
// A click on the selection grabs it, for Release to move it where it goes
// (B557).
func (e *Session) Click(id, x, y int) {
	w := e.windowOf(id)
	if w == nil || e.busy() {
		return
	}
	if f := e.floatAt(atCursor); f != nil && f != w && f != e.win {
		e.closeFloat(f)
	}
	if e.pick != nil {
		e.chose(e.pick, -1, false)
	}
	e.pointing = nil
	if s, ok := e.vim.Selection(); ok && !s.Block && w == e.win && y >= 0 && y < w.h {
		if p := e.posAt(w, x, y, false); p >= s.Start && p < s.End {
			e.grabbed = true
			return
		}
	}
	insert := e.vim.Mode() == vim.Insert
	if insert || e.vim.Mode() == vim.Visual {
		e.Feed("<Esc>")
	}
	e.win = w
	if y < w.h && w.buf.term == nil {
		e.clicked = e.posAt(w, x, y, insert)
		e.setCursor(e.clicked)
	}
	if insert {
		e.Feed("i")
	}
	e.reveal()
}

// Drag selects as v does from where the last click put the cursor to the
// cell at column x and row y of the window with the id, which the click
// gave the focus (B552).
func (e *Session) Drag(id, x, y int) {
	w := e.windowOf(id)
	if w == nil || w != e.win || e.busy() || w.buf.term != nil || e.grabbed {
		return
	}
	if e.vim.Mode() != vim.Visual {
		if e.vim.Mode() == vim.Insert {
			e.Feed("<Esc>")
		}
		e.setCursor(e.clicked)
		e.Feed("v")
	}
	e.setCursor(e.posAt(w, x, min(max(y, 0), max(w.h-1, 0)), false))
	e.reveal()
}

// Release ends a click at the cell at column x and row y of the window
// with the id. A selection the click grabbed moves there, and lines
// selected with V move to the start of that cell's line. A selection let
// go on itself ends visual mode with the cursor there, as a click
// elsewhere does (B557).
func (e *Session) Release(id, x, y int) {
	w := e.windowOf(id)
	if !e.grabbed || w == nil {
		return
	}
	e.grabbed = false
	s, ok := e.vim.Selection()
	if !ok || w != e.win {
		return
	}
	t := w.buf.text.Snapshot()
	to := e.posAt(w, x, min(max(y, 0), max(w.h-1, 0)), true)
	// Lines move with their line breaks.
	if s.Kind == "V" {
		to = vim.LineStart(t, to)
		s.Start, s.End = vim.LineStart(t, s.Start), min(vim.LineEnd(t, max(s.End-1, s.Start))+1, t.Len())
	}
	e.Feed("<Esc>")
	defer e.reveal()
	if to >= s.Start && to <= s.End {
		e.setCursor(min(to, max(s.End-1, s.Start)))
		return
	}
	if err := e.mayEdit(); err != nil {
		e.message = err.Error()
		return
	}
	text, at := t.Slice(s.Start, s.End), to
	edits := []kernel.Edit{{Start: to, End: to, Text: text}, {Start: s.Start, End: s.End}}
	if to > s.Start {
		edits[0], edits[1] = edits[1], edits[0]
		at -= len(text)
	}
	tx := kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: "move", Time: e.now(), Edits: edits}
	if err := w.buf.text.Apply(tx); err != nil {
		e.message = fmt.Sprintf("move the selection: %v", err)
		return
	}
	w.buf.head = w.buf.text.Head()
	e.setCursor(at)
}

// Drop takes the files at paths let go on the cell at column x and row y
// of the window with the id. On a markdown file's text each PNG, JPEG or
// GIF goes in at that cell as a link to it, one per line, as one change.
// That window takes the focus. Every other file then opens as :e opens it.
// While the command line, the finder or a question shows, it does nothing
// (B555, B595).
func (e *Session) Drop(id, x, y int, paths []string) {
	if e.busy() {
		return
	}
	defer e.reveal()
	insert := e.vim.Mode() == vim.Insert
	if insert || e.vim.Mode() == vim.Visual {
		e.Feed("<Esc>")
	}
	w := e.windowOf(id)
	markdown := w != nil && y >= 0 && y < w.h && e.filetype(w.buf) == "markdown"
	var links, rest []string
	for _, p := range paths {
		if markdown {
			if _, _, err := imageFile(p); err == nil {
				links = append(links, imageLink(w.buf.file.path, p))
				continue
			}
		}
		rest = append(rest, p)
	}
	if len(links) > 0 {
		e.win = w
		at, text := e.posAt(w, x, y, true), strings.Join(links, "\n")
		if err := (host{e}).Apply([]vim.Edit{{Start: at, End: at, Text: text}}, "drop"); err != nil {
			e.message = err.Error()
			return
		}
		e.setCursor(at + len(text) - 1)
		if insert && len(rest) == 0 {
			e.setCursor(at + len(text))
			e.Feed("i")
		}
	}
	for _, p := range rest {
		if err := e.edit(p); err != nil {
			e.message = err.Error()
			return
		}
	}
}

// imageLink is a markdown link to the image at path from the markdown file
// at from, relative to it when it can be. A path with a space goes in <>.
func imageLink(from, path string) string {
	to := path
	if dir, err := filepath.Abs(filepath.Dir(from)); from != "" && err == nil {
		if rel, err := filepath.Rel(dir, path); err == nil {
			to = rel
		}
	}
	if to = filepath.ToSlash(to); strings.Contains(to, " ") {
		to = "<" + to + ">"
	}
	name := filepath.Base(path)
	return "![" + strings.TrimSuffix(name, filepath.Ext(name)) + "](" + to + ")"
}

// Scroll moves the view of the window with the id n lines down, or up
// for a negative n, as <C-e> and <C-y> do, and its cursor the least it
// must to stay in the view. The focus stays where it is (B552).
func (e *Session) Scroll(id, n int) {
	w := e.windowOf(id)
	if w == nil || w.buf.term != nil {
		return
	}
	t := w.buf.text.Snapshot()
	top := e.topOf(w)
	for ; n > 0; n-- {
		next, ok := w.nextLine(t, top)
		if !ok {
			break
		}
		top = next
	}
	for ; n < 0 && top > 0; n++ {
		top = w.prevLine(t, top)
	}
	w.buf.text.MoveMark(w.top, top)
	// The cursor keeps its column on the nearest line in view, past
	// scroll_off lines from either edge.
	ranges, _ := e.view(w, t, top, e.tabWidth(w.buf))
	first, last := t.LineOf(top), t.LineOf(ranges[len(ranges)-1].End)
	off := min(w.opts.ScrollOff, (max(w.h, 1)-1)/2)
	c := w.pos()
	line := min(max(t.LineOf(c), first+off), max(last-off, first))
	if line != t.LineOf(c) {
		ls := t.LineStart(line)
		end := vim.LineEnd(t, ls)
		if end > ls && e.vim.Mode() != vim.Insert {
			end--
		}
		w.buf.text.MoveMark(w.cursor, min(ls+c-vim.LineStart(t, c), end))
	}
}

// pointed is the screen cell of a word of w's text that the mouse rests
// on. Module p's hover shows below it (B562).
type pointed struct {
	p    api.Module
	w    *window
	x, y int
}

// Hover shows what the language server says of the word at the cell at
// column x and row y of the current window, below that cell, as K does at
// the cursor, and leaves the cursor where it is. It first closes the hover
// the last call showed, so a cell off a word only closes it. A word with
// nothing to say shows nothing, with no message (B562).
func (e *Session) Hover(id, x, y int) {
	if f := e.floatAt(atCursor); f != nil && f.at != nil {
		e.closeFloat(f)
	}
	e.pointing = nil
	w := e.windowOf(id)
	if w == nil || w != e.win || x < w.gutter() || y < 0 || y >= w.h || w.buf.term != nil || e.busy() || e.pick != nil || e.vim.Mode() != vim.Normal {
		return
	}
	t, pos := w.buf.text.Snapshot(), e.posAt(w, x, y, true)
	if pos >= t.Len() || !isWord(t.Slice(pos, pos+1)[0]) {
		return
	}
	for _, p := range e.modules {
		for _, r := range p.Registered() {
			if r.Kind == api.Plug && r.Name == "hover" {
				e.pointing = &pointed{p, w, w.x + x, w.y + y}
				e.run(p, r.ID, e.scope(p, w.buf, t), pos, 1)
				return
			}
		}
	}
}

// Pick takes item n of the Items of the menu that shows, as a click on it
// does (B556, B560). The command line's menu puts it in the line, as <Tab>
// does, the finder opens it and a menu at the cursor takes it as <CR> or
// <C-y> does. A heading, and an n with no item, do nothing.
func (e *Session) Pick(n int) {
	defer e.reveal()
	switch c := e.cmdline.menu; {
	case e.finder != nil:
		f := e.finder
		if n >= 0 && n < len(f.rows) && f.rows[n] >= 0 {
			f.sel = f.rows[n]
			e.Feed("<CR>")
		}
	case e.vim.Mode() == vim.Cmdline && c != nil:
		line, _ := strings.CutPrefix(e.vim.Line(), ":")
		if n >= 0 && n < len(c.items) {
			c.sel = n
			e.vim.SetLine(line[:c.start] + c.items[n])
		}
	case e.pick != nil:
		if n >= 0 && n < len(e.pick.items) {
			e.chose(e.pick, n, true)
		}
	case e.completion.menu != nil:
		if n >= 0 && n < len(e.completion.menu.items) {
			e.choose(n)
			e.closeMenu()
		}
	}
}

// busy reports whether the command line, the finder or a question takes
// the keys, which a click leaves alone.
func (e *Session) busy() bool {
	return e.vim.Mode() == vim.Cmdline || e.finder != nil || e.ask != nil
}

// windowOf returns the window with the id, or nil.
func (e *Session) windowOf(id int) *window {
	ws := e.windows()
	if i := slices.IndexFunc(ws, func(w *window) bool { return w.id == id }); i >= 0 {
		return ws[i]
	}
	return nil
}

// posAt returns the place in w's text that the cell at column x and row y
// of w shows, gutter included. A cell in the gutter, or in a row that
// decorations put above or below a line, is the start of its row. A cell
// past the end of a row's text is its last character, or with end set the
// end of the line when the row is its last.
func (e *Session) posAt(w *window, x, y int, end bool) int {
	t, tab := w.buf.text.Snapshot(), e.tabWidth(w.buf)
	top := e.topOf(w)
	_, d := e.view(w, t, top, tab)
	column := func(ls, p int) int {
		if len(d.inlines) == 0 {
			return w.buf.column(t, ls, p, tab)
		}
		return vim.Column(t, ls, p, tab, d.inlines)
	}
	row := 0
	for p, from := top, w.firstRow(); ; {
		above, below := 0, 0
		for _, v := range d.virtual[t.LineOf(p)] {
			if v.Above {
				above++
			} else {
				below++
			}
		}
		starts := w.starts(t, p, tab, d)
		for len(starts) > 1 && starts[1] <= from {
			starts = starts[1:]
		}
		next, more := w.nextLine(t, p)
		if y >= row+above+len(starts)+below && more {
			row += above + len(starts) + below
			p, from = next, next
			continue
		}
		r := min(max(y-row-above, 0), len(starts)-1)
		start, stop := starts[r], vim.LineEnd(t, p)
		last := r == len(starts)-1
		if !last {
			stop = starts[r+1]
		}
		col := max(x-w.gutter(), 0)
		if !w.opts.Wrap {
			col += w.left
		} else {
			col += column(p, start)
			if start > p {
				col -= w.hang(t, p, tab, d.show)
			}
		}
		// The character that covers the column is the one before the
		// first that starts after it.
		at := stop
		if col < column(p, stop) {
			at = vim.ColumnStart(t, p, col+1, tab, d.inlines)
		} else if end && last {
			return stop
		}
		if at > start {
			_, n := utf8.DecodeLastRuneInString(t.Slice(start, at))
			at -= n
		}
		return max(at, start)
	}
}
