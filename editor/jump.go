package editor

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/vim"
)

// jumpLabels are the labels a jump's matches get, nearest first.
const jumpLabels = "asdfghjklqwertyuiopzxcvbnm"

// jump is a jump being typed: the text to find, the matches of it in view,
// nearest the cursor first, and the match each label goes to (B218).
type jump struct {
	query   string
	matches [][2]int
	labels  map[string][2]int
}

// Label is the label of a jump's match, drawn over the character at Pos,
// the one after the match (B218).
type Label struct {
	Pos  int
	Text string
}

// jumpKey takes a key while a jump is typed. A label goes to its match,
// <CR> to the nearest, <BS> drops the last character typed and <Esc>
// cancels. Any other character adds to the text to find.
func (e *Session) jumpKey(k vim.Key) {
	j := e.jumping
	s := string(k)
	switch {
	case k == "<Esc>":
		e.jumping = nil
	case k == "<CR>":
		e.jumping = nil
		if len(j.matches) > 0 {
			e.leapTo(j.matches[0][0])
		}
	case k == "<BS>":
		if j.query == "" {
			e.jumping = nil
			return
		}
		_, size := utf8.DecodeLastRuneInString(j.query)
		j.query = j.query[:len(j.query)-size]
		e.findJumps()
	case strings.HasPrefix(s, "<") && s != "<":
	default:
		if m, ok := j.labels[s]; ok {
			e.jumping = nil
			e.leapTo(m[0])
			return
		}
		j.query += s
		e.findJumps()
	}
}

// findJumps finds the jump's text in the current window's view, ignoring
// case unless it has a capital, and labels the matches nearest the cursor
// first. A label is never the character after a match, so typing more of
// the text never jumps.
func (e *Session) findJumps() {
	j := e.jumping
	j.matches, j.labels = nil, map[string][2]int{}
	if j.query == "" {
		return
	}
	t, top, c := e.Text(), e.Top(), e.Cursor()
	last := vim.LineEnd(t, t.LineStart(min(t.LineOf(top)+max(e.win.h-2, 0), t.Lines()-1)))
	hay, q := t.Slice(top, last), j.query
	if q == strings.ToLower(q) {
		hay = strings.ToLower(hay)
	}
	next := map[string]bool{}
	for at := 0; ; {
		i := strings.Index(hay[at:], q)
		if i < 0 {
			break
		}
		start := top + at + i
		j.matches = append(j.matches, [2]int{start, start + len(q)})
		if r, _ := utf8.DecodeRuneInString(hay[at+i+len(q):]); r != utf8.RuneError {
			next[strings.ToLower(string(r))] = true
		}
		at += i + 1
	}
	dist := func(m [2]int) int { return max(m[0]-c, c-m[0]) }
	slices.SortStableFunc(j.matches, func(a, b [2]int) int { return cmp.Compare(dist(a), dist(b)) })
	labels := []rune(jumpLabels)
	for _, m := range j.matches {
		for len(labels) > 0 && next[string(labels[0])] {
			labels = labels[1:]
		}
		if len(labels) == 0 {
			break
		}
		j.labels[string(labels[0])] = m
		labels = labels[1:]
	}
}

// leapTo moves the cursor to at, recording where it was as a jump.
func (e *Session) leapTo(at int) {
	e.pushJump(e.Cursor())
	e.setCursor(at)
}
