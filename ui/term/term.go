// Package term is the terminal client. It draws an editor on a tcell screen
// and feeds it keys.
package term

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// Run shows e in the terminal until the user quits.
func Run(e *editor.Session) error {
	tty, err := tcell.NewDevTty()
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	s, err := screen(tty)
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	defer s.Fini()
	s.EnablePaste()
	s.EnableFocus()

	c := newClient(s, e)
	c.draw()
	// Plugins load after the first frame, so they never delay it (B56).
	e.Load()
	var paste strings.Builder
	pasting := false
	var timeout <-chan time.Time
	for {
		var ev tcell.Event
		select {
		case ev = <-s.EventQ():
		case <-timeout:
			e.Timeout()
		case f := <-e.Queue():
			f()
		}
		switch ev := ev.(type) {
		case *tcell.EventResize:
			s.Sync()
		case *tcell.EventFocus:
			if ev.Focused {
				e.Focus()
			}
		case *tcell.EventPaste:
			pasting = ev.Start()
			if pasting {
				paste.Reset()
			} else {
				e.Paste(paste.String())
			}
		case *tcell.EventKey:
			switch {
			case !ev.Pressed(): // a key release
			case pasting:
				paste.WriteString(text(ev))
			default:
				for _, k := range keys(ev) {
					e.Feed(k)
				}
			}
		}
		if e.Done() {
			return nil
		}
		timeout = nil
		if d, ok := e.Wait(); ok {
			timeout = time.After(d)
		}
		c.draw()
	}
}

// selected reports whether the byte at, drawn at column x, is in sel.
func selected(sel vim.Selection, at, x int) bool {
	if len(sel.Regions) > 0 {
		return slices.ContainsFunc(sel.Regions, func(r [2]int) bool { return at >= r[0] && at < r[1] })
	}
	return at >= sel.Start && at < sel.End && (!sel.Block || x >= sel.Left && x <= sel.Right)
}

// paint returns the style of each byte from top that a highlight colours,
// and the base style for the bytes between them.
func paint(spans []syntax.Span, top int) []tcell.Style {
	var out []tcell.Style
	for _, s := range spans {
		if s.End <= top {
			continue
		}
		for len(out) < s.End-top {
			out = append(out, base)
		}
		if c, ok := underlines[s.Name]; ok {
			for i := max(s.Start-top, 0); i < s.End-top; i++ {
				out[i] = out[i].Underline(true, c)
			}
			continue
		}
		style, ok := colours[s.Name]
		for name := s.Name; !ok && strings.Contains(name, "."); {
			name = name[:strings.LastIndexByte(name, '.')]
			style, ok = colours[name]
		}
		if !ok {
			continue
		}
		for i := max(s.Start-top, 0); i < s.End-top; i++ {
			out[i] = style
		}
	}
	return out
}

type client struct {
	s tcell.Screen
	e *editor.Session
}

// newClient draws e on s, in the theme e's config names, or dark for a name
// it does not know.
func newClient(s tcell.Screen, e *editor.Session) *client {
	name, colours := e.Theme()
	t, ok := themes[name]
	switch {
	case colours != nil:
		t = fromBase16(*colours)
	case !ok:
		t = themes["dark"]
	}
	useTheme(t)
	return &client{s: s, e: e}
}

