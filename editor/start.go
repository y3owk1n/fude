package editor

import (
	"slices"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
)

// Welcome tells the modules that fude started with no file to open, not
// restarting, as the start screen shows (B225, B319).
func (e *Session) Welcome() { e.emit("editor.started", "", e.win.buf) }

// screenPanel shows p in the current window in place of b, while the
// window shows b. The window shows no line numbers, guides or wrap, and
// gets its options back once it shows another buffer (B320).
func (e *Session) screenPanel(b *buffer, p api.Panel) {
	if slices.ContainsFunc(e.root.windows(), func(w *window) bool { return w.buf.title == p.Title }) {
		e.panel(nil, p)
		return
	}
	if p.Update || e.win.buf != b {
		return
	}
	s, _, err := openBuffer(e.next, "")
	if err == nil {
		t := s.text.Snapshot()
		err = s.text.Apply(kernel.Transaction{Base: t.Version(), Edits: []kernel.Edit{{Text: p.Text}}})
	}
	if err != nil {
		e.message = err.Error()
		return
	}
	e.next++
	s.title, s.file.readonly, s.screen = p.Title, true, true
	s.saved = s.text.Head()
	e.bufs = append(e.bufs, s)
	e.win.show(s)
	opts := e.win.opts
	e.win.saved, e.win.opts = &opts, options{}
	s.keys = e.panelKeys(s, p.Keys)
	e.style(s, styled(p.Spans))
	e.setCursor(min(max(p.Cursor, 0), s.text.Snapshot().Len()))
}
