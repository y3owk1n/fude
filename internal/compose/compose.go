// Package compose draws a window of a session into a grid of styled cells,
// for any frontend: its text, highlights, gutter, ghost text, notes, rows
// a decoration adds, pinned lines, guides and cursors (D63). A frontend
// paints the cells that changed.
package compose

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"

	look "github.com/y3owk1n/fude/internal/theme"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Composer composes windows. It reuses the buffers one window's lines took
// for the next window, so composing allocates little.
type Composer struct {
	lk     look.Look
	styles []look.Style
	sig    sig
}

// Options are what composing a window takes besides the window: the look,
// the frame being drawn, the session's mode, cursors and selection, and how
// the frontend shows the cursor.
type Options struct {
	Look      look.Look
	Frame     int
	Insert    bool
	Cursors   []int // the multicursors, for the current window
	Selection vim.Selection
	Visual    bool
	// Shape is the cursor's shape, as the frontend numbers shapes. Underline
	// is set when that shape is an underline, and multicursors then show as
	// one too.
	Shape     int
	Underline bool
	Tint      look.RGB // the multicursors' colour
	// OwnCursors is set when the frontend draws the multicursors itself, so
	// their cells keep their own style.
	OwnCursors bool
}

// named is the style of text called name, with no colours when the look has
// none.
func (p *Composer) named(name string) look.Style {
	if s, ok := p.lk.Names[name]; ok {
		return s
	}
	return look.Style{Fg: look.None, Bg: look.None}
}

func withFg(s look.Style, c look.RGB) look.Style { s.Fg = c; return s }
func withBg(s look.Style, c look.RGB) look.Style { s.Bg = c; return s }

// underlined is s underlined in c.
func underlined(s look.Style, c look.RGB) look.Style {
	s.Underline, s.Under, s.UnderSet = true, c, true
	return s
}

// presence returns the colour of other sessions numbered n (B253).
func (p *Composer) presence(n int) look.RGB {
	t := p.lk.Theme
	return [...]look.RGB{t.Insert, t.Visual, t.Cmd, t.Hint, t.Warn, t.Normal}[n%6]
}

// Composed is what composing a window gives besides its cells: where the
// cursor is, the multicursors drawn, the image rows, and the first row of
// each line a float shows. Each counts in the grid's cells.
type Composed struct {
	X, Y   int
	Found  bool // the cursor is in the window
	Marks  [][2]int
	Images []ImageRow
	Starts [][2]int // a float's rows that start a line, and the line's start
}

// ImageRow is a row of an image a window shows below or above Line, at row
// Y of the grid.
type ImageRow struct {
	Line int
	Row  wire.Virtual
	Y    int
}

