package editor

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Windows returns every window as a client draws it, from the top left.
func (e *Session) Windows() []wire.Window {
	e.follow()
	// Opening or closing a buffer may show or hide the buffer line.
	if e.topRow() != e.lined {
		e.layout()
		e.reveal()
	}
	shown := map[*buffer][]any{}
	for _, s := range e.sessions {
		for _, w := range s.windows() {
			shown[w.buf] = append(shown[w.buf], w)
		}
	}
	for b, ws := range shown {
		if b.syntax != nil {
			for _, w := range slices.Clone(ws) {
				for i := range w.(*window).pins.starts {
					ws = append(ws, contextRow{w.(*window), i})
				}
			}
			b.syntax.Keep(ws)
		}
	}
	// The focus leaving a zoomed window shows every window again.
	if z := e.zoomed; z != nil && e.under() != z {
		e.zoomed = nil
		e.layout()
	}
	var out []wire.Window
	for _, w := range e.root.windows() {
		if e.zoomed == nil || w == e.zoomed {
			out = append(out, e.draw(w))
		}
	}
	for _, f := range e.floats {
		e.place(f)
		out = append(out, e.floatWindow(f))
	}
	return out
}

// draw is how a client draws w.
func (e *Session) draw(w *window) wire.Window {
	_, _, centered := e.area()
	e.fit(w)
	t, top, tab := w.buf.text.Snapshot(), e.topOf(w), e.tabWidth(w.buf)
	v := wire.Window{
		ID: w.id, Buffer: w.buf.id, Text: t, Top: top, Cursor: w.pos(),
		X: w.x, Y: w.y, W: w.w, H: w.h,
		Name: w.buf.name(), Modified: w.buf.modified(), Current: w == e.win, Zoomed: w == e.zoomed,
		Gutter: w.gutter(), SignCols: w.signCols(), Number: w.numberCols() > 0 && w.opts.Number, Relative: w.numberCols() > 0 && w.opts.RelativeNumber,
		Wrap: w.opts.Wrap, LineBreak: w.opts.LineBreak, BreakIndent: w.opts.BreakIndent, ShowBreak: e.cfg.ShowBreak, Left: w.left, CursorLine: w.opts.CursorLine, List: w.opts.List,
		Tab: tab, ColorColumn: w.opts.ColorColumn, ListChars: e.cfg.ListChars, FillChars: e.cfg.FillChars,
	}
	// Centered, the colour column is the windows' edge, so it shows none.
	if centered {
		v.ColorColumn = 0
	}
	if term := w.buf.term; term != nil && term.typing {
		v.Cells, v.CellX, v.CellY = term.screen()
	}
	ranges, d := e.view(w, t, top, tab)
	last := ranges[len(ranges)-1].End
	v.Ranges = ranges
	v.Virtual = w.unfilled(d.virtual, t.LineOf(top))
	if w == e.win {
		v.Past = e.vim.Past()
	}
	if w.opts.CursorColumn {
		_, col := w.screenAt(t, tab, d)
		v.CursorColumn = col + v.Past + 1
	}
	for _, f := range w.buf.folds {
		first, end := w.fold(t, t.LineOf(w.buf.text.Mark(f.start)))
		if s := t.LineStart(first); s >= top && s <= last {
			if v.Folds == nil {
				v.Folds = map[int]int{}
			}
			v.Folds[first] = end - first
		}
	}
	if w.guides.buf == w.buf {
		v.Guides = w.guides.lines
	}
	if h := w.buf.syntax; h != nil {
		for _, r := range ranges {
			v.Highlights = shownSpans(v.Highlights, h.Show(t, w, r.Start, r.End))
		}
		v.Context = pinned(w, t, top)
		for i, start := range v.Context {
			v.ContextHighlights = shownSpans(v.ContextHighlights, h.Show(t, contextRow{w, i}, start, vim.LineEnd(t, start)))
		}
	}
	// A server's semantic tokens draw over the syntax's highlights (B471).
	for _, r := range ranges {
		v.Highlights = append(v.Highlights, w.buf.tokensIn(r.Start, r.End)...)
		if w.opts.HLSearch {
			v.Matches = append(v.Matches, e.vim.Matches(t, r.Start, r.End)...)
		}
	}
	if w == e.win && w.opts.IncSearch {
		if s, end, ok := e.vim.Incremental(); ok {
			v.Searched = [2]int{s, end}
		}
	}
	if v.Current {
		v.Tasks = e.runningTasks()
		v.Sharing = e.sharing()
	}
	v.Others = e.others(w.buf)
	v.Signs = e.signs(w.buf, top, last)
	for _, p := range e.modules {
		for _, d := range e.decos[p] {
			if d.buf != w.buf || d.note == "" || d.inline() {
				continue
			}
			if at := w.buf.text.Mark(d.start); at >= top && at <= last {
				if v.Notes == nil {
					v.Notes = map[int]wire.Note{}
				}
				v.Notes[t.LineOf(at)] = wire.Note{Text: d.note, Name: d.style}
			}
		}
	}
	if w.opts.Icons {
		v.Icons = e.icons(w.buf, top, last)
		for line, s := range v.Signs {
			if icon, ok := diagnosticIcons[s.Name]; ok {
				v.Signs[line] = wire.Sign{Text: icon, Name: s.Name}
			}
		}
	}
	v.Pair = e.pair(w, t, ranges)
	v.Ruler = ruler(w.buf, t, w.pos(), top, max(w.h, 1), tab, func(p int) int { return w.rows(t, p, tab, d) })
	for _, p := range e.modules {
		if s := e.segments[p][w.buf]; s != "" {
			v.Segments = append(v.Segments, s)
		}
		if s := e.leftSegments[p][w.buf]; s != "" {
			v.LeftSegments = append(v.LeftSegments, s)
		}
		if s := e.ownLeft[p][w.buf]; s != "" {
			v.LeftSegments = append(v.LeftSegments, s)
		}
	}
	v.Filetype = e.filetype(w.buf)
	var names []string
	for _, s := range w.buf.servers {
		names = append(names, s.name)
		v.Progress = cmp.Or(v.Progress, s.doing())
	}
	v.Server = strings.Join(names, ", ")
	// The modules' diagnostics count, by their styles (B147).
	for _, p := range e.modules {
		for _, d := range e.decos[p] {
			if d.buf != w.buf {
				continue
			}
			switch d.style {
			case "diagnostic.error":
				v.Errors++
			case "diagnostic.warn":
				v.Warnings++
			}
		}
	}
	if w == e.win {
		// The :s being typed shows its matches, then each match struck
		// out before its replacement (B154).
		var edits []vim.Edit
		replacing := false
		for _, r := range ranges {
			in, rep := e.vim.Preview(r.Start, r.End)
			edits, replacing = append(edits, in...), rep
		}
		for _, ed := range edits {
			if !slices.ContainsFunc(ranges, func(r wire.Range) bool { return ed.End >= r.Start && ed.Start <= r.End }) {
				continue
			}
			if !replacing {
				v.Matches = append(v.Matches, [2]int{ed.Start, ed.End})
				continue
			}
			v.Decorations = append(v.Decorations, wire.Span{Start: ed.Start, End: ed.End, Name: "sub.old"})
			v.Ghosts = append(v.Ghosts, wire.Ghost{Pos: ed.End, Text: ed.Text, Style: "sub.new"})
		}
	}
	if w == e.win {
		v.Decorations = append(v.Decorations, e.snippetSpan(w.buf)...)
		v.Decorations = append(v.Decorations, e.linkedSpans(w.buf)...)
	}
	for _, ds := range e.decos {
		for _, d := range ds {
			if d.buf != w.buf {
				continue
			}
			s, end := w.buf.text.Mark(d.start), w.buf.text.Mark(d.end)
			if hex, ok := strings.CutPrefix(d.style, "colour #"); ok {
				if rgb, err := strconv.ParseInt(hex, 16, 32); err == nil && s < end && end > top && s < last {
					v.Colours = append(v.Colours, wire.Colour{Start: s, End: end, RGB: int32(rgb)})
				}
				continue
			}
			if d.line {
				if v.Grounds == nil {
					v.Grounds = map[int]string{}
				}
				if end >= top && s <= last {
					for l := t.LineOf(max(s, top)); l <= t.LineOf(min(end, last)); l++ {
						v.Grounds[l] = d.style
					}
				}
				// A pinned line keeps its ground (B217).
				for _, p := range v.Context {
					if l := t.LineOf(p); l >= t.LineOf(s) && l <= t.LineOf(end) {
						v.Grounds[l] = d.style
					}
				}
				continue
			}
			if !d.insert && !d.replace && !d.cover && !d.block && s < end && end > top && s < last {
				v.Decorations = append(v.Decorations, wire.Span{Start: s, End: end, Name: d.style})
			}
		}
	}
	v.Ghosts = append(v.Ghosts, d.ghosts...)
	// A pinned line shows as it draws in the window (B217, B295).
	for _, p := range v.Context {
		v.Ghosts = append(v.Ghosts, e.shown(w, p, vim.LineEnd(t, p)).ghosts...)
	}
	slices.SortStableFunc(v.Ghosts, func(a, b wire.Ghost) int { return cmp.Compare(a.Pos, b.Pos) })
	return v
}

