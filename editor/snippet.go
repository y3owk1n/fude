package editor

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// snippetVars are the variables a snippet can use, for the file of b with
// the cursor at c.
func (e *Session) snippetVars(b *buffer, c int) map[string]string {
	t, now := b.text.Snapshot(), e.now()
	line := t.LineOf(c)
	ls := t.LineStart(line)
	path, _ := filepath.Abs(b.file.path)
	name := filepath.Base(b.file.path)
	return map[string]string{
		"TM_FILENAME": name, "TM_FILENAME_BASE": strings.TrimSuffix(name, filepath.Ext(name)),
		"TM_FILEPATH": path, "TM_DIRECTORY": filepath.Dir(path),
		"TM_LINE_INDEX": strconv.Itoa(line), "TM_LINE_NUMBER": strconv.Itoa(line + 1),
		"TM_CURRENT_LINE": t.Slice(ls, vim.LineEnd(t, ls)), "TM_SELECTED_TEXT": "",
		"CURRENT_YEAR": now.Format("2006"), "CURRENT_MONTH": now.Format("01"), "CURRENT_DATE": now.Format("02"),
		"CURRENT_HOUR": now.Format("15"), "CURRENT_MINUTE": now.Format("04"), "CURRENT_SECOND": now.Format("05"),
		"CURRENT_DAY_NAME": now.Weekday().String(), "CURRENT_MONTH_NAME": now.Month().String(),
		"CURRENT_SECONDS_UNIX": strconv.FormatInt(now.Unix(), 10),
	}
}

// snippetIndent returns the indent of the line a snippet goes in at at in
// t, which its lines take, and the unit of indent b's window inserts.
func (e *Session) snippetIndent(b *buffer, t kernel.Snapshot, at int) (indent, unit string) {
	before := t.Slice(vim.LineStart(t, at), at)
	indent, unit = before[:len(before)-len(strings.TrimLeft(before, " \t"))], "\t"
	if e.win.opts.ExpandTab {
		unit = strings.Repeat(" ", e.indentWidth(b))
	}
	return indent, unit
}

// snippet is a snippet being filled in: each of its stops, in the order
// <Tab> visits them, with every place it shows, the first being the one the
// cursor goes to (B207).
type snippet struct {
	b     *buffer
	stops [][][2]kernel.MarkID
	at    int  // the stop the cursor is in
	fresh bool // its placeholder is as the snippet put it, so typing replaces it
}

// startSnippet starts filling in a snippet whose text, expanded with
// stops, starts at at in the current buffer, at its first stop. Stop 0, or
// else the snippet's end, comes last.
func (e *Session) startSnippet(at int, text string, stops []lsp.Stop) {
	b := e.win.buf
	var ns []int
	for _, s := range stops {
		if !slices.Contains(ns, s.N) {
			ns = append(ns, s.N)
		}
	}
	slices.SortFunc(ns, func(x, y int) int {
		// 0 comes after every other stop.
		if x == 0 || y == 0 {
			return cmp.Compare(y, x)
		}
		return cmp.Compare(x, y)
	})
	if !slices.Contains(ns, 0) {
		ns = append(ns, 0)
		stops = append(stops, lsp.Stop{N: 0, Start: len(text), End: len(text)})
	}
	sn := &snippet{b: b}
	for _, n := range ns {
		var places [][2]kernel.MarkID
		for _, s := range stops {
			if s.N == n {
				places = append(places, [2]kernel.MarkID{b.text.SetMark(at+s.Start, kernel.Left), b.text.SetMark(at+s.End, kernel.Right)})
			}
		}
		sn.stops = append(sn.stops, places)
	}
	e.endSnippet()
	e.snip = sn
	e.snipJump(0)
}

// snipJump moves the cursor d stops on in the snippet, and reports whether it
// did. The last stop ends the snippet, with the cursor there.
func (e *Session) snipJump(d int) bool {
	sn := e.snip
	i := sn.at + d
	if i < 0 {
		return false
	}
	start, end := sn.place(i, 0)
	e.setCursor(start)
	sn.at, sn.fresh = i, end > start
	if i == len(sn.stops)-1 {
		e.endSnippet()
	}
	return true
}

// place returns where the j-th place of stop i is.
func (sn *snippet) place(i, j int) (int, int) {
	p := sn.stops[i][j]
	return sn.b.text.Mark(p[0]), sn.b.text.Mark(p[1])
}

// key handles k for the snippet being filled in, before the vim machine
// sees it, and reports whether it took it. <Tab> and <S-Tab> go to
// the next and the stop before, and a key that types replaces a fresh
// placeholder.
func (sn *snippet) key(e *Session, k vim.Key) bool {
	if sn == nil || e.vim.Mode() != vim.Insert || e.win.buf != sn.b {
		return false
	}
	defer e.reveal()
	switch k {
	case "\t":
		return e.snipJump(1)
	case "<S-Tab>":
		return e.snipJump(-1)
	}
	if sn.fresh && (k == "<BS>" || k == "<Del>" || !strings.HasPrefix(string(k), "<") || k == "<") {
		sn.fresh = false
		start, end := sn.place(sn.at, 0)
		if err := (host{e}).Apply([]vim.Edit{{Start: start, End: end}}, "snippet"); err != nil {
			e.message = err.Error()
		}
		e.setCursor(start)
		return k == "<BS>" || k == "<Del>"
	}
	return false
}

// snippetAfter follows a key: the snippet ends when insert mode does or
// the cursor leaves its stop, and otherwise every other place of the stop
// takes the text of the first (B207).
func (e *Session) snippetAfter() {
	sn := e.snip
	if sn == nil {
		return
	}
	start, end := sn.place(sn.at, 0)
	if c := e.Cursor(); e.vim.Mode() != vim.Insert || e.win.buf != sn.b || c < start || c > end {
		e.endSnippet()
		return
	}
	t := sn.b.text.Snapshot()
	text := t.Slice(start, end)
	var edits []vim.Edit
	for j := 1; j < len(sn.stops[sn.at]); j++ {
		if s, end := sn.place(sn.at, j); t.Slice(s, end) != text {
			edits = append(edits, vim.Edit{Start: s, End: end, Text: text})
		}
	}
	slices.SortFunc(edits, func(x, y vim.Edit) int { return x.Start - y.Start })
	if len(edits) > 0 {
		if err := (host{e}).Apply(edits, "snippet"); err != nil {
			e.message = err.Error()
		}
	}
}

// endSnippet stops filling in the snippet and lets its marks go.
func (e *Session) endSnippet() {
	sn := e.snip
	if sn == nil {
		return
	}
	for _, places := range sn.stops {
		for _, p := range places {
			sn.b.text.DeleteMark(p[0])
			sn.b.text.DeleteMark(p[1])
		}
	}
	e.snip = nil
}

// snippetSpan is the fresh placeholder the cursor is on, drawn selected.
func (e *Session) snippetSpan(b *buffer) []wire.Span {
	sn := e.snip
	if sn == nil || !sn.fresh || sn.b != b {
		return nil
	}
	start, end := sn.place(sn.at, 0)
	return []wire.Span{{Start: start, End: end, Name: "snippet.placeholder"}}
}
