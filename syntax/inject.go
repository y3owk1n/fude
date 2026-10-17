package syntax

import (
	"fmt"
	"slices"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// Region is where text of another language sits inside a buffer: its
// filetype and its bytes (B166).
type Region struct {
	Lang       string
	Start, End int
}

// region is the text an injection marks: its filetype and the ranges it
// parses as one.
type region struct {
	lang   string
	ranges []ts.Range
}

// injected is a language an injection names, with its queries.
type injected struct {
	lang                   *ts.Language
	highlights, injections *ts.Query
}

// injector highlights the regions of other languages in a buffer, on its
// worker. It has a parser and cursor of its own, so the buffer's parser keeps
// its tree (B166).
type injector struct {
	dir     string // where query overrides are
	parser  *ts.Parser
	cursor  *ts.QueryCursor
	langs   map[string]*injected // by filetype, nil for one without a grammar
	missing []problem            // injected languages that could not load, met since the loop heard
}

// injectionLevels is how deep injections nest. Markdown's inline text inside
// a block is one level, and HTML inside that is two.
const injectionLevels = 3

func newInjector(dir string) *injector {
	return &injector{dir: dir, parser: ts.NewParser(), cursor: ts.NewQueryCursor(), langs: map[string]*injected{}}
}

func (in *injector) close() {
	in.parser.Close()
	in.cursor.Close()
	for _, l := range in.langs {
		if l != nil {
			l.highlights.Close()
			l.injections.Close()
		}
	}
}

// problem is an injected language that did not load. err is nil when it
// only lacks a grammar fude could build, and else says why it failed.
type problem struct {
	filetype string
	err      error
}

// get returns ft's language and queries, or nil when it has no grammar or
// they do not load. Either goes in missing. A grammar fude could build is
// looked for again next time, as it may have built since.
func (in *injector) get(ft string) *injected {
	if l, ok := in.langs[ft]; ok {
		return l
	}
	l, ok, err := lookup(ft)
	if err == nil && !ok && Buildable(ft) {
		in.missing = append(in.missing, problem{ft, nil})
		return nil
	}
	in.langs[ft] = nil
	if err != nil || !ok {
		if err != nil {
			in.missing = append(in.missing, problem{ft, err})
		}
		return nil
	}
	hq, err := query(l.lang, in.dir, ft, "highlights", l.highlights)
	if err != nil {
		in.missing = append(in.missing, problem{ft, err})
		return nil
	}
	iq, err := query(l.lang, in.dir, ft, "injections", l.injections)
	if err != nil {
		hq.Close()
		in.missing = append(in.missing, problem{ft, err})
		return nil
	}
	in.langs[ft] = &injected{l.lang, hq, iq}
	return in.langs[ft]
}

// spans returns the spans of the regions q marks in tree within [from, to),
// to paint over tree's own, and the regions, levels deep.
func (in *injector) spans(q *ts.Query, tree *ts.Tree, text []byte, from, to, levels int) ([]Span, []Region) {
	if levels == 0 {
		return nil, nil
	}
	var spans []Span
	var regions []Region
	for _, r := range in.regions(q, tree, text, from, to) {
		l := in.get(r.lang)
		if l == nil {
			continue
		}
		if err := in.parser.SetLanguage(l.lang); err != nil {
			continue
		}
		if err := in.parser.SetIncludedRanges(r.ranges); err != nil {
			continue
		}
		t := in.parser.Parse(text, nil)
		if t == nil {
			continue
		}
		for _, rg := range r.ranges {
			regions = append(regions, Region{r.lang, int(rg.StartByte), int(rg.EndByte)})
		}
		spans = append(spans, highlight(in.cursor, l.highlights, t, text, from, to)...)
		s, rs := in.spans(l.injections, t, text, from, to, levels-1)
		spans, regions = append(spans, s...), append(regions, rs...)
		t.Close()
	}
	return spans, regions
}

// regions returns the regions q marks in tree within [from, to). A combined
// injection's ranges of one pattern and language parse as one.
func (in *injector) regions(q *ts.Query, tree *ts.Tree, text []byte, from, to int) []region {
	in.cursor.SetByteRange(uint(from), uint(to))
	names := q.CaptureNames()
	combined := map[string]int{} // the region a combined pattern's language goes in
	var out []region
	ms := in.cursor.Matches(q, tree.RootNode(), text)
	for m := ms.Next(); m != nil; m = ms.Next() {
		lang, children, joined := "", false, false
		for _, s := range q.PropertySettings(m.PatternIndex) {
			switch s.Key {
			case "injection.language":
				if s.Value != nil {
					lang = *s.Value
				}
			case "injection.include-children":
				children = true
			case "injection.combined":
				joined = true
			}
		}
		var ranges []ts.Range
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "injection.language":
				lang = c.Node.Utf8Text(text)
			case "injection.content":
				ranges = append(ranges, nodeRanges(&c.Node, children)...)
			}
		}
		lang = resolve(strings.TrimSpace(lang))
		if lang == "" || len(ranges) == 0 {
			continue
		}
		if joined {
			key := fmt.Sprint(m.PatternIndex, lang)
			if i, ok := combined[key]; ok {
				out[i].ranges = append(out[i].ranges, ranges...)
				continue
			}
			combined[key] = len(out)
		}
		out = append(out, region{lang, ranges})
	}
	for i := range out {
		slices.SortFunc(out[i].ranges, func(a, b ts.Range) int { return int(a.StartByte) - int(b.StartByte) })
	}
	return out
}

// nodeRanges returns n's range, without its named children's unless
// children. Its anonymous children, such as markdown's * and ` in its
// inline text, stay, as the injected language reads them.
func nodeRanges(n *ts.Node, children bool) []ts.Range {
	r := n.Range()
	if children || n.NamedChildCount() == 0 {
		return []ts.Range{r}
	}
	var out []ts.Range
	start, at := r.StartByte, r.StartPoint
	for i := range n.NamedChildCount() {
		c := n.NamedChild(i)
		if c.StartByte() > start {
			out = append(out, ts.Range{StartByte: start, EndByte: c.StartByte(), StartPoint: at, EndPoint: c.StartPosition()})
		}
		start, at = c.EndByte(), c.EndPosition()
	}
	if r.EndByte > start {
		out = append(out, ts.Range{StartByte: start, EndByte: r.EndByte, StartPoint: at, EndPoint: r.EndPoint})
	}
	return out
}

// injectionSettings are the settings an injections query may make.
var injectionSettings = []string{"injection.language", "injection.combined", "injection.include-children", "priority"}

// compileInjections compiles an injections query. A pattern with a predicate
// fude does not evaluate is left out rather than failing the query, as an
// injection only adds colour (B166).
func compileInjections(lang *ts.Language, src string) (*ts.Query, error) {
	q, qerr := ts.NewQuery(lang, plainLuaMatches(src))
	if qerr != nil {
		return nil, fmt.Errorf("line %d: %s", qerr.Row+1, qerr.Message)
	}
	for i := range q.PatternCount() {
		if unknownPredicate(q, i, injectionSettings) != "" {
			q.DisablePattern(i)
		}
	}
	return q, nil
}
