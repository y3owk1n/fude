package editor

import (
	"strings"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// comment is the gc operator. It toggles comments on every line of the
// target in the comment syntax of the current buffer's language.
func (e *Session) comment(t vim.Text, start, end int, linewise bool) []vim.Edit {
	cs, ok := syntax.Comment(e.languageAt(e.win.buf, e.Cursor()))
	if !ok {
		e.message = "no comment syntax for this file"
		return nil
	}
	last := t.LineOf(end)
	if !linewise && end > start {
		// A charwise target's end is exclusive, and may sit just past a line
		// break.
		last = t.LineOf(end - 1)
	}
	var lines []string
	for n := t.LineOf(start); n <= last; n++ {
		ls := t.LineStart(n)
		lines = append(lines, t.Slice(ls, vim.LineEnd(t, ls)))
	}
	var edits []vim.Edit
	pos := vim.LineStart(t, start)
	for i, line := range toggle(lines, cs) {
		if line != lines[i] {
			ed := replace(lines[i], line)
			edits = append(edits, vim.Edit{Start: pos + ed.Start, End: pos + ed.End, Text: ed.Text})
		}
		pos += len(lines[i]) + 1
	}
	return edits
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
