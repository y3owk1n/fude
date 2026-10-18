package editor

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Views returns every window from the top left.
func (e *Session) Views() []wire.View {
	e.follow()
	// Opening or closing a buffer may show or hide the buffer line.
	if (len(e.Buffers()) > 0) != e.lined {
		e.layout()
		e.reveal()
	}
	shown := map[*buffer][]any{}
	for _, s := range e.sessions {
		for _, w := range s.root.windows() {
			shown[w.buf] = append(shown[w.buf], w)
		}
	}
	for b, ws := range shown {
		if b.syntax != nil {
			for _, w := range slices.Clone(ws) {
				for i := range w.(*window).opts.StickyContextLines {
					ws = append(ws, contextRow{w.(*window), i})
				}
			}
			b.syntax.Keep(ws)
		}
	}
	var out []wire.View
	_, _, centered := e.area()
	for _, w := range e.root.windows() {
		e.fit(w)
		t, top, tab := w.buf.text.Snapshot(), e.topOf(w), e.tabWidth(w.buf)
		v := wire.View{
			Buffer: w.buf.id, Text: t, Top: top, Cursor: w.pos(),
			X: w.x, Y: w.y, W: w.w, H: w.h,
			Name: w.buf.name(), Modified: w.buf.modified(), Current: w == e.win || w == e.under,
			Gutter: w.gutter(), SignCols: w.signCols(), Number: w.numberCols() > 0 && w.opts.Number, Relative: w.numberCols() > 0 && w.opts.RelativeNumber,
			Wrap: w.opts.Wrap, Left: w.left, CursorLine: w.opts.CursorLine, List: w.opts.List,
			Indent: e.indentWidth(w.buf), Tab: tab, Guides: w.opts.IndentGuides, ColorColumn: w.opts.ColorColumn,
		}
		// Centered, the colour column is the windows' edge, so it shows none.
		if centered {
			v.ColorColumn = 0
		}
		if term := w.buf.term; term != nil && term.typing {
			v.Cells, v.CellX, v.CellY = term.screen()
		}
		// The last line shown, a closed fold counting as one (B224).
		lastStart := t.LineStart(min(t.LineOf(top)+max(w.h-2, 0), t.Lines()-1))
		if len(w.buf.folds) > 0 {
			lastStart = top
			for range max(w.h-2, 0) {
				next, ok := w.nextLine(t, lastStart)
				if !ok {
					break
				}
				lastStart = next
			}
		}
		last := vim.LineEnd(t, lastStart)
		ghosts := e.shown(w, top, last)
		inlines := wire.Inlines(ghosts)
		for _, f := range w.buf.folds {
			first, end := w.fold(t, t.LineOf(w.buf.text.Mark(f.start)))
			if s := t.LineStart(first); s >= top && s <= last {
				if v.Folds == nil {
					v.Folds = map[int]int{}
				}
				v.Folds[first] = end - first
			}
		}
		if h := w.buf.syntax; h != nil {
			v.Highlights = h.Show(t, w, top, last)
			if w.opts.StickyContext {
				v.Context = e.pinned(w, t, top)
				for i, start := range v.Context {
					v.ContextHighlights = append(v.ContextHighlights, h.Show(t, contextRow{w, i}, start, vim.LineEnd(t, start))...)
				}
			}
		}
		v.Matches = e.vim.Matches(t, top, last)
		if s, end, ok := e.vim.Incremental(); ok && w == e.win && w.opts.IncSearch {
			v.Searched = [2]int{s, end}
		}
		if v.Current {
			v.Tasks = e.runningTasks()
			v.Sharing = e.sharing()
		}
		v.Others = e.others(w.buf)
		v.Signs = e.signs(w.buf, top, last)
		for line, n := range w.buf.notes(t, top, last) {
			if v.Notes == nil {
				v.Notes = map[int]wire.Note{}
			}
			v.Notes[line] = wire.Note{Text: n}
		}
		for _, p := range e.modules {
			for _, d := range e.decos[p] {
				if d.buf != w.buf || d.note == "" || d.inline() || !w.opts.DiagnosticNotes && strings.HasPrefix(d.style, "diagnostic.") {
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
			v.Icons = w.buf.icons(t, top, last)
			for line, s := range v.Signs {
				if icon, ok := diagnosticIcons[s.Name]; ok {
					v.Signs[line] = wire.Sign{Text: icon, Name: s.Name}
				}
			}
		}
		v.Pair = e.pair(w, t, top, last)
		v.Ruler = ruler(t, w.pos(), top, max(w.h-1, 1), tab, func(p int) int { return w.rows(t, p, tab, inlines) })
		for _, p := range e.modules {
			if s := e.segments[p][w.buf]; s != "" {
				v.Segments = append(v.Segments, s)
			}
		}
		v.Branch, v.Filetype = w.buf.branch, e.filetype(w.buf)
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
			edits, replacing := e.vim.Preview(top, last)
			for _, ed := range edits {
				if !replacing {
					v.Matches = append(v.Matches, [2]int{ed.Start, ed.End})
					continue
				}
				v.Decorations = append(v.Decorations, syntax.Span{Start: ed.Start, End: ed.End, Name: "sub.old"})
				v.Ghosts = append(v.Ghosts, wire.Ghost{Pos: ed.End, Text: ed.Text, Style: "sub.new"})
			}
		}
		if f := e.flash; f != nil && f.b == w.buf && f.end > top && f.start < last {
			// Only what shows, as an undo can touch the whole text.
			v.Decorations = append(v.Decorations, syntax.Span{Start: max(f.start, top), End: min(f.end, last), Name: "flash"})
		}
		if w == e.win {
			v.Decorations = append(v.Decorations, e.snippetSpan(w.buf)...)
			if j := e.jumping; j != nil {
				for _, m := range j.matches {
					v.Decorations = append(v.Decorations, syntax.Span{Start: m[0], End: m[1], Name: "jump.match"})
				}
				for l, m := range j.labels {
					v.Labels = append(v.Labels, wire.Label{Pos: m[1], Text: l})
				}
			}
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
				if d.line && end >= top && s <= last {
					if v.Grounds == nil {
						v.Grounds = map[int]string{}
					}
					for l := t.LineOf(max(s, top)); l <= t.LineOf(min(end, last)); l++ {
						v.Grounds[l] = d.style
					}
					continue
				}
				if d.style != "inlay" && !d.replace && s < end && end > top && s < last {
					v.Decorations = append(v.Decorations, syntax.Span{Start: s, End: end, Name: d.style})
				}
			}
		}
		v.Ghosts = append(v.Ghosts, ghosts...)
		slices.SortStableFunc(v.Ghosts, func(a, b wire.Ghost) int { return cmp.Compare(a.Pos, b.Pos) })
		out = append(out, v)
	}
	return out
}