// Compose draws v into g, its cells counted from the gutter's left and the
// window's top. A line keeps its cells when the last frame drew it from the
// same, unless it holds the cursor.
func (p *Composer) Compose(g *Grid, v wire.Window, o Options) Composed {
	p.lk = o.Look
	var out Composed
	sel, visual := o.Selection, o.Visual
	t := v.Text
	// The text goes right of the sign column and the line numbers.
	numbers, cur := 0, t.LineOf(v.Cursor)
	v.X, v.Y = v.Gutter, 0
	v.W -= v.Gutter
	insert := o.Insert
	pos := v.Top
	// The rows show at most one line each from the top's, a closed fold
	// taking one (B224). place is each line's place among them, for
	// relative numbers, which count a closed fold as one line.
	var place map[int]int
	if len(v.Folds) > 0 {
		place = map[int]int{}
		for i, last := 0, t.LineOf(v.Top); i <= v.H+1 && last < t.Lines(); i++ {
			place[last] = i
			last += 1 + v.Folds[last]
		}
	}
	p.styles = Styles(p.styles, append(v.Highlights, v.Decorations...), v.Ranges, p.lk)
	paint := p.styles
	// index returns where in paint the style of the byte at p is, or -1. A
	// line's bytes are in the range ri, whose styles start at ro in paint.
	ri, ro := 0, 0
	index := func(p int) int {
		if ri < len(v.Ranges) && p >= v.Ranges[ri].Start && p <= v.Ranges[ri].End {
			return ro + p - v.Ranges[ri].Start
		}
		return -1
	}
	// Text that names a colour shows on it, in black or white, whichever
	// reads better (B208).
	off := 0
	for _, rg := range v.Ranges {
		for _, col := range v.Colours {
			r, gr, b := col.RGB>>16&0xff, col.RGB>>8&0xff, col.RGB&0xff
			ink := look.RGB(0x000000)
			if r*299+gr*587+b*114 < 128000 {
				ink = 0xffffff
			}
			for p := max(col.Start, rg.Start); p < min(col.End, rg.End+1); p++ {
				paint[off+p-rg.Start] = withFg(withBg(paint[off+p-rg.Start], look.RGB(col.RGB)), ink)
			}
		}
		off += max(rg.End+1-rg.Start, 0)
	}
	cursors := o.Cursors
	base, th := p.lk.Base, p.lk.Theme
	// win holds what every line of the window draws from.
	win := sig(nil).int(v.X).int(v.W).int(v.Left).flag(v.Wrap).flag(v.BreakIndent).str(v.ShowBreak).flag(v.List).int(v.ColorColumn).int(v.CursorColumn).int(v.Tab).int(o.Shape).int(int(o.Tint)).flag(o.OwnCursors)
	list := v.ListChars
	var tabs []rune
	if v.List {
		tabs = []rune(list.Tab)
		win = win.str(list.Tab).str(list.Space).str(list.Lead).str(list.Trail).str(list.Nbsp).str(list.Eol).str(list.Extends).str(list.Precedes)
	}
	win = win.str(v.FillChars.Fold)
	// Pinned lines cover rows, so a change to them draws every row again.
	for _, p := range v.Context {
		win = win.int(p)
	}
	if visual {
		win = win.int(sel.Start).int(sel.End).flag(sel.Block).int(sel.Left).int(sel.Right)
		for _, r := range sel.Regions {
			win = win.int(r[0]).int(r[1])
		}
	}
	for _, o := range v.Others {
		win = win.int(o.Colour).int(o.Cursor)
		if s := o.Selection; s != nil {
			win = win.int(s.Start).int(s.End).flag(s.Block).int(s.Left).int(s.Right)
			for _, r := range s.Regions {
				win = win.int(r[0]).int(r[1])
			}
		}
	}
	in := wire.Inlines(v.Ghosts)
	row := 0
	for row < v.H {
		end, line := vim.LineEnd(t, pos), t.LineOf(pos)
		// The rows a decoration draws above a line come before it, and those
		// below after it (B299).
		if row = p.virtual(g, v, line, true, numbers, row, &out); row >= v.H {
			break
		}
		var matches [][2]int
		for _, mt := range v.Matches {
			if mt[1] > pos && mt[0] <= end {
				matches = append(matches, mt)
			}
		}
		for ri < len(v.Ranges)-1 && v.Ranges[ri].End < pos {
			ro += v.Ranges[ri].End + 1 - v.Ranges[ri].Start
			ri++
		}
		// With wrap the top line shows from its first row shown, where the
		// first range starts. startCol is the column that row starts at.
		from, startCol := pos, -1
		if v.Wrap && pos == v.Top && ri < len(v.Ranges) && v.Ranges[ri].Start > pos {
			from, startCol = v.Ranges[ri].Start, v.Ranges[ri].Col
		}
		// A line wraps onto as many rows as it needs, those after the first
		// hang columns in (B37, B489). The rows with no room are cut, and the
		// text shown ends where the first of them starts.
		starts, hang, room := []int{from}, 0, v.H-row
		if v.Wrap {
			hang = vim.Hang(t, pos, v.W, v.Tab, v.BreakIndent, v.ShowBreak)
			starts = vim.RowsFrom(t, pos, from, max(startCol, 0), room+1, math.MaxInt, vim.Wrap{Cols: v.W, Hang: hang, Tab: v.Tab, In: in, Words: v.Float != "" || v.LineBreak}, nil)
		}
		shownStart, shownEnd, lastRow := from, end, len(starts)-1
		if len(starts) > room {
			shownEnd, lastRow, starts = starts[room], -1, starts[:room]
		}
		// Without wrap the row starts at the first cluster v.Left columns
		// into the line, and a long line shows only its range, the part in
		// view.
		switch {
		case v.Wrap:
		case ri < len(v.Ranges) && (v.Ranges[ri].Start > pos || v.Ranges[ri].End < end):
			shownStart, shownEnd, startCol = v.Ranges[ri].Start, v.Ranges[ri].End, v.Ranges[ri].Col
		default:
			shownStart = vim.ColumnStart(t, pos, v.Left, v.Tab, in)
		}
		// The gutter draws in every frame, as relative numbers change with
		// the cursor's line.
		g.FillArea(numbers, v.Y+row, v.Gutter, len(starts), ' ', base)
		if s, ok := v.Signs[line]; ok {
			g.putClipped(numbers, v.Y+row, g.w, s.Text, withFg(base, p.lk.Signs[s.Name]))
		}
		if s, ok := v.Icons[line]; ok {
			g.putClipped(v.X-2, v.Y+row, g.w, s.Text, withFg(base, p.lk.Signs[s.Name]))
		}
		if v.Number || v.Relative {
			rel := max(line-cur, cur-line)
			if at, ok := place[cur]; ok {
				rel = max(place[line]-at, at-place[line])
			}
			p.number(g, line, rel, line == cur, numbers+v.SignCols, v.Y+row, v)
		}
		shade := v.CursorLine && line == cur
		// A line decoration's ground shades the line's rows, in place of the
		// cursor line's (B296).
		ground := base
		if name, ok := v.Grounds[line]; ok {
			ground, shade = withBg(base, p.named(name).Bg), false
		}
		if v.Float != "" {
			out.Starts = append(out.Starts, [2]int{row, pos})
		}
		var guides []int
		if !v.List {
			guides = v.Guides[line]
		}
		text := t.Slice(shownStart, shownEnd)
		// A tag starts each part of the sig, so two different lines never
		// read the same. Its positions count from the line's start, so a
		// line keeps its sig when text above it changes.
		k := append(p.sig[:0], win...).int(row).int(len(starts)).int(shownStart - pos).str(text).flag(shade).ints(guides).str(v.Notes[line].Text).str(v.Notes[line].Name).int(v.Folds[line]).str(v.Grounds[line])
		for _, p := range v.Pair {
			if p >= pos && p <= end {
				k = k.int(1).int(p - pos)
			}
		}
		for _, mt := range matches {
			k = k.int(2).int(mt[0] - pos).int(mt[1] - pos)
		}
		if s := v.Searched; s[1] > pos && s[0] <= end {
			k = k.int(4).int(s[0] - pos).int(s[1] - pos)
		}
		for _, p := range cursors {
			if p >= pos && p <= end {
				k = k.int(3).int(p - pos)
			}
		}
		for _, gh := range v.Ghosts {
			if gh.Pos >= pos && gh.Pos <= end {
				k = k.int(6).int(gh.Pos - pos).int(gh.End - pos).str(gh.Text).str(gh.Style).flag(gh.Stale)
			}
		}
		p.sig = k
		var styles []look.Style
		if i := index(shownStart); i >= 0 {
			styles = paint[i:min(i+shownEnd-shownStart, len(paint))]
		}
		last := g.lines[row]
		if last != nil && last.frame == o.Frame-1 && bytes.Equal(last.sig, k) && slices.Equal(last.styles, styles) &&
			!(v.Current && v.Cursor >= pos && v.Cursor <= end) {
			last.frame = o.Frame
		} else {
			if last == nil {
				last = &shown{}
				g.lines[row] = last
			}
			last.frame, last.sig, last.styles, last.marks = o.Frame, append(last.sig[:0], k...), append(last.styles[:0], styles...), last.marks[:0]
			g.FillArea(v.X, v.Y+row, v.W, len(starts), ' ', ground)
			// trail is where the line's trailing spaces start, and lead where its
			// leading blanks end. col is the column in the line, which tabs are
			// measured from.
			trail := trailing(t, pos, end)
			lead := leading(t, pos, end)
			col := max(startCol, 0)
			for i, start := range starts {
				y, stop := v.Y+row+i, shownEnd
				if i+1 < len(starts) {
					stop = starts[i+1]
				}
				x := 0
				if i > 0 || start > pos {
					x = hang
					bg := ground
					if shade {
						bg = withBg(base, th.CursorLine)
					}
					g.FillArea(v.X, y, hang, 1, ' ', bg)
					g.putClipped(v.X+max(hang-displaywidth.String(v.ShowBreak), 0), y, v.X+hang, v.ShowBreak, withFg(bg, th.Faint))
				}
				// Without wrap the row starts after blanks for a wide cluster cut
				// by the edge.
				if !v.Wrap {
					start, col = shownStart, startCol
					if col < 0 {
						col = vim.Column(t, pos, start, v.Tab, nil)
					}
					x = col - v.Left
					if len(in) > 0 {
						x = vim.Column(t, pos, start, v.Tab, in) - v.Left
					}
				}
				for rest := t.Slice(start, stop); ; {
					at := stop - len(rest)
					// In insert mode the cursor shows before a ghost at it, where
					// typing goes. Else it shows on the character after (B384, B216).
					cursor := v.Current && at == v.Cursor && (rest != "" || stop == end)
					if cursor && insert {
						out.X, out.Y, out.Found = v.X+min(x+v.Past, v.W-1), y, true
					}
					// A ghost at a row's end draws at the next row's start, where
					// vim.Rows puts it. At the line's end it draws after the text.
					hidden := at
					if rest != "" || stop == end {
						x, hidden = p.ghosts(g, v, at, stop, x, y, ground.Bg)
					}
					if hidden > at {
						// A replacement's text shows in place of the text it hides
						// (B295).
						col = vim.Column(t, pos, hidden, v.Tab, nil)
						rest = t.Slice(hidden, stop)
						continue
					}
					if cursor && !insert {
						out.X, out.Y, out.Found = v.X+min(x+v.Past, v.W-1), y, true
					}
					if rest != "" && x >= v.W && v.List && list.Extends != "" {
						g.Put(v.X+v.W-1, y, list.Extends, withFg(ground, th.Faint))
					}
					if rest == "" || x >= v.W {
						break
					}
					style := base
					if i := index(at); i >= 0 && i < len(paint) {
						style = paint[i]
					}
					if style.Bg == base.Bg {
						style = withBg(style, ground.Bg)
					}
					// The cursor's line shades only what has no ground of its own.
					if shade && style.Bg == base.Bg {
						style = withBg(style, th.CursorLine)
					}
					if slices.Contains(v.Pair, at) {
						// A matched bracket is bold and underlined.
						style.Bold, style.Underline = true, true
					}
					for _, mt := range matches {
						if at >= mt[0] && at < mt[1] {
							style = withFg(withBg(style, th.Search), th.Bg)
						}
					}
					// The match the search being typed goes to stands out (B21).
					if s := v.Searched; at >= s[0] && at < s[1] {
						style = withFg(withBg(style, th.Number), th.Bg)
					}
					if visual && selected(sel, at, col) {
						// Reversing syntax colours would stripe the selection, so it
						// drops them.
						style = withBg(base, th.Selection)
					}
					if slices.Contains(cursors, at) {
						style = p.multicursor(style, base, o)
						last.marks = append(last.marks, [2]int{v.X + x, y})
					}
					// Other sessions' selections draw underlined, and their
					// cursors draw as cells, in their colours (B253).
					for _, o := range v.Others {
						if o.Selection != nil && selected(*o.Selection, at, col) {
							style = underlined(style, p.presence(o.Colour))
						}
						if at == o.Cursor {
							style = withFg(withBg(base, p.presence(o.Colour)), th.Bg)
						}
					}
					if rest[0] == '\t' {
						w := v.Tab - col%v.Tab
						g.FillArea(v.X+x, y, min(w, v.W-x), 1, ' ', style)
						// [list_chars] tab gives the first cell, the ones after
						// and, with a third character, the last (B486).
						for j := 0; len(tabs) > 0 && j < min(w, v.W-x); j++ {
							mark := tabs[min(j, 1)]
							if len(tabs) == 3 && j == w-1 {
								mark = tabs[2]
							}
							g.Put(v.X+x+j, y, string(mark), withFg(style, th.Faint))
						}
						x, col, rest = x+w, col+w, rest[1:]
						continue
					}
					if Control(rest[0]) {
						// A terminal would act on the byte, so it shows as ^ and a
						// letter (B540).
						g.Put(v.X+x, y, "^", withFg(style, th.Faint))
						if x+1 < v.W {
							g.Put(v.X+x+1, y, string(rest[0]^0x40), withFg(style, th.Faint))
						}
						x, col, rest = x+2, col+2, rest[1:]
						continue
					}
					if v.List {
						if mark, ok := listChar(list, rest, at < lead, at >= trail); ok {
							// The mark takes the place of the one-column character.
							g.Put(v.X+x, y, mark, withFg(style, th.Faint))
							_, size := utf8.DecodeRuneInString(rest)
							x, col, rest = x+1, col+1, rest[size:]
							continue
						}
					}
					var width int
					rest, width = g.Put(v.X+x, y, rest, style)
					x, col = x+width, col+width
				}
				if v.List && list.Precedes != "" && !v.Wrap && start > pos {
					g.Put(v.X, y, list.Precedes, withFg(ground, th.Faint))
				}
				if v.List && list.Eol != "" && i == lastRow && x < v.W {
					g.Put(v.X+x, y, list.Eol, withFg(ground, th.Faint))
					x++
				}
				// A closed fold's first line says how many lines it hides.
				if n := v.Folds[line]; n > 0 && i == lastRow && x < v.W {
					s := fmt.Sprintf("  ⋯ %d lines ", n)
					if n == 1 {
						s = "  ⋯ 1 line "
					}
					x = g.putClipped(v.X+x, y, v.X+v.W, s, withBg(withFg(base, th.Muted), th.Surface)) - v.X
					for ; v.FillChars.Fold != "" && x < v.W; x++ {
						g.Put(v.X+x, y, v.FillChars.Fold, withFg(base, th.Muted))
					}
				}
				if n := v.Notes[line]; i == lastRow && x >= 0 && n.Text != "" {
					x = p.note(g, v, x, y, n, shade)
				}
				if shade && x < v.W {
					g.FillArea(v.X+x, y, v.W-x, 1, ' ', withBg(base, th.CursorLine))
				}
				if ground != base && x < v.W {
					g.FillArea(v.X+x, y, v.W-x, 1, ' ', ground)
				}
				if i == 0 {
					p.guides(g, v, guides, y)
				}
				p.colorColumn(g, v, y)
				p.shade(g, v, v.CursorColumn-1, y, th.CursorLine)
				if i == lastRow && slices.Contains(cursors, end) && x < v.W {
					g.Put(v.X+x, y, " ", p.multicursor(base, base, o))
					last.marks = append(last.marks, [2]int{v.X + x, y})
				}
				for _, o := range v.Others {
					if i == lastRow && o.Cursor == end && x < v.W {
						g.Put(v.X+x, y, " ", withBg(base, p.presence(o.Colour)))
					}
				}
			}
		}
		out.Marks = append(out.Marks, last.marks...)
		row = p.virtual(g, v, line, false, numbers, row+len(starts), &out)
		// A closed fold's other lines do not show.
		if n := v.Folds[line]; n > 0 {
			if line+n+1 >= t.Lines() {
				break
			}
			end = vim.LineEnd(t, t.LineStart(line+n))
		}
		if end == t.Len() {
			break
		}
		pos = end + 1
	}
	// Rows past the end of the text show ~, and a float's show nothing.
	for ; row < v.H; row++ {
		g.FillArea(numbers, v.Y+row, v.Gutter+v.W, 1, ' ', base)
		if v.Float == "" {
			g.Put(numbers, v.Y+row, cmp.Or(v.FillChars.Eob, " "), withFg(base, th.Faint))
		}
	}
	p.pin(g, v, numbers, cur)
	return out
}

