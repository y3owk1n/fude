// Package term is the terminal client. It draws an editor on a tcell screen
// and feeds it keys.
package term

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Run shows e in the terminal until the user quits.
func Run(e Session) error {
	tty, err := tcell.NewDevTty()
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	var extra atomic.Bool
	s, err := screen(tty, &extra)
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	defer s.Fini()
	s.EnablePaste()
	s.EnableFocus()

	c := &client{s: s, e: e, img: newImages(tty, s.Colors()), extra: &extra, out: tty}
	c.draw()
	// Plugins load after the first frame, so they never delay it (B56).
	e.Load()
	var paste strings.Builder
	pasting := false
	var timeout <-chan time.Time
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
		if e.Suspend() {
			if err := suspend(s); err != nil {
				return err
			}
			// The shell drew over the screen, so every line draws again.
			c.lines = nil
		}
		// A key starts both waits again. Other events, such as a spinner's
		// frame, leave them running.
		_, key := ev.(*tcell.EventKey)
		if d, ok := e.Wait(); !ok {
			timeout = nil
		} else if timeout == nil || key {
			timeout = time.After(d)
		}
		// A key waiting draws its own frame, which shows this too.
		if len(s.EventQ()) > 0 {
			continue
		}
		c.draw()
	}
}

// dirty reports whether a cell of s changed since s last showed. A screen
// that cannot tell counts as changed.
func dirty(s tcell.Screen) bool {
	cs, ok := s.(interface{ GetCells() *tcell.CellBuffer })
	if !ok {
		return true
	}
	cells := cs.GetCells()
	w, h := cells.Size()
	for y := range h {
		for x := range w {
			if cells.Dirty(x, y) {
				return true
			}
		}
	}
	return false
}

// drain runs the work queued for the loop that is waiting now, so a burst
// of it, such as a server's answers, draws one frame. It stops when a key
// is waiting, which goes first.
func drain(e Session, key func() bool) {
	for !key() {
		select {
		case f := <-e.Queue():
			f()
		default:
			return
		}
	}
}

// presence returns the colour of other sessions numbered n (B253).
func presence(n int) color.Color {
	return [...]color.Color{th.insert, th.visual, th.cmd, th.hint, th.warn, th.normal}[n%6]
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
	e        Session
	theme    string            // the theme drawn in, as e names it
	colours  *[16]int32        // its base16 scheme's colours, if it is one
	over     wire.Colors       // the colours [colors] puts in place of its own
	restyled int               // how many times the modules' styles had changed (B345)
	spinning bool              // the last frame shows a spinner
	styles   []tcell.Style     // a window's, kept between frames so painting them allocates nothing
	frame    int               // counts the frames drawn
	lines    map[[2]int]*shown // the windows' lines drawn, by the cell each starts at
	overlaid bool              // the last frame drew a box, the command line or a message over the windows
	size     [4]int            // the screen's size, and where the windows started and ended, in the last frame
	sig      sig               // reused to build each line's sig
	img      images            // how the client shows images (B303)
	menuTop  int               // the first row of the cursor's menu in the last frame
	cursor   [3]int            // where the last frame written put the cursor, and its shape
	shape    tcell.CursorStyle // the cursor's shape in the frame being drawn
	tint     color.Color       // the multicursors' colour then, the mode's
	// extra is set once the terminal says it draws cursors of its own, as
	// kitty's multiple cursors protocol does, which the client writes to out
	// at the cells in marks (B401).
	extra *atomic.Bool
	out   io.Writer
	marks [][2]int
	wrote string // the cursors last written to out
	title string // the terminal's title last set
}

// Kitty's multiple cursors protocol: clearCursors removes the terminal's
// extra cursors, and drawCursors starts the sequence that draws them in the
// main cursor's shape.
const (
	clearCursors = "\x1b[>0;4 q"
	drawCursors  = "\x1b[>29"
)

// shown is what a frame drew a window's line from. When the next frame
// would draw the line from the same, it leaves the line's rows as they are.
type shown struct {
	frame  int
	sig    sig
	styles []tcell.Style
	marks  [][2]int // the cells of the multicursors drawn
}

