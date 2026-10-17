// Package diff compares lines of text, for the editor and its modules.
package diff

import (
	"fmt"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Op is one line of a diff: kept, removed or added.
type Op struct {
	Kind byte // ' ', '-' or '+'
	Line string
}

// Lines returns the shortest edit script turning lines a into lines b,
// by Myers' algorithm. The lines both start and end with are kept, and only
// those between are searched, so a small change to a long text is cheap.
func Lines(a, b []string) []Op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ops := make([]Op, 0, len(a)+len(b)-pre-suf)
	for _, l := range a[:pre] {
		ops = append(ops, Op{' ', l})
	}
	ops = append(ops, myers(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, Op{' ', l})
	}
	return ops
}

// myers returns the shortest edit script turning lines a into lines b.
func myers(a, b []string) []Op {
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

// backtrack walks Lines's trace back from the end to list the ops.
func backtrack(trace [][]int, a, b []string, off int) []Op {
	var ops []Op
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
			ops = append(ops, Op{' ', a[x-1]})
			x, y = x-1, y-1
		}
		if x == px {
			ops = append(ops, Op{'+', b[y-1]})
			y--
		} else {
			ops = append(ops, Op{'-', a[x-1]})
			x--
		}
	}
	for x > 0 && y > 0 {
		ops = append(ops, Op{' ', a[x-1]})
		x, y = x-1, y-1
	}
	slices.Reverse(ops)
	return ops
}

// Unified renders ops as hunks of changed lines with context lines around
// them, each under a @@ header giving its first line on each side.
func Unified(ops []Op, context int) string {
	near := make([]bool, len(ops))
	for i, o := range ops {
		if o.Kind != ' ' {
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
			out = append(out, string(o.Kind)+o.Line)
		}
		if o.Kind != '+' {
			la++
		}
		if o.Kind != '-' {
			lb++
		}
	}
	if len(out) == 0 {
		return "no difference"
	}
	return strings.Join(out, "\n")
}

// Edits returns the edits that turn old into new, one for each run of
// changed lines, so marks on the lines that stay keep their place. When a
// run has as many new lines as old ones, each line changes on its own and
// keeps its line break, so a mark on it stays on its line.
func Edits(old, new string) []api.Edit {
	var edits []api.Edit
	pos := 0 // where the next op's line starts in old
	ops := Lines(strings.SplitAfter(old, "\n"), strings.SplitAfter(new, "\n"))
	for i := 0; i < len(ops); {
		if ops[i].Kind == ' ' {
			pos, i = pos+len(ops[i].Line), i+1
			continue
		}
		var gone, added []string
		for ; i < len(ops) && ops[i].Kind != ' '; i++ {
			if ops[i].Kind == '-' {
				gone = append(gone, ops[i].Line)
			} else {
				added = append(added, ops[i].Line)
			}
		}
		if len(gone) == len(added) {
			for k, line := range gone {
				e := api.Edit{Start: pos, End: pos + len(line), Text: added[k]}
				if strings.HasSuffix(line, "\n") && strings.HasSuffix(e.Text, "\n") {
					e.End, e.Text = e.End-1, strings.TrimSuffix(e.Text, "\n")
				}
				edits = append(edits, e)
				pos += len(line)
			}
			continue
		}
		e := api.Edit{Start: pos, Text: strings.Join(added, "")}
		for _, line := range gone {
			pos += len(line)
		}
		e.End = pos
		edits = append(edits, e)
	}
	return edits
}
