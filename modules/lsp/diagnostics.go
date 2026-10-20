package lsp

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// diagnostic is one a server published, as sent, and the decoration that
// shows it (B61).
type diagnostic struct {
	raw        json.RawMessage
	severity   int
	message    string
	source     string
	code       string
	start, end int // where it was when published
	id         int // its decoration, which moves with edits
}

// severities are the styles of diagnostics, and signs their letters, by
// the protocol's severity.
var (
	severities = map[int]string{1: "diagnostic.error", 2: "diagnostic.warn", 3: "diagnostic.info", 4: "diagnostic.hint"}
	letters    = map[int]string{1: "E", 2: "W", 3: "I", 4: "H"}
)

// published takes the diagnostics a server published for the buffer, in
// place of its last, and shows the buffer's anew. A publish that reads text
// since changed shows nothing until the server publishes again, which it
// does after each change.
func (m *Module) published(res *api.Result, s api.Scope) {
	var p struct {
		Diagnostics []json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(s.About["params"]), &p); err != nil {
		res.Log = []string{fmt.Sprintf("diagnostics from %s: %v", s.About["server"], err)}
		return
	}
	buf, enc := s.About["buffer"], s.About["encoding"]
	var ds []diagnostic
	for _, raw := range p.Diagnostics {
		var d struct {
			Range    lsp.Range       `json:"range"`
			Severity int             `json:"severity"`
			Message  string          `json:"message"`
			Source   string          `json:"source"`
			Code     json.RawMessage `json:"code"`
		}
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		if severities[d.Severity] == "" {
			d.Severity = 1
		}
		// A code is a number or a string.
		ds = append(ds, diagnostic{raw: raw, severity: d.Severity, message: d.Message, source: d.Source, code: strings.Trim(string(d.Code), `"`), start: lsp.Offset(s.Text, d.Range.Start, enc), end: lsp.Offset(s.Text, d.Range.End, enc)})
	}
	if m.diags[buf] == nil {
		m.diags[buf] = map[string][]diagnostic{}
	}
	m.diags[buf][s.About["server"]] = ds
	m.redraw(res, s, buf)
}

// detached drops a server's diagnostics for the buffer it left.
func (m *Module) detached(res *api.Result, s api.Scope, server string) {
	buf := s.About["buffer"]
	delete(m.diags[buf], server)
	m.redraw(res, s, buf)
}

// redraw shows the buffer's diagnostics from every server anew, the worst
// last so its sign shows on a line with others, each a decoration with its
// letter as the sign and its message as the note, and tells of them.
func (m *Module) redraw(res *api.Result, s api.Scope, buf string) {
	id, _ := strconv.Atoi(buf)
	for _, d := range s.Decorations() {
		if d.Buffer == id && severities[severityOf(d.Style)] != "" {
			res.Undecorate = append(res.Undecorate, d.ID)
		}
	}
	var all []*diagnostic
	for _, ds := range m.diags[buf] {
		for i := range ds {
			all = append(all, &ds[i])
		}
	}
	slices.SortStableFunc(all, func(x, y *diagnostic) int { return cmp.Compare(y.severity, x.severity) })
	// With diagnostic_notes off, the messages do not show after their lines.
	on, _ := s.Option("diagnostic_notes")
	m.notes[buf] = on == "true"
	for _, d := range all {
		m.ids++
		d.id = m.ids
		note := d.message
		if !m.notes[buf] {
			note = ""
		}
		res.Decorate = append(res.Decorate, api.Decoration{ID: d.id, Buffer: id, Start: d.start, End: d.end, Style: severities[d.severity], Sign: letters[d.severity], Note: note, Layer: 1})
	}
	res.Emit = append(res.Emit, "diagnostics")
}

// severityOf is the severity a diagnostic style names, or 0.
func severityOf(style string) int {
	for sev, st := range severities {
		if st == style {
			return sev
		}
	}
	return 0
}

// at returns the buffer's diagnostics by where each is now.
func (m *Module) at(s api.Scope, buf string) map[int][]*diagnostic {
	byID := map[int]*diagnostic{}
	for _, ds := range m.diags[buf] {
		for i := range ds {
			byID[ds[i].id] = &ds[i]
		}
	}
	id, _ := strconv.Atoi(buf)
	out := map[int][]*diagnostic{}
	for _, d := range s.Decorations() {
		if dg := byID[d.ID]; dg != nil && d.Buffer == id {
			out[d.Start] = append(out[d.Start], dg)
		}
	}
	return out
}