// sig holds what a line draws from, as bytes to compare.
type sig []byte

func (k sig) int(n int) sig    { return binary.AppendVarint(k, int64(n)) }
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

// cursorShape is the cursor's shape. It is an underline in replace mode,
// while an operator waits for its target and while r waits for its
// character, a bar in insert mode, and a block otherwise (B402).
func (c *client) cursorShape() tcell.CursorStyle {
	switch {
	case c.e.Mode() == vim.Insert:
		return tcell.CursorStyleSteadyBar
	case c.e.Mode() == vim.Replace, c.e.Pending():
		return tcell.CursorStyleSteadyUnderline
	}
	return tcell.CursorStyleSteadyBlock
}

// multicursor is style for a cell under a multicursor, in the mode's
// colour. Where the terminal draws cursors of its own it stays as it is.
// Elsewhere an underline shows as one, and a block or a bar, which a cell
// cannot draw, as a block (B401).
func (c *client) multicursor(style, base tcell.Style) tcell.Style {
	switch {
	case c.extra != nil && c.extra.Load():
		return style
	case c.shape == tcell.CursorStyleSteadyUnderline:
		return style.Underline(true, c.tint)
	}
	return base.Background(c.tint).Foreground(th.bg)
}

// showCursors has a terminal that draws cursors of its own draw one at
// each multicursor, in the shape of the cursor and the mode's colour, when
// they changed or the screen was cleared, which takes them off (B401).
func (c *client) showCursors(cleared bool) {
	if c.extra == nil || !c.extra.Load() || c.out == nil {
		return
	}
	var b strings.Builder
	b.WriteString(clearCursors)
	if len(c.marks) > 0 {
		r, g, bl := c.tint.RGB()
		fmt.Fprintf(&b, "\x1b[>40;2:%d:%d:%d q", r, g, bl)
		b.WriteString(drawCursors)
		for _, m := range c.marks {
			fmt.Fprintf(&b, ";2:%d:%d", m[1]+1, m[0]+1)
		}
		b.WriteString(" q")
	}
	if b.String() == c.wrote && !cleared {
		return
	}
	c.wrote = b.String()
	if _, err := io.WriteString(c.out, c.wrote); err != nil {
		trace.Record("cursors", err.Error(), 0)
	}
}