func (c *client) draw() {
	c.s.Fill(' ', base)
	// The windows take every row: the command line floats and messages
	// are toasts (B148, B149).
	w, h := c.s.Size()
	c.e.SetSize(w, h)
	sel, visual := c.e.Selection()

	cx, cy := 0, 0
	for _, v := range c.e.Views() {
		if x, y, ok := c.view(v, sel, visual && v.Current); ok {
			cx, cy = x, y
		}
	}

	f, floats := c.e.Float()
	switch {
	case c.e.Mode() == vim.Cmdline:
		var left, bottom int
		cx, cy, left, bottom = c.commandLine(w, h)
		if floats {
			// The completion menu goes under the command line.
			c.float(f, left, bottom, w, h)
		}
	case floats:
		if x, y, ok := c.float(f, cx, cy, w, h); ok {
			cx, cy = x, y
		}
	}
	if msg := c.e.Message(); msg != "" {
		c.toast(msg, w)
	}

	shape := tcell.CursorStyleSteadyBlock
	switch c.e.Mode() {
	case vim.Insert:
		shape = tcell.CursorStyleSteadyBar
	case vim.Replace:
		shape = tcell.CursorStyleSteadyUnderline
	}
	if text, ok := c.e.TerminalClipboard(); ok {
		c.s.SetClipboard([]byte(text))
	}
	c.s.SetCursorStyle(shape)
	c.s.ShowCursor(cx, cy)
	c.s.Show()
}

// float draws f in a bordered box at the cursor, cx and cy, below its line,
// or above it when there is more room there, at most 80 columns and 20 rows.
// Its documentation goes in a box beside it. w and h are the space the
// windows have. While f has the cursor, its rows scroll to show it, and
// float reports where it is.
func (c *client) float(f editor.Float, cx, cy, w, h int) (int, int, bool) {
	if f.W > 2 && f.H > 2 {
		if f.DocW > 2 {
			// The documentation box is as tall as the box beside it.
			doc := f.Doc + strings.Repeat("\n", max(f.H-3-strings.Count(f.Doc, "\n"), 0))
			rows, _ := wrap(doc, f.DocSpans, f.DocW-2)
			c.box(doc, f.DocSpans, rows[:min(len(rows), f.H-2)], f.X+f.W, f.Y, f.DocW-2, -1)
		}
		rows, _ := wrap(f.Text, f.Spans, f.W-2)
		return c.box(f.Text, f.Spans, rows[:min(len(rows), f.H-2)], f.X, f.Y, f.W-2, f.Cursor)
	}
	rows, width := wrap(f.Text, f.Spans, min(80, w-2))
	below, above := h-cy-1, cy
	n := min(len(rows), max(below, above)-2, 20)
	if width < 1 || n < 1 {
		return 0, 0, false
	}
	// rowOf is the row that shows the byte at p.
	rowOf := func(p int) int {
		i, _ := slices.BinarySearchFunc(rows, p, func(r row, p int) int { return cmp.Compare(r.start, p+1) })
		return max(i-1, 0)
	}
	first := 0
	if f.Cursor >= 0 {
		cur := rowOf(f.Cursor)
		first = min(max(rowOf(f.Top), cur-n+1), cur)
	}
	y := cy + 1
	if below < n+2 && above > below {
		y = cy - n - 2
	}
	x := min(cx, max(w-width-2, 0))
	fx, fy, found := c.box(f.Text, f.Spans, rows[first:min(first+n, len(rows))], x, y, width, f.Cursor)
	// The documentation goes right of the box, or left of it when there is
	// more room there.
	right, left := w-x-width-2, x
	if room := max(right, left) - 2; f.Doc != "" && room >= 10 {
		rows, dw := wrap(f.Doc, f.DocSpans, min(80, room))
		dx := x + width + 2
		if right < left {
			dx = x - dw - 2
		}
		c.box(f.Doc, f.DocSpans, rows[:min(len(rows), h-y-2, 20)], dx, y, dw, -1)
	}
	return fx, fy, found
}

// row is one row of a box: bytes of its text, or a rule across it.
type row struct {
	start, end int
	rule       bool
}

