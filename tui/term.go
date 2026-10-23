// Package term is the terminal client. It draws an editor on a tcell screen
// and feeds it keys.
package tui

import (
	"cmp"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	fudeclient "github.com/y3owk1n/fude/client"
	"github.com/y3owk1n/fude/internal/compose"
	look "github.com/y3owk1n/fude/internal/theme"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Run shows e in the terminal until the user quits.
func Run(e fudeclient.Session) error {
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
			c.grids = nil
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
func drain(e fudeclient.Session, key func() bool) {
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

type client struct {
	s        tcell.Screen
	e        fudeclient.Session
	theme    string                // the theme drawn in, as e names it
	colours  *[16]int32            // its base16 scheme's colours, if it is one
	over     wire.Colors           // the colours [colors] puts in place of its own
	restyled int                   // how many times the modules' styles had changed (B345)
	spinning bool                  // the last frame shows a spinner
	frame    int                   // counts the frames drawn
	grids    map[int]*compose.Grid // each window's cells, by its id
	comp     compose.Composer      // composes the windows into their grids
	painted  look.Style            // the style the last cell painted was in, and painting it as tcell does
	paintedT tcell.Style
	overlaid bool              // the last frame drew a box, the command line or a message over the windows
	size     [4]int            // the screen's size, and where the windows started and ended, in the last frame
	img      images            // how the client shows images (B303)
	menuTop  int               // the first row of the cursor's menu in the last frame
	cursor   [3]int            // where the last frame written put the cursor, and its shape
	shape    tcell.CursorStyle // the cursor's shape in the frame being drawn
	tint     color.Color       // the multicursors' colour then, the mode's
	tintRGB  look.RGB          // the same, for composing
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
	fresh := c.grids == nil
	// The theme e names may change at any time with :theme (B158).
	name, colours, given := c.e.Theme()
	if given == nil {
		given = &wire.Colors{}
	}
	own, restyled := c.e.Styles()
	if name != c.theme || colours != c.colours || *given != c.over || restyled != c.restyled {
		fresh = true
		c.theme, c.colours, c.over, c.restyled = name, colours, *given, restyled
		t := look.Themes[name]
		if colours != nil {
			t = look.FromBase16(*colours)
		}
		useTheme(t.With(*given), own)
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
	c.tintRGB = look.RGB(c.tint.Hex())

	// A modal box, the command line or the finder, has the windows under it
	// drawn in the tinted theme, and draws on the tinted ground (B151).
	f, floats := c.e.Menu()
	msg, ask := carets(c.e.Message()), c.e.Confirm()
	wins := c.e.Windows()
	// A float in the middle of the screen dims the windows under it, as a
	// modal box does.
	middle, floated := false, false
	for _, v := range wins {
		middle, floated = middle || v.Float == "middle", floated || v.Float != ""
	}
	// A :s being typed shows its preview in the windows, so they stay lit.
	modal := c.e.Mode() == vim.Cmdline && !c.e.Substituting() || floats && f.Anchor == "middle" || middle || ask != ""
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
		if c.grids == nil {
			c.grids = map[int]*compose.Grid{}
		}
		for _, g := range c.grids {
			g.Clear(lk.Base)
		}
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
	c.topLine(c.e.Buffers(), c.e.Pages(), w)
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
// column the windows start at, which has none. For the current window it
// reports where the cursor is.
func (c *client) window(v wire.Window, left int, sel vim.Selection, visual bool) (int, int, bool) {
	if v.Cells != nil {
		return c.cells(v, left)
	}
	g := c.grids[v.ID]
	if g == nil || !sameSize(g, v) {
		g = compose.NewGrid(v.W, v.H, lk.Base)
		c.grids[v.ID] = g
	}
	var cursors []int
	if v.Current {
		cursors = c.e.Cursors()
	}
	out := c.comp.Compose(g, v, compose.Options{
		Look: lk, Frame: c.frame, Insert: c.e.Mode() == vim.Insert, Cursors: cursors, Selection: sel, Visual: visual,
		Shape: int(c.shape), Underline: c.shape == tcell.CursorStyleSteadyUnderline, Tint: c.tintRGB, OwnCursors: c.extra != nil && c.extra.Load(),
	})
	g.Paint(v.X, v.Y, c.put)
	// The client draws images and a float's rules over the text's cells.
	tv := v
	tv.X, tv.W = v.X+v.Gutter, v.W-v.Gutter
	for _, r := range out.Images {
		c.image(tv, r.Line, r.Row, v.Y+r.Y)
	}
	for _, s := range out.Starts {
		if compose.FloatRule(v, s[1]) {
			c.rule(tv.X-2, v.Y+s[0], tv.W+2)
		}
	}
	for _, m := range out.Marks {
		c.marks = append(c.marks, [2]int{v.X + m[0], v.Y + m[1]})
	}
	if v.Float == "" {
		c.edge(v, left)
	}
	return v.X + out.X, v.Y + out.Y, out.Found
}

// sameSize reports whether g is as wide and tall as v.
func sameSize(g *compose.Grid, v wire.Window) bool {
	w, h := g.Size()
	return w == v.W && h == v.H
}

// put paints a grid's cell on the screen. Cells side by side mostly share a
// style, so it converts a style to tcell's once for each run of them.
func (c *client) put(x, y int, text string, s look.Style) {
	if s != c.painted || c.paintedT == (tcell.Style{}) {
		c.painted, c.paintedT = s, tcellStyle(s)
	}
	c.s.Put(x, y, text, c.paintedT)
}

// edge draws v's status line on its bottom row, and a separator on its
// left unless the windows start there, at left.
func (c *client) edge(v wire.Window, left int) {
	if v.X > left {
		for y := v.Y; y <= v.Y+v.H; y++ {
			c.s.Put(v.X-1, y, cmp.Or(v.FillChars.Vert, " "), base.Foreground(th.border))
		}
	}
	c.status(v, v.X, v.W)
}

// carets returns s with each control character, a tab too, as ^ and a
// letter, as ^A for 0x01. A line break stays, as it ends a message's line.
func carets(s string) string {
	var b strings.Builder
	for i := range len(s) {
		if s[i] == '\t' || s[i] != '\n' && compose.Control(s[i]) {
			b.WriteByte('^')
			b.WriteByte(s[i] ^ 0x40)
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
