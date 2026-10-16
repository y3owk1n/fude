// Package syntax parses buffers with tree-sitter off the editor loop and
// finds the highlight captures of the text on screen.
package syntax

import (
	"cmp"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/y3owk1n/fude/api"
)

var (
	//go:embed queries/go/highlights.scm
	goHighlights string
	//go:embed queries/go/textobjects.scm
	goObjects string
)

// language is a grammar and its queries.
type language struct {
	lang                            *ts.Language
	highlights, objects, injections string
	comment                         string // %s stands for the commented text
}

// linked are the grammars fude links in, by filetype (D29).
var linked = map[string]*language{
	"go": {ts.NewLanguage(tsgo.Language()), goHighlights, goObjects, "", "// %s"},
}

// Comment returns how filetype comments a line, such as "// %s", or false
// when fude knows no comment syntax for it.
func Comment(filetype string) (string, bool) {
	if l, ok := linked[filetype]; ok {
		return l.comment, true
	}
	g := registry[filetype]
	return g.comment, g.comment != ""
}

// Object is a text object a grammar gives: the capture it selects, or ""
// for one that selects syntax nodes, and what it selects.
type Object struct {
	Capture, Help string
}

// Objects are the text objects a grammar gives, by their keys. aa also
// takes a comma.
var Objects = map[string]Object{
	"af": {"function.outer", "a function"}, "if": {"function.inner", "the body of a function"},
	"ac": {"class.outer", "a class"}, "ic": {"class.inner", "the body of a class"},
	"aa": {"parameter.inner", "an argument and its comma"}, "ia": {"parameter.inner", "an argument"},
	"an": {"", "the syntax node that holds the selection, or its parent"},
	"in": {"", "the child of the node an came from, or the first child"},
	"]n": {"", "the next syntax node beside the selection"},
	"[n": {"", "the syntax node before the selection"},
}

// chunk is the most text a parse reads at once.
const chunk = 64 << 10

// Span is the range a highlight capture covers, named as the query names it,
// such as keyword or function.builtin.
type Span struct {
	Start, End int
	Name       string
}

// Buffer is one buffer's syntax. It parses the buffer on its own goroutine.
// Its methods run on the editor loop, and its results come back through the
// loop's queue.
type Buffer struct {
	queue  chan<- func()
	parsed func()

	// The editor loop's side.
	seq     int      // changes heard
	log     []change // changes since the text the spans and tree are for
	spans   []Span   // in paint order, later spans drawing over earlier ones
	regions []Region // of other languages, inner ones after the regions holding them
	tree    *ts.Tree // a clone of the worker's last tree
	lang    *ts.Language
	objects *ts.Query
	cursor  *ts.QueryCursor
	closed  bool
	asked   request
	missing func(filetype string) // told once of each injected language with no grammar fude could build
	wanted  map[string]bool
	// climb is where an last climbed to and the nodes it climbed from, so in
	// can go back down. It lasts until the text changes.
	climb struct {
		seq  int
		at   span
		from []span
	}

	// Shared with the worker.
	mu    sync.Mutex
	req   request
	edits []pending // heard since the worker last took them
	wake  chan struct{}
	quit  chan struct{}
}