// contextRow names a line sticky context pins over w's rows, for the
// highlights of a line above what w shows.
type contextRow struct {
	w   *window
	row int
}

// pinned returns the starts of the lines a module pinned over w's first
// rows, those that still start a line above top (B217).
func pinned(w *window, t kernel.Snapshot, top int) []int {
	if w.pins.buf != w.buf {
		return nil
	}
	var out []int
	for _, s := range w.pins.starts {
		if s < top && t.LineStart(t.LineOf(s)) == s {
			out = append(out, s)
		}
	}
	return out
}

// pair returns the bracket at w's cursor and its match within the range of
// the window's text that holds it, or nil. In insert mode the bracket may
// be the one before the cursor.
func (e *Session) pair(w *window, t kernel.Snapshot, ranges []wire.Range) []int {
	at := []int{w.pos()}
	if w == e.win && e.vim.Mode() == vim.Insert && at[0] > 0 {
		at = append(at, at[0]-1)
	}
	for _, p := range at {
		for _, r := range ranges {
			if p < r.Start || p > r.End {
				continue
			}
			if q, ok := vim.Pair(t, p, r.Start, r.End); ok {
				return []int{p, q}
			}
		}
	}
	return nil
}

// signs returns each line of b's sign, from the modules' decorations.
func (e *Session) signs(b *buffer, top, last int) map[int]wire.Sign {
	if b.signs == 0 {
		return nil
	}
	t := b.text.Snapshot()
	out := map[int]wire.Sign{}
	// The highest layer's sign shows, and among a layer's the latest: git's
	// at -1, most at 0, diagnostics at 1 and a breakpoint at 2 (B184, B262).
	var signed []decoration
	for _, p := range e.modules {
		for _, d := range e.decos[p] {
			if d.buf == b && d.sign != "" && !d.icon {
				signed = append(signed, d)
			}
		}
	}
	slices.SortStableFunc(signed, func(x, y decoration) int { return cmp.Compare(x.layer, y.layer) })
	for _, d := range signed {
		if at := b.text.Mark(d.start); at >= top && at <= last {
			out[t.LineOf(at)] = wire.Sign{Text: d.sign, Name: d.style}
		}
	}
	return out
}