// multicursor is style for a cell under a multicursor, in the mode's
// colour. Where the frontend draws cursors of its own it stays as it
// is. Elsewhere an underline shows as one, and a block or a bar, which a
// cell cannot draw, as a block (B401).
func (p *Composer) multicursor(style, base look.Style, o Options) look.Style {
	switch {
	case o.OwnCursors:
		return style
	case o.Underline:
		return underlined(style, o.Tint)
	}
	return withFg(withBg(base, o.Tint), p.lk.Theme.Bg)
}

// virtual draws the rows v draws above line, or below it, from row, each
// cut at the window's edge, and returns the row after them. An image's rows
// go to out, for the frontend to show.
func (p *Composer) virtual(g *Grid, v wire.Window, line int, above bool, numbers, row int, out *Composed) int {
	for _, r := range v.Virtual[line] {
		if r.Above != above || row >= v.H {
			continue
		}
		y := v.Y + row
		g.FillArea(numbers, y, v.Gutter+v.W, 1, ' ', p.lk.Base)
		if r.Image != 0 {
			out.Images = append(out.Images, ImageRow{line, r, y})
		} else {
			g.putClipped(v.X, y, v.X+v.W, expandTabs(r.Text, v.Tab), p.named(r.Style))
		}
		row++
	}
	return row
}

