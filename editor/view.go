package editor

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// Other is another session as a view shows it (B252, B253).
type Other struct {
	Name      string
	Colour    int // which of six colours
	Cursor    int
	Selection *vim.Selection `json:",omitempty"`
}

// View is one window as a client draws it.
type View struct {
	Buffer            int             // the buffer's number, which a frame's Texts name
	Text              kernel.Snapshot `json:"-"`
	Top, Cursor       int
	X, Y, W, H        int // H counts the status line on the bottom row
	Name              string
	Modified, Current bool
	Highlights        []syntax.Span // in paint order, later ones drawing over earlier ones
	Decorations       []syntax.Span // plugins' styles, drawn over highlights
	Ghosts            []Ghost
	Matches           [][2]int       // of the search, shown in every window
	Segments          []string       // plugins' statusline segments, in the order they loaded
	Ruler             string         // the cursor's line and column, and where the view is (B38)
	Branch            string         // the git branch of the buffer's repository, "" outside one (B147)
	Errors, Warnings  int            // the buffer's diagnostics of each severity (B147)
	Filetype          string         // the buffer's filetype, "" for none (B164)
	Server            string         // the buffer's language servers, "" for none (B161, B169)
	Progress          string         // what the server is doing, "" once it is ready (B161)
	Gutter            int            // the columns the sign column and line numbers take on the left
	SignCols          int            // the columns of the sign column, at the gutter's left
	Signs             map[int]Sign   // by line, the sign each one shows (B119)
	Notes             map[int]string // by line, dim text after its end (B127)
	Icons             map[int]Sign   // by line, a listing's icon at the gutter's right (B128)
	Number, Relative  bool           // show line numbers, and relative ones (B39)
	Wrap              bool           // long lines go on the rows below
	CursorLine        bool           // shade the cursor's line (B117)
	Pair              []int          // the bracket at the cursor and its match, shown bold (B121)
	List              bool           // show tabs, trailing spaces and no-break spaces (B118)
	Left              int            // the first column shown without wrap
	Indent            int            // the columns of an indent level, where guides go
	Tab               int            // the columns a tab shows as
	Guides            bool           // draw indent guides (B153)
	ColorColumn       int            // the column of the text to shade, from 1, or 0 (B200)
	Colours           []Colour       // text that names a colour, shown on it (B208, B209)
	Labels            []Label        // a jump's labels, drawn over the text (B218)
	Folds             map[int]int    // by the first line of each closed fold shown, the lines it hides (B224)
	Tasks             []string       // the tasks running, on the current window's status line (B231)
	Others            []Other        // the other sessions whose current window shows the buffer (B253)
	// Context is the start of each line pinned over the window's first rows,
	// the functions and classes the text under them is in, with their
	// highlights (B217).
	Context           []int
	ContextHighlights []syntax.Span
	// A terminal window in terminal mode shows its program's screen, with
	// the program's cursor at CellX and CellY, or -1 when it hides it (B205).
	Cells        [][]Cell
	CellX, CellY int
}

// Ghost is a proposal's text, drawn before the byte at Pos but not in the
// text (B71).
type Ghost struct {
	Pos   int
	Text  string
	Stale bool
	Style string // how it shows: "" for a proposal's text, sub.new for :s's (B154) or inlay for an inlay hint (B216)
}

