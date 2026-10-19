package lsp

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/api/diff"
	"github.com/y3owk1n/fude/internal/paths"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// action is a code action from a server, as the menu shows it (B227).
type action struct {
	raw       json.RawMessage
	server    string
	title     string
	kind      string
	preferred bool
	disabled  string
	resolving bool
}

// actions asks the buffer's servers for the code actions from start to
// end, with the diagnostics there (B227).
func (m *Module) actions(res *api.Result, s api.Scope, start, end int) {
	params, _ := json.Marshal(map[string]any{"context": map[string]any{"diagnostics": m.context(s, start, end)}})
	res.Requests = []api.Request{{ID: actionsReq, Method: "textDocument/codeAction", Pos: start, End: end, Params: string(params)}}
}

// offered takes a server's code actions and shows them to pick from: the
// preferred first, then fixes, refactors and the rest, disabled ones last.
func (m *Module) offered(res *api.Result, s api.Scope) {
	var raws []json.RawMessage
	json.Unmarshal([]byte(s.About["output"]), &raws)
	m.menu, m.encoding = nil, s.About["encoding"]
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
		act := action{raw: raw, server: s.About["server"], title: a.Title, kind: kind, preferred: a.IsPreferred}
		if a.Disabled != nil {
			act.disabled = cmp.Or(a.Disabled.Reason, "disabled")
		}
		m.menu = append(m.menu, act)
	}
	if len(m.menu) == 0 {
		res.Message = "no code actions"
		return
	}
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
	slices.SortStableFunc(m.menu, func(x, y action) int { return cmp.Compare(rank(x), rank(y)) })
	m.showing = true
	m.showMenu(res, s)
}

// showMenu shows the actions as a menu, each with what it does beside it.
// An action that has neither edit nor command is resolved first, when its
// server can, and shows what it does once it is.
func (m *Module) showMenu(res *api.Result, s api.Scope) {
	items := make([]api.MenuItem, len(m.menu))
	for i := range m.menu {
		a := &m.menu[i]
		items[i] = api.MenuItem{Title: a.title, Kind: a.kind, Disabled: a.disabled, Doc: m.preview(s, a)}
		if items[i].Doc == nil && !a.resolving {
			a.resolving = true
			res.Requests = append(res.Requests, api.Request{ID: resolveReq + uint32(i), Method: "codeAction/resolve", Pos: -1, End: -1, Params: string(a.raw), Server: a.server})
		}
	}
	res.Menu = &api.Menu{ID: menuID, Items: items}
}

// resolved takes the resolved action i in place of the one the menu holds,
// and shows the menu anew.
func (m *Module) resolved(res *api.Result, s api.Scope, i int) {
	if !m.showing || i >= len(m.menu) || s.About["output"] == "" {
		return
	}
	m.menu[i].raw = json.RawMessage(s.About["output"])
	m.showMenu(res, s)
}

// preview returns what a does: why it is disabled, each file's lines its
// edit changes with a line around them, or the command it runs. It is nil
// for an action that needs resolving first.
func (m *Module) preview(s api.Scope, a *action) *api.Float {
	if a.disabled != "" {
		return &api.Float{Text: a.disabled, Spans: []api.Span{{End: len(a.disabled), Name: "comment"}}}
	}
	var raw struct {
		Edit    json.RawMessage `json:"edit"`
		Command json.RawMessage `json:"command"`
		Data    json.RawMessage `json:"data"`
	}
	json.Unmarshal(a.raw, &raw)
	if raw.Edit == nil && raw.Command == nil && raw.Data != nil {
		return nil
	}
	if raw.Edit != nil {
		if f := m.editPreview(s, raw.Edit); f.Text != "" {
			return f
		}
	}
	if raw.Command != nil {
		// A command is an object, or a bare action's command is its name.
		var c struct {
			Title   string `json:"title"`
			Command string `json:"command"`
		}
		if json.Unmarshal(raw.Command, &c) != nil {
			json.Unmarshal(raw.Command, &c.Command)
		}
		text := "runs " + cmp.Or(c.Title, c.Command)
		return &api.Float{Text: text, Spans: []api.Span{{End: len(text), Name: "comment"}}}
	}
	return &api.Float{Spans: []api.Span{}}
}

// textEdit is an edit as the protocol sends it.
type textEdit struct {
	Range   lsp.Range `json:"range"`
	NewText string    `json:"newText"`
}