// step moves the cursor to the nth diagnostic after it, or before it when
// n is negative, going round the ends, and shows those there in a float
// (B61).
func (m *Module) step(res *api.Result, s api.Scope, cursor, n int) {
	at := m.at(s, s.About["buffer"])
	if len(at) == 0 {
		res.Error = "no diagnostics"
		return
	}
	starts := slices.Sorted(maps.Keys(at))
	i, found := slices.BinarySearch(starts, cursor)
	if n > 0 {
		if found {
			i++
		}
		i += n - 1
	} else {
		i += n
	}
	p := starts[((i%len(starts))+len(starts))%len(starts)]
	res.Jump = &api.Jump{Pos: p}
	res.Float = float(at[p])
}

// show shows the diagnostics on the cursor's line in a float, as <C-w>d
// does, or says there are none (B61).
func (m *Module) show(res *api.Result, s api.Scope, cursor int) {
	line := s.Text.LineOf(cursor)
	at := m.at(s, s.About["buffer"])
	var here []*diagnostic
	for _, p := range slices.Sorted(maps.Keys(at)) {
		if s.Text.LineOf(p) == line {
			here = append(here, at[p]...)
		}
	}
	if len(here) == 0 {
		res.Error = "no diagnostics on this line"
		return
	}
	res.Float = float(here)
}

// float returns the float that shows ds, each in its severity's colour with
// its source and code.
func float(ds []*diagnostic) *api.Float {
	f := &api.Float{Spans: []api.Span{}}
	var text strings.Builder
	for _, d := range ds {
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		f.Spans = append(f.Spans, api.Span{Start: text.Len(), End: text.Len() + len(d.message), Name: severities[d.severity] + ".text"})
		text.WriteString(d.message)
		source := d.source
		if d.code != "" {
			source += "(" + d.code + ")"
		}
		if source != "" {
			f.Spans = append(f.Spans, api.Span{Start: text.Len() + 2, End: text.Len() + 2 + len(source), Name: "comment"})
			text.WriteString("  " + source)
		}
	}
	f.Text = text.String()
	return f
}

// places returns the diagnostics of every buffer as places, the current
// buffer's first, each buffer's worst first and then in the order of its
// text, each as its line, severity and message (B196).
func (m *Module) places(s api.Scope) []string {
	current, _ := strconv.Atoi(s.About["buffer"])
	ids := slices.Sorted(maps.Keys(s.Buffers))
	ids = slices.Concat([]int{current}, slices.DeleteFunc(ids, func(id int) bool { return id == current }))
	var out []string
	for _, id := range ids {
		b, ok := s.Buffers[id]
		if !ok {
			continue
		}
		type placed struct {
			d     *diagnostic
			start int
		}
		var ds []placed
		for start, here := range m.at(s, strconv.Itoa(id)) {
			for _, d := range here {
				ds = append(ds, placed{d, start})
			}
		}
		slices.SortStableFunc(ds, func(x, y placed) int {
			return cmp.Or(cmp.Compare(x.d.severity, y.d.severity), cmp.Compare(x.start, y.start))
		})
		for _, p := range ds {
			line := b.Text.LineOf(p.start)
			note, _, _ := strings.Cut(p.d.message, "\n")
			out = append(out, fmt.Sprintf("%s:%d:%d:%s  %s", b.Path, line+1, p.start-b.Text.LineStart(line)+1, strings.TrimPrefix(severities[p.d.severity], "diagnostic."), note))
		}
	}
	return out
}

// context returns the raw diagnostics on the lines from start to end of the
// buffer, for a code action request.
func (m *Module) context(s api.Scope, start, end int) []json.RawMessage {
	out := []json.RawMessage{}
	first, last := s.Text.LineOf(start), s.Text.LineOf(end)
	for p, here := range m.at(s, s.About["buffer"]) {
		if l := s.Text.LineOf(p); l >= first && l <= last {
			for _, d := range here {
				out = append(out, d.raw)
			}
		}
	}
	return out
}