// contextRow names a line sticky context pins over w's rows, for the
// highlights of a line above what w shows.
type contextRow struct {
	w   *window
	row int
}

// pinned returns the starts of the lines pinned over w's first rows: those
// of the functions and classes that hold the first line they leave in view
// and start above it, the outermost first. They never cover the cursor's
// line, and are at most sticky_context_lines (B217).
func (e *Session) pinned(w *window, t kernel.Snapshot, top int) []int {
	p, h := &w.pinned, w.buf.syntax
	if p.version == t.Version() && p.top == top && p.cursor == w.pos() && p.parses == h.Parses() && p.lines == w.opts.StickyContextLines {
		return p.starts
	}
	var starts []int
	first := t.LineOf(top)
	// A cursor above the view, as while a search shows its match (B21),
	// has no line here to keep uncovered.
	below := t.LineOf(w.pos()) - first
	if below < 0 {
		below = w.opts.StickyContextLines
	}
	room := min(w.opts.StickyContextLines, below, max(w.h-3, 0))
	// Each pinned line covers one more line of text, so the loop asks again
	// about the line under them until the answer stops changing.
	for range w.opts.StickyContextLines {
		under := first + len(starts)
		var found []int
		for _, s := range h.Context(t.LineStart(min(under, t.Lines()-1))) {
			if ls := t.LineStart(t.LineOf(s)); t.LineOf(s) < under && !slices.Contains(found, ls) {
				found = append(found, ls)
			}
		}
		found = found[max(len(found)-room, 0):]
		if slices.Equal(found, starts) {
			break
		}
		starts = found
	}
	p.version, p.top, p.cursor, p.parses, p.lines, p.starts = t.Version(), top, w.pos(), h.Parses(), w.opts.StickyContextLines, starts
	return starts
}

// pair returns the bracket at w's cursor and its match within the lines
// from top to last, or nil. In insert mode the bracket may be the one
// before the cursor.
func (e *Session) pair(w *window, t kernel.Snapshot, top, last int) []int {
	at := []int{w.pos()}
	if w == e.win && e.vim.Mode() == vim.Insert && at[0] > 0 {
		at = append(at, at[0]-1)
	}
	for _, p := range at {
		if q, ok := vim.Pair(t, p, top, last); ok {
			return []int{p, q}
		}
	}
	return nil
}

// signs returns each line of b's sign, from the modules' decorations. A
// listing's lines from top to last show the git status of their names.
func (e *Session) signs(b *buffer, top, last int) map[int]wire.Sign {
	if b.signs == 0 && len(b.status) == 0 {
		return nil
	}
	t := b.text.Snapshot()
	out := map[int]wire.Sign{}
	// The highest layer's sign shows, and among a layer's the latest: git's
	// at -1, most at 0, diagnostics at 1 and a breakpoint at 2 (B184, B262).
	var signed []decoration
	for _, p := range e.modules {
		for _, d := range e.decos[p] {
			if d.buf == b && d.sign != "" {
				signed = append(signed, d)
			}
		}
	}
	slices.SortStableFunc(signed, func(x, y decoration) int { return cmp.Compare(x.layer, y.layer) })
	for _, d := range signed {
		out[t.LineOf(b.text.Mark(d.start))] = wire.Sign{Text: d.sign, Name: d.style}
	}
	for line := t.LineOf(top); len(b.status) > 0 && line <= t.LineOf(last); line++ {
		start := t.LineStart(line)
		if s := b.status[t.Slice(start, vim.LineEnd(t, start))]; s.Text != "" {
			out[line] = s
		}
	}
	return out
}

// ruler returns the cursor's place for a window that shows rows rows from
// top, whose lines take rowsOf rows each. It is line,col, then -screen
// column when that differs, then All, Top, Bot or how far down the view is
// (B38).
func ruler(t kernel.Snapshot, c, top, rows, tab int, rowsOf func(int) int) string {
	line, ls := t.LineOf(c), vim.LineStart(t, c)
	col, vcol := c-ls+1, vim.Column(t, ls, c, tab, nil)+1
	if ls == vim.LineEnd(t, ls) {
		col = 0
	}
	at := fmt.Sprintf("%d,%d", line+1, col)
	if vcol != col {
		at += fmt.Sprintf("-%d", vcol)
	}
	// last is the last line the view shows.
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
