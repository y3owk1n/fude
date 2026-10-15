// Package term is the terminal client. It draws an editor on a tcell screen
// and feeds it keys.
package term

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"

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

	c := client{s: s, e: e}
	c.draw()
	var paste strings.Builder
	pasting := false
	for ev := range s.EventQ() {
		switch ev := ev.(type) {
		case *tcell.EventResize:
			s.Sync()
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
				if k := key(ev); k != "" {
					e.Feed(k)
				}
			}
		}
		if e.Done() {
			return nil
		}
		c.draw()
	}
	return nil
}

type client struct {
	s   tcell.Screen
	e   *editor.Editor
	top int // byte offset of the first visible line
}

func (c *client) draw() {
	c.s.Clear()
	w, h := c.s.Size()
	t, cur := c.e.Text(), c.e.Cursor()
	c.scroll(t, cur, h-1)

	cx, cy := 0, 0
	pos := c.top
	for y := 0; y < h-1; y++ {
		end := vim.LineEnd(t, pos)
		line := t.Slice(pos, end)
		x := 0
		for rest := line; ; {
			at := pos + len(line) - len(rest)
			if at == cur || x >= w && cur >= at && cur <= end {
				cx, cy = min(x, w-1), y
			}
			if rest == "" || x >= w {
				break
			}
			if rest[0] == '\t' {
				x += vim.Tabstop - x%vim.Tabstop
				rest = rest[1:]
				continue
			}
			var width int
			rest, width = c.s.Put(x, y, rest, tcell.StyleDefault)
			x += width
		}
		if end == t.Len() {
			break
		}
		pos = end + 1
	}

	style := tcell.CursorStyleSteadyBlock
	switch c.e.Mode() {
	case vim.Insert:
		style = tcell.CursorStyleSteadyBar
		c.s.PutStr(0, h-1, "-- INSERT --")
	case vim.Cmdline:
		c.s.PutStr(0, h-1, ":"+c.e.Line())
		cx, cy = 1+len(c.e.Line()), h-1
	default:
		c.s.PutStr(0, h-1, c.e.Message())
	}
	c.s.SetCursorStyle(style)
	c.s.ShowCursor(cx, cy)
	c.s.Show()
}

// scroll moves top so the line holding cur is on screen.
func (c *client) scroll(t vim.Text, cur, rows int) {
	if cur < c.top {
		c.top = vim.LineStart(t, cur)
		return
	}
	below := 0
	for p := c.top; ; below++ {
		end := vim.LineEnd(t, p)
		if cur <= end {
			break
		}
		p = end + 1
	}
	for ; below >= rows; below-- {
		c.top = vim.LineEnd(t, c.top) + 1
	}
}

// key returns ev in Vim notation, or "" for a key fude does not use yet.
func key(ev *tcell.EventKey) vim.Key {
	switch ev.Key() {
	case tcell.KeyRune:
		if ev.Modifiers()&^tcell.ModShift == 0 {
			return vim.Key(ev.Str())
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
