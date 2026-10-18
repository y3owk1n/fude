package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/api/diff"
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

// travel runs :earlier or :later. The argument is a count of states, a time
// such as 10s, 5m, 2h or 1d, or a count of writes such as 1f.
func (e *Session) travel(arg string, later bool) {
	b := e.win.buf
	ids := states(b)
	cur := slices.Index(ids, b.text.Head())
	if arg == "" {
		arg = "1"
	}
	unit := arg[len(arg)-1]
	n, err := strconv.Atoi(strings.TrimRight(arg, "smhdf"))
	if err != nil || n < 0 {
		e.message = "bad count: " + arg
		return
	}
	target := cur
	switch unit {
	case 's', 'm', 'h', 'd':
		d := time.Duration(n) * map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[unit]
		when := e.timeOf(b, ids[cur])
		if later {
			// The first state made at least d after this one, or the newest.
			target = len(ids) - 1
			for i := cur + 1; i < len(ids); i++ {
				if !e.timeOf(b, ids[i]).Before(when.Add(d)) {
					target = i
					break
				}
			}
		} else {
			// The last state made at least d before this one, or the first.
			target = 0
			for i := 1; i <= cur; i++ {
				if !e.timeOf(b, ids[i]).After(when.Add(-d)) {
					target = i
				}
			}
		}
	case 'f':
		target = e.saveFrom(b, ids, cur, n, later)
	default:
		target = cur - n
		if later {
			target = cur + n
		}
	}
	start, ok := goTo(b, ids[max(0, min(target, len(ids)-1))])
	if !ok {
		e.message = "no such state"
		return
	}
	e.setCursor(start)
}

// timeOf returns when node id was made, or the zero time for the text
// before any edit.
func (e *Session) timeOf(b *buffer, id kernel.NodeID) time.Time {
	for _, n := range b.text.Nodes() {
		if n.ID == id {
			return n.Time
		}
	}
	return time.Time{}
}

// saveFrom returns the state n writes back from cur, or forward when later
// is set. Going back from changes made since the last write counts that
// write as the first. Past the newest write it is the newest state.
func (e *Session) saveFrom(b *buffer, ids []kernel.NodeID, cur, n int, later bool) int {
	var saved []int
	for _, s := range b.saves {
		saved = append(saved, slices.Index(ids, s))
	}
	slices.Sort(saved)
	saved = slices.Compact(saved)
	if later {
		i, _ := slices.BinarySearch(saved, cur+1)
		if i+n-1 < len(saved) {
			return saved[i+n-1]
		}
		return len(ids) - 1
	}
	i, _ := slices.BinarySearch(saved, cur)
	if i-n < 0 {
		return 0
	}
	return saved[i-n]
}

// recover moves to the newest unsaved change from a session that did not
// close.
func (e *Session) recover() {
	b := e.win.buf
	if b.unsaved == (kernel.NodeID{}) {
		e.message = "nothing to recover"
		return
	}
	id := b.unsaved
	b.unsaved = kernel.NodeID{}
	if start, ok := goTo(b, id); ok {
		e.setCursor(start)
	}
}

// timeline opens a window listing every state of the current buffer, one
// per line with its number, time and the command that made it. * marks the
// state the text is at, and (from N) a state that does not follow the one
// before it.
func (e *Session) timeline() {
	src := e.win.buf
	if src.source != nil {
		src = src.source
	}
	text, ids := timelineLines(src)
	e.scratch("[timeline]", text, src, ids)
}

