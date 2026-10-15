// Package term is the terminal client. It draws an editor on a tcell screen
// and feeds it keys.
package term

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

// Run shows e in the terminal until the user quits.
func Run(e *editor.Editor) error {
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
	var paste strings.Builder
	pasting := false
	var timeout <-chan time.Time
	for {
		var ev tcell.Event
		select {
		case ev = <-s.EventQ():
		case <-timeout:
			e.Timeout()
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

var visualLabels = map[vim.Key]string{"v": "-- VISUAL --", "V": "-- VISUAL LINE --", "<C-v>": "-- VISUAL BLOCK --"}

type client struct {
	s tcell.Screen
	e *editor.Editor
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
	c.s.SetCursorStyle(shape)
	c.s.ShowCursor(cx, cy)
	c.s.Show()
}

// view draws one window: its text, a status line on its bottom row, and a
// separator on its left when another window is beside it. For the current
// window it reports where the cursor is.
func (c *client) view(v editor.View, sel vim.Selection, visual bool) (int, int, bool) {
	t := v.Text
	cx, cy, found := 0, 0, false
	pos := v.Top
	for row := 0; row < v.H-1; row++ {
		y := v.Y + row
		end := vim.LineEnd(t, pos)
		line := t.Slice(pos, end)
		var matches [][2]int
		if v.Current {
			matches = c.e.Matches(pos, end)
		}
		x := 0
		for rest := line; ; {
			at := pos + len(line) - len(rest)
			if v.Current && (at == v.Cursor || x >= v.W && v.Cursor >= at && v.Cursor <= end) {
				cx, cy, found = v.X+min(x, v.W-1), y, true
			}
			if rest == "" || x >= v.W {
				break
			}
			if rest[0] == '\t' {
				x += vim.Tabstop - x%vim.Tabstop
				rest = rest[1:]
				continue
			}
			style := tcell.StyleDefault
			for _, mt := range matches {
				if at >= mt[0] && at < mt[1] {
					style = style.Background(color.Yellow).Foreground(color.Black)
				}
			}
			if visual && at >= sel.Start && at < sel.End && (!sel.Block || x >= sel.Left && x <= sel.Right) {
				style = style.Reverse(true)
			}
			var width int
			rest, width = c.s.Put(v.X+x, y, rest, style)
			x += width
		}
		if end == t.Len() {
			break
		}
		pos = end + 1
	}
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
	return cx, cy, found
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
