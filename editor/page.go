package editor

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/frontend/wire"
)

// tabPage is a tab page: its layout, its floats and the window with the
// focus (B544). The page that shows keeps these in the session's own
// fields, and the session keeps the others in tabpages, as they were when
// they stopped showing.
type tabPage struct {
	id               int
	root             *node
	win              *window
	floats           []*window
	zoomed           *window
	unzoomed         [4]int
	lastWin, prevWin *window
}

// pageIDs is the id the last tab page got.
var pageIDs atomic.Int64

// newPage returns a tab page with w alone in it.
func newPage(w *window) tabPage {
	return tabPage{id: int(pageIDs.Add(1)), root: &node{win: w}, win: w}
}

// park keeps the page that shows in tabpages, to show again later.
func (e *Session) park() {
	p := &e.tabpages[e.tabpage]
	p.root, p.win, p.floats, p.zoomed, p.unzoomed, p.lastWin, p.prevWin = e.root, e.win, e.floats, e.zoomed, e.unzoomed, e.lastWin, e.prevWin
}

// showPage shows the page at i.
func (e *Session) showPage(i int) {
	if i != e.tabpage {
		e.park()
		e.loadPage(i)
	}
}

// loadPage shows the page at i in place of the one that shows, which it
// does not keep.
func (e *Session) loadPage(i int) {
	e.tabpage = i
	p := e.tabpages[i]
	e.root, e.win, e.floats, e.zoomed, e.unzoomed, e.lastWin, e.prevWin = p.root, p.win, p.floats, p.zoomed, p.unzoomed, p.lastWin, p.prevWin
	e.closed = nil
	// The modules hear the windows moved, as edits on another page may have
	// changed what they show.
	e.seen = seenAt{}
	e.layout()
	e.reveal()
	e.emit("tabpage.changed", "", e.win.buf)
}

// parked returns the windows in the layouts of the pages that do not show.
func (e *Session) parked() []*window {
	var out []*window
	for i, p := range e.tabpages {
		if i != e.tabpage {
			out = append(out, p.root.windows()...)
		}
	}
	return out
}

// unseen returns the windows out of sight: those hidden, and those and the
// floats on the pages that do not show.
func (e *Session) unseen() []*window {
	out := append(e.parked(), e.hidden...)
	for i, p := range e.tabpages {
		if i != e.tabpage {
			out = append(out, p.floats...)
		}
	}
	return out
}

// openPage shows a new page after the one that shows, with w alone in it.
func (e *Session) openPage(w *window) {
	e.tabpages = slices.Insert(e.tabpages, e.tabpage+1, newPage(w))
	e.showPage(e.tabpage + 1)
}

// tabNew shows a new page with a window on the file at path, or on a new
// empty buffer without one (B544).
func (e *Session) tabNew(path string) error {
	old := e.win
	w := newWindow(old.buf, old.pos(), old.buf.text.Mark(old.top))
	w.opts = old.opts
	e.openPage(w)
	if path != "" {
		return e.edit(path)
	}
	if _, err := e.addAs("", true); err != nil {
		return err
	}
	e.show(e.bufs[len(e.bufs)-1])
	return nil
}

// windowPage moves the current window to a new page of its own (B544).
func (e *Session) windowPage() error {
	w := e.under()
	if e.root.win == w {
		e.message = "already only one window"
		return nil
	}
	if err := e.unlay(w); err != nil {
		return err
	}
	e.openPage(w)
	return nil
}

// closePage closes the page at i. A window on it whose program runs hides,
// and the others go, as closeWindow does. The page on its right shows
// instead, or else the new last one (B544).
func (e *Session) closePage(i int) error {
	if len(e.tabpages) == 1 {
		return errors.New("cannot close the last tab page")
	}
	if i == e.tabpage {
		e.park()
	}
	p := e.tabpages[i]
	var gone []*window
	for _, w := range append(p.root.windows(), p.floats...) {
		if t := w.buf.term; t != nil && !t.exited && w.anchor == "" {
			// When it shows again it has no neighbour to go beside, so it
			// goes below the current window.
			w.was = slot{}
			e.hidden = append(e.hidden, w)
			continue
		}
		w.buf.setMark('"', w.pos())
		w.buf.lastTop = w.buf.text.Mark(w.top)
		w.drop()
		gone = append(gone, w)
	}
	e.tabpages = slices.Delete(e.tabpages, i, i+1)
	switch {
	case i == e.tabpage:
		e.loadPage(min(i, len(e.tabpages)-1))
	case i < e.tabpage:
		e.tabpage--
		e.emit("tabpage.changed", "", e.win.buf)
	default:
		e.emit("tabpage.changed", "", e.win.buf)
	}
	// An ended terminal's buffer goes with its last window.
	for _, w := range gone {
		if t := w.buf.term; t != nil && t.exited && slices.Contains(e.bufs, w.buf) && !e.showing(w.buf) {
			e.close(w.buf, true)
		}
	}
	return nil
}