type change struct {
	seq   int
	edits []api.Edit
	tree  []ts.InputEdit // the same edits, last first, for a tree
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

// New returns the syntax of a buffer of filetype, which sends its results to
// queue, or false when fude has no grammar for it. parsed runs on the loop
// after each new parse lands, and missing once for each language the buffer
// injects that has no grammar fude could build. A query in dir/<filetype>/
// replaces the shipped one of the same name.
func New(filetype, dir string, queue chan<- func(), parsed func(), missing func(filetype string)) (*Buffer, bool, error) {
	l, ok, err := lookup(filetype)
	if !ok || err != nil {
		return nil, false, err
	}
	q, err := query(l.lang, dir, filetype, "highlights", l.highlights)
	if err != nil {
		return nil, false, err
	}
	objects, err := query(l.lang, dir, filetype, "textobjects", l.objects)
	if err != nil {
		q.Close()
		return nil, false, err
	}
	inj, err := query(l.lang, dir, filetype, "injections", l.injections)
	if err != nil {
		q.Close()
		objects.Close()
		return nil, false, err
	}
	p := ts.NewParser()
	if err := p.SetLanguage(l.lang); err != nil {
		p.Close()
		q.Close()
		objects.Close()
		inj.Close()
		return nil, false, fmt.Errorf("grammar for %s: %w", filetype, err)
	}
	h := &Buffer{
		queue: queue, parsed: parsed, missing: missing, wanted: map[string]bool{}, lang: l.lang, objects: objects, cursor: ts.NewQueryCursor(),
		wake: make(chan struct{}, 1), quit: make(chan struct{}),
	}
	go h.run(filetype, p, q, inj, newInjector(dir))
	return h, true, nil
}

// query compiles the query name of filetype from dir when the file is
// there, and from shipped otherwise.
func query(lang *ts.Language, dir, filetype, name, shipped string) (*ts.Query, error) {
	path, src := filepath.Join(filetype, name+".scm"), shipped
	if dir != "" {
		p := filepath.Join(dir, path)
		b, err := os.ReadFile(p)
		switch {
		case err == nil:
			path, src = p, string(b)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("read query: %w", err)
		}
	}
	compiler := compile
	if name == "injections" {
		compiler = compileInjections
	}
	q, err := compiler(lang, src)
	if err != nil {
		return nil, fmt.Errorf("%s %w", path, err)
	}
	return q, nil
}

// compile compiles the query src. It refuses a predicate fude does not
// evaluate, which is any outside the eq?, match? and any-of? families. Its
// errors name the line in src.
func compile(lang *ts.Language, src string) (*ts.Query, error) {
	q, qerr := ts.NewQuery(lang, plainLuaMatches(src))
	if qerr != nil {
		return nil, fmt.Errorf("line %d: %s", qerr.Row+1, qerr.Message)
	}
	for i := range q.PatternCount() {
		if unknown := unknownPredicate(q, i, []string{"priority"}); unknown != "" {
			line := strings.Count(src[:q.StartByteForPattern(i)], "\n") + 1
			q.Close()
			return nil, fmt.Errorf("line %d: fude does not evaluate #%s", line, unknown)
		}
	}
	return q, nil
}

// unknownPredicate names the first predicate of pattern i that fude does not
// evaluate, a setting outside settings among them, or is "".
func unknownPredicate(q *ts.Query, i uint, settings []string) string {
	switch {
	case len(q.GeneralPredicates(i)) > 0:
		return q.GeneralPredicates(i)[0].Operator
	// A #set! "priority" orders highlights in other editors, and means
	// nothing to fude.
	case slices.ContainsFunc(q.PropertySettings(i), func(p ts.QueryProperty) bool { return !slices.Contains(settings, p.Key) }):
		return "set!"
	// fude keeps no locals, so #is-not? local always holds (D28).
	case slices.ContainsFunc(q.PropertyPredicates(i), func(p ts.PropertyPredicate) bool { return p.Positive || p.Property.Key != "local" }):
		if q.PropertyPredicates(i)[0].Positive {
			return "is?"
		}
		return "is-not?"
	}
	return ""
}

// plainLuaMatches rewrites each #lua-match? in src whose pattern means the
// same as a regular expression to #match? (D28).
func plainLuaMatches(src string) string {
	return luaMatch.ReplaceAllStringFunc(src, func(m string) string {
		if pat := luaMatch.FindStringSubmatch(m)[2]; plainLua(pat) {
			return strings.Replace(m, "#lua-match?", "#match?", 1)
		}
		return m
	})
}

// luaMatch finds a #lua-match? predicate, with its capture and pattern.
var luaMatch = regexp.MustCompile(`#lua-match\? +(@[\w.]+) +"((?:[^"\\]|\\.)*)"`)

// plainLua reports whether the Lua pattern pat means the same as a regular
// expression: it has no % item, and no - outside a set, which in Lua repeats
// lazily (D28).
func plainLua(pat string) bool {
	set := false
	for _, c := range pat {
		switch {
		case c == '%':
			return false
		case c == '[':
			set = true
		case c == ']':
			set = false
		case c == '-' && !set:
			return false
		}
	}
	return true
}

// Close stops the buffer's goroutine, which frees its tree-sitter state, and
// frees the editor loop's.
func (h *Buffer) Close() {
	close(h.quit)
	h.closed = true
	h.objects.Close()
	h.cursor.Close()
	if h.tree != nil {
		h.tree.Close()
	}
}

// Changed takes a change to the text, with the text from before it.
func (h *Buffer) Changed(before api.Text, edits []api.Edit) {
	h.seq++
	c := change{seq: h.seq, edits: edits}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Last first, so each edit's positions hold when it is made.
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		start := point(before, e.Start)
		in := ts.InputEdit{
			StartByte: uint(e.Start), OldEndByte: uint(e.End), NewEndByte: uint(e.Start + len(e.Text)),
			StartPosition: start, OldEndPosition: point(before, e.End), NewEndPosition: after(start, e.Text),
		}
		c.tree = append(c.tree, in)
		h.edits = append(h.edits, pending{h.seq, in, e})
	}
	h.log = append(h.log, c)
}