// pin draws the lines of v.Context over v's first rows on the surface
// colour, each with its number and highlights, cut at the window's edge
// (B217). numbers is where the gutter starts.
func (p *Composer) pin(g *Grid, v wire.Window, numbers, cur int) {
	t := v.Text
	base, th := p.lk.Base, p.lk.Theme
	for i, start := range v.Context {
		y, end := v.Y+i, vim.LineEnd(t, start)
		ground := withBg(base, th.Surface)
		if name, ok := v.Grounds[t.LineOf(start)]; ok {
			ground = withBg(base, p.named(name).Bg)
		}
		g.FillArea(numbers, y, v.Gutter+v.W, 1, ' ', ground)
		if v.Number || v.Relative {
			line := t.LineOf(start)
			p.number(g, line, max(line-cur, cur-line), false, numbers+v.SignCols, y, v)
			for x := numbers; x < v.X; x++ {
				if s, style, _ := g.Get(x, y); s != "" {
					g.Put(x, y, s, withBg(style, th.Surface))
				}
			}
		}
		styles := Styles(nil, v.ContextHighlights, []wire.Range{{Start: start, End: end - 1}}, p.lk)
		x, col := 0, 0
		for rest := t.Slice(start, end); x < v.W; {
			at := end - len(rest)
			var hidden int
			if x, hidden = p.ghosts(g, v, at, end, x, y, ground.Bg); hidden > at {
				col = vim.Column(t, start, hidden, v.Tab, nil)
				rest = t.Slice(hidden, end)
				continue
			}
			if rest == "" {
				break
			}
			style := styles[len(styles)-len(rest)]
			if style.Bg == base.Bg {
				style = withBg(style, ground.Bg)
			}
			if rest[0] == '\t' {
				w := v.Tab - col%v.Tab
				x, col, rest = x+w, col+w, rest[1:]
				continue
			}
			var width int
			rest, width = g.Put(v.X+x, y, rest, style)
			x, col = x+width, col+width
		}
	}
}