// Views returns every window from the top left.
func (e *Session) Views() []View {
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
				for i := range maxContext {
					ws = append(ws, contextRow{w.(*window), i})
				}
			}
			b.syntax.Keep(ws)
		}
	}
	var out []View
	_, _, centered := e.area()
	for _, w := range e.root.windows() {
		e.fit(w)
		t, top, tab := w.buf.text.Snapshot(), e.topOf(w), e.tabWidth(w.buf)
		v := View{
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
		if len(w.folds) > 0 {
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
		for _, f := range w.folds {
			first, end := w.fold(t, t.LineOf(w.buf.text.Mark(f.first)))
			if s := t.LineStart(first); s >= top && s <= last {
				if v.Folds == nil {
					v.Folds = map[int]int{}
				}
				v.Folds[first] = end - first
			}
		}
		if h := w.buf.syntax; h != nil {
			v.Highlights = h.Show(t, w, top, last)
			if e.cfg.StickyContext {
				v.Context = e.pinned(w, t, top)
				for i, start := range v.Context {
					v.ContextHighlights = append(v.ContextHighlights, h.Show(t, contextRow{w, i}, start, vim.LineEnd(t, start))...)
				}
			}
		}
		v.Matches = e.vim.Matches(t, top, last)
		if e.cfg.HighlightColors {
			v.Colours = textColours(t.Slice(top, last), top)
		}
		e.diffGit(w.buf)
		if v.Current {
			v.Tasks = e.runningTasks()
		}
		v.Others = e.others(w.buf)
		if b := w.buf; b.ask {
			b.ask = false
			for _, s := range b.servers {
				e.askColours(s, b)
				e.askHints(s, b)
			}
		}
		v.Signs = e.signs(w.buf, top, last)
		v.Notes = w.buf.notes(t, top, last)
		if e.blame && w == e.win {
			if note := e.blameNote(w); note != "" {
				if v.Notes == nil {
					v.Notes = map[int]string{}
				}
				v.Notes[t.LineOf(w.pos())] = "   " + note
			}
		}
		for _, p := range e.plugins {
			for _, d := range e.decos[p] {
				if d.buf != w.buf || d.note == "" {
					continue
				}
				if at := w.buf.text.Mark(d.start); at >= top && at <= last {
					if v.Notes == nil {
						v.Notes = map[int]string{}
					}
					v.Notes[t.LineOf(at)] = d.note
				}
			}
		}
		if w.opts.Icons {
			v.Icons = w.buf.icons(t, top, last)
			for line, s := range v.Signs {
				if icon, ok := diagnosticIcons[s.Name]; ok {
					v.Signs[line] = Sign{icon, s.Name}
				}
			}
		}
		v.Pair = e.pair(w, t, top, last)
		v.Ruler = ruler(t, w.pos(), top, max(w.h-1, 1), tab, func(p int) int { return w.rows(t, p, tab) })
		for _, p := range e.plugins {
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
		for _, d := range e.diagnostics(w.buf) {
			switch d.style {
			case "diagnostic.error":
				v.Errors++
			case "diagnostic.warn":
				v.Warnings++
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
				v.Ghosts = append(v.Ghosts, Ghost{Pos: ed.End, Text: ed.Text, Style: "sub.new"})
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
					v.Labels = append(v.Labels, Label{Pos: m[1], Text: l})
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
						v.Colours = append(v.Colours, Colour{s, end, int32(rgb)})
					}
					continue
				}
				if d.style == "inlay" {
					if s >= top && s <= last {
						v.Ghosts = append(v.Ghosts, Ghost{Pos: s, Text: d.note, Style: "inlay"})
					}
					continue
				}
				if s < end && end > top && s < last {
					v.Decorations = append(v.Decorations, syntax.Span{Start: s, End: end, Name: d.style})
				}
				if strings.HasPrefix(d.style, "proposal") && d.note != "" && end >= top && end <= last {
					v.Ghosts = append(v.Ghosts, Ghost{Pos: end, Text: d.note, Stale: d.style == "proposal.stale"})
				}
			}
		}
		out = append(out, v)
	}
	return out
}

// maxContext is the most lines sticky context pins (B217).
const maxContext = 3

// contextRow names a line sticky context pins over w's rows, for the
// highlights of a line above what w shows.
type contextRow struct {
	w   *window
	row int
}

// pinned returns the starts of the lines pinned over w's first rows: those
// of the functions and classes that hold the first line they leave in view
// and start above it, the outermost first. They never cover the cursor's
// line (B217).
func (e *Session) pinned(w *window, t kernel.Snapshot, top int) []int {
	p, h := &w.pinned, w.buf.syntax
	if p.version == t.Version() && p.top == top && p.cursor == w.pos() && p.parses == h.Parses() {
		return p.starts
	}
	var starts []int
	first := t.LineOf(top)
	room := min(maxContext, t.LineOf(w.pos())-first, max(w.h-3, 0))
	// Each pinned line covers one more line of text, so the loop asks again
	// about the line under them until the answer stops changing.
	for range maxContext {
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
	p.version, p.top, p.cursor, p.parses, p.starts = t.Version(), top, w.pos(), h.Parses(), starts
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

// Sign is a letter in the sign column and the style it takes its colour
// from, such as diagnostic.error.
type Sign struct {
	Text, Name string
}

// diagnosticSigns are the diagnostic letters by severity, the worst first.
var diagnosticSigns = []Sign{{"E", "diagnostic.error"}, {"W", "diagnostic.warn"}, {"I", "diagnostic.info"}, {"H", "diagnostic.hint"}}

// signs returns each line of b's sign: the letter of its worst diagnostic,
// or else its git sign. A
// listing's lines from top to last show the git status of their names.
func (e *Session) signs(b *buffer, top, last int) map[int]Sign {
	if !b.signed && b.signs == 0 && len(b.git) == 0 && len(b.status) == 0 {
		return nil
	}
	t := b.text.Snapshot()
	out := maps.Clone(b.git)
	if out == nil {
		out = map[int]Sign{}
	}
	// A plugin's sign wins over git's, and a diagnostic over a plugin's
	// (B184).
	for _, p := range e.plugins {
		for _, d := range e.decos[p] {
			if d.buf == b && d.sign != "" {
				out[t.LineOf(b.text.Mark(d.start))] = Sign{d.sign, d.style}
			}
		}
	}
	for _, d := range e.diagnostics(b) {
		line := t.LineOf(b.text.Mark(d.start))
		i := slices.IndexFunc(diagnosticSigns, func(s Sign) bool { return s.Name == d.style })
		if old, ok := out[line]; i >= 0 && (!ok || !slices.Contains(diagnosticSigns, old) || i < slices.Index(diagnosticSigns, old)) {
			out[line] = diagnosticSigns[i]
		}
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
	col, vcol := c-ls+1, vim.Column(t, ls, c, tab)+1
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
func (e *Session) others(b *buffer) []Other {
	var out []Other
	for _, s := range e.sessions {
		if s == e || s.win.buf != b {
			continue
		}
		o := Other{Name: s.label(), Colour: s.colour, Cursor: s.win.pos()}
		if sel, ok := s.Selection(); ok {
			o.Selection = &sel
		}
		out = append(out, o)
	}
	return out
}
