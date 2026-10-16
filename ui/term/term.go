// Package term is the terminal client. It draws an editor on a tcell screen
// and feeds it keys.
package term

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
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

	c := &client{s: s, e: e}
	c.draw()
	// Plugins load after the first frame, so they never delay it (B56).
	e.Load()
	var paste strings.Builder
	pasting := false
	var timeout, hints <-chan time.Time
	for {
		// While a frame shows a spinner, draw again a frame later to turn it.
		var tick <-chan time.Time
		if c.spinning {
			tick = time.After(spinnerFrame)
		}
		var ev tcell.Event
		select {
		case ev = <-s.EventQ():
		case <-tick:
		case <-timeout:
			timeout = nil
			e.Timeout()
		case <-hints:
			hints = nil
			e.ShowHints()
		case f := <-e.Queue():
			f()
			drain(e, func() bool { return len(s.EventQ()) > 0 })
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
		// A key starts both waits again. Other events, such as a spinner's
		// frame, leave them running.
		_, key := ev.(*tcell.EventKey)
		if d, ok := e.Wait(); !ok {
			timeout = nil
		} else if timeout == nil || key {
			timeout = time.After(d)
		}
		if d, ok := e.HintsIn(); !ok {
			hints = nil
		} else if hints == nil || key {
			hints = time.After(d)
		}
		// A key waiting draws its own frame, which shows this too.
		if len(s.EventQ()) > 0 {
			continue
		}
		c.draw()
	}
}

// drain runs the work queued for the loop that is waiting now, so a burst
// of it, such as a server's answers, draws one frame. It stops when a key
// is waiting, which goes first.
func drain(e *editor.Session, key func() bool) {
	for !key() {
		select {
		case f := <-e.Queue():
			f()
		default:
			return
		}
	}
}

// selected reports whether the byte at, drawn at column x, is in sel.
func selected(sel vim.Selection, at, x int) bool {
	if len(sel.Regions) > 0 {
		return slices.ContainsFunc(sel.Regions, func(r [2]int) bool { return at >= r[0] && at < r[1] })
	}
	return at >= sel.Start && at < sel.End && (!sel.Block || x >= sel.Left && x <= sel.Right)
}