// colorColumn shades the colour column on row y of v's text (B200).
func (p *Composer) colorColumn(g *Grid, v wire.Window, y int) {
	x := v.ColorColumn - 1
	if !v.Wrap {
		x -= v.Left
	}
	if v.ColorColumn > 0 {
		p.shade(g, v, x, y, p.lk.Theme.Raised)
	}
}

// shade puts bg under the cell at column x of row y of v's text, where
// the cell keeps the ground, so the cursor's line, a selection and a match
// keep theirs (B200, B483).
func (p *Composer) shade(g *Grid, v wire.Window, x, y int, bg look.RGB) {
	if x < 0 || x >= v.W {
		return
	}
	if s, style, _ := g.Get(v.X+x, y); s != "" && style.Bg == p.lk.Base.Bg {
		g.Put(v.X+x, y, s, withBg(style, bg))
	}
}

// guides draws a faint line at each of a line's guide columns, on row
// y, where the cell is blank (B153).
func (p *Composer) guides(g *Grid, v wire.Window, cols []int, y int) {
	left := 0
	if !v.Wrap {
		left = v.Left
	}
	for _, col := range cols {
		x := v.X + col - left
		if x < v.X || x >= v.X+v.W {
			continue
		}
		if s, style, _ := g.Get(x, y); s == " " {
			g.Put(x, y, "│", withFg(style, p.lk.Theme.Border))
		}
	}
}

