package editor

import (
	"math"
	"slices"
	"strings"

	"github.com/clipperhouse/displaywidth"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Where a float sits over the layout (D55).
const (
	middle      = "middle" // in the middle of the screen, as a panel does
	atCursor    = "cursor" // below the cursor of the window it opened from, or above it when there is more room there
	aboveCursor = "above"  // above that cursor, as signature help does
)

// openFloat shows b in a float at anchor over the current window, which it
// opened from. With focus it takes the keys, and closes every other float,
// as one float takes the keys at a time. Without, it replaces the float at
// anchor.
func (e *Session) openFloat(b *buffer, anchor string, focus bool) *window {
	for _, f := range slices.Clone(e.floats) {
		if focus || f.anchor == anchor {
			e.closeFloat(f)
		}
	}
	w := newWindow(b, 0, 0)
	w.opts.Wrap, w.opts.Clipboard = true, e.opts.Clipboard
	w.anchor, w.from = anchor, e.win
	e.floats = append(e.floats, w)
	e.place(w)
	if focus {
		e.win = w
	}
	return w
}

// closeFloat closes f and the floats opened from it, and frees its buffer.
func (e *Session) closeFloat(f *window) {
	if !slices.Contains(e.floats, f) {
		return
	}
	e.unfloat(f)
	if f != e.closed {
		f.drop()
		e.forget(f.buf)
	}
}

// unfloat takes f out of the floats and closes the floats opened from it.
// The focus goes back to the window f opened from, or the first window
// when that one is gone.
func (e *Session) unfloat(f *window) {
	e.floats = slices.DeleteFunc(e.floats, func(g *window) bool { return g == f })
	e.closeFloatsOver(f)
	if e.win == f {
		e.win = f.from
		if !slices.Contains(e.windows(), e.win) {
			e.win = e.root.windows()[0]
		}
	}
}

// forget frees what b holds once no float shows it, unless it is a buffer
// of the session's own.
func (e *Session) forget(b *buffer) {
	if slices.Contains(e.bufs, b) || slices.ContainsFunc(e.windows(), func(w *window) bool { return w.buf == b }) {
		return
	}
	e.style(b, nil)
	b.close()
}

// closeFloatsOver closes the floats opened from w, as w closes.
func (e *Session) closeFloatsOver(w *window) {
	for _, f := range slices.Clone(e.floats) {
		if f.from == w {
			e.closeFloat(f)
		}
	}
}

// floatAt returns the float at anchor, or nil.
func (e *Session) floatAt(anchor string) *window {
	if i := slices.IndexFunc(e.floats, func(f *window) bool { return f.anchor == anchor }); i >= 0 {
		return e.floats[i]
	}
	return nil
}

// windows returns the windows in the layout, then the floats.
func (e *Session) windows() []*window { return append(e.root.windows(), e.floats...) }

// under returns the window in the layout that the current window is over,
// or the current window when it is in the layout.
func (e *Session) under() *window {
	w := e.win
	for w.from != nil {
		w = w.from
	}
	return w
}

// floatBuffer returns a read-only buffer titled title holding text, styled
// by spans, for a float to show.
func (e *Session) floatBuffer(title, text string, spans []wire.Span) *buffer {
	// A float is gone with its next key, so it takes no number.
	b := &buffer{id: e.newID(false), text: kernel.New(text, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}, title: title}
	b.saved = b.text.Head()
	b.file.readonly = true
	e.style(b, spans)
	return b
}

// refill replaces b's text with text, styled by spans, keeping its marks
// where the text around them stays. b is a view, so it keeps no timeline
// of what it showed before (B381).
func (e *Session) refill(b *buffer, text string, spans []wire.Span) error {
	t := b.text.Snapshot()
	if old := t.Slice(0, t.Len()); old != text {
		if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Edits: []kernel.Edit{replace(old, text)}, Forget: true}); err != nil {
			return err
		}
		b.saved = b.text.Head()
	}
	e.style(b, spans)
	return nil
}

// popup shows text at the cursor in a float that the next key closes, with
// the image at image below it, or closes the one showing for "" (B309).
func (e *Session) popup(text string, spans []wire.Span, image string) {
	if text == "" {
		if f := e.floatAt(atCursor); f != nil && f != e.win {
			e.closeFloat(f)
		}
		return
	}
	e.openFloat(e.floatBuffer("[float]", text, spans), atCursor, false).image = image
}

// showSignature shows text above the cursor while insert mode types, or
// closes it for "" (B203).
func (e *Session) showSignature(text string, spans []wire.Span) {
	f := e.floatAt(aboveCursor)
	switch {
	case text == "" && f != nil:
		e.closeFloat(f)
	case text == "":
	case f != nil:
		if err := e.refill(f.buf, text, spans); err != nil {
			e.message = err.Error()
		}
	default:
		e.openFloat(e.floatBuffer("[signature]", text, spans), aboveCursor, false)
	}
}

// enter moves the focus into the float at the cursor the key being handled
// closed, as hover again does (B62).
func (e *Session) enter() bool {
	f := e.closed
	if f == nil || !slices.Contains(e.windows(), f.from) {
		return false
	}
	e.closed = nil
	e.floats = append(e.floats, f)
	e.win = f
	return true
}

