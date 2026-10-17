package editor

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/vim"
)

// messages lists the session's messages in a window, or with yank puts the
// last on the clipboard (B157, B268).
func (e *Session) messages(arg string) {
	switch {
	case len(e.history) == 0:
		e.message = "no messages"
	case arg == "yank":
		// The clipboard is the client's for a session other than the first
		// (B241).
		if err := (host{e}).SetClipboard(e.history[len(e.history)-1]); err != nil {
			e.message = err.Error()
			return
		}
		e.message = "copied the last message"
	case arg != "":
		e.message = "messages takes yank or nothing"
	default:
		e.scratch("[messages]", strings.Join(e.history, "\n"), nil, nil)
	}
}

// placeLine is a line of a list of places: the place's line and column and
// the text of its line, and the spot <CR> jumps to.
func placeLine(b *buffer, pos int) (string, spot) {
	t := b.text.Snapshot()
	pos = min(pos, t.Len())
	ls := vim.LineStart(t, pos)
	line := t.LineOf(pos)
	text := strings.TrimSpace(t.Slice(ls, vim.LineEnd(t, ls)))
	return fmt.Sprintf("%d:%d  %s", line+1, pos-ls+1, text), spot{b.file.path, lsp.Position{Line: line, Character: pos - ls}, "utf-8"}
}

// marks lists the current buffer's marks (B157).
func (e *Session) marks() {
	b := e.win.buf
	var lines []string
	var spots []spot
	for _, name := range slices.Sorted(maps.Keys(b.marks)) {
		l, sp := placeLine(b, b.text.Mark(b.marks[name]))
		lines, spots = append(lines, string(name)+"  "+l), append(spots, sp)
	}
	e.places("marks", lines, spots)
}

// jumps lists the current window's jumps, oldest first (B157).
func (e *Session) jumps() {
	var lines []string
	var spots []spot
	for i, j := range e.win.jumps {
		l, sp := placeLine(j.buf, j.buf.text.Mark(j.mark))
		lines, spots = append(lines, fmt.Sprintf("%d  %s  %s", len(e.win.jumps)-i, j.buf.name(), l)), append(spots, sp)
	}
	e.places("jumps", lines, spots)
}

// changeList lists where the current buffer changed, oldest first (B157).
func (e *Session) changeList() {
	b := e.win.buf
	var lines []string
	var spots []spot
	for i, id := range b.changes {
		l, sp := placeLine(b, b.text.Mark(id))
		lines, spots = append(lines, fmt.Sprintf("%d  %s", len(b.changes)-i, l)), append(spots, sp)
	}
	e.places("changes", lines, spots)
}

// places shows lines in a list window titled what, or says there are none.
func (e *Session) places(what string, lines []string, spots []spot) {
	if len(lines) == 0 {
		e.message = "no " + what
		return
	}
	e.showList("["+what+"]", lines, spots)
}