// number draws line's number in the gutter at x, y: from the cursor's
// line cur with relative numbers, and its own on that line when numbers are
// on too, put on the left (B39).
func (p *Composer) number(g *Grid, line, rel int, onCursor bool, x, y int, v wire.Window) {
	n := line + 1
	if v.Relative {
		n = rel
	}
	w := v.Gutter - v.SignCols - 1
	text := fmt.Sprintf("%*d ", w, n)
	if v.Relative && v.Number && onCursor {
		text = fmt.Sprintf("%-*d ", w, line+1)
	}
	style := withFg(p.lk.Base, p.lk.Theme.Faint)
	if v.CursorLine && onCursor {
		// The cursor's line number stands out.
		style = p.lk.Base
		style.Bold = true
	}
	g.putClipped(x, y, g.w, text, style)
}

// ghosts draws v's ghosts at position at, from column x of row y, on bg.
// It returns the column after them and the end of the text they hide, at
// least at and at most to.
func (p *Composer) ghosts(g *Grid, v wire.Window, at, to, x, y int, bg look.RGB) (int, int) {
	hidden := at
	for _, gh := range v.Ghosts {
		if gh.Pos == at {
			x = p.ghost(g, gh, v.X, x, y, v.W, bg)
			hidden = max(hidden, min(gh.End, to))
		}
	}
	return x, hidden
}