// files reads a workspace edit's changes and document changes, by the path
// of the file each edits.
func files(edit json.RawMessage) map[string][]textEdit {
	var w struct {
		Changes         map[string][]textEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []textEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	json.Unmarshal(edit, &w)
	out := map[string][]textEdit{}
	for uri, edits := range w.Changes {
		if path, ok := lsp.Path(uri); ok {
			out[path] = edits
		}
	}
	for _, d := range w.DocumentChanges {
		if path, ok := lsp.Path(d.TextDocument.URI); ok {
			out[path] = d.Edits
		}
	}
	return out
}

// editPreview returns the lines a workspace edit changes, file by file as a
// diff, under each file's name when it changes another file than the call's.
// A file no buffer holds is read from disk by the host alone (B245).
func (m *Module) editPreview(s api.Scope, edit json.RawMessage) *api.Float {
	fs := files(edit)
	f := &api.Float{Spans: []api.Span{}}
	var out strings.Builder
	own, _ := filepath.Abs(s.About["path"])
	for _, path := range slices.Sorted(maps.Keys(fs)) {
		before, ok := m.textOf(s, path)
		if !ok {
			continue
		}
		edits := make([]api.Edit, 0, len(fs[path]))
		for _, ed := range fs[path] {
			edits = append(edits, api.Edit{Start: lsp.Offset(before, ed.Range.Start, m.encoding), End: lsp.Offset(before, ed.Range.End, m.encoding), Text: ed.NewText})
		}
		slices.SortStableFunc(edits, func(x, y api.Edit) int { return cmp.Compare(x.Start, y.Start) })
		text := before.Slice(0, before.Len())
		after := text
		for _, ed := range slices.Backward(edits) {
			after = after[:ed.Start] + ed.Text + after[ed.End:]
		}
		if path != own || len(fs) > 1 {
			name := filepath.Base(path)
			f.Spans = append(f.Spans, api.Span{Start: out.Len(), End: out.Len() + len(name), Name: "markup.heading"})
			out.WriteString(name + "\n")
		}
		// A run of changes after the first shows after ⋯, and none says
		// where it is.
		for i, l := range strings.Split(diff.Unified(diff.Lines(strings.Split(text, "\n"), strings.Split(after, "\n")), 1), "\n") {
			switch {
			case l == "" || strings.HasPrefix(l, "@@") && i == 0:
				continue
			case strings.HasPrefix(l, "@@"):
				l = "⋯"
			}
			if style := map[byte]string{'-': "diff.minus", '+': "diff.plus"}[l[0]]; style != "" {
				f.Spans = append(f.Spans, api.Span{Start: out.Len(), End: out.Len() + len(l), Name: style})
			}
			out.WriteString(l + "\n")
		}
	}
	f.Text = strings.TrimSuffix(out.String(), "\n")
	return f
}

// textOf returns the text of the file at path. It is the buffer's, or the
// file's when the session is the host's, since a guest sees only the
// project (B245).
func (m *Module) textOf(s api.Scope, path string) (api.Text, bool) {
	for _, b := range s.Buffers {
		if samePath(b.Path, path) {
			return b.Text, true
		}
	}
	if s.About["power"] != "host" {
		return nil, false
	}
	at := time.Now()
	data, err := os.ReadFile(path)
	trace.Since("file", fmt.Sprintf("read %q, %d bytes", path, len(data)), at, err)
	if err != nil {
		return nil, false
	}
	return text(strings.TrimSuffix(string(data), "\n")), true
}

// samePath reports whether a and b name the same file.
func samePath(a, b string) bool {
	x, _ := filepath.Abs(a)
	y, _ := filepath.Abs(b)
	return paths.Same(x, y)
}

// picked applies the edit of the action picked from the menu and runs its
// command. An action with neither but data is resolved first.
func (m *Module) picked(res *api.Result, s api.Scope) {
	m.showing = false
	i, _ := strconv.Atoi(s.About["picked"])
	if s.About["ok"] != "true" || i < 0 || i >= len(m.menu) {
		return
	}
	a := m.menu[i]
	if a.disabled != "" {
		res.Message = a.title + ": " + a.disabled
		return
	}
	m.act(res, s, a, actReq+uint32(i))
}

// act applies a's edit and runs its command, or asks its server to resolve
// it first and acts on the answer, which comes back with id. An answer to
// that comes with id 0, and is not resolved again.
func (m *Module) act(res *api.Result, s api.Scope, a action, id uint32) {
	var action struct {
		Edit    json.RawMessage `json:"edit"`
		Command json.RawMessage `json:"command"`
		Data    json.RawMessage `json:"data"`
	}
	json.Unmarshal(a.raw, &action)
	if action.Edit == nil && action.Command == nil && action.Data != nil {
		if id == 0 {
			res.Message = a.title + " could not be resolved"
			return
		}
		res.Requests = append(res.Requests, api.Request{ID: id, Method: "codeAction/resolve", Pos: -1, End: -1, Params: string(a.raw), Server: a.server})
		return
	}
	if action.Command != nil && s.About["power"] != "host" {
		res.Message = "a guest may not take a code action that runs a command"
		return
	}
	if action.Edit != nil {
		res.Apply = &api.WorkspaceEdit{Server: a.server, Edit: string(action.Edit), Label: "code action"}
	}
	if action.Command == nil {
		return
	}
	// A code action's command is an object of its own, and a bare command
	// names itself with a string.
	params := action.Command
	if action.Command[0] != '{' {
		params = a.raw
	}
	res.Requests = append(res.Requests, api.Request{ID: ranReq, Method: "workspace/executeCommand", Pos: -1, End: -1, Params: string(params), Server: a.server})
}

// text is a string as an api.Text, for a file no buffer holds.
type text string

func (t text) Len() int                    { return len(t) }
func (t text) Slice(start, end int) string { return string(t[start:end]) }
func (t text) Lines() int                  { return strings.Count(string(t), "\n") + 1 }
func (t text) LineStart(line int) int {
	at := 0
	for ; line > 0; line-- {
		i := strings.IndexByte(string(t[at:]), '\n')
		if i < 0 {
			return len(t)
		}
		at += i + 1
	}
	return at
}

func (t text) LineOf(pos int) int { return strings.Count(string(t[:min(pos, len(t))]), "\n") }
