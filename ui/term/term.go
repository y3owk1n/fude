// Package term is the terminal client. It draws an editor on a tcell screen
// and feeds it keys.
package term

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// Run shows e in the terminal until the user quits.
func Run(e *editor.Session) error {
	// Negotiation waits on a terminal round trip, so fude turns it off and
	// asks for the kitty keyboard protocol without querying (D26).
	s, err := tcell.NewScreen(
		tcell.OptNegotiation(false),
		tcell.OptKeyboardProtocol(tcell.KittyKeyboard),
		tcell.OptAdvancedKeys(true),
	)
	if err == nil {
		err = s.Init()
	}
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	defer s.Fini()
	s.EnablePaste()
	s.EnableFocus()

	c := client{s: s, e: e}
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

// colours are the styles of highlight captures. A capture with no entry takes
// the one for its name without its last part, so function.builtin falls back
// to function. Palette colours follow the terminal's theme.
var colours = map[string]tcell.Style{
	"comment": tcell.StyleDefault.Foreground(color.Gray),
	"keyword": tcell.StyleDefault.Foreground(color.Purple),
	// Markdown in floats, such as hover.
	"markup.strong":  tcell.StyleDefault.Bold(true),
	"markup.italic":  tcell.StyleDefault.Italic(true),
	"markup.heading": tcell.StyleDefault.Bold(true),
	"markup.raw":     tcell.StyleDefault.Foreground(color.Teal),
	"markup.link":    tcell.StyleDefault.Underline(true),
	"markup.rule":    tcell.StyleDefault.Foreground(color.Gray),
	"menu.selected":  tcell.StyleDefault.Reverse(true),
	// What a proposal would replace.
	"proposal":       tcell.StyleDefault.Foreground(color.Gray).StrikeThrough(true),
	"proposal.stale": tcell.StyleDefault.Foreground(color.Red).StrikeThrough(true),
	"string":         tcell.StyleDefault.Foreground(color.Green),
	"escape":         tcell.StyleDefault.Foreground(color.Teal),
	"number":         tcell.StyleDefault.Foreground(color.Olive),
	"constant":       tcell.StyleDefault.Foreground(color.Olive),
	"function":       tcell.StyleDefault.Foreground(color.Navy),
	"type":           tcell.StyleDefault.Foreground(color.Teal),
}

// underlines are the colours diagnostics underline their text in, over its
// other colours.
var underlines = map[string]color.Color{
	"diagnostic.error": color.Red, "diagnostic.warn": color.Yellow,
	"diagnostic.info": color.Blue, "diagnostic.hint": color.Aqua,
}

// paint returns the style of each byte from top that a highlight colours.
func paint(spans []syntax.Span, top int) []tcell.Style {
	var out []tcell.Style
	for _, s := range spans {
		if s.End <= top {
			continue
		}
		if n := s.End - top; n > len(out) {
			out = append(out, make([]tcell.Style, n-len(out))...)
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

var visualLabels = map[vim.Key]string{"v": "-- VISUAL --", "V": "-- VISUAL LINE --", "<C-v>": "-- VISUAL BLOCK --"}

type client struct {
	s tcell.Screen
	e *editor.Session
}

func (c *client) draw() {
	c.s.Clear()
	w, h := c.s.Size()
	c.e.SetSize(w, h-1)
	sel, visual := c.e.Selection()

	cx, cy := 0, 0
	for _, v := range c.e.Views() {
		if x, y, ok := c.view(v, sel, visual && v.Current); ok {
			cx, cy = x, y
		}
	}

	if f, ok := c.e.Float(); ok {
		if x, y, ok := c.float(f, cx, cy, w, h-1); ok {
			cx, cy = x, y
		}
	}

	shape := tcell.CursorStyleSteadyBlock
	switch c.e.Mode() {
	case vim.Insert:
		shape = tcell.CursorStyleSteadyBar
		c.s.PutStr(0, h-1, "-- INSERT --")
	case vim.Replace:
		shape = tcell.CursorStyleSteadyUnderline
		c.s.PutStr(0, h-1, "-- REPLACE --")
	case vim.Visual:
		c.s.PutStr(0, h-1, visualLabels[sel.Kind])
	case vim.Cmdline:
		c.s.PutStr(0, h-1, c.e.Line())
		cx, cy = len(c.e.Line()), h-1
	default:
		msg := c.e.Message()
		if r := c.e.Recording(); r != 0 && msg == "" {
			msg = "recording @" + string(r)
		}
		c.s.PutStr(0, h-1, msg)
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
	style := tcell.StyleDefault
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
	// The text goes right of the line numbers.
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
		starts := vim.Rows(t, pos, v.W)
		col := 0
		for i, start := range starts {
			if row >= v.H-1 {
				break
			}
			y, stop := v.Y+row, end
			if i == 0 && v.Gutter > 0 {
				c.number(t.LineOf(pos), cur, numbers, y, v)
			}
			if i+1 < len(starts) {
				stop = starts[i+1]
			}
			x := 0
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
				style := tcell.StyleDefault
				if i := at - v.Top; i < len(paint) {
					style = paint[i]
				}
				for _, mt := range matches {
					if at >= mt[0] && at < mt[1] {
						style = style.Background(color.Yellow).Foreground(color.Black)
					}
				}
				if visual && selected(sel, at, col) || slices.Contains(cursors, at) {
					// Reversing syntax colours would stripe the selection, so it
					// drops them.
					style = tcell.StyleDefault.Reverse(true)
				}
				if rest[0] == '\t' {
					w := vim.Tabstop - col%vim.Tabstop
					c.s.FillArea(v.X+x, y, min(w, v.W-x), 1, ' ', style)
					x, col, rest = x+w, col+w, rest[1:]
					continue
				}
				var width int
				rest, width = c.s.Put(v.X+x, y, rest, style)
				x, col = x+width, col+width
			}
			if i == len(starts)-1 && slices.Contains(cursors, end) && x < v.W {
				c.s.Put(v.X+x, y, " ", tcell.StyleDefault.Reverse(true))
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
		c.s.Put(numbers, v.Y+row, "~", tcell.StyleDefault.Foreground(color.Gray))
	}
	// The separator and status line span the whole window.
	v.X, v.W = numbers, v.W+v.Gutter
	if v.X > 0 {
		for y := v.Y; y < v.Y+v.H; y++ {
			c.s.Put(v.X-1, y, "│", tcell.StyleDefault)
		}
	}
	status := " " + v.Name
	if v.Modified {
		status += " [+]"
	}
	if r := []rune(status); len(r) > v.W {
		// Cut the start of a long name, so the end and [+] stay in view.
		status = "<" + string(r[len(r)-v.W+1:])
	}
	style := tcell.StyleDefault.Reverse(true).Bold(v.Current)
	c.s.FillArea(v.X, v.Y+v.H-1, v.W, 1, ' ', style)
	c.s.PutStrStyled(v.X, v.Y+v.H-1, status, style)
	// Plugins' segments, the keys being typed and the ruler go on the right
	// when they fit beside the name (B38).
	shown := ""
	if v.Current {
		shown = c.e.Shown()
	}
	right := fmt.Sprintf("%-11s%s ", shown, v.Ruler)
	if len(v.Segments) > 0 {
		right = strings.Join(v.Segments, "  ") + "  " + right
	}
	if len([]rune(status))+len([]rune(right)) < v.W {
		c.s.PutStrStyled(v.X+v.W-len([]rune(right)), v.Y+v.H-1, right, style)
	}
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
	text := fmt.Sprintf("%*d ", v.Gutter-1, n)
	if v.Relative && v.Number && line == cur {
		text = fmt.Sprintf("%-*d ", v.Gutter-1, line+1)
	}
	c.s.PutStrStyled(x, y, text, tcell.StyleDefault.Foreground(color.Gray))
}

// ghostText shows a line break and a tab in a proposal's text as one cell
// each.
var ghostText = strings.NewReplacer("\n", "⏎", "\t", " ")

// ghost draws g's text from column x of a window at left that is w wide,
// and returns the column after it.
func (c *client) ghost(g editor.Ghost, left, x, y, w int) int {
	style := tcell.StyleDefault.Foreground(color.Gray).Italic(true)
	if g.Stale {
		style = style.Foreground(color.Red)
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