// ghost draws gh's text from column x of a window at left that is w wide,
// on bg where its style has no ground of its own, and returns the column
// after it.
func (p *Composer) ghost(g *Grid, gh wire.Ghost, left, x, y, w int, bg look.RGB) int {
	style := withFg(p.lk.Base, p.lk.Theme.Faint)
	style.Italic = true
	switch {
	case gh.Style != "":
		style = p.named(gh.Style)
	case gh.Stale:
		style = withFg(style, p.lk.Theme.Error)
	}
	if style.Bg == p.lk.Base.Bg {
		style = withBg(style, bg)
	}
	return g.putClipped(left+x, y, left+w, vim.InlineText(gh.Text), style) - left
}

// note draws n after the end of a line of v, at column x of row y, and
// returns the column after it. A diagnostic's shows after a gap, on its
// severity's tint in its colour, as a label of its own. Any other is dim
// (B61, B184).
func (p *Composer) note(g *Grid, v wire.Window, x, y int, n wire.Note, shade bool) int {
	gap := p.lk.Base
	if shade {
		gap = withBg(gap, p.lk.Theme.CursorLine)
	}
	if style, ok := p.lk.Names[n.Name+".note"]; ok {
		x = g.putClipped(v.X+x, y, v.X+v.W, "  ", gap) - v.X
		return g.putClipped(v.X+x, y, v.X+v.W, " "+n.Text+" ", style) - v.X
	}
	return g.putClipped(v.X+x, y, v.X+v.W, n.Text, withFg(gap, p.lk.Theme.Faint)) - v.X
}

// Styles returns the style of each byte of the ranges, one range after
// another and each with the byte after it, that a highlight colours in lk,
// and lk's base style for the bytes between them. It reuses out.
func Styles(out []look.Style, spans []wire.Span, ranges []wire.Range, lk look.Look) []look.Style {
	n := 0
	for _, r := range ranges {
		n += max(r.End+1-r.Start, 0)
	}
	out = slices.Grow(out[:0], n)[:n]
	for i := range out {
		out[i] = lk.Base
	}
	off := 0
	for _, r := range ranges {
		top, end := r.Start, r.End+1
		for _, s := range spans {
			s.Start, s.End = max(s.Start, top), min(s.End, end)
			if s.Start >= s.End {
				continue
			}
			if c, ok := lk.Underlines[s.Name]; ok {
				for i := off + s.Start - top; i < off+s.End-top; i++ {
					out[i] = underlined(out[i], c)
				}
				continue
			}
			style, ok := lk.Name(s.Name)
			if !ok {
				continue
			}
			for i := off + s.Start - top; i < off+s.End-top; i++ {
				out[i] = style
			}
		}
		off += max(end-top, 0)
	}
	return out
}

