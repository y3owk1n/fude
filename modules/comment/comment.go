// Package comment is gc, which comments the lines a motion or object
// covers, or uncomments them, and after an operator is the block of
// commented lines around the cursor, so gcgc uncomments it (B44).
package comment

import (
	"context"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Module is gc.
type Module struct{}

// New returns gc.
func New() Module { return Module{} }

func (Module) Name() string         { return "comment" }
func (Module) Subscribed() []string { return nil }

func (Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Operator, Name: "gc", Help: "comments the lines a motion or object covers, or uncomments them", ID: 1},
		{Kind: api.Object, Name: "gc", Help: "the commented lines around the cursor, up to a line not commented or blank", ID: 2},
	}
}

func (Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call toggles comments on every line of [start, end), in the comment
// syntax of the language where it starts, or selects the block of commented
// lines around start.
func (Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	t, start, end := s.Text, args[0], args[1]
	cs, ok := s.Comment(start)
	if !ok {
		return api.Result{Error: "no comment syntax for this file"}, nil
	}
	if id == 2 {
		return block(t, t.LineOf(start), cs), nil
	}
	linewise := args[2] == 1
	last := t.LineOf(end)
	if !linewise && end > start {
		// A charwise target's end is exclusive, and may sit just past a line
		// break.
		last = t.LineOf(end - 1)
	}
	var lines []string
	for n := t.LineOf(start); n <= last; n++ {
		lines = append(lines, lineText(t, n))
	}
	var res api.Result
	pos := t.LineStart(t.LineOf(start))
	for i, line := range toggle(lines, cs) {
		if line != lines[i] {
			ed := replace(lines[i], line)
			res.Edits = append(res.Edits, api.Edit{Start: pos + ed.Start, End: pos + ed.End, Text: ed.Text})
		}
		pos += len(lines[i]) + 1
	}
	return res, nil
}

func (Module) Event(context.Context, string, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}
func (Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (Module) Close(context.Context) error                   { return nil }

// block selects the lines around line that are commented with cs and not
// blank, from the first's start to the start of the line after the last,
// or one past the text's end for the last line, which the vim machine
// takes as whole lines. It selects nothing when line is not commented.
func block(t api.Text, line int, cs string) api.Result {
	commented := func(n int) bool {
		return n >= 0 && n < t.Lines() && isComment(lineText(t, n), cs)
	}
	if !commented(line) {
		return api.Result{}
	}
	first, last := line, line
	for commented(first - 1) {
		first--
	}
	for commented(last + 1) {
		last++
	}
	end := t.Len() + 1
	if last+1 < t.Lines() {
		end = t.LineStart(last + 1)
	}
	return api.Result{Selected: &[2]int{t.LineStart(first), end}}
}

// lineText returns line n's text, without its line break.
func lineText(t api.Text, n int) string {
	ls, le := t.LineStart(n), t.Len()
	if n+1 < t.Lines() {
		le = t.LineStart(n+1) - 1
	}
	return t.Slice(ls, le)
}

// isComment reports whether line, not blank, is commented with cs.
func isComment(line, cs string) bool {
	left, right, _ := strings.Cut(cs, "%s")
	tl, tr := strings.TrimSpace(left), strings.TrimSpace(right)
	body := strings.Trim(line, " \t")
	return body != "" && strings.HasPrefix(body, tl) && strings.HasSuffix(body[len(tl):], tr)
}

// replace returns the edit that makes old into new, keeping what they
// share at either end.
func replace(old, new string) api.Edit {
	pre, suf := 0, 0
	for pre < len(old) && pre < len(new) && old[pre] == new[pre] {
		pre++
	}
	for suf < len(old)-pre && suf < len(new)-pre && old[len(old)-1-suf] == new[len(new)-1-suf] {
		suf++
	}
	return api.Edit{Start: pre, End: len(old) - suf, Text: new[pre : len(new)-suf]}
}

// toggle comments lines with cs, at the least indent of those not blank, or
// uncomments them when every line that is not blank is commented. A blank
// line becomes the comment's bare marker.
func toggle(lines []string, cs string) []string {
	left, right, _ := strings.Cut(cs, "%s")
	tl, tr := strings.TrimSpace(left), strings.TrimSpace(right)
	indent, commented, allBlank := "", true, true
	for _, l := range lines {
		body := strings.TrimLeft(l, " \t")
		if body == "" {
			continue
		}
		if ind := l[:len(l)-len(body)]; allBlank || len(ind) < len(indent) {
			indent, allBlank = ind, false
		}
		commented = commented && isComment(l, cs)
	}
	if allBlank {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case commented:
			out[i] = uncomment(l, left, right)
		case strings.TrimSpace(l) == "":
			out[i] = indent + tl + tr
		default:
			out[i] = indent + left + l[len(indent):] + right
		}
	}
	return out
}

// uncomment takes the comment marks off a line, trying them as written and
// then without their blanks. A line left blank loses its indent.
func uncomment(line, left, right string) string {
	ind := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	rest := line[len(ind):]
	body := strings.TrimRight(rest, " \t")
	trail := rest[len(body):]
	for _, p := range [][2]string{{left, right}, {strings.TrimSpace(left), strings.TrimSpace(right)}} {
		if strings.HasPrefix(body, p[0]) && strings.HasSuffix(body[len(p[0]):], p[1]) {
			mid := body[len(p[0]) : len(body)-len(p[1])]
			if strings.TrimSpace(mid) == "" {
				return mid
			}
			return ind + mid + trail
		}
	}
	return line
}