// Show returns the spans that colour [from, to) of t, the text now. When t
// or the range has moved past the last request, it asks the worker for new
// spans. Until they come, it returns the old ones moved through the edits
// made since.
func (h *Buffer) Show(t api.Text, from, to int) []Span {
	if a := h.asked; a.text == nil || a.seq != h.seq || from < a.from || to > a.to {
		n := to - from
		h.asked = request{t, h.seq, max(from-n, 0), min(to+n, t.Len())}
		h.mu.Lock()
		h.req = h.asked
		h.mu.Unlock()
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

// Refresh makes the next Show ask for spans again, as for a grammar of an
// injected language that came since the last (B166).
func (h *Buffer) Refresh() { h.asked = request{} }

// LanguageAt returns the filetype of the injected region at pos in the text
// now, the innermost when they nest, or "" outside them (B44, B166).
func (h *Buffer) LanguageAt(pos int) string {
	lang := ""
	for _, r := range h.regions {
		for _, c := range h.log {
			r.Start, r.End = through(c.edits, r.Start, true), through(c.edits, r.End, false)
		}
		if r.Start <= pos && pos < r.End {
			lang = r.Lang
		}
	}
	return lang
}

// take keeps the spans, regions and tree found for the text after change
// seq, and tells of the injected languages with no grammar.
func (h *Buffer) take(seq int, spans []Span, regions []Region, missing []string, tree *ts.Tree) {
	if h.closed {
		tree.Close()
		return
	}
	for _, ft := range missing {
		if !h.wanted[ft] && h.missing != nil {
			h.wanted[ft] = true
			h.missing(ft)
		}
	}
	if h.tree != nil {
		h.tree.Close()
	}
	h.spans, h.regions, h.tree = spans, regions, tree
	i := 0
	for i < len(h.log) && h.log[i].seq <= seq {
		i++
	}
	h.log = h.log[i:]
	h.parsed()
}

// run parses the filetype's text and highlights each newest request until
// Close, with the regions of other languages that inj marks.
func (h *Buffer) run(filetype string, p *ts.Parser, q, inj *ts.Query, in *injector) {
	defer p.Close()
	defer q.Close()
	defer inj.Close()
	defer in.close()
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
			// A parse runs to the end even when a newer request comes, since a
			// stopped parse would start over. Only Close stops one. Reset does
			// not clear a parser stopped as it balances its tree, so its next
			// parse resumes the freed tree and aborts.
			stop := func(ts.ParseState) bool {
				select {
				case <-h.quit:
					return true
				default:
					return false
				}
			}
			// The binding copies each chunk the callback returns, so chunks stay
			// small.
			at := time.Now()
			next := p.ParseWithOptions(func(i int, _ ts.Point) []byte { return text[i:min(i+chunk, len(text))] }, tree,
				&ts.ParseOptions{ProgressCallback: stop})
			if next == nil {
				return
			}
			if tree != nil {
				tree.Close()
			}
			tree, parsed = next, r.seq
			api.Trace("syntax", fmt.Sprintf("parsed %s, %d bytes after %d edits", filetype, len(text), len(edits)), time.Since(at))
		}
		at := time.Now()
		spans := highlight(c, q, tree, text, r.from, r.to)
		inner, regions := in.spans(inj, tree, text, r.from, r.to, injectionLevels)
		spans = append(spans, inner...)
		api.Trace("syntax", fmt.Sprintf("highlighted bytes %d to %d of %s, %d spans", r.from, r.to, filetype, len(spans)), time.Since(at))
		missing := in.missing
		in.missing = nil
		clone := tree.Clone()
		select {
		case h.queue <- func() { h.take(r.seq, spans, regions, missing, clone) }:
		case <-h.quit:
			clone.Close()
			return
		}
	}
}