// timelineLines returns the lines of src's timeline window and the state on
// each.
func timelineLines(src *buffer) (string, []kernel.NodeID) {
	var lines []string
	nodes := src.text.Nodes()
	ids := []kernel.NodeID{{}}
	for i, n := range append([]kernel.Node{{}}, nodes...) {
		mark := " "
		if n.ID == src.text.Head() {
			mark = "*"
		}
		if i == 0 {
			lines = append(lines, mark+" 0            the text before any change")
			continue
		}
		ids = append(ids, n.ID)
		line := fmt.Sprintf("%s %d  %s  %s", mark, i, n.Time.Format("15:04:05"), n.Label)
		if prev := ids[i-1]; n.Parent != prev {
			line += fmt.Sprintf("  (from %d)", slices.Index(ids, n.Parent))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), ids
}

// timelineKeys are a timeline window's own keys, which [keymap.timeline]
// changes (B142).
var timelineKeys = map[string]string{
	"[": "<Plug>(timeline.earlier)", "]": "<Plug>(timeline.later)", "d": "<Plug>(timeline.diff)", "<CR>": "<Plug>(timeline.restore)",
	"q": ":close<CR>",
}

// scratchKeys are the keys of a window fude wrote to show something, such
// as :messages or a diff, which [keymap.scratch] changes (B142, B212).
var scratchKeys = map[string]string{"q": ":close<CR>"}

// timelineAct runs act in a timeline window. earlier and later move the
// text to the state on the line above or below. diff diffs the state on the
// cursor's line against now. restore restores that state on top of the
// newest one.
func (e *Session) timelineAct(act string) {
	if e.win.buf.lines == nil {
		e.message = "not in a timeline"
		return
	}
	src, i := e.state("")
	switch act {
	case "earlier", "later":
		j := i - 1
		if act == "later" {
			j = i + 1
		}
		if ids := states(src); j >= 0 && j < len(ids) {
			e.scrub(src, ids[j])
			e.relist(j)
		}
	case "diff":
		e.diff("")
	case "restore":
		ids := states(src)
		e.scrub(src, ids[len(ids)-1])
		e.restore(strconv.Itoa(i))
		e.relist(len(states(src)) - 1)
	}
}

// scrub moves src to state id. It moves the cursor of each window showing src
// to where the last change there starts.
func (e *Session) scrub(src *buffer, id kernel.NodeID) {
	start, _ := goTo(src, id)
	for _, w := range e.root.windows() {
		if w.buf == src {
			src.text.MoveMark(w.cursor, start)
		}
	}
}

// relist rewrites the current timeline window from its buffer's timeline
// and puts the cursor on line.
func (e *Session) relist(line int) {
	b := e.win.buf
	text, ids := timelineLines(b.source)
	old := b.text.Snapshot()
	if err := b.text.Apply(kernel.Transaction{Base: old.Version(), Edits: []kernel.Edit{{End: old.Len(), Text: text}}}); err != nil {
		e.message = err.Error()
		return
	}
	b.saved, b.lines = b.text.Head(), ids
	t, p := b.text.Snapshot(), 0
	for range line {
		p = vim.LineEnd(t, p) + 1
	}
	e.setCursor(p)
}

// restore makes the text of state N the newest state, as a new node, so no
// state is lost. In a timeline window, N defaults to the line under the
// cursor.
func (e *Session) restore(arg string) {
	if err := e.mayEdit(); err != nil {
		e.message = err.Error()
		return
	}
	b, i := e.state(arg)
	if b == nil {
		return
	}
	target, _ := b.text.At(states(b)[i])
	now := b.text.Snapshot()
	old, new := now.Slice(0, now.Len()), target.Slice(0, target.Len())
	if old == new {
		e.message = fmt.Sprintf("the text is already state %d", i)
		return
	}
	ed := replace(old, new)
	t := kernel.Transaction{
		Base: now.Version(), Origin: e.origin, Label: "restore " + strconv.Itoa(i), Time: e.now(),
		Edits: []kernel.Edit{ed},
	}
	if err := b.text.Apply(t); err != nil {
		e.message = err.Error()
		return
	}
	b.head = b.text.Head()
	b.pushChange(ed.Start)
	e.message = fmt.Sprintf("restored state %d as a new state", i)
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

// diff opens a window with the lines that differ between states N and M of
// the current buffer, M being the state the text is at when left out.
func (e *Session) diff(arg string) {
	from, to, _ := strings.Cut(arg, " ")
	b, i := e.state(from)
	if b == nil {
		return
	}
	j := slices.Index(states(b), b.text.Head())
	if to = strings.TrimSpace(to); to != "" {
		if _, j = e.state(to); j < 0 {
			return
		}
	}
	x, _ := b.text.At(states(b)[i])
	y, _ := b.text.At(states(b)[j])
	ops := diff.Lines(strings.Split(x.Slice(0, x.Len()), "\n"), strings.Split(y.Slice(0, y.Len()), "\n"))
	e.scratch(fmt.Sprintf("[diff %d %d]", i, j), diff.Unified(ops, 3), b, nil)
	e.colourDiff(e.win.buf)
}

// state resolves a state number for :restore and :diff, from arg or from the
// line under the cursor in a timeline window. It returns the buffer the
// state belongs to, or nil after telling the user why not.
func (e *Session) state(arg string) (*buffer, int) {
	b := e.win.buf
	if b.source != nil {
		b = b.source
	}
	ids := states(b)
	if arg == "" && e.win.buf.lines != nil {
		t := e.Text()
		line := t.LineOf(e.Cursor())
		return b, slices.Index(ids, e.win.buf.lines[line])
	}
	i, err := strconv.Atoi(arg)
	if err != nil || i < 0 || i >= len(ids) {
		e.message = "no such state: " + arg
		return nil, -1
	}
	return b, i
}

// panel shows text in the read-only window titled title, coloured as
// filetype once its grammar is built. It opens the window unless one of
// that title is open. It reports whether it could, and says why not when it
// could not.
// panel shows p in a read-only window titled p.Title, or in place of what
// the window of that title shows, keeping its cursor's line and the focus
// where they are. It reports whether it did (B186).
func (e *Session) panel(p api.Panel) bool {
	if p.Float {
		return e.boxPanel(p)
	}
	if i := slices.IndexFunc(e.root.windows(), func(w *window) bool { return w.buf.title == p.Title }); i >= 0 {
		b := e.root.windows()[i].buf
		t := b.text.Snapshot()
		if old := t.Slice(0, t.Len()); old != p.Text {
			if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Edits: []kernel.Edit{replace(old, p.Text)}}); err != nil {
				e.message = err.Error()
				return false
			}
			b.saved = b.text.Head()
		}
		e.style(b, p.Spans)
		return true
	}
	if p.Update {
		return false
	}
	e.scratch(p.Title, p.Text, nil, nil)
	b := e.win.buf
	b.keys = e.panelKeys(b, p.Keys)
	e.style(b, p.Spans)
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

// scratch shows text in a new read-only window titled title, about src.
func (e *Session) scratch(title, text string, src *buffer, lines []kernel.NodeID) {
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
	b.title, b.source, b.lines, b.file.readonly, b.shown = title, src, lines, true, true
	e.bufs = append(e.bufs, b)
	e.splitBeside(false, true)
	e.win.show(b)
}

// panelStyles names the decorations that style a panel's text.
type panelStyles struct{ b *buffer }

// style puts spans on b, a panel, in place of the styles it had (B276).
func (e *Session) style(b *buffer, spans []api.Span) {
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

// colourDiff colours the lines of b, a diff window: a removed line in the
// diff.minus style, an added one in diff.plus and a hunk's header dim
// (B222).
func (e *Session) colourDiff(b *buffer) {
	t := b.text.Snapshot()
	var ds []decoration
	for line := range t.Lines() {
		start := t.LineStart(line)
		end := vim.LineEnd(t, start)
		style := ""
		switch text := t.Slice(start, end); {
		case strings.HasPrefix(text, "@@"):
			style = "comment"
		case strings.HasPrefix(text, "-"):
			style = "diff.minus"
		case strings.HasPrefix(text, "+"):
			style = "diff.plus"
		}
		if style != "" && end > start {
			ds = append(ds, decoration{buf: b, start: b.text.SetMark(start, kernel.Left), end: b.text.SetMark(end, kernel.Right), style: style})
		}
	}
	e.decos[panelStyles{b}] = ds
}