// icons returns the icon of each line of b from top to last that a
// module's decoration gives one (B128, B337).
func (e *Session) icons(b *buffer, top, last int) map[int]wire.Sign {
	t := b.text.Snapshot()
	var out map[int]wire.Sign
	for _, p := range e.modules {
		for _, d := range e.decos[p] {
			if d.buf != b || !d.icon {
				continue
			}
			if at := b.text.Mark(d.start); at >= top && at <= last {
				if out == nil {
					out = map[int]wire.Sign{}
				}
				out[t.LineOf(at)] = wire.Sign{Text: d.sign, Name: d.style}
			}
		}
	}
	return out
}

// ruler returns the cursor's place for a window that shows rows rows from
// top, whose lines take rowsOf rows each. It is line,col, then -screen
// column when that differs, then All, Top, Bot or how far down the window is
// (B38).
func ruler(b *buffer, t kernel.Snapshot, c, top, rows, tab int, rowsOf func(int) int) string {
	line, ls := t.LineOf(c), vim.LineStart(t, c)
	col, vcol := c-ls+1, b.column(t, ls, c, tab)+1
	if ls == vim.LineEnd(t, ls) {
		col = 0
	}
	at := fmt.Sprintf("%d,%d", line+1, col)
	if vcol != col {
		at += fmt.Sprintf("-%d", vcol)
	}
	// last is the last line the window shows.
	last := t.LineOf(top)
	for p, n := top, 0; ; p = vim.LineEnd(t, p) + 1 {
		if n += rowsOf(p); n > rows {
			break
		}
		last = t.LineOf(p)
		if vim.LineEnd(t, p) == t.Len() {
			break
		}
	}
	above, below := t.LineOf(top), t.Lines()-1-last
	where := fmt.Sprintf("%d%%", above*100/max(above+below, 1))
	switch {
	case above == 0 && below == 0:
		where = "All"
	case above == 0:
		where = "Top"
	case below == 0:
		where = "Bot"
	}
	return fmt.Sprintf("%-*s %s", 17-len(where), at, where)
}

// others returns the other sessions whose current window shows b, with
// their cursors and selections (B253).
func (e *Session) others(b *buffer) []wire.Other {
	var out []wire.Other
	for _, s := range e.sessions {
		if s == e || s.win.buf != b {
			continue
		}
		o := wire.Other{Name: s.label(), Colour: s.colour, Cursor: s.win.pos()}
		if sel, ok := s.Selection(); ok {
			o.Selection = &sel
		}
		out = append(out, o)
	}
	return out
}
