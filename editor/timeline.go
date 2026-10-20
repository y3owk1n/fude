package editor

import (
	"fmt"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// states returns the buffer's timeline in the order it was made: the text
// before any edit, then every node.
func states(b *buffer) []kernel.NodeID {
	ids := []kernel.NodeID{{}}
	for _, n := range b.text.Nodes() {
		ids = append(ids, n.ID)
	}
	return ids
}

// goTo moves b to node id and returns where the last change there starts,
// for the cursor.
func goTo(b *buffer, id kernel.NodeID) (int, bool) {
	start, ok := b.text.Goto(id)
	if !ok {
		return 0, false
	}
	b.head = b.text.Head()
	t := b.text.Snapshot()
	start = min(start, t.Len())
	// Stay off the end of a line that has text, as normal mode does.
	if ls := vim.LineStart(t, start); start == vim.LineEnd(t, start) && start > ls {
		_, n := utf8.DecodeLastRuneInString(t.Slice(ls, start))
		start -= n
	}
	return start, true
}

// step moves n states later in the order they were made, or earlier when n
// is negative, as g+ and g- do.
func (e *Session) step(n int) (int, bool) {
	ids := states(e.win.buf)
	i := slices.Index(ids, e.win.buf.text.Head()) + n
	if i < 0 || i >= len(ids) || n == 0 {
		return 0, false
	}
	return goTo(e.win.buf, ids[i])
}

// scratchKeys are the keys of a window fude wrote to show something, such
// as :messages or a diff, which [keymap.scratch] changes (B142, B212).
var scratchKeys = map[string]string{"q": ":close<CR>"}

// scrub moves src to state id. It moves the cursor of each window showing src
// to where the last change there starts.
func (e *Session) scrub(src *buffer, id kernel.NodeID) {
	start, _ := goTo(src, id)
	for _, w := range e.windows() {
		if w.buf == src {
			src.text.MoveMark(w.cursor, start)
		}
	}
}

// replace returns one edit that turns old into new, keeping the text they
// start and end with.
func replace(old, new string) kernel.Edit {
	pre, suf := 0, 0
	for pre < len(old) && pre < len(new) && old[pre] == new[pre] {
		pre++
	}
	for suf < len(old)-pre && suf < len(new)-pre && old[len(old)-1-suf] == new[len(new)-1-suf] {
		suf++
	}
	return kernel.Edit{Start: pre, End: len(old) - suf, Text: new[pre : len(new)-suf]}
}

// panel shows p in a read-only window titled p.Title, in a split below or
// a float in the middle of the screen, or in place of what the window of
// that title shows, keeping its cursor's line and the focus where they
// are. It reports whether it did (B186, B318).
func (e *Session) panel(owner api.Module, p api.Panel) bool {
	if i := slices.IndexFunc(e.windows(), func(w *window) bool { return w.buf.title == p.Title }); i >= 0 {
		b := e.windows()[i].buf
		if err := e.refill(b, p.Text, styled(p.Spans)); err != nil {
			e.message = err.Error()
			return false
		}
		b.foot, b.footSpans = p.Foot, styled(p.FootSpans)
		return true
	}
	if p.Update {
		return false
	}
	e.scratch(p.Title, p.Text, e.placement(owner, p.Where, p.Lock, "below"))
	e.win.locked = p.Lock
	b := e.win.buf
	b.panelKind = p.Kind
	b.keys = e.panelKeys(b, p.Keys)
	b.foot, b.footSpans = p.Foot, styled(p.FootSpans)
	e.style(b, styled(p.Spans))
	e.setCursor(min(max(p.Cursor, 0), b.text.Snapshot().Len()))
	if b.ft = p.Filetype; b.ft == "" {
		return true
	}
	if ok, err := e.attachSyntax(b); err != nil {
		e.message = err.Error()
	} else if !ok && syntax.Buildable(b.ft) && e.cfg.AutoInstall && e.buildGrammar(b.ft, false) {
		e.message = "building the " + b.ft + " grammar"
	}
	return true
}

// panelAt moves the focus to the window of the panel titled title, with
// line at its top and the cursor on it (B334).
func (e *Session) panelAt(title string, line int) {
	i := slices.IndexFunc(e.windows(), func(w *window) bool { return w.buf.title == title })
	if i < 0 {
		return
	}
	e.win = e.windows()[i]
	t := e.win.buf.text.Snapshot()
	at := t.LineStart(min(line, t.Lines()-1))
	e.setCursor(at)
	e.win.buf.text.MoveMark(e.win.top, at)
}

// panelKeys are a panel's own keys, under those config.toml sets for its
// kind, or nil for a panel with none (B291).
func (e *Session) panelKeys(b *buffer, keys map[string]string) *vim.Keymaps {
	if len(keys) == 0 {
		return nil
	}
	km := &vim.Keymaps{}
	for from, to := range keys {
		km.Map(vim.Normal, from, to)
	}
	if l := e.locals[b.kind()]; l != nil {
		l.Each(vim.Normal, func(from, to string) { km.Map(vim.Normal, from, to) })
	}
	return km
}

// localKeys are b's own keys when it is a panel with some, or else those
// of its kind.
func (e *Session) localKeys(b *buffer) *vim.Keymaps {
	if b.keys != nil {
		return b.keys
	}
	return e.locals[b.kind()]
}

// scratch shows text in a new read-only window titled title, placed where
// says.
func (e *Session) scratch(title, text, where string) {
	b, _, err := openBuffer(e.next, "")
	if err != nil {
		e.message = err.Error()
		return
	}
	e.next++
	if err := b.text.Apply(kernel.Transaction{Edits: []kernel.Edit{{Text: text}}}); err != nil {
		e.message = err.Error()
		return
	}
	b.saved = b.text.Head()
	b.title, b.file.readonly, b.shown = title, true, true
	e.bufs = append(e.bufs, b)
	e.openIn(b, where)
}

// panelStyles names the decorations that style a panel's text.
type panelStyles struct{ b *buffer }

// style puts spans on b, a panel, in place of the styles it had (B276).
func (e *Session) style(b *buffer, spans []syntax.Span) {
	for _, d := range e.decos[panelStyles{b}] {
		d.remove()
	}
	delete(e.decos, panelStyles{b})
	var ds []decoration
	for _, s := range spans {
		if s.Start < s.End {
			ds = append(ds, decoration{buf: b, start: b.text.SetMark(s.Start, kernel.Left), end: b.text.SetMark(s.End, kernel.Right), style: s.Name})
		}
	}
	if len(ds) > 0 {
		e.decos[panelStyles{b}] = ds
	}
}

// stateList is b's states as a module reads them (B328).
func (b *buffer) stateList() []api.State {
	head := b.text.Head()
	ids := []kernel.NodeID{{}}
	out := []api.State{{Current: head == (kernel.NodeID{}), Label: "the text before any change"}}
	for _, n := range b.text.Nodes() {
		ids = append(ids, n.ID)
		out = append(out, api.State{
			Time: n.Time, Label: n.Label, Parent: slices.Index(ids, n.Parent), Current: n.ID == head,
			Saved: slices.Contains(b.saves, n.ID), Unsaved: n.ID == b.unsaved && b.unsaved != (kernel.NodeID{}),
		})
	}
	return out
}

// stateText is the text of b's state i (B328).
func (b *buffer) stateText(i int) (string, bool) {
	ids := states(b)
	if i < 0 || i >= len(ids) {
		return "", false
	}
	t, ok := b.text.At(ids[i])
	if !ok {
		return "", false
	}
	return t.Slice(0, t.Len()), true
}

// travel moves b's text as tr asks: to a state, or with New making a
// state's text the newest state, on top of the newest (B31, B328).
func (e *Session) travel(b *buffer, tr api.Travel) {
	ids := states(b)
	if tr.To < 0 || tr.To >= len(ids) {
		e.message = fmt.Sprintf("no such state: %d", tr.To)
		return
	}
	if !tr.New {
		e.scrub(b, ids[tr.To])
		// Going to the change a session left unsaved recovers it (B34).
		if ids[tr.To] == b.unsaved {
			b.unsaved = kernel.NodeID{}
		}
		return
	}
	if err := e.mayEdit(); err != nil {
		e.message = err.Error()
		return
	}
	e.scrub(b, ids[len(ids)-1])
	target, _ := b.text.At(ids[tr.To])
	now := b.text.Snapshot()
	old, new := now.Slice(0, now.Len()), target.Slice(0, target.Len())
	if old == new {
		e.message = fmt.Sprintf("the text is already state %d", tr.To)
		return
	}
	ed := replace(old, new)
	t := kernel.Transaction{
		Base: now.Version(), Origin: e.origin, Label: "restore " + strconv.Itoa(tr.To), Time: e.now(),
		Edits: []kernel.Edit{ed},
	}
	if err := b.text.Apply(t); err != nil {
		e.message = err.Error()
		return
	}
	b.head = b.text.Head()
	b.pushChange(ed.Start)
	e.message = fmt.Sprintf("restored state %d as a new state", tr.To)
}
