// Package comment is gc, which comments the lines a motion or object
// covers, or uncomments them (B44).
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
	return []api.Registration{{Kind: api.Operator, Name: "gc", Help: "comments the lines a motion or object covers, or uncomments them", ID: 1}}
}

func (Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call toggles comments on every line of [start, end), in the comment
// syntax of the language where it starts.
func (Module) Call(_ context.Context, _ uint32, s api.Scope, args ...int) (api.Result, error) {
	t, start, end, linewise := s.Text, args[0], args[1], args[2] == 1
	cs, ok := s.Comment(start)
	if !ok {
		return api.Result{Message: "no comment syntax for this file"}, nil
	}
	last := t.LineOf(end)
	if !linewise && end > start {
		// A charwise target's end is exclusive, and may sit just past a line
		// break.
		last = t.LineOf(end - 1)
	}
	var lines []string
	for n := t.LineOf(start); n <= last; n++ {
		ls, le := t.LineStart(n), t.Len()
		if n+1 < t.Lines() {
			le = t.LineStart(n+1) - 1
		}
		lines = append(lines, t.Slice(ls, le))
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
		body = strings.TrimRight(body, " \t")
		commented = commented && strings.HasPrefix(body, tl) && strings.HasSuffix(body[len(tl):], tr)
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
