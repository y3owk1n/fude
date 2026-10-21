// Package review shows the proposals clients made in a window, each as its
// number and the lines it changes, where a, r, A and R accept and reject
// them, and <CR> goes to one (B391). Its window follows as proposals come,
// go and go stale.
package review

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/api/diff"
)

// What review registers, by the ids Call runs them by.
const (
	show uint32 = iota + 1
	accept
	reject
	acceptAll
	rejectAll
	visit
)

// title is the review window's.
const title = "[review]"

// keys are the review window's own keys, which [keymap.review] changes.
var keys = map[string]string{
	"a": "<Plug>(review.accept)", "r": "<Plug>(review.reject)",
	"A": "<Plug>(review.accept_all)", "R": "<Plug>(review.reject_all)",
	"<CR>": "<Plug>(review.open)", "q": ":close<CR>",
}

// Module is review. It keeps nothing, as each call reads the proposals as
// they are.
type Module struct{}

// New returns review.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "review" }

func (m *Module) Subscribed() []string { return []string{"proposals.changed"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "review", Help: "shows the proposals clients made, to accept or reject", ID: show},
		{Kind: api.Plug, Name: "accept", Help: "accepts the proposal under the cursor", ID: accept},
		{Kind: api.Plug, Name: "reject", Help: "rejects the proposal under the cursor", ID: reject},
		{Kind: api.Plug, Name: "accept_all", Help: "accepts every proposal the window shows", ID: acceptAll},
		{Kind: api.Plug, Name: "reject_all", Help: "rejects every proposal the window shows", ID: rejectAll},
		{Kind: api.Plug, Name: "open", Help: "goes to the proposal under the cursor", ID: visit},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call shows the window, or acts on the proposal under the cursor or on
// all of them.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	ps := s.Proposals()
	if id == show {
		if len(ps) == 0 {
			return api.Result{Error: "no proposals"}, nil
		}
		return api.Result{Show: panel(s, ps)}, nil
	}
	if s.About["title"] != title {
		return api.Result{Error: "not in the review window"}, nil
	}
	if id == acceptAll || id == rejectAll {
		ex := map[uint32]string{acceptAll: "accept", rejectAll: "reject"}[id]
		for _, p := range ps {
			ex += fmt.Sprintf(" %d", p.ID)
		}
		return api.Result{Ex: []string{ex}}, nil
	}
	_, _, shown := render(s, ps)
	line := s.Text.LineOf(args[0])
	if line >= len(shown) || shown[line] == 0 {
		return api.Result{Error: "not on a proposal"}, nil
	}
	i := slices.IndexFunc(ps, func(p api.Proposal) bool { return p.ID == shown[line] })
	p := ps[i]
	switch id {
	case accept:
		return api.Result{Ex: []string{fmt.Sprintf("accept %d", p.ID)}}, nil
	case reject:
		return api.Result{Ex: []string{fmt.Sprintf("reject %d", p.ID)}}, nil
	}
	t := s.Buffers[p.Buffer].Text
	at := t.LineOf(p.Start)
	return api.Result{Jump: &api.Jump{Buffer: p.Buffer, Line: at + 1, Column: p.Start - t.LineStart(at) + 1}}, nil
}

// Event shows the proposals again in the review window, if one is open.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	p := panel(s, s.Proposals())
	p.Update = true
	return api.Result{Show: p}, nil
}

// panel is the review window, showing ps.
func panel(s api.Scope, ps []api.Proposal) *api.Panel {
	text, spans, _ := render(s, ps)
	return &api.Panel{Unnumbered: true, Title: title, Text: text, Spans: spans, Keys: keys, Kind: "review"}
}

// render returns the text that shows ps, by buffer, its styles, and by
// line the number of the proposal each line shows, 0 for none.
func render(s api.Scope, ps []api.Proposal) (string, []api.Span, []int) {
	if len(ps) == 0 {
		return "no proposals", nil, []int{0}
	}
	var lines []string
	var spans []api.Span
	var shown []int
	at := 0
	add := func(line, style string, id int) {
		if style != "" {
			spans = append(spans, api.Span{Start: at, End: at + len(line), Name: style})
		}
		lines, shown = append(lines, line), append(shown, id)
		at += len(line) + 1
	}
	// A buffer's proposals show together, the buffers in the order of
	// their first.
	var order []int
	for _, p := range ps {
		if !slices.Contains(order, p.Buffer) {
			order = append(order, p.Buffer)
		}
	}
	for _, b := range order {
		buf := s.Buffers[b]
		if len(lines) > 0 {
			add("", "", 0)
		}
		name := buf.Name
		// A path a client gave whole shows from the working directory.
		if rel, err := filepath.Rel(s.About["cwd"], name); filepath.IsAbs(name) && err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
		add(name, "markup.heading", 0)
		for _, p := range ps {
			if p.Buffer != b {
				continue
			}
			t := buf.Text
			first := t.LineOf(p.Start)
			head, style := fmt.Sprintf("%d at line %d by %s", p.ID, first+1, p.By), "comment"
			if p.Stale {
				head, style = head+", stale", "proposal.stale"
			}
			add(head, style, p.ID)
			// The lines the proposal touches, before and after it.
			start, end := t.LineStart(first), t.Len()
			if last := t.LineOf(p.End); last+1 < t.Lines() {
				end = t.LineStart(last+1) - 1
			}
			before := t.Slice(start, end)
			after := t.Slice(start, p.Start) + p.Text + t.Slice(p.End, end)
			for _, op := range diff.Lines(strings.Split(before, "\n"), strings.Split(after, "\n")) {
				switch op.Kind {
				case '-':
					add("- "+op.Line, "diff.minus", p.ID)
				case '+':
					add("+ "+op.Line, "diff.plus", p.ID)
				default:
					add("  "+op.Line, "", p.ID)
				}
			}
		}
	}
	return strings.Join(lines, "\n"), spans, shown
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
