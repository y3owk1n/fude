package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
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
	text, ids := listing(src)
	e.scratch("[timeline]", text, src, ids)
}

// listing returns the lines of src's timeline window and the state on each.
func listing(src *buffer) (string, []kernel.NodeID) {
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

// timelineKey runs k in a timeline window and reports whether k is one of
// its keys. [ and ] move the text to the state on the line above or below. d
// diffs the state on the cursor's line against now. <CR> restores that state
// on top of the newest one.
func (e *Session) timelineKey(k vim.Key) bool {
	src, i := e.state("")
	switch k {
	case "[", "]":
		j := i - 1
		if k == "]" {
			j = i + 1
		}
		if ids := states(src); j >= 0 && j < len(ids) {
			e.scrub(src, ids[j])
			e.relist(j)
		}
	case "d":
		e.diff("")
	case "<CR>":
		ids := states(src)
		e.scrub(src, ids[len(ids)-1])
		e.restore(strconv.Itoa(i))
		e.relist(len(states(src)) - 1)
	default:
		return false
	}
	return true
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
	text, ids := listing(b.source)
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
	ops := lineDiff(strings.Split(x.Slice(0, x.Len()), "\n"), strings.Split(y.Slice(0, y.Len()), "\n"))
	e.scratch(fmt.Sprintf("[diff %d %d]", i, j), unified(ops, 3), b, nil)
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
	b.title, b.source, b.lines, b.file.readonly = title, src, lines, true
	e.bufs = append(e.bufs, b)
	e.split(false)
	e.win.show(b)
}

// op is one line of a diff: kept, removed or added.
type op struct {
	kind byte // ' ', '-' or '+'
	line string
}

// lineDiff returns the shortest edit script turning lines a into lines b,
// by Myers' algorithm.
func lineDiff(a, b []string) []op {
	n, m := len(a), len(b)
	off := n + m + 1
	v := make([]int, 2*off+1)
	var trace [][]int
	for d := 0; d <= n+m; d++ {
		trace = append(trace, slices.Clone(v))
		for k := -d; k <= d; k += 2 {
			x := v[off+k-1] + 1
			if k == -d || k != d && v[off+k-1] < v[off+k+1] {
				x = v[off+k+1]
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, a, b, off)
			}
		}
	}
	return nil
}

// backtrack walks lineDiff's trace back from the end to list the ops.
func backtrack(trace [][]int, a, b []string, off int) []op {
	var ops []op
	x, y := len(a), len(b)
	for d := len(trace) - 1; d > 0; d-- {
		v, k := trace[d], x-y
		pk := k - 1
		if k == -d || k != d && v[off+k-1] < v[off+k+1] {
			pk = k + 1
		}
		px := v[off+pk]
		py := px - pk
		for x > px && y > py {
			ops = append(ops, op{' ', a[x-1]})
			x, y = x-1, y-1
		}
		if x == px {
			ops = append(ops, op{'+', b[y-1]})
			y--
		} else {
			ops = append(ops, op{'-', a[x-1]})
			x--
		}
	}
	for x > 0 && y > 0 {
		ops = append(ops, op{' ', a[x-1]})
		x, y = x-1, y-1
	}
	slices.Reverse(ops)
	return ops
}

// unified renders ops as hunks of changed lines with context lines around
// them, each under a @@ header giving its first line on each side.
func unified(ops []op, context int) string {
	near := make([]bool, len(ops))
	for i, o := range ops {
		if o.kind != ' ' {
			for j := max(0, i-context); j <= min(len(ops)-1, i+context); j++ {
				near[j] = true
			}
		}
	}
	var out []string
	la, lb := 1, 1
	for i, o := range ops {
		if near[i] && (i == 0 || !near[i-1]) {
			out = append(out, fmt.Sprintf("@@ -%d +%d @@", la, lb))
		}
		if near[i] {
			out = append(out, string(o.kind)+o.line)
		}
		if o.kind != '+' {
			la++
		}
		if o.kind != '-' {
			lb++
		}
	}
	if len(out) == 0 {
		return "no difference"
	}
	return strings.Join(out, "\n")
}
