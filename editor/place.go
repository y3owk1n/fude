package editor

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/y3owk1n/fude/api"
)

// placements are where a window opens, as a module's result, config.toml's
// [open] and a modifier name them (D55).
var placements = []string{"above", "below", "left", "right", "here", "float", "cursor"}

// placement returns where the window p's result asks for opens: where a
// modifier typed before the command says, else where config.toml's [open]
// says for p, else asked, else def. A locked window opens as asked, or def
// (B365, B366, B367).
func (e *Session) placement(p api.Module, asked string, locked bool, def string) string {
	typed := e.placing
	e.placing = ""
	switch {
	case locked:
	case typed != "":
		return typed
	case p != nil && e.cfg.Open[p.Name()] != "":
		return e.cfg.Open[p.Name()]
	}
	if asked != "" && !slices.Contains(placements, asked) {
		e.message = fmt.Sprintf("no placement %q, so it opens %s", asked, def)
		return def
	}
	return cmp.Or(asked, def)
}

// openIn shows b in a window where says and moves to it: a new split above,
// below, left or right of the window in the layout the current one is
// over, the current window for here, or a float in the middle of the
// screen or at the cursor (D55).
func (e *Session) openIn(b *buffer, where string) {
	switch where {
	case "float":
		e.openFloat(b, middle, true)
	case "cursor":
		e.openFloat(b, atCursor, true)
	case "here":
		e.show(b)
	default:
		// A new window leaves no jump behind.
		e.win = e.under()
		e.splitBeside(where == "left" || where == "right", where == "below" || where == "right")
		e.win.show(b)
	}
}

// move puts w where says, keeping its buffer, cursor and program: into a
// float in the middle of the screen, or at the edge of the layout above,
// below, left or right of every window, spanning the layout (B365).
func (e *Session) move(w *window, where string) error {
	switch {
	case w.locked:
		return fmt.Errorf("%s cannot move", w.buf.name())
	case w.anchor == "" && e.root.win == w:
		return errors.New("cannot move the last window")
	}
	if w.anchor != "" {
		e.unfloat(w)
	} else if err := e.unlay(w); err != nil {
		return err
	}
	if where == "float" {
		w.anchor, w.from = middle, e.under()
		e.floats = append(e.floats, w)
		e.place(w)
		e.win = w
		return nil
	}
	w.anchor, w.from = "", nil
	across, after := where == "left" || where == "right", where == "below" || where == "right"
	// It goes beside every window: at the end of the outermost split when
	// that split is its way, or around all of the layout.
	l := e.root
	if l.win == nil && l.across == across {
		l = l.kids[0]
		if after {
			l = e.root.kids[len(e.root.kids)-1]
		}
	}
	e.insert(w, l, across, after)
	return nil
}