// wrap splits text into rows at most limit columns wide and returns them
// with the widest one's width. A row ends after the last space that fits,
// or at the limit when none does. A markup.rule line is a rule.
func wrap(text string, spans []syntax.Span, limit int) ([]row, int) {
	var rows []row
	width := 0
	for at := 0; at <= len(text); {
		end := strings.IndexByte(text[at:], '\n')
		if end < 0 {
			end = len(text) - at
		}
		end += at
		if slices.ContainsFunc(spans, func(s syntax.Span) bool { return s.Name == "markup.rule" && s.Start == at }) {
			rows = append(rows, row{at, end, true})
		} else {
			start, col := at, 0
			brk, brkCol := start, 0 // just after the row's last space, and its column
			for i, r := range text[at:end] {
				cw := 1
				if r == '\t' {
					cw = 4
				}
				if col+cw > limit {
					if brk == start {
						brk, brkCol = at+i, col
					}
					rows = append(rows, row{start, brk, false})
					start, col = brk, col-brkCol
				}
				col += cw
				width = max(width, col)
				if r == ' ' {
					brk, brkCol = at+i+1, col
				}
			}
			rows = append(rows, row{start, end, false})
		}
		at = end + 1
	}
	return rows, width
}

// box draws rows of text, styled by spans, in a border at x and y, width
// columns wide inside it, and reports where the byte at cursor is.
func (c *client) box(text string, spans []syntax.Span, rows []row, x, y, width, cursor int) (int, int, bool) {
	styles := paint(spans, 0)
	style := base
	fx, fy, found := 0, 0, false
	c.s.PutStrStyled(x, y, "┌"+strings.Repeat("─", width)+"┐", style)
	for i, r := range rows {
		c.s.FillArea(x, y+1+i, width+2, 1, ' ', style)
		c.s.Put(x, y+1+i, "│", style)
		if r.rule {
			c.s.PutStrStyled(x+1, y+1+i, strings.Repeat("─", width), colours["markup.rule"])
		}
		col := 0
		for rest := text[r.start:r.end]; ; {
			p := r.end - len(rest)
			// The end of a wrapped row is the start of the next one.
			if p == cursor && (rest != "" || i == len(rows)-1 || rows[i+1].start != p) {
				fx, fy, found = x+1+min(col, width-1), y+1+i, true
			}
			if r.rule || rest == "" {
				break
			}
			s := style
			if p < len(styles) {
				s = styles[p]
			}
			if rest[0] == '\t' {
				col, rest = col+4, rest[1:]
				continue
			}
			var cw int
			rest, cw = c.s.Put(x+1+col, y+1+i, rest, s)
			col += cw
		}
		c.s.Put(x+width+1, y+1+i, "│", style)
	}
	c.s.PutStrStyled(x, y+len(rows)+1, "└"+strings.Repeat("─", width)+"┘", style)
	return fx, fy, found
}

