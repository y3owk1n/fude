// Package syntax parses buffers with tree-sitter off the editor loop and
// finds the highlight captures of the text on screen.
package syntax

import (
	"cmp"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/y3owk1n/fude/api"
)

//go:embed queries/go/highlights.scm
var goHighlights string

// languages are the grammars fude links in, by filetype, with their highlight
// queries (D28, D29).
var languages = map[string]struct {
	lang       *ts.Language
	highlights string
}{
	"go": {ts.NewLanguage(tsgo.Language()), goHighlights},
}

// chunk is the most text a parse reads at once.
const chunk = 64 << 10

// Span is the range a highlight capture covers, named as the query names it,
// such as keyword or function.builtin.
type Span struct {
	Start, End int
	Name       string
}

// Highlighter parses one buffer on its own goroutine. Its methods run on the
// editor loop, and its results come back through the loop's queue.
type Highlighter struct {
	queue chan<- func()

	// The editor loop's side.
	seq   int      // changes heard
	log   []change // changes since the text the spans are for
	spans []Span   // in paint order, later spans drawing over earlier ones
	asked request

	// Shared with the worker.
	mu     sync.Mutex
	req    request
	edits  []pending    // heard since the worker last took them
	latest atomic.Int64 // the newest request, so the worker drops older parses
	wake   chan struct{}
	quit   chan struct{}
}

type change struct {
	seq   int
	edits []api.Edit
}

// pending is an edit the worker has yet to make to its tree and text, from
// the change numbered seq.
type pending struct {
	seq  int
	tree ts.InputEdit
	text api.Edit
}

type request struct {
	text          api.Text
	seq, from, to int
}

// New returns a highlighter for buffers of filetype, which sends its results
// to queue, or false when fude has no grammar for it.
func New(filetype string, queue chan<- func()) (*Highlighter, bool, error) {
	l, ok := languages[filetype]
	if !ok {
		return nil, false, nil
	}
	q, qerr := ts.NewQuery(l.lang, l.highlights)
	if qerr != nil {
		return nil, false, fmt.Errorf("highlights for %s, line %d: %s", filetype, qerr.Row+1, qerr.Message)
	}
	p := ts.NewParser()
	if err := p.SetLanguage(l.lang); err != nil {
		p.Close()
		q.Close()
		return nil, false, fmt.Errorf("grammar for %s: %w", filetype, err)
	}
	h := &Highlighter{queue: queue, wake: make(chan struct{}, 1), quit: make(chan struct{})}
	go h.run(p, q)
	return h, true, nil
}

// Close stops the highlighter's goroutine, which frees its tree-sitter state.
func (h *Highlighter) Close() { close(h.quit) }

// Changed takes a change to the text, with the text from before it.
func (h *Highlighter) Changed(before api.Text, edits []api.Edit) {
	h.seq++
	h.log = append(h.log, change{h.seq, edits})
	h.mu.Lock()
	defer h.mu.Unlock()
	// Last first, so each edit's positions hold when it is made.
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		start := point(before, e.Start)
		h.edits = append(h.edits, pending{h.seq, ts.InputEdit{
			StartByte: uint(e.Start), OldEndByte: uint(e.End), NewEndByte: uint(e.Start + len(e.Text)),
			StartPosition: start, OldEndPosition: point(before, e.End), NewEndPosition: after(start, e.Text),
		}, e})
	}
}

// Show returns the spans that colour [from, to) of t, the text now. When t
// or the range has moved past the last request, it asks the worker for new
// spans. Until they come, it returns the old ones moved through the edits
// made since.
func (h *Highlighter) Show(t api.Text, from, to int) []Span {
	if a := h.asked; a.text == nil || a.seq != h.seq || from < a.from || to > a.to {
		n := to - from
		h.asked = request{t, h.seq, max(from-n, 0), min(to+n, t.Len())}
		h.mu.Lock()
		h.req = h.asked
		h.mu.Unlock()
		h.latest.Store(int64(h.seq))
		select {
		case h.wake <- struct{}{}:
		default:
		}
	}
	var out []Span
	for _, s := range h.spans {
		for _, c := range h.log {
			s.Start, s.End = through(c.edits, s.Start, true), through(c.edits, s.End, false)
		}
		if s.Start < s.End && s.End > from && s.Start < to {
			out = append(out, s)
		}
	}
	return out
}