// cursorCell returns the screen cell of w's cursor, as the client draws it.
func (e *Session) cursorCell(w *window) (int, int) {
	t, tab, top, c := w.buf.text.Snapshot(), e.tabWidth(w.buf), e.topOf(w), w.pos()
	start := vim.LineStart(t, c)
	d := e.shown(w, top, vim.LineEnd(t, c))
	y := w.y
	for p := top; p < start; {
		y += w.rows(t, p, tab, d)
		next, ok := w.nextLine(t, p)
		if !ok {
			break
		}
		p = next
	}
	for _, r := range d.virtual[t.LineOf(start)] {
		if r.Above {
			y++
		}
	}
	r, x := w.screenAt(t, tab, d)
	y += r
	return w.x + w.gutter() + min(x, w.cols()-1), min(y, w.y+w.h-1)
}

// place puts the float f on the screen. It is as big as its text, up to
// the screen, and a float at the cursor is at most 80 columns and 20 rows.
// Its x, y, w and h are its text's. Its border and a column of room each
// side go around them, and a foot goes under a rule above the bottom of the
// border.
func (e *Session) place(f *window) {
	if t := f.buf.term; t != nil {
		// A terminal takes four fifths of the screen, in its middle.
		f.w, f.h = max(e.width*4/5-4, 1), max(e.height*4/5-2, 1)
		f.x, f.y = (e.width-f.w)/2, (e.height-f.h-2)/2+1
		if err := t.fit(f.w, f.h); err != nil {
			e.message = err.Error()
		}
		return
	}
	t, tab := f.buf.text.Snapshot(), e.tabWidth(f.buf)
	lo, wide, _ := e.area()
	hi := lo + wide
	foot := 0
	if f.buf.foot != "" {
		foot = strings.Count(f.buf.foot, "\n") + 2
	}
	// width is the widest row the text takes at most limit columns wide,
	// and rows how many rows it takes.
	measure := func(limit int) (width, rows int) {
		for line := range t.Lines() {
			start := t.LineStart(line)
			starts := append(vim.WordRows(t, start, math.MaxInt, limit, 0, tab, nil), vim.LineEnd(t, start))
			for i := range starts[:len(starts)-1] {
				width = max(width, vim.Column(t, start, starts[i+1], tab, nil)-vim.Column(t, start, starts[i], tab, nil))
			}
			rows += len(starts) - 1
		}
		return width, rows
	}
	if f.anchor == middle {
		width, rows := measure(max(e.width-4, 1))
		for line := range strings.Lines(f.buf.foot) {
			width = max(width, displaywidth.String(strings.TrimSuffix(line, "\n")))
		}
		f.w = min(width, e.width-4)
		outer := min(rows+2+foot, e.height-2)
		f.h = max(outer-2-foot, 1)
		f.x, f.y = (e.width-f.w-4)/2+2, max(min(e.height/5, e.height-1-outer), 0)+1
		return
	}
	width, rows := measure(max(min(80, wide-4), 1))
	cx, cy := e.cursorCell(f.from)
	below, above := e.height-cy-1, cy
	n, y := 0, 0
	if f.anchor == aboveCursor {
		// Signature help is at most 10 rows, and goes below only when it
		// does not fit above and there is more room there.
		want, up, down := min(rows, 10), cy-2, e.height-cy-3
		n, y = min(want, up), cy-min(want, up)-2
		if up < want && down > up {
			n, y = min(want, down), cy+1
		}
	} else {
		// An image goes under the text when the text and the image both fit.
		// The box widens to the image, up to the windows' width (B309).
		f.imageRows, f.imageCols = 0, 0
		var p *picture
		if f.image != "" && e.cell[0] > 0 && e.cell[1] > 0 {
			p = e.picture(f.image)
		}
		if p != nil {
			if r, c := e.fitImage(p, 76, 12); rows+r <= min(max(below, above)-2, 20) {
				f.imageRows, f.imageCols = r, c
				width = max(width, min(c, wide-4))
				rows += r
			}
		}
		n = min(rows, max(below, above)-2, 20)
		y = cy + 1
		if below < n+2 && above > below {
			y = cy - n - 2
		}
	}
	f.w, f.h = max(width, 1), max(n, 1)
	f.x, f.y = max(min(cx, hi-f.w-4), lo)+2, y+1
}

// floatWindow is how a client draws f, a float e.place put on the screen.
func (e *Session) floatWindow(f *window) wire.Window {
	v := e.draw(f)
	// Only a float in the middle of the screen has a title.
	v.Float, v.Name, v.Foot, v.FootSpans = f.anchor, "", f.buf.foot, f.buf.footSpans
	if f.anchor == middle {
		v.Name = strings.Trim(f.buf.title, "[]")
	}
	if f.imageRows > 0 {
		p := e.picture(f.image)
		last := f.buf.text.Snapshot().Lines() - 1
		if v.Virtual == nil {
			v.Virtual = map[int][]wire.Virtual{}
		}
		for row := range f.imageRows {
			v.Virtual[last] = append(v.Virtual[last], wire.Virtual{Image: p.id, Row: row, Cols: f.imageCols})
		}
	}
	return v
}