func (c *client) draw() {
	at := time.Now()
	defer func() { trace.Record("frame", "drawn", time.Since(at)) }()
	c.frame++
	c.spinning = false
	fresh := c.lines == nil
	// The theme e names may change at any time with :theme (B158).
	name, colours, given := c.e.Theme()
	if given == nil {
		given = &wire.Colors{}
	}
	own, restyled := c.e.Styles()
	if name != c.theme || colours != c.colours || *given != c.over || restyled != c.restyled {
		fresh = true
		c.theme, c.colours, c.over, c.restyled = name, colours, *given, restyled
		t := themes[name]
		if colours != nil {
			t = fromBase16(*colours)
		}
		useTheme(t.with(*given), own)
	}
	// The windows take every row: the command line floats and messages
	// are toasts (B148, B149).
	w, h := c.s.Size()
	c.e.SetSize(w, h)
	// A new size draws every cell again, over the images painted.
	if c.img.screen != [2]int{w, h} {
		c.img.screen, c.img.painted = [2]int{w, h}, nil
	}
	if cell := c.img.size(); cell != c.img.shown {
		c.img.shown = cell
		c.e.SetCell(cell[0], cell[1])
	}
	sel, visual := c.e.Selection()
	c.shape, c.marks = c.cursorShape(), c.marks[:0]
	_, c.tint = c.mode()

	// A modal box, the command line or the finder, has the windows under it
	// drawn in the tinted theme, and draws on the tinted ground (B151).
	f, floats := c.e.Menu()
	msg, ask := c.e.Message(), c.e.Confirm()
	wins := c.e.Windows()
	// A float in the middle of the screen dims the windows under it, as a
	// modal box does.
	middle, floated := false, false
	for _, v := range wins {
		middle, floated = middle || v.Float == "middle", floated || v.Float != ""
	}
	// A :s being typed shows its preview in the windows, so they stay lit.
	modal := c.e.Mode() == vim.Cmdline && !c.e.Substituting() || floats && f.W > 0 && !f.Lit || middle || ask != ""
	if modal {
		apply(dimmed)
		defer apply(lit)
	}
	// No line's sig holds what draws over the windows, so every line draws
	// while something is over them and in the frame after.
	over := c.e.Mode() == vim.Cmdline || floats || floated || msg != "" || ask != ""
	// The windows cover from left to right, which changes when :center does.
	left, right := w, 0
	for _, v := range wins {
		if v.Float == "" {
			left, right = min(left, v.X), max(right, v.X+v.W)
		}
	}
	// A new size clears the screen, and the terminal's own cursors with it.
	cleared := fresh || c.size[0] != w || c.size[1] != h
	if fresh || over || c.overlaid || c.size != [4]int{w, h, left, right} {
		c.s.Fill(' ', base)
		c.lines = map[[2]int]*shown{}
	}
	c.overlaid, c.size = over, [4]int{w, h, left, right}
	cx, cy := 0, 0
	for _, v := range wins {
		if v.Float != "" {
			continue
		}
		if x, y, ok := c.window(v, left, sel, visual && v.Current); ok {
			cx, cy = x, y
		}
	}
	c.bufferLine(c.e.Buffers(), w)
	if modal {
		apply(boxed)
	}
	// The floats draw over the layout, lit, and a completion menu draws over
	// signature help if they meet (B203).
	for _, v := range wins {
		if v.Float == "" {
			continue
		}
		if x, y, ok := c.floating(v, sel, visual && v.Current); ok {
			cx, cy = x, y
		}
	}
	switch {
	case c.e.Mode() == vim.Cmdline:
		// The completion menu is part of the command line's box.
		if !floats {
			f = wire.Menu{}
		}
		cx, cy = c.commandLine(w, h, f.Text, f.Spans)
	case floats:
		if x, y, ok := c.menu(f, cx, cy, left, right, h); ok {
			cx, cy = x, y
		}
	}
	if msg != "" {
		c.toast(msg, left, right, h)
	}
	if ask != "" {
		// The question takes every key, so no cursor shows.
		c.confirm(ask, w, h)
		cx, cy = -1, -1
	}

	shape := c.shape
	if text, ok := c.e.TerminalClipboard(); ok {
		c.s.SetClipboard([]byte(text))
	}
	// The screen puts the terminal's old title back as it ends (B480).
	if title := c.e.Title(); title != "" && title != c.title {
		c.s.SetTitle(title)
		c.title = title
	}
	c.s.SetCursorStyle(shape)
	c.s.ShowCursor(cx, cy)
	for _, id := range c.e.Freed() {
		c.img.free(id)
	}
	now := c.img.release(c.s)
	// The images go before the frame whose cells show them, and painted ones
	// after it, over its cells.
	if err := c.img.flush(); err != nil {
		trace.Record("image", err.Error(), 0)
	}
	// Skip a frame that changes no cell and leaves the cursor as it was. A
	// module's result often draws one.
	if cursor := [3]int{cx, cy, int(shape)}; cursor != c.cursor || dirty(c.s) {
		c.s.Show()
		c.cursor = cursor
	}
	c.showCursors(cleared)
	if err := c.img.paint(c.s, now); err != nil {
		trace.Record("image", err.Error(), 0)
	}
}

