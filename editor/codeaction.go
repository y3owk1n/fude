package editor

import (
	"cmp"
	"encoding/json"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api/diff"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// pick is a code action menu: the actions, the best first, and the one
// selected (B227).
type pick struct {
	b       *buffer
	t       kernel.Snapshot
	actions []action
	sel     int
}

// action is a code action from a server, as the menu shows it.
type action struct {
	reply
	title, kind, disabled string
	preferred             bool
	preview               string // what it changes, as a diff
	spans                 []syntax.Span
	resolving             bool
}

// codeActions asks each server for the code actions at the cursor, or for
// the selection in visual mode, with the diagnostics it published on those
// lines, and shows them to pick from. With several servers, each action
// names its own (B66, B169, B227).
func (e *Session) codeActions() {
	b := e.win.buf
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	start, end := c, c
	if sel, ok := e.Selection(); ok {
		start, end = sel.Start, sel.End
		e.settle(e.vim.Feed("<Esc>"))
	}
	// build runs off the loop, so this gathers the diagnostics first.
	diags := map[*server][]json.RawMessage{}
	for _, s := range b.servers {
		diags[s] = []json.RawMessage{}
		for _, d := range e.decos[diagnosed{s, b}] {
			if l := t.LineOf(b.text.Mark(d.start)); d.raw != nil && l >= t.LineOf(start) && l <= t.LineOf(end) {
				diags[s] = append(diags[s], d.raw)
			}
		}
	}
	asked := ask(b.servers, "textDocument/codeAction", func(s *server, enc string) any {
		return map[string]any{
			"textDocument": map[string]any{"uri": uri}, "range": lsp.Range{Start: lsp.Point(t, start, enc), End: lsp.Point(t, end, enc)},
			"context": map[string]any{"diagnostics": diags[s]},
		}
	}, func(rs []reply) {
		var actions []action
		for _, r := range rs {
			var raws []json.RawMessage
			json.Unmarshal(r.res, &raws)
			for _, raw := range raws {
				var a struct {
					Title       string `json:"title"`
					Kind        string `json:"kind"`
					IsPreferred bool   `json:"isPreferred"`
					Disabled    *struct {
						Reason string `json:"reason"`
					} `json:"disabled"`
				}
				json.Unmarshal(raw, &a)
				// A kind such as source.organizeImports shows as its family, source.
				kind, _, _ := strings.Cut(a.Kind, ".")
				act := action{reply: reply{r.s, raw}, title: a.Title, kind: kind, preferred: a.IsPreferred}
				if a.Disabled != nil {
					act.disabled = cmp.Or(a.Disabled.Reason, "disabled")
				}
				if len(rs) > 1 {
					act.kind = strings.TrimSpace(act.kind + " " + r.s.name)
				}
				actions = append(actions, act)
			}
		}
		if len(actions) == 0 {
			e.message = "no code actions"
			return
		}
		// The preferred ones first, then fixes, refactors and the rest, each
		// in the servers' order.
		rank := func(a action) int {
			switch {
			case a.disabled != "":
				return 4
			case a.preferred:
				return 0
			case strings.HasPrefix(a.kind, "quickfix"):
				return 1
			case strings.HasPrefix(a.kind, "refactor"):
				return 2
			}
			return 3
		}
		slices.SortStableFunc(actions, func(x, y action) int { return cmp.Compare(rank(x), rank(y)) })
		e.pick = &pick{b: b, t: t, actions: actions}
		e.showActions()
	})
	if !asked {
		e.message = "no language server offers code actions"
	}
}

// showActions draws the code action menu at the cursor, each action with
// its number, title and kind, and the selected one's changes beside it.
func (e *Session) showActions() {
	p := e.pick
	var s strings.Builder
	var spans []syntax.Span
	width := 0
	for _, a := range p.actions {
		width = max(width, utf8.RuneCountInString(a.title))
	}
	for i, a := range p.actions {
		if i > 0 {
			s.WriteByte('\n')
		}
		start := s.Len()
		num := " "
		if i < 9 {
			num = strconv.Itoa(i + 1)
		}
		spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(num), Name: "comment"})
		s.WriteString(num + "  " + a.title)
		if a.kind != "" {
			s.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(a.title)+2))
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(a.kind), Name: "comment"})
			s.WriteString(a.kind)
		}
		if a.disabled != "" {
			spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: "comment"})
		}
		if i == p.sel {
			spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: "menu.selected"})
		}
	}
	e.float.text, e.float.spans = s.String(), spans
	e.float.doc, e.float.docSpans = e.actionPreview(p, p.sel)
}

// actionPreview returns what action i of p changes: each file's lines that
// differ, with a line around them, or the command it runs, or why it is
// disabled. An action that has neither edit nor command is resolved first,
// when its server can, and shows once it is.
func (e *Session) actionPreview(p *pick, i int) (string, []syntax.Span) {
	a := &p.actions[i]
	if a.disabled != "" {
		return a.disabled, []syntax.Span{{Start: 0, End: len(a.disabled), Name: "comment"}}
	}
	if a.preview != "" {
		return a.preview, a.spans
	}
	var raw struct {
		Edit    json.RawMessage `json:"edit"`
		Command json.RawMessage `json:"command"`
		Data    json.RawMessage `json:"data"`
	}
	json.Unmarshal(a.res, &raw)
	if raw.Edit == nil && raw.Command == nil && raw.Data != nil && resolvable(a.s) {
		if !a.resolving {
			a.resolving = true
			a.s.client.Request("codeAction/resolve", func(string) any { return a.res }, func(res json.RawMessage) {
				if res != nil && e.pick == p {
					a.res = res
					e.showActions()
				}
			})
		}
		return "", nil
	}
	if raw.Edit != nil {
		a.preview, a.spans = e.editPreview(a.s, p, raw.Edit)
	}
	if a.preview == "" && raw.Command != nil {
		// A command is an object, or a bare action's command is its name.
		var c struct {
			Title   string `json:"title"`
			Command string `json:"command"`
		}
		if json.Unmarshal(raw.Command, &c) != nil {
			json.Unmarshal(raw.Command, &c.Command)
		}
		a.preview = "runs " + cmp.Or(c.Title, c.Command)
		a.spans = []syntax.Span{{Start: 0, End: len(a.preview), Name: "comment"}}
	}
	return a.preview, a.spans
}