// Object returns the range of t, the text now, that the text object name
// selects from [start, end), the selection or the character under the
// cursor. It queries the last parse, moved through the edits made since. It
// is false before the first parse, or when there is no such object. name is
// a key of Objects.
func (h *Buffer) Object(t api.Text, name string, start, end int) (int, int, bool) {
	if h.tree == nil {
		return 0, 0, false
	}
	tree, done := h.current()
	defer done()
	if Objects[name].Capture == "" {
		return h.node(tree.RootNode(), name, span{start, end})
	}
	want, _ := h.objects.CaptureIndexForName(Objects[name].Capture)
	h.cursor.SetByteRange(uint(start), uint(max(end, start+1)))
	var best span
	var node *ts.Node
	matches := h.cursor.Matches(h.objects, tree.RootNode(), nil)
	for m := matches.Next(); m != nil; m = matches.Next() {
		// A capture that repeats in one match, such as a struct's fields,
		// selects from the first to the last.
		s, e, n := -1, 0, (*ts.Node)(nil)
		for _, c := range m.Captures {
			if c.Index != uint32(want) {
				continue
			}
			if s < 0 {
				s, n = int(c.Node.StartByte()), &c.Node
			}
			e = int(c.Node.EndByte())
		}
		if s >= 0 && s <= start && end <= e && start < e && (node == nil || e-s < best.end-best.start) {
			best, node = span{s, e}, n
		}
	}
	if node == nil {
		return 0, 0, false
	}
	best = trim(t, best)
	if name == "aa" {
		best.start, best.end = withComma(node, best.start, best.end)
	}
	return best.start, best.end, true
}

// Select returns the range of every node a capture of the query src names
// within [start, end) of t, the text now. It queries the last parse, moved
// through the edits made since.
func (h *Buffer) Select(t api.Text, src string, start, end int) ([][2]int, error) {
	if h.tree == nil {
		return nil, errors.New("the file is not parsed yet")
	}
	q, err := compile(h.lang, src)
	if err != nil {
		return nil, fmt.Errorf("query %w", err)
	}
	defer q.Close()
	if len(q.CaptureNames()) == 0 {
		return nil, errors.New("the query captures nothing")
	}
	tree, done := h.current()
	defer done()
	c := ts.NewQueryCursor()
	defer c.Close()
	c.SetByteRange(uint(start), uint(end))
	var out [][2]int
	caps := c.Captures(q, tree.RootNode(), []byte(t.Slice(0, t.Len())))
	for m, i := caps.Next(); m != nil; m, i = caps.Next() {
		n := m.Captures[i].Node
		if s, e := int(n.StartByte()), int(n.EndByte()); start <= s && e <= end {
			out = append(out, [2]int{s, e})
		}
	}
	return out, nil
}

// current returns the last parse moved through the edits made since, and
// the func to call when done with it.
func (h *Buffer) current() (*ts.Tree, func()) {
	if len(h.log) == 0 {
		return h.tree, func() {}
	}
	tree := h.tree.Clone()
	for _, c := range h.log {
		for i := range c.tree {
			tree.Edit(&c.tree[i])
		}
	}
	return tree, tree.Close
}

// span is a range of bytes.
type span struct{ start, end int }

// trim drops the blanks and newline a grammar may end a node with.
func trim(t api.Text, s span) span {
	for s.end > s.start && strings.Contains(" \t\n", t.Slice(s.end-1, s.end)) {
		s.end--
	}
	return s
}