// paint returns the style of each byte in [top, end) that a highlight
// colours, and the base style for the bytes between them. It reuses out.
func paint(out []tcell.Style, spans []syntax.Span, top, end int) []tcell.Style {
	n := max(end-top, 0)
	out = slices.Grow(out[:0], n)[:n]
	for i := range out {
		out[i] = base
	}
	for _, s := range spans {
		s.Start, s.End = max(s.Start, top), min(s.End, end)
		if s.Start >= s.End {
			continue
		}
		if c, ok := underlines[s.Name]; ok {
			for i := s.Start - top; i < s.End-top; i++ {
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
		for i := s.Start - top; i < s.End-top; i++ {
			out[i] = style
		}
	}
	return out
}

type client struct {
	s        tcell.Screen
	e        *editor.Session
	theme    string            // the theme drawn in, as e names it
	colours  *[16]int32        // its base16 scheme's colours, if it is one
	spinning bool              // the last frame shows a spinner
	styles   []tcell.Style     // a view's, kept between frames so painting them allocates nothing
	frame    int               // counts the frames drawn
	lines    map[[2]int]*shown // the windows' lines drawn, by the cell each starts at
	overlaid bool              // the last frame drew a box, the command line or a message over the windows
	size     [4]int            // the screen's size, and where the windows started and ended, in the last frame
	sig      sig               // reused to build each line's sig
}

// shown is what a frame drew a window's line from. When the next frame
// would draw the line from the same, it leaves the line's rows as they are.
type shown struct {
	frame  int
	sig    sig
	styles []tcell.Style
}

// sig holds what a line draws from, as bytes to compare.
type sig []byte

func (k sig) int(n int) sig    { return binary.AppendVarint(k, int64(n)) }
func (k sig) str(s string) sig { return append(k.int(len(s)), s...) }

func (k sig) flag(b bool) sig {
	if b {
		return k.int(1)
	}
	return k.int(0)
}

func (c *client) draw() {
	at := time.Now()
	defer func() { api.Trace("frame", "drawn", time.Since(at)) }()
	c.frame++
	c.spinning = false
	fresh := c.lines == nil
	// The theme e names may change at any time with :theme (B158).
	if name, colours := c.e.Theme(); name != c.theme || colours != c.colours {
		fresh = true
		c.theme, c.colours = name, colours
		t := themes[name]
		if colours != nil {
			t = fromBase16(*colours)
		}
		useTheme(t)
	}
	// The windows take every row: the command line floats and messages
	// are toasts (B148, B149).
	w, h := c.s.Size()
	c.e.SetSize(w, h)
	sel, visual := c.e.Selection()

	// A modal box, the command line or the finder, has the windows under it
	// drawn in the tinted theme, and draws on the tinted ground (B151).
	f, floats := c.e.Float()
	sig, signs := c.e.Signature()
	msg := c.e.Message()
	// A :s being typed shows its preview in the windows, so they stay lit.
	modal := c.e.Mode() == vim.Cmdline && !c.e.Substituting() || floats && f.W > 0 && !f.Lit
	if modal {
		apply(dimmed)
		defer apply(lit)
	}
	// No line's sig holds what draws over the windows, so every line draws
	// while something is over them and in the frame after.
	over := c.e.Mode() == vim.Cmdline || floats || signs || msg != ""
	// The windows cover from left to right, which changes when :center does.
	views := c.e.Views()
	left, right := w, 0
	for _, v := range views {
		left, right = min(left, v.X), max(right, v.X+v.W)
	}
	if fresh || over || c.overlaid || c.size != [4]int{w, h, left, right} {
		c.s.Fill(' ', base)
		c.lines = map[[2]int]*shown{}
	}
	c.overlaid, c.size = over, [4]int{w, h, left, right}
	cx, cy := 0, 0
	for _, v := range views {
		if x, y, ok := c.view(v, left, sel, visual && v.Current); ok {
			cx, cy = x, y
		}
	}
	if modal {
		apply(lit)
		base = base.Background(dimmed.th.bg)
	}
	// The signature goes above the cursor's line, and a completion menu below
	// it draws over it if they meet (B203).
	if signs {
		c.above(sig, cx, cy, w, h)
	}
	switch {
	case c.e.Mode() == vim.Cmdline:
		// The completion menu is part of the command line's box.
		if !floats {
			f = editor.Float{}
		}
		cx, cy = c.commandLine(w, h, f.Text, f.Spans)
	case floats:
		if x, y, ok := c.float(f, cx, cy, w, h); ok {
			cx, cy = x, y
		}
	}
	if msg != "" {
		c.toast(msg, left, right, h)
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
			rows, _ := wrap(doc, f.DocSpans, f.DocW-4)
			c.box(doc, f.DocSpans, rows[:min(len(rows), f.H-2)], f.X+f.W, f.Y, f.DocW-4, -1)
		}
		rows, _ := wrap(f.Text, f.Spans, f.W-4)
		first := scroll(rows, f, f.H-2)
		x, y, ok := c.box(f.Text, f.Spans, rows[first:min(first+f.H-2, len(rows))], f.X, f.Y, f.W-4, f.Cursor)
		if f.Title != "" {
			c.s.PutStrStyled(f.X+2, f.Y, " "+f.Title+" ", base.Foreground(th.muted))
		}
		return x, y, ok
	}
	rows, width := wrap(f.Text, f.Spans, min(80, w-4))
	below, above := h-cy-1, cy
	n := min(len(rows), max(below, above)-2, 20)
	if width < 1 || n < 1 {
		return 0, 0, false
	}
	first := scroll(rows, f, n)
	y := cy + 1
	if below < n+2 && above > below {
		y = cy - n - 2
	}
	x := min(cx, max(w-width-4, 0))
	fx, fy, found := c.box(f.Text, f.Spans, rows[first:min(first+n, len(rows))], x, y, width, f.Cursor)
	// The documentation goes right of the box, or left of it when there is
	// more room there.
	right, left := w-x-width-4, x
	if room := max(right, left) - 4; f.Doc != "" && room >= 10 {
		rows, dw := wrap(f.Doc, f.DocSpans, min(80, room))
		dx := x + width + 4
		if right < left {
			dx = x - dw - 4
		}
		c.box(f.Doc, f.DocSpans, rows[:min(len(rows), h-y-2, 20)], dx, y, dw, -1)
	}
	return fx, fy, found
}

// above draws f in a box above the cursor's line at cx and cy, at most 80
// columns and 10 rows. When it does not fit there and there is more room
// below, it goes below. w and h are the space the windows have.
func (c *client) above(f editor.Float, cx, cy, w, h int) {
	rows, width := wrap(f.Text, f.Spans, min(80, w-4))
	want, up, down := min(len(rows), 10), cy-2, h-cy-3
	n, y := min(want, up), cy-min(want, up)-2
	if up < want && down > up {
		n, y = min(want, down), cy+1
	}
	if width < 1 || n < 1 {
		return
	}
	c.box(f.Text, f.Spans, rows[:n], min(cx, max(w-width-4, 0)), y, width, -1)
}

// scroll returns the first of rows to show in a box n rows tall. It is f's
// top row, moved so the cursor's row shows when f has the cursor. A menu
// without the cursor shows its selected item (B64).
func scroll(rows []row, f editor.Float, n int) int {
	// rowOf is the row that shows the byte at p.
	rowOf := func(p int) int {
		i, _ := slices.BinarySearchFunc(rows, p, func(r row, p int) int { return cmp.Compare(r.start, p+1) })
		return max(i-1, 0)
	}
	if f.Cursor < 0 {
		i := slices.IndexFunc(f.Spans, func(s syntax.Span) bool { return s.Name == "menu.selected" })
		if i < 0 {
			return 0
		}
		return max(rowOf(f.Spans[i].Start)-n+1, 0)
	}
	cur := rowOf(f.Cursor)
	return min(max(rowOf(f.Top), cur-n+1), cur)
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

// box draws rows of text, styled by spans, in a panel at x and y, width
// columns wide inside it, and reports where the byte at cursor is. A row a
// selected item starts takes the raised colour, with an accent bar for its
// left border (B150).
func (c *client) box(text string, spans []syntax.Span, rows []row, x, y, width, cursor int) (int, int, bool) {
	styles := paint(nil, spans, 0, len(text))
	style := base
	fx, fy, found := 0, 0, false
	// The text has a column of room on either side inside the border.
	c.panel(x, y, width+2, len(rows))
	x++
	for i, r := range rows {
		ground := base.GetBackground()
		if r.rule {
			c.rule(x-1, y+1+i, width+2)
		}
		if slices.ContainsFunc(spans, func(s syntax.Span) bool {
			return s.Name == "menu.selected" && s.Start <= r.start && r.start < s.End
		}) {
			ground = th.raised
			c.s.FillArea(x, y+1+i, width+2, 1, ' ', style.Background(ground))
			c.s.Put(x-1, y+1+i, "▌", base.Foreground(th.normal))
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
			s = s.Background(ground)
			if rest[0] == '\t' {
				col, rest = col+4, rest[1:]
				continue
			}
			next, cw := c.s.Put(x+1+col, y+1+i, rest, s)
			// Put draws nothing off the screen, so the row stops there.
			if len(next) == len(rest) {
				break
			}
			rest, col = next, col+cw
		}
	}
	return fx, fy, found
}

// view draws one window: its text, a status line on its bottom row, and a
// separator on its left when another window is beside it. left is the
// column the windows start at, which has none. A line keeps its
// rows when the last frame drew it from the same, unless it holds the
// cursor. For the current window it reports where the cursor is.
func (c *client) view(v editor.View, left int, sel vim.Selection, visual bool) (int, int, bool) {
	if v.Cells != nil {
		return c.cells(v, left)
	}
	t := v.Text
	// The text goes right of the sign column and the line numbers.
	numbers, cur := v.X, t.LineOf(v.Cursor)
	v.X, v.W = v.X+v.Gutter, v.W-v.Gutter
	cx, cy, found := 0, 0, false
	pos := v.Top
	// The rows show at most one line each from the top's.
	end := vim.LineEnd(t, t.LineStart(min(t.LineOf(v.Top)+v.H, t.Lines()-1))) + 1
	c.styles = paint(c.styles, append(v.Highlights, v.Decorations...), v.Top, end)
	paint := c.styles
	// Text that names a colour shows on it, in black or white, whichever
	// reads better (B208).
	for _, col := range v.Colours {
		r, g, b := col.RGB>>16&0xff, col.RGB>>8&0xff, col.RGB&0xff
		ink := color.Black
		if r*299+g*587+b*114 < 128000 {
			ink = color.White
		}
		for i := max(col.Start, v.Top) - v.Top; i < min(col.End, end)-v.Top && i < len(paint); i++ {
			paint[i] = paint[i].Background(color.NewHexColor(col.RGB)).Foreground(ink)
		}
	}
	var cursors []int
	if v.Current {
		cursors = c.e.Cursors()
	}
	// win holds what every line of the window draws from.
	win := sig(nil).int(v.X).int(v.Y).int(v.W).int(v.Left).flag(v.Wrap).flag(v.List).int(v.ColorColumn)
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
	row := 0
	for row < v.H-1 {
		end, line := vim.LineEnd(t, pos), t.LineOf(pos)
		var matches [][2]int
		for _, mt := range v.Matches {
			if mt[1] > pos && mt[0] <= end {
				matches = append(matches, mt)
			}
		}
		// A line wraps onto as many rows as it needs (B37).
		starts := []int{pos}
		if v.Wrap {
			starts = vim.Rows(t, pos, v.W)
		}
		starts = starts[:min(len(starts), v.H-1-row)]
		// The gutter draws in every frame, as relative numbers change with
		// the cursor's line.
		c.s.FillArea(numbers, v.Y+row, v.Gutter, len(starts), ' ', base)
		if s, ok := v.Signs[line]; ok {
			c.s.PutStrStyled(numbers, v.Y+row, s.Text, base.Foreground(signColours[s.Name]))
		}
		if s, ok := v.Icons[line]; ok {
			c.s.PutStrStyled(v.X-2, v.Y+row, s.Text, base.Foreground(signColours[s.Name]))
		}
		if v.Number || v.Relative {
			c.number(line, cur, numbers+v.SignCols, v.Y+row, v)
		}
		shade := v.CursorLine && line == cur
		lead := -1
		if v.Guides && !v.List {
			lead = indentOf(t, pos)
		}
		text := t.Slice(pos, end)
		// A tag starts each part of the sig, so two different lines never
		// read the same. Its positions count from the line's start, so a
		// line keeps its sig when text above it changes.
		k := append(c.sig[:0], win...).int(row).int(len(starts)).str(text).flag(shade).int(lead).str(v.Notes[line])
		for _, p := range v.Pair {
			if p >= pos && p <= end {
				k = k.int(1).int(p - pos)
			}
		}
		for _, mt := range matches {
			k = k.int(2).int(mt[0] - pos).int(mt[1] - pos)
		}
		for _, p := range cursors {
			if p >= pos && p <= end {
				k = k.int(3).int(p - pos)
			}
		}
		for _, g := range v.Ghosts {
			if g.Pos >= pos && g.Pos <= end {
				k = k.int(4).int(g.Pos - pos).str(g.Text).str(g.Style).flag(g.Stale)
			}
		}
		c.sig = k
		styles := paint[min(pos-v.Top, len(paint)):min(end-v.Top, len(paint))]
		at := [2]int{v.X, v.Y + row}
		last := c.lines[at]
		if last != nil && last.frame == c.frame-1 && bytes.Equal(last.sig, k) && slices.Equal(last.styles, styles) &&
			!(v.Current && v.Cursor >= pos && v.Cursor <= end) {
			last.frame = c.frame
		} else {
			if last == nil {
				last = &shown{}
				c.lines[at] = last
			}
			last.frame, last.sig, last.styles = c.frame, append(last.sig[:0], k...), append(last.styles[:0], styles...)
			c.s.FillArea(v.X, v.Y+row, v.W, len(starts), ' ', base)
			// trail is where the line's trailing spaces start. col is the column
			// in the line, which tabs are measured from.
			trail := pos + len(strings.TrimRight(text, " "))
			col := 0
			for i, start := range starts {
				y, stop := v.Y+row+i, end
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
					// The cursor's line shades only what has no ground of its own.
					if shade && style.GetBackground() == base.GetBackground() {
						style = style.Background(th.cursorLine)
					}
					if slices.Contains(v.Pair, at) {
						// A matched bracket is bold and underlined.
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
				if n := v.Notes[line]; i == len(starts)-1 && x >= 0 {
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
				if i == 0 && lead > 0 {
					c.guides(v, lead, y)
				}
				c.colorColumn(v, y)
				if i == len(starts)-1 && slices.Contains(cursors, end) && x < v.W {
					c.s.Put(v.X+x, y, " ", base.Background(th.selection))
				}
			}
		}
		row += len(starts)
		if end == t.Len() {
			break
		}
		pos = end + 1
	}
	// Rows past the end of the text show ~.
	for ; row < v.H-1; row++ {
		c.s.FillArea(numbers, v.Y+row, v.Gutter+v.W, 1, ' ', base)
		c.s.Put(numbers, v.Y+row, "~", base.Foreground(th.faint))
	}
	c.pin(v, numbers, cur)
	// The separator and status line span the whole window.
	v.X, v.W = numbers, v.W+v.Gutter
	c.edge(v, left)
	return cx, cy, found
}

// pin draws the lines of v.Context over v's first rows on the surface
// colour, each with its number and highlights, cut at the window's edge
// (B217). numbers is where the gutter starts.
func (c *client) pin(v editor.View, numbers, cur int) {
	t := v.Text
	for i, start := range v.Context {
		y, end := v.Y+i, vim.LineEnd(t, start)
		ground := base.Background(th.surface)
		c.s.FillArea(numbers, y, v.Gutter+v.W, 1, ' ', ground)
		if v.Number || v.Relative {
			c.number(t.LineOf(start), cur, numbers+v.SignCols, y, v)
			for x := numbers; x < v.X; x++ {
				if s, style, _ := c.s.Get(x, y); s != "" {
					c.s.Put(x, y, s, style.Background(th.surface))
				}
			}
		}
		styles := paint(nil, v.ContextHighlights, start, end)
		x, col := 0, 0
		for rest := t.Slice(start, end); rest != "" && x < v.W; {
			style := styles[len(styles)-len(rest)]
			if style.GetBackground() == base.GetBackground() {
				style = style.Background(th.surface)
			}
			if rest[0] == '\t' {
				w := vim.Tabstop - col%vim.Tabstop
				x, col, rest = x+w, col+w, rest[1:]
				continue
			}
			var width int
			rest, width = c.s.Put(v.X+x, y, rest, style)
			x, col = x+width, col+width
		}
	}
}

// edge draws v's status line on its bottom row, and a separator on its
// left unless the windows start there, at left.
func (c *client) edge(v editor.View, left int) {
	if v.X > left {
		for y := v.Y; y < v.Y+v.H; y++ {
			c.s.Put(v.X-1, y, "│", base.Foreground(th.border))
		}
	}
	c.status(v, v.X, v.W)
}

// colorColumn shades the colour column on row y of v's text, where the
// row's cell keeps the ground, so the cursor's line, a selection and a
// match keep theirs (B200).
func (c *client) colorColumn(v editor.View, y int) {
	x := v.ColorColumn - 1
	if !v.Wrap {
		x -= v.Left
	}
	if v.ColorColumn == 0 || x < 0 || x >= v.W {
		return
	}
	if s, style, _ := c.s.Get(v.X+x, y); s != "" && style.GetBackground() == base.GetBackground() {
		c.s.Put(v.X+x, y, s, style.Background(th.raised))
	}
}

// indentOf returns the columns of the leading blanks of the line at start.
// A blank line takes the indent of the next line with text, so the guides
// run through it (B153).
func indentOf(t kernel.Snapshot, start int) int {
	lead := 0
	// A long run of blank lines looks 200 lines ahead at most.
	for p, n := start, 0; n < 200; n++ {
		end := vim.LineEnd(t, p)
		text := t.Slice(p, end)
		if rest := strings.TrimLeft(text, " \t"); rest != "" {
			lead = vim.Column(t, p, p+len(text)-len(rest))
			break
		}
		if end == t.Len() {
			break
		}
		p = end + 1
	}
	return lead
}

// guides draws a faint line at each indent level in the first lead columns
// of a line, on row y.
func (c *client) guides(v editor.View, lead, y int) {
	left := 0
	if !v.Wrap {
		left = v.Left
	}
	for col := 0; col < lead; col += max(v.Indent, 1) {
		x := v.X + col - left
		if x < v.X || x >= v.X+v.W {
			continue
		}
		if s, style, _ := c.s.Get(x, y); s == " " {
			c.s.Put(x, y, "│", style.Foreground(th.border))
		}
	}
}

// number draws line's number in the gutter at x, y: from the cursor's
// line cur with relative numbers, and its own on that line when numbers are
// on too, put on the left (B39).
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
		// The cursor's line number stands out.
		style = base.Bold(true)
	}
	c.s.PutStrStyled(x, y, text, style)
}

// listChar returns what list shows in place of the character rest starts
// with: - for a trailing space and + for a
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
	switch {
	case g.Style != "":
		style = colours[g.Style]
	case g.Stale:
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