// resolvable reports whether s resolves code actions.
func resolvable(s *server) bool {
	var p struct {
		ResolveProvider bool `json:"resolveProvider"`
	}
	json.Unmarshal(s.client.Capability("codeActionProvider"), &p)
	return p.ResolveProvider
}

// editPreview returns the lines a workspace edit changes, file by file as
// a diff, under each file's name when it changes another file than p's.
func (e *Session) editPreview(s *server, p *pick, edit json.RawMessage) (string, []syntax.Span) {
	files, err := workspaceFiles(edit)
	if err != nil {
		return err.Error(), nil
	}
	var out strings.Builder
	var spans []syntax.Span
	for _, uri := range slices.Sorted(maps.Keys(files)) {
		u, err := url.Parse(uri)
		if err != nil {
			continue
		}
		var before kernel.Snapshot
		switch i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(u.Path) }); {
		case p.b.is(u.Path):
			before = p.t
		case i >= 0:
			before = e.bufs[i].text.Snapshot()
		default:
			if e.outside(u.Path) != nil {
				continue
			}
			data, err := os.ReadFile(u.Path)
			if err != nil {
				continue
			}
			before = kernel.New(strings.TrimSuffix(string(data), "\n"), nil).Snapshot()
		}
		edits := kernelEdits(before, files[uri].edits, s.client.Encoding())
		after := before.Slice(0, before.Len())
		for _, ed := range slices.Backward(edits) {
			after = after[:ed.Start] + ed.Text + after[ed.End:]
		}
		if !p.b.is(u.Path) || len(files) > 1 {
			name := filepath.Base(u.Path)
			spans = append(spans, syntax.Span{Start: out.Len(), End: out.Len() + len(name), Name: "markup.heading"})
			out.WriteString(name + "\n")
		}
		// A run of changes after the first shows after ⋯, and none says
		// where it is.
		for i, l := range strings.Split(diff.Unified(diff.Lines(strings.Split(before.Slice(0, before.Len()), "\n"), strings.Split(after, "\n")), 1), "\n") {
			switch {
			case l == "" || strings.HasPrefix(l, "@@") && i == 0:
				continue
			case strings.HasPrefix(l, "@@"):
				l = "⋯"
			}
			if style := map[byte]string{'-': "diff.minus", '+': "diff.plus"}[l[0]]; style != "" {
				spans = append(spans, syntax.Span{Start: out.Len(), End: out.Len() + len(l), Name: style})
			}
			out.WriteString(l + "\n")
		}
	}
	return strings.TrimSuffix(out.String(), "\n"), spans
}

// picking takes a key while code actions show. <C-n>, <C-p>, j, k and the
// arrows move the selection, <CR> takes the selected action and a digit the
// one it numbers, and any other key closes the menu.
func (p *pick) key(e *Session, k vim.Key) bool {
	if p == nil {
		return false
	}
	defer e.reveal()
	switch k {
	case "<C-n>", "<Down>", "j", "\t":
		p.sel = (p.sel + 1) % len(p.actions)
	case "<C-p>", "<Up>", "k", "<S-Tab>":
		p.sel = (p.sel + len(p.actions) - 1) % len(p.actions)
	case "<CR>", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := p.sel
		if k != "<CR>" {
			i = int(k[0] - '1')
		}
		e.pick, e.float.text, e.float.doc, e.float.docSpans = nil, "", "", nil
		switch {
		case i >= len(p.actions):
		case p.actions[i].disabled != "":
			e.message = p.actions[i].title + ": " + p.actions[i].disabled
		default:
			e.act(p, p.actions[i].reply)
		}
		return true
	default:
		e.pick, e.float.text, e.float.doc, e.float.docSpans = nil, "", "", nil
		return true
	}
	e.showActions()
	return true
}

// act takes a code action from its server: it applies its edit and runs its
// command. An action with neither but data is resolved first, when the
// server can.
func (e *Session) act(p *pick, a reply) {
	s, raw := a.s, a.res
	var action struct {
		Edit      json.RawMessage `json:"edit"`
		Command   json.RawMessage `json:"command"`
		Arguments json.RawMessage `json:"arguments"`
		Data      json.RawMessage `json:"data"`
	}
	json.Unmarshal(raw, &action)
	if action.Edit == nil && action.Command == nil && action.Data != nil && resolvable(s) {
		s.client.Request("codeAction/resolve", func(string) any { return raw }, func(res json.RawMessage) {
			if res != nil {
				e.act(p, reply{s, res})
			}
		})
		return
	}
	if action.Command != nil && e.power != Host {
		e.message = "a guest may not take a code action that runs a command"
		return
	}
	if action.Edit != nil {
		if _, problems := e.workspaceEdit(s, p.b, p.t, action.Edit, "code action"); len(problems) > 0 {
			e.message = strings.Join(problems, "  ")
			return
		}
	}
	var command json.RawMessage
	switch {
	case action.Command == nil:
		return
	case action.Command[0] == '{':
		// A code action's command is an object of its own.
		command = action.Command
	default:
		// A bare command names itself with a string.
		command = raw
	}
	s.client.Request("workspace/executeCommand", func(string) any { return command }, func(json.RawMessage) {})
}