// view draws one window: its text, a status line on its bottom row, and a
// separator on its left when another window is beside it. For the current
// window it reports where the cursor is.
func (c *client) view(v editor.View, sel vim.Selection, visual bool) (int, int, bool) {
	t := v.Text
	// The text goes right of the sign column and the line numbers.
	numbers, cur := v.X, t.LineOf(v.Cursor)
	v.X, v.W = v.X+v.Gutter, v.W-v.Gutter
	cx, cy, found := 0, 0, false
	pos := v.Top
	paint := paint(append(v.Highlights, v.Decorations...), v.Top)
	var cursors []int
	if v.Current {
		cursors = c.e.Cursors()
	}
	row := 0
	for row < v.H-1 {
		end := vim.LineEnd(t, pos)
		var matches [][2]int
		for _, mt := range v.Matches {
			if mt[1] > pos && mt[0] <= end {
				matches = append(matches, mt)
			}
		}
		// A line wraps onto as many rows as it needs (B37). col is the
		// column in the line, which tabs are measured from.
		starts := []int{pos}
		if v.Wrap {
			starts = vim.Rows(t, pos, v.W)
		}
		col := 0
		for i, start := range starts {
			if row >= v.H-1 {
				break
			}
			y, stop := v.Y+row, end
			if s, ok := v.Signs[t.LineOf(pos)]; ok && i == 0 {
				c.s.PutStrStyled(numbers, y, s.Text, base.Foreground(signColours[s.Name]))
			}
			if s, ok := v.Icons[t.LineOf(pos)]; ok && i == 0 {
				c.s.PutStrStyled(v.X-2, y, s.Text, base.Foreground(signColours[s.Name]))
			}
			if i == 0 && (v.Number || v.Relative) {
				c.number(t.LineOf(pos), cur, numbers+v.SignCols, y, v)
			}
			shade := v.CursorLine && t.LineOf(pos) == cur
			// trail is where the line's trailing spaces start.
			trail := pos + len(strings.TrimRight(t.Slice(pos, end), " "))
			if i+1 < len(starts) {
				stop = starts[i+1]
			}
			// Without wrap the row starts at the first cluster v.Left columns
			// into the line, after blanks for a wide one cut by the edge.
			x := 0
			if !v.Wrap {
				start = vim.ColumnStart(t, pos, v.Left)
				col = vim.Column(t, pos, start)
				x = col - v.Left
			}
			for rest := t.Slice(start, stop); ; {
				at := stop - len(rest)
				for _, g := range v.Ghosts {
					if g.Pos == at {
						x = c.ghost(g, v.X, x, y, v.W)
					}
				}
				if v.Current && at == v.Cursor && (rest != "" || stop == end) {
					cx, cy, found = v.X+min(x, v.W-1), y, true
				}
				if rest == "" || x >= v.W {
					break
				}
				style := base
				if i := at - v.Top; i < len(paint) {
					style = paint[i]
				}
				if shade {
					style = style.Background(th.cursorLine)
				}
				if slices.Contains(v.Pair, at) {
					// As Neovim's MatchParen shows in a terminal.
					style = style.Bold(true).Underline(true)
				}
				for _, mt := range matches {
					if at >= mt[0] && at < mt[1] {
						style = style.Background(th.warn).Foreground(th.bg)
					}
				}
				if visual && selected(sel, at, col) || slices.Contains(cursors, at) {
					// Reversing syntax colours would stripe the selection, so it
					// drops them.
					style = base.Background(th.selection)
				}
				if rest[0] == '\t' {
					w := vim.Tabstop - col%vim.Tabstop
					c.s.FillArea(v.X+x, y, min(w, v.W-x), 1, ' ', style)
					if v.List {
						c.s.Put(v.X+x, y, ">", style.Foreground(th.faint))
					}
					x, col, rest = x+w, col+w, rest[1:]
					continue
				}
				if v.List {
					if mark, ok := listChar(rest, at >= trail); ok {
						// The mark takes the place of the one-column character.
						c.s.Put(v.X+x, y, mark, style.Foreground(th.faint))
						_, size := utf8.DecodeRuneInString(rest)
						x, col, rest = x+1, col+1, rest[size:]
						continue
					}
				}
				var width int
				rest, width = c.s.Put(v.X+x, y, rest, style)
				x, col = x+width, col+width
			}
			if n := v.Notes[t.LineOf(pos)]; i == len(starts)-1 && x >= 0 {
				style := base.Foreground(th.faint)
				if shade {
					style = style.Background(th.cursorLine)
				}
				for n != "" && x < v.W {
					var width int
					n, width = c.s.Put(v.X+x, y, n, style)
					x += width
				}
			}
			if shade && x < v.W {
				c.s.FillArea(v.X+x, y, v.W-x, 1, ' ', base.Background(th.cursorLine))
			}
			if i == len(starts)-1 && slices.Contains(cursors, end) && x < v.W {
				c.s.Put(v.X+x, y, " ", base.Background(th.selection))
			}
			row++
		}
		if end == t.Len() {
			break
		}
		pos = end + 1
	}
	// Rows past the end of the text show ~.
	for ; row < v.H-1; row++ {
		c.s.Put(numbers, v.Y+row, "~", base.Foreground(th.faint))
	}
	// The separator and status line span the whole window.
	v.X, v.W = numbers, v.W+v.Gutter
	if v.X > 0 {
		for y := v.Y; y < v.Y+v.H; y++ {
			c.s.Put(v.X-1, y, "│", base)
		}
	}
	c.status(v, v.X, v.W)
	return cx, cy, found
}