// onlyPage closes every page but the one that shows (B544).
func (e *Session) onlyPage() error {
	for i := len(e.tabpages) - 1; i >= 0; i-- {
		if i != e.tabpage {
			if err := e.closePage(i); err != nil {
				return err
			}
		}
	}
	return nil
}

// nextPage shows page n counted from 1, the page n after the one that
// shows with +n, or the next one, after the last the first, without n
// (B544).
func (e *Session) nextPage(arg string) error {
	if arg == "" {
		e.showPage((e.tabpage + 1) % len(e.tabpages))
		return nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(arg, "+"))
	switch {
	case err != nil:
		return fmt.Errorf("not a tab page: %s", arg)
	case strings.HasPrefix(arg, "+"):
		n += e.tabpage + 1
	}
	if n < 1 || n > len(e.tabpages) {
		return fmt.Errorf("no tab page %d", n)
	}
	e.showPage(n - 1)
	return nil
}

// previousPage shows the page n before the one that shows, or the one
// before it, going round from the first to the last (B544).
func (e *Session) previousPage(arg string) error {
	n := 1
	if arg != "" {
		var err error
		if n, err = strconv.Atoi(arg); err != nil || n < 1 {
			return fmt.Errorf("not a count of tab pages: %s", arg)
		}
	}
	k := len(e.tabpages)
	e.showPage(((e.tabpage-n)%k + k) % k)
	return nil
}

// movePage moves the page that shows after page n, counted from 1 before
// it moves, to the first place with 0, n places on with +n or back with -n,
// or to the last place without n (B544).
func (e *Session) movePage(arg string) error {
	k := len(e.tabpages)
	to := k - 1
	if arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil {
			return fmt.Errorf("not a tab page: %s", arg)
		}
		switch {
		case arg[0] == '+' || arg[0] == '-':
			to = e.tabpage + n
		case n > e.tabpage:
			to = n - 1
		default:
			to = n
		}
		if to < 0 || to >= k || arg[0] != '+' && arg[0] != '-' && n > k {
			return fmt.Errorf("cannot move the tab page to %s", arg)
		}
	}
	e.park()
	p := e.tabpages[e.tabpage]
	e.tabpages = slices.Insert(slices.Delete(e.tabpages, e.tabpage, e.tabpage+1), to, p)
	e.tabpage = to
	e.emit("tabpage.changed", "", e.win.buf)
	return nil
}

// topRow reports whether the top row of the screen shows, for the buffers
// or the tab pages (B258, B545).
func (e *Session) topRow() bool { return len(e.Buffers()) > 0 || len(e.Pages()) > 0 }

// Pages returns the tab pages the top row shows, those a module named
// (B545).
func (e *Session) Pages() []wire.Page {
	var out []wire.Page
	for i, p := range e.tabpages {
		name, ok := e.tabNames[p.id]
		if !ok {
			continue
		}
		w := e.pageWindow(i)
		out = append(out, wire.Page{Name: name, Modified: w.buf.modified(), Current: i == e.tabpage})
	}
	return out
}

// pageWindow returns the window of the page at i that has the focus, or
// the one a float there opened from.
func (e *Session) pageWindow(i int) *window {
	w := e.tabpages[i].win
	if i == e.tabpage {
		w = e.win
	}
	for w.from != nil {
		w = w.from
	}
	return w
}

// scopePages returns the tab pages as a module reads them (B545).
func (e *Session) scopePages() []api.TabPage {
	out := make([]api.TabPage, len(e.tabpages))
	for i, p := range e.tabpages {
		out[i] = api.TabPage{ID: p.id, Buffer: e.pageWindow(i).buf.id, Current: i == e.tabpage}
	}
	return out
}