// node runs an, in, ]n or [n on sel. an
// selects the node that holds sel, or its parent when sel is that node. in
// goes back to the child an came from, or to the first child. ]n and [n
// select the next and previous node beside it.
func (h *Buffer) node(root *ts.Node, name string, sel span) (int, int, bool) {
	of := func(n *ts.Node) span { return span{int(n.StartByte()), int(n.EndByte())} }
	n := root.NamedDescendantForByteRange(uint(sel.start), uint(sel.end))
	if n == nil {
		return 0, 0, false
	}
	// up climbs past parents with n's range, and down past such children.
	up := func(n *ts.Node) *ts.Node {
		for p := n.Parent(); p != nil && of(p) == of(n); p = n.Parent() {
			n = p
		}
		return n
	}
	down := func(n *ts.Node) *ts.Node {
		for {
			var same *ts.Node
			for i := uint(0); i < n.NamedChildCount() && same == nil; i++ {
				if c := n.NamedChild(i); of(c) == of(n) {
					same = c
				}
			}
			if same == nil {
				return n
			}
			n = same
		}
	}
	var to span
	switch name {
	case "an":
		if of(n) != sel {
			to = of(n)
			break
		}
		n = up(n)
		p := n.Parent()
		if p == nil {
			return 0, 0, false
		}
		if h.climb.seq != h.seq || h.climb.at != of(n) {
			h.climb.from = nil
		}
		h.climb.from = append(h.climb.from, of(n))
		h.climb.seq, h.climb.at = h.seq, of(p)
		to = of(p)
	case "in":
		n = down(n)
		switch {
		case of(n) != sel:
			to = of(n)
			if c := inside(n, sel, of); c != nil {
				to = of(c)
			}
		case h.climb.seq == h.seq && h.climb.at == sel && len(h.climb.from) > 0:
			last := len(h.climb.from) - 1
			to, h.climb.from = h.climb.from[last], h.climb.from[:last]
			h.climb.at = to
		default:
			h.climb.from = nil
			to = of(n)
			for i := uint(0); i < n.NamedChildCount(); i++ {
				if c := of(n.NamedChild(i)); c.start < c.end {
					to = c
					break
				}
			}
		}
	case "]n", "[n":
		n = up(n)
		p := n.Parent()
		if p == nil {
			return 0, 0, false
		}
		i, count, step := 0, int(p.NamedChildCount()), 1
		if name == "[n" {
			step = -1
		}
		for i < count && p.NamedChild(uint(i)).Id() != n.Id() {
			i++
		}
		for {
			if i += step; i < 0 || i >= count {
				return 0, 0, false
			}
			if to = of(p.NamedChild(uint(i))); to.start < to.end {
				break
			}
		}
	}
	return to.start, to.end, true
}

// inside returns the first node under n that lies within sel, or nil.
func inside(n *ts.Node, sel span, of func(*ts.Node) span) *ts.Node {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		r := of(c)
		switch {
		case r.start == r.end:
		case sel.start <= r.start && r.end <= sel.end:
			return c
		case r.start < sel.end && sel.start < r.end:
			if d := inside(c, sel, of); d != nil {
				return d
			}
		}
	}
	return nil
}

// withComma widens a parameter to the comma and blanks after it, or when it
// is last, to the end of the one before it.
func withComma(n *ts.Node, start, end int) (int, int) {
	if next := n.NextSibling(); next != nil && next.Kind() == "," {
		if after := next.NextNamedSibling(); after != nil {
			return start, int(after.StartByte())
		}
		return start, int(next.EndByte())
	}
	if prev := n.PrevSibling(); prev != nil && prev.Kind() == "," {
		if before := prev.PrevNamedSibling(); before != nil {
			return int(before.EndByte()), end
		}
		return int(prev.StartByte()), end
	}
	return start, end
}

// Highlight returns the highlight captures of text in filetype's language,
// in paint order, or none when fude has no grammar for it. It parses on the
// caller's goroutine, so text should be short, such as code in a hover. A
// query in dir/<filetype>/ replaces the shipped one, as for buffers.
func Highlight(filetype, dir, text string) ([]Span, error) {
	l, ok, err := lookup(filetype)
	if !ok || err != nil {
		return nil, err
	}
	q, err := query(l.lang, dir, filetype, "highlights", l.highlights)
	if err != nil {
		return nil, err
	}
	defer q.Close()
	p := ts.NewParser()
	defer p.Close()
	if err := p.SetLanguage(l.lang); err != nil {
		return nil, fmt.Errorf("grammar for %s: %w", filetype, err)
	}
	inj, err := query(l.lang, dir, filetype, "injections", l.injections)
	if err != nil {
		return nil, err
	}
	defer inj.Close()
	tree := p.Parse([]byte(text), nil)
	defer tree.Close()
	c := ts.NewQueryCursor()
	defer c.Close()
	in := newInjector(dir)
	defer in.close()
	spans := highlight(c, q, tree, []byte(text), 0, len(text))
	inner, _ := in.spans(inj, tree, []byte(text), 0, len(text), injectionLevels)
	return append(spans, inner...), nil
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