// number draws line's number in the gutter at x, y: from the cursor's
// line cur with relative numbers, and its own on that line when numbers are
// on too, put on the left as Neovim puts it (B39).
func (c *client) number(line, cur, x, y int, v editor.View) {
	n := line + 1
	if v.Relative {
		n = max(line-cur, cur-line)
	}
	w := v.Gutter - v.SignCols - 1
	text := fmt.Sprintf("%*d ", w, n)
	if v.Relative && v.Number && line == cur {
		text = fmt.Sprintf("%-*d ", w, line+1)
	}
	style := base.Foreground(th.faint)
	if v.CursorLine && line == cur {
		// The cursor's line number stands out, as Neovim's CursorLineNr.
		style = base.Bold(true)
	}
	c.s.PutStrStyled(x, y, text, style)
}

// listChar returns what list shows in place of the character rest starts
// with, Neovim's default listchars: - for a trailing space and + for a
// no-break space (B118).
func listChar(rest string, trailing bool) (string, bool) {
	switch {
	case rest[0] == ' ' && trailing:
		return "-", true
	case strings.HasPrefix(rest, "\u00a0"):
		return "+", true
	}
	return "", false
}

// ghostText shows a line break and a tab in a proposal's text as one cell
// each.
var ghostText = strings.NewReplacer("\n", "⏎", "\t", " ")

// ghost draws g's text from column x of a window at left that is w wide,
// and returns the column after it.
func (c *client) ghost(g editor.Ghost, left, x, y, w int) int {
	style := base.Foreground(th.faint).Italic(true)
	if g.Stale {
		style = style.Foreground(th.err)
	}
	for s := ghostText.Replace(g.Text); s != "" && x < w; {
		var width int
		s, width = c.s.Put(left+x, y, s, style)
		x += width
	}
	return x
}

// keys returns ev in Vim notation, or nothing for a key fude does not use
// yet. An Alt chord is <Esc> and the key, as a terminal without kitty sends
// it (D26).
func keys(ev *tcell.EventKey) []vim.Key {
	if ev.Key() == tcell.KeyBacktab || ev.Key() == tcell.KeyTab && ev.Modifiers()&tcell.ModShift != 0 {
		return []vim.Key{"<S-Tab>"}
	}
	if ev.Modifiers()&^tcell.ModShift == tcell.ModAlt {
		if k := key(ev.Key(), 0, ev.Str()); k != "" {
			return []vim.Key{"<Esc>", k}
		}
	}
	if k := key(ev.Key(), ev.Modifiers()&^tcell.ModShift, ev.Str()); k != "" {
		return []vim.Key{k}
	}
	return nil
}

func key(k tcell.Key, mods tcell.ModMask, str string) vim.Key {
	switch k {
	case tcell.KeyRune:
		switch {
		case mods == 0:
			return vim.Key(str)
		case mods == tcell.ModCtrl && (str == "6" || str == "^"):
			return "<C-^>"
		case mods == tcell.ModCtrl && len(str) == 1:
			return vim.Key("<C-" + strings.ToLower(str) + ">")
		}
	case tcell.KeyEnter:
		return "<CR>"
	case tcell.KeyEsc:
		return "<Esc>"
	case tcell.KeyBackspace:
		return "<BS>"
	case tcell.KeyLeft:
		return "<Left>"
	case tcell.KeyRight:
		return "<Right>"
	case tcell.KeyUp:
		return "<Up>"
	case tcell.KeyDown:
		return "<Down>"
	case tcell.KeyHome:
		return "<Home>"
	case tcell.KeyEnd:
		return "<End>"
	case tcell.KeyDelete:
		return "<Del>"
	case tcell.KeyPgUp:
		return "<PageUp>"
	case tcell.KeyPgDn:
		return "<PageDown>"
	case tcell.KeyTab:
		return "\t"
	}
	return ""
}

// text returns what a key inside a paste stands for.
func text(ev *tcell.EventKey) string {
	switch ev.Key() {
	case tcell.KeyEnter:
		return "\n"
	case tcell.KeyTab:
		return "\t"
	}
	return ev.Str()
}
