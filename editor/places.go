package editor

import (
	"cmp"
	"fmt"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// spot is a place in a file, and what its column counts in.
type spot struct {
	path string
	pos  lsp.Position
	enc  string // the encoding pos counts in
}

// setPlaces makes pl the last list of places, which ]q and the :c commands
// go through, and tells the modules, which show it when it asks (B347).
func (e *Session) setPlaces(pl *api.Places) {
	l := *pl
	e.lastList = &l
	detail := ""
	if pl.Show {
		detail = "show"
	}
	e.emit("places.set", detail, e.win.buf)
}

// leaveList moves to a window that does not show a list of places, when
// there is one, so going to a place keeps the list in view.
func (e *Session) leaveList() {
	if e.win.buf.kind() != "list" {
		return
	}
	for _, w := range e.root.windows() {
		if w.buf.kind() != "list" {
			e.win = w
			return
		}
	}
}

// visit jumps to sp, opening its file when no buffer has it.
func (e *Session) visit(sp spot) {
	// Moving to another buffer is a jump of its own.
	if e.win.buf.is(sp.path) {
		e.pushJump(e.Cursor())
	} else {
		e.edit(sp.path)
	}
	if e.win.buf.is(sp.path) {
		e.setCursor(lsp.Offset(e.Text(), sp.pos, sp.enc))
		e.reveal()
	}
}

// runAt runs a's command line at each of its places, or once in each of
// their files, as :cdo and :cfdo do (B257, B347). Marks keep the places on
// their text as the command edits around them.
func (e *Session) runAt(a api.Each) {
	type stop struct {
		b    *buffer
		mark kernel.MarkID
	}
	var stops []stop
	files := map[string]bool{}
	for _, item := range a.Items {
		path, line, col, _ := grepSpot(item + ":")
		if a.PerFile && files[path] {
			continue
		}
		e.leaveList()
		// A place that visit refuses, such as one outside a guest's
		// project, is skipped.
		sp := spot{path, lsp.Position{Line: max(line-1, 0), Character: max(col-1, 0)}, cmp.Or(a.Encoding, "utf-8")}
		if e.visit(sp); !e.win.buf.is(sp.path) {
			continue
		}
		files[sp.path] = true
		stops = append(stops, stop{e.win.buf, e.win.buf.text.SetMark(e.Cursor(), kernel.Right)})
	}
	for _, st := range stops {
		e.show(st.b)
		e.setCursor(st.b.text.Mark(st.mark))
		st.b.text.DeleteMark(st.mark)
		if err := e.vim.Ex(a.Ex); err != nil {
			e.message = err.Error()
			return
		}
	}
	if a.PerFile {
		e.message = fmt.Sprintf("ran :%s in %d files", a.Ex, len(files))
		return
	}
	e.message = fmt.Sprintf("ran :%s at %d places in %d files", a.Ex, len(stops), len(files))
}