// take keeps spans found for the text after change seq.
func (h *Highlighter) take(seq int, spans []Span) {
	h.spans = spans
	i := 0
	for i < len(h.log) && h.log[i].seq <= seq {
		i++
	}
	h.log = h.log[i:]
}

// run parses and highlights each newest request until Close.
func (h *Highlighter) run(p *ts.Parser, q *ts.Query) {
	defer p.Close()
	defer q.Close()
	c := ts.NewQueryCursor()
	defer c.Close()
	var tree *ts.Tree
	defer func() {
		if tree != nil {
			tree.Close()
		}
	}()
	var text []byte
	parsed := -1
	for {
		select {
		case <-h.quit:
			return
		case <-h.wake:
		}
		// Take the edits up to the request's text and no further, so the tree
		// and the text stay that text.
		h.mu.Lock()
		r := h.req
		n := 0
		for n < len(h.edits) && h.edits[n].seq <= r.seq {
			n++
		}
		edits := slices.Clone(h.edits[:n])
		h.edits = slices.Delete(h.edits, 0, n)
		h.mu.Unlock()
		if tree != nil {
			for i := range edits {
				tree.Edit(&edits[i].tree)
				e := edits[i].text
				text = slices.Replace(text, e.Start, e.End, []byte(e.Text)...)
			}
		}
		if r.seq != parsed || tree == nil {
			if tree == nil {
				text = []byte(r.text.Slice(0, r.text.Len()))
			}
			// The binding copies each chunk the callback returns, so chunks stay
			// small.
			next := p.ParseWithOptions(func(i int, _ ts.Point) []byte { return text[i:min(i+chunk, len(text))] }, tree,
				&ts.ParseOptions{ProgressCallback: func(ts.ParseState) bool { return h.latest.Load() != int64(r.seq) }})
			if next == nil {
				// A newer request came, and its wake is waiting.
				p.Reset()
				continue
			}
			if tree != nil {
				tree.Close()
			}
			tree, parsed = next, r.seq
		}
		spans := highlight(c, q, tree, text, r.from, r.to)
		select {
		case h.queue <- func() { h.take(r.seq, spans) }:
		case <-h.quit:
			return
		}
	}
}

// highlight returns the captures in [from, to) in paint order, as
// tree-sitter's own highlighter picks them. A capture on a node inside
// another paints after it, and of the captures on one node only the last
// pattern's is kept.
func highlight(c *ts.QueryCursor, q *ts.Query, tree *ts.Tree, text []byte, from, to int) []Span {
	type capture struct {
		Span
		pattern uint
	}
	names := q.CaptureNames()
	c.SetByteRange(uint(from), uint(to))
	var cs []capture
	caps := c.Captures(q, tree.RootNode(), text)
	for m, i := caps.Next(); m != nil; m, i = caps.Next() {
		n := m.Captures[i].Node
		cs = append(cs, capture{Span{int(n.StartByte()), int(n.EndByte()), names[m.Captures[i].Index]}, m.PatternIndex})
	}
	slices.SortStableFunc(cs, func(a, b capture) int {
		return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(b.End, a.End), cmp.Compare(a.pattern, b.pattern))
	})
	var spans []Span
	for i, c := range cs {
		// Of the captures on one node, only the last pattern's counts.
		if i+1 < len(cs) && cs[i+1].Start == c.Start && cs[i+1].End == c.End {
			continue
		}
		spans = append(spans, c.Span)
	}
	return spans
}

// point returns pos as a tree-sitter row and byte column in t.
func point(t api.Text, pos int) ts.Point {
	row := t.LineOf(pos)
	return ts.Point{Row: uint(row), Column: uint(pos - t.LineStart(row))}
}

// after returns where text ends when it is inserted at start.
func after(start ts.Point, text string) ts.Point {
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		return ts.Point{Row: start.Row + uint(strings.Count(text, "\n")), Column: uint(len(text) - i - 1)}
	}
	return ts.Point{Row: start.Row, Column: start.Column + uint(len(text))}
}

// through moves pos through a change's sorted edits. Text inserted at pos
// goes before it when right is set.
func through(edits []api.Edit, pos int, right bool) int {
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		switch {
		case pos > e.End || pos == e.End && (e.Start < e.End || right):
			pos += len(e.Text) - (e.End - e.Start)
		case pos > e.Start:
			pos = e.Start
		}
	}
	return pos
}