// window draws one window: its text, a status line on its bottom row, and a
// separator on its left when another window is beside it. left is the
// column the windows start at, which has none. A line keeps its
// rows when the last frame drew it from the same, unless it holds the
// cursor. For the current window it reports where the cursor is.
func (c *client) window(v wire.Window, left int, sel vim.Selection, visual bool) (int, int, bool) {
	if v.Cells != nil {
		return c.cells(v, left)
	}
	t := v.Text
	// The text goes right of the sign column and the line numbers.
	numbers, cur := v.X, t.LineOf(v.Cursor)
	v.X, v.W = v.X+v.Gutter, v.W-v.Gutter
	cx, cy, found := 0, 0, false
	pos := v.Top
	// The rows show at most one line each from the top's, a closed fold
	// taking one (B224). place is each line's place among them, for
	// relative numbers, which count a closed fold as one line.
	var place map[int]int
	last := t.LineOf(v.Top) + v.H
	if len(v.Folds) > 0 {
		place, last = map[int]int{}, t.LineOf(v.Top)
		for i := 0; i <= v.H && last < t.Lines(); i++ {
			place[last] = i
			last += 1 + v.Folds[last]
		}
	}
	end := vim.LineEnd(t, t.LineStart(min(last, t.Lines()-1))) + 1
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
	win := sig(nil).int(v.X).int(v.Y).int(v.W).int(v.Left).flag(v.Wrap).flag(v.List).int(v.ColorColumn).int(v.CursorColumn).int(v.Tab).int(int(c.shape)).int(int(c.tint.Hex())).flag(c.extra != nil && c.extra.Load())
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
	for row < v.H-1 {
		end, line := vim.LineEnd(t, pos), t.LineOf(pos)
		// The rows a decoration draws above a line come before it, and those
		// below after it (B299).
		if row = c.virtual(v, line, true, numbers, row); row >= v.H-1 {
			break
		}
		var matches [][2]int
		for _, mt := range v.Matches {
			if mt[1] > pos && mt[0] <= end {
				matches = append(matches, mt)
			}
		}
		// A line wraps onto as many rows as it needs (B37).
		starts := []int{pos}
		switch {
		case v.Wrap && (v.Float != "" || v.LineBreak):
			starts = vim.WordRows(t, pos, v.W, v.Tab, in)
		case v.Wrap:
			starts = vim.Rows(t, pos, v.W, v.Tab, in)
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
			rel := max(line-cur, cur-line)
			if at, ok := place[cur]; ok {
				rel = max(place[line]-at, at-place[line])
			}
			c.number(line, rel, line == cur, numbers+v.SignCols, v.Y+row, v)
		}
		shade := v.CursorLine && line == cur
		// A line decoration's ground shades the line's rows, in place of the
		// cursor line's (B296).
		ground := base
		if name, ok := v.Grounds[line]; ok {
			ground, shade = base.Background(colours[name].GetBackground()), false
		}
		var guides []int
		if !v.List {
			guides = v.Guides[line]
		}
		text := t.Slice(pos, end)
		// A tag starts each part of the sig, so two different lines never
		// read the same. Its positions count from the line's start, so a
		// line keeps its sig when text above it changes.
		k := append(c.sig[:0], win...).int(row).int(len(starts)).str(text).flag(shade).ints(guides).str(v.Notes[line].Text).str(v.Notes[line].Name).int(v.Folds[line]).str(v.Grounds[line])
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
		for _, g := range v.Ghosts {
			if g.Pos >= pos && g.Pos <= end {
				k = k.int(6).int(g.Pos - pos).int(g.End - pos).str(g.Text).str(g.Style).flag(g.Stale)
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
			last.frame, last.sig, last.styles, last.marks = c.frame, append(last.sig[:0], k...), append(last.styles[:0], styles...), last.marks[:0]
			c.s.FillArea(v.X, v.Y+row, v.W, len(starts), ' ', ground)
			// trail is where the line's trailing spaces start, and lead where its
			// leading blanks end. col is the column in the line, which tabs are
			// measured from.
			trail := pos + len(strings.TrimRight(text, " "))
			lead := end - len(strings.TrimLeft(text, " \t"))
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
					start = vim.ColumnStart(t, pos, v.Left, v.Tab, in)
					col = vim.Column(t, pos, start, v.Tab, nil)
					x = vim.Column(t, pos, start, v.Tab, in) - v.Left
				}
				for rest := t.Slice(start, stop); ; {
					at := stop - len(rest)
					// A ghost at a row's end draws at the next row's start, where
					// vim.Rows puts it. At the line's end it draws after the text.
					hidden := at
					if rest != "" || stop == end {
						x, hidden = c.ghosts(v, at, stop, x, y, ground.GetBackground())
					}
					if hidden > at {
						// A replacement's text shows in place of the text it hides
						// (B295).
						col = vim.Column(t, pos, hidden, v.Tab, nil)
						rest = t.Slice(hidden, stop)
						continue
					}
					if v.Current && at == v.Cursor && (rest != "" || stop == end) {
						cx, cy, found = v.X+min(x, v.W-1), y, true
					}
					if rest != "" && x >= v.W && v.List && list.Extends != "" {
						c.s.Put(v.X+v.W-1, y, list.Extends, ground.Foreground(th.faint))
					}
					if rest == "" || x >= v.W {
						break
					}
					style := base
					if i := at - v.Top; i < len(paint) {
						style = paint[i]
					}
					if style.GetBackground() == base.GetBackground() {
						style = style.Background(ground.GetBackground())
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
							style = style.Background(th.search).Foreground(th.bg)
						}
					}
					// The match the search being typed goes to stands out (B21).
					if s := v.Searched; at >= s[0] && at < s[1] {
						style = style.Background(th.number).Foreground(th.bg)
					}
					if visual && selected(sel, at, col) {
						// Reversing syntax colours would stripe the selection, so it
						// drops them.
						style = base.Background(th.selection)
					}
					if slices.Contains(cursors, at) {
						style = c.multicursor(style, base)
						last.marks = append(last.marks, [2]int{v.X + x, y})
					}
					// Other sessions' selections draw underlined, and their
					// cursors draw as cells, in their colours (B253).
					for _, o := range v.Others {
						if o.Selection != nil && selected(*o.Selection, at, col) {
							style = style.Underline(true, presence(o.Colour))
						}
						if at == o.Cursor {
							style = base.Background(presence(o.Colour)).Foreground(th.bg)
						}
					}
					if rest[0] == '\t' {
						w := v.Tab - col%v.Tab
						c.s.FillArea(v.X+x, y, min(w, v.W-x), 1, ' ', style)
						// [list_chars] tab gives the first cell, the ones after
						// and, with a third character, the last (B486).
						for j := 0; len(tabs) > 0 && j < min(w, v.W-x); j++ {
							mark := tabs[min(j, 1)]
							if len(tabs) == 3 && j == w-1 {
								mark = tabs[2]
							}
							c.s.Put(v.X+x+j, y, string(mark), style.Foreground(th.faint))
						}
						x, col, rest = x+w, col+w, rest[1:]
						continue
					}
					if v.List {
						if mark, ok := listChar(list, rest, at < lead, at >= trail); ok {
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
				if v.List && list.Precedes != "" && !v.Wrap && start > pos {
					c.s.Put(v.X, y, list.Precedes, ground.Foreground(th.faint))
				}
				if v.List && list.Eol != "" && i == len(starts)-1 && x < v.W {
					c.s.Put(v.X+x, y, list.Eol, ground.Foreground(th.faint))
					x++
				}
				// A closed fold's first line says how many lines it hides.
				if n := v.Folds[line]; n > 0 && i == len(starts)-1 && x < v.W {
					s := fmt.Sprintf("  ⋯ %d lines ", n)
					if n == 1 {
						s = "  ⋯ 1 line "
					}
					x = c.putClipped(v.X+x, y, v.X+v.W, part{s, base.Foreground(th.muted).Background(th.surface)}) - v.X
					for ; v.FillChars.Fold != "" && x < v.W; x++ {
						c.s.Put(v.X+x, y, v.FillChars.Fold, base.Foreground(th.muted))
					}
				}
				if n := v.Notes[line]; i == len(starts)-1 && x >= 0 && n.Text != "" {
					x = c.note(v, x, y, n, shade)
				}
				if shade && x < v.W {
					c.s.FillArea(v.X+x, y, v.W-x, 1, ' ', base.Background(th.cursorLine))
				}
				if ground != base && x < v.W {
					c.s.FillArea(v.X+x, y, v.W-x, 1, ' ', ground)
				}
				if i == 0 {
					c.guides(v, guides, y)
				}
				c.colorColumn(v, y)
				c.shade(v, v.CursorColumn-1, y, th.cursorLine)
				if i == len(starts)-1 && slices.Contains(cursors, end) && x < v.W {
					c.s.Put(v.X+x, y, " ", c.multicursor(base, base))
					last.marks = append(last.marks, [2]int{v.X + x, y})
				}
				for _, o := range v.Others {
					if i == len(starts)-1 && o.Cursor == end && x < v.W {
						c.s.Put(v.X+x, y, " ", base.Background(presence(o.Colour)))
					}
				}
			}
		}
		c.marks = append(c.marks, last.marks...)
		if v.Float != "" {
			c.floatRows(v, pos, row)
		}
		row = c.virtual(v, line, false, numbers, row+len(starts))
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
	for ; row < v.H-1; row++ {
		c.s.FillArea(numbers, v.Y+row, v.Gutter+v.W, 1, ' ', base)
		if v.Float == "" {
			c.s.Put(numbers, v.Y+row, cmp.Or(v.FillChars.Eob, " "), base.Foreground(th.faint))
		}
	}
	c.pin(v, numbers, cur)
	if v.Float != "" {
		return cx, cy, found
	}
	// The separator and status line span the whole window.
	v.X, v.W = numbers, v.W+v.Gutter
	c.edge(v, left)
	return cx, cy, found
}

// virtual draws the rows v draws above line, or below it, from row, each
// cut at the window's edge, and returns the row after them.
func (c *client) virtual(v wire.Window, line int, above bool, numbers, row int) int {
	for _, r := range v.Virtual[line] {
		if r.Above != above || row >= v.H-1 {
			continue
		}
		y := v.Y + row
		c.s.FillArea(numbers, y, v.Gutter+v.W, 1, ' ', base)
		if r.Image != 0 {
			c.image(v, line, r, y)
		} else {
			c.putClipped(v.X, y, v.X+v.W, part{expandTabs(r.Text, v.Tab), colours[r.Style]})
		}
		row++
	}
	return row
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

// pin draws the lines of v.Context over v's first rows on the surface
// colour, each with its number and highlights, cut at the window's edge
// (B217). numbers is where the gutter starts.
func (c *client) pin(v wire.Window, numbers, cur int) {
	t := v.Text
	for i, start := range v.Context {
		y, end := v.Y+i, vim.LineEnd(t, start)
		ground := base.Background(th.surface)
		if name, ok := v.Grounds[t.LineOf(start)]; ok {
			ground = base.Background(colours[name].GetBackground())
		}
		c.s.FillArea(numbers, y, v.Gutter+v.W, 1, ' ', ground)
		if v.Number || v.Relative {
			line := t.LineOf(start)
			c.number(line, max(line-cur, cur-line), false, numbers+v.SignCols, y, v)
			for x := numbers; x < v.X; x++ {
				if s, style, _ := c.s.Get(x, y); s != "" {
					c.s.Put(x, y, s, style.Background(th.surface))
				}
			}
		}
		styles := paint(nil, v.ContextHighlights, start, end)
		x, col := 0, 0
		for rest := t.Slice(start, end); x < v.W; {
			at := end - len(rest)
			var hidden int
			if x, hidden = c.ghosts(v, at, end, x, y, ground.GetBackground()); hidden > at {
				col = vim.Column(t, start, hidden, v.Tab, nil)
				rest = t.Slice(hidden, end)
				continue
			}
			if rest == "" {
				break
			}
			style := styles[len(styles)-len(rest)]
			if style.GetBackground() == base.GetBackground() {
				style = style.Background(ground.GetBackground())
			}
			if rest[0] == '\t' {
				w := v.Tab - col%v.Tab
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
func (c *client) edge(v wire.Window, left int) {
	if v.X > left {
		for y := v.Y; y < v.Y+v.H; y++ {
			c.s.Put(v.X-1, y, cmp.Or(v.FillChars.Vert, " "), base.Foreground(th.border))
		}
	}
	c.status(v, v.X, v.W)
}

// colorColumn shades the colour column on row y of v's text (B200).
func (c *client) colorColumn(v wire.Window, y int) {
	x := v.ColorColumn - 1
	if !v.Wrap {
		x -= v.Left
	}
	if v.ColorColumn > 0 {
		c.shade(v, x, y, th.raised)
	}
}

// shade puts bg under the cell at column x of row y of v's text, where the
// cell keeps the ground, so the cursor's line, a selection and a match
// keep theirs (B200, B483).
func (c *client) shade(v wire.Window, x, y int, bg color.Color) {
	if x < 0 || x >= v.W {
		return
	}
	if s, style, _ := c.s.Get(v.X+x, y); s != "" && style.GetBackground() == base.GetBackground() {
		c.s.Put(v.X+x, y, s, style.Background(bg))
	}
}

// guides draws a faint line at each of a line's guide columns, on row y,
// where the cell is blank (B153).
func (c *client) guides(v wire.Window, cols []int, y int) {
	left := 0
	if !v.Wrap {
		left = v.Left
	}
	for _, col := range cols {
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
func (c *client) number(line, rel int, onCursor bool, x, y int, v wire.Window) {
	n := line + 1
	if v.Relative {
		n = rel
	}
	w := v.Gutter - v.SignCols - 1
	text := fmt.Sprintf("%*d ", w, n)
	if v.Relative && v.Number && onCursor {
		text = fmt.Sprintf("%-*d ", w, line+1)
	}
	style := base.Foreground(th.faint)
	if v.CursorLine && onCursor {
		// The cursor's line number stands out.
		style = base.Bold(true)
	}
	c.s.PutStrStyled(x, y, text, style)
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

// ghosts draws v's ghosts at position at, from column x of row y, on bg.
// It returns the column after them and the end of the text they hide, at
// least at and at most to.
func (c *client) ghosts(v wire.Window, at, to, x, y int, bg color.Color) (int, int) {
	hidden := at
	for _, g := range v.Ghosts {
		if g.Pos == at {
			x = c.ghost(g, v.X, x, y, v.W, bg)
			hidden = max(hidden, min(g.End, to))
		}
	}
	return x, hidden
}

// ghost draws g's text from column x of a window at left that is w wide,
// on bg where its style has no ground of its own, and returns the column
// after it.
func (c *client) ghost(g wire.Ghost, left, x, y, w int, bg color.Color) int {
	style := base.Foreground(th.faint).Italic(true)
	switch {
	case g.Style != "":
		style = colours[g.Style]
	case g.Stale:
		style = style.Foreground(th.err)
	}
	if style.GetBackground() == base.GetBackground() {
		style = style.Background(bg)
	}
	return c.putClipped(left+x, y, left+w, part{vim.InlineText(g.Text), style}) - left
}

// note draws n after the end of a line of v, at column x of row y, and
// returns the column after it. A diagnostic's shows after a gap, on its
// severity's tint in its colour, as a label of its own. Any other is dim
// (B61, B184).
func (c *client) note(v wire.Window, x, y int, n wire.Note, shade bool) int {
	gap := base
	if shade {
		gap = gap.Background(th.cursorLine)
	}
	if style, ok := colours[n.Name+".note"]; ok {
		x = c.putClipped(v.X+x, y, v.X+v.W, part{"  ", gap}) - v.X
		return c.putClipped(v.X+x, y, v.X+v.W, part{" " + n.Text + " ", style}) - v.X
	}
	return c.putClipped(v.X+x, y, v.X+v.W, part{n.Text, gap.Foreground(th.faint)}) - v.X
}