// FloatRule reports whether the line at pos of v, a float, is a
// markup.rule one, which the float's border draws across.
func FloatRule(v wire.Window, pos int) bool {
	rule := func(s wire.Span) bool { return s.Name == "markup.rule" && s.Start == pos }
	return slices.ContainsFunc(v.Highlights, rule) || slices.ContainsFunc(v.Decorations, rule)
}

// selected reports whether the byte at, drawn at column x, is in sel.
func selected(sel vim.Selection, at, x int) bool {
	if len(sel.Regions) > 0 {
		return slices.ContainsFunc(sel.Regions, func(r [2]int) bool { return at >= r[0] && at < r[1] })
	}
	return at >= sel.Start && at < sel.End && (!sel.Block || x >= sel.Left && x <= sel.Right)
}

// trailing returns where the spaces that end the line from pos to end
// start, reading back from end.
func trailing(t vim.Text, pos, end int) int {
	for end > pos {
		lo := max(end-256, pos)
		if n := len(strings.TrimRight(t.Slice(lo, end), " ")); n > 0 {
			return lo + n
		}
		end = lo
	}
	return pos
}

// leading returns where the blanks that start the line from pos to end
// end, reading on from pos.
func leading(t vim.Text, pos, end int) int {
	for ; pos < end; pos += 256 {
		s := t.Slice(pos, min(pos+256, end))
		if n := len(s) - len(strings.TrimLeft(s, " \t")); n < len(s) {
			return pos + n
		}
	}
	return end
}

// expandTabs returns s with each tab as the spaces to the next tab stop,
// every tab columns, as the text's own tabs draw.
func expandTabs(s string, tab int) string {
	var b strings.Builder
	col := 0
	for {
		i := strings.IndexByte(s, '\t')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		col += displaywidth.String(s[:i])
		n := tab - col%tab
		b.WriteString(s[:i])
		b.WriteString(strings.Repeat(" ", n))
		col, s = col+n, s[i+1:]
	}
}

// listChar returns what list shows in place of the character rest starts
// with, from [list_chars]: trail for a trailing space, lead for a leading
// one, else space, and nbsp for a no-break space (B118, B486).
func listChar(list wire.ListChars, rest string, leading, trailing bool) (string, bool) {
	var mark string
	switch {
	case rest[0] == ' ' && trailing:
		mark = cmp.Or(list.Trail, list.Space)
	case rest[0] == ' ' && leading:
		mark = cmp.Or(list.Lead, list.Space)
	case rest[0] == ' ':
		mark = list.Space
	case strings.HasPrefix(rest, "\u00a0"):
		mark = list.Nbsp
	}
	return mark, mark != ""
}

// shown is what a frame drew a window's line from. When the next frame
// would draw the line from the same, it leaves the line's rows as they are.
type shown struct {
	frame  int
	sig    sig
	styles []look.Style
	marks  [][2]int // the cells of the multicursors drawn
}

// sig holds what a line draws from, as bytes to compare.
type sig []byte

func (k sig) int(n int) sig { return binary.AppendVarint(k, int64(n)) }

func (k sig) str(s string) sig { return append(k.int(len(s)), s...) }

func (k sig) ints(ns []int) sig {
	k = k.int(len(ns))
	for _, n := range ns {
		k = k.int(n)
	}
	return k
}

func (k sig) flag(b bool) sig {
	if b {
		return k.int(1)
	}
	return k.int(0)
}

// Control reports whether b is a control character other than a tab.
func Control(b byte) bool { return b < ' ' && b != '\t' || b == 0x7f }
