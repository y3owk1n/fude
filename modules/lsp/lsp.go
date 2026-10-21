// Package lsp is what a language server tells about the symbol under the
// cursor: hover, its definition, declaration, type definition,
// implementations and references, the file's symbols, the workspace's, and
// renaming it (B62, B66, B214, B460). Its :lsp panel shows the language servers and restarts,
// stops, disables or enables them (B159).
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// What lsp registers, by the ids Call runs them by.
const (
	hover uint32 = iota + 1
	definition
	typeDefinition
	implementation
	references
	symbols
	renameKey
	renameCmd
	workspace
	nextDiag
	prevDiag
	showDiag
	codeActions
	diagnosticsSrc
	lspCmd
	restartKey
	stopKey
	disableKey
	enableKey
	firstDiag
	lastDiag
	declaration
)

// The callbacks lsp asks for, by id. A request's id is the id of the call
// it answers, apart from these, and a workspace symbol search has an id
// from searches up.
const (
	picked     uint32 = iota + 100 // the symbols list
	diagPick                       // the diagnostics list
	menuID                         // the code action menu
	actionsReq                     // the code actions asked for
	ranReq                         // a code action's command
	resolveReq uint32 = 200        // a menu item being resolved, plus its index
	actReq     uint32 = 300        // an action being resolved to act on, plus its index
	searches   uint32 = 1000
)

// methods are the requests of each call.
var methods = map[uint32]string{
	hover: "textDocument/hover", definition: "textDocument/definition", typeDefinition: "textDocument/typeDefinition",
	implementation: "textDocument/implementation", references: "textDocument/references", symbols: "textDocument/documentSymbol",
	declaration: "textDocument/declaration",
}

// what names each request in a message.
var what = map[uint32]string{
	hover: "hover", definition: "definition", typeDefinition: "type definition", implementation: "implementations",
	references: "references", symbols: "symbols", declaration: "declaration",
}

// kinds are the names of the protocol's symbol kinds.
var kinds = []string{
	"", "File", "Module", "Namespace", "Package", "Class", "Method", "Property", "Field",
	"Constructor", "Enum", "Interface", "Function", "Variable", "Constant", "String", "Number", "Boolean",
	"Array", "Object", "Key", "Null", "EnumMember", "Struct", "Event", "Operator", "TypeParameter",
}

// limit is the most workspace symbols a search shows.
const limit = 1000

// Module is lsp.
type Module struct {
	mu       sync.Mutex
	search   uint32                             // the newest workspace symbol search
	diags    map[string]map[string][]diagnostic // by buffer, by server, its diagnostics
	notes    map[string]bool                    // by buffer, diagnostic_notes was on when it last drew
	ids      int                                // the last decoration id given
	menu     []action                           // the code actions the menu shows
	showing  bool                               // the menu is up
	encoding string                             // the encoding of the menu's server
}

// New returns lsp.
func New() *Module {
	return &Module{diags: map[string]map[string][]diagnostic{}, notes: map[string]bool{}}
}

func (m *Module) Name() string { return "lsp" }

func (m *Module) Subscribed() []string {
	return []string{"lsp.published", "lsp.detached", "lsp.changed", "buffer.closed", "window.moved", "option.set", "config.reloaded"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Plug, Name: "hover", Keys: "K", Help: "shows what the language server says of the symbol under the cursor", ID: hover, Enters: true},
		{Kind: api.Plug, Name: "definition", Keys: "<C-]>", Help: "goes to the definition of the symbol under the cursor", ID: definition},
		{Kind: api.Plug, Name: "type_definition", Keys: "grt", Help: "goes to the definition of the type of the symbol under the cursor", ID: typeDefinition, Attached: true},
		{Kind: api.Plug, Name: "implementations", Keys: "gri", Help: "lists the implementations of the symbol under the cursor", ID: implementation, Attached: true},
		{Kind: api.Plug, Name: "references", Keys: "grr", Help: "lists the references to the symbol under the cursor", ID: references, Attached: true},
		{Kind: api.Plug, Name: "symbols", Keys: "gO", Help: "lists the file's symbols", ID: symbols, Attached: true},
		{Kind: api.Plug, Name: "rename", Keys: "grn", Help: "renames the symbol under the cursor in every file", ID: renameKey, Attached: true},
		{Kind: api.Command, Name: "rename", Help: "renames the symbol under the cursor in every file, through the language server", ID: renameCmd},
		{Kind: api.Source, Name: "symbols", Help: "the symbols the language server finds for the query, asked again as it is typed", ID: workspace},
		{Kind: api.Plug, Name: "next_diagnostic", Keys: "]d", Help: "goes to the next diagnostic", ID: nextDiag},
		{Kind: api.Plug, Name: "prev_diagnostic", Keys: "[d", Help: "goes to the diagnostic before", ID: prevDiag},
		{Kind: api.Plug, Name: "first_diagnostic", Keys: "[D", Help: "goes to the first diagnostic", ID: firstDiag},
		{Kind: api.Plug, Name: "last_diagnostic", Keys: "]D", Help: "goes to the last diagnostic", ID: lastDiag},
		{Kind: api.Plug, Name: "diagnostics", Keys: "<C-w>d", Help: "shows the diagnostics on the cursor's line in full", ID: showDiag},
		{Kind: api.Plug, Name: "actions", Keys: "gra", Help: "lists the language server's actions for the cursor's place", ID: codeActions, Attached: true, Visual: true},
		{Kind: api.Source, Name: "diagnostics", Help: "the diagnostics of every open buffer, the current buffer's first", ID: diagnosticsSrc},
		{Kind: api.Command, Name: "lsp", Help: "opens the panel of language servers, or acts on the file's servers or a named one: restart, stop, disable or enable", ID: lspCmd, Complete: true},
		{Kind: api.Plug, Name: "restart", Help: "restarts the language server under the cursor", ID: restartKey},
		{Kind: api.Plug, Name: "stop", Help: "stops the language server under the cursor", ID: stopKey},
		{Kind: api.Plug, Name: "disable", Help: "keeps the language server under the cursor off", ID: disableKey},
		{Kind: api.Plug, Name: "enable", Help: "lets the language server under the cursor start again", ID: enableKey},
		{Kind: api.Plug, Name: "declaration", Help: "goes to the declaration of the symbol under the cursor", ID: declaration},
		{Kind: api.Option, Name: "diagnostic_notes", Default: "true", Help: "a diagnostic's message shows after its line", Window: true},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call asks the buffer's servers about the cursor, starts a rename on the
// command line or sends it, or opens the symbols list.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cursor := args[0]
	var res api.Result
	switch id {
	case nextDiag:
		m.step(&res, s, cursor, max(args[1], 1))
		return res, nil
	case prevDiag:
		m.step(&res, s, cursor, -max(args[1], 1))
		return res, nil
	case firstDiag:
		// The first is the next one from before the text, and the last
		// the one before from past it.
		m.step(&res, s, -1, 1)
		return res, nil
	case lastDiag:
		m.step(&res, s, s.Text.Len()+1, -1)
		return res, nil
	case showDiag:
		m.show(&res, s, cursor)
		return res, nil
	case diagnosticsSrc:
		return api.Result{Pick: &api.Pick{ID: diagPick, Title: "diagnostics", Items: m.places(s), Places: true}}, nil
	case lspCmd, restartKey, stopKey, disableKey, enableKey:
		m.serverCall(&res, s, id, cursor)
		return res, nil
	case codeActions:
		start, end := cursor, cursor
		if s.About["mode"] == "visual" {
			start, end = args[0], args[1]
		}
		m.actions(&res, s, start, end)
		return res, nil
	}
	if s.About["servers"] == "" {
		if id == workspace {
			return api.Result{Error: "no language server offers workspace symbols"}, nil
		}
		return api.Result{Error: "no language server"}, nil
	}
	switch id {
	case renameKey:
		// grn asks for the new name on the command line, starting from the
		// word under the cursor, and :rename sends it from the same place.
		return api.Result{Cmdline: "rename " + word(s.Text, cursor)}, nil
	case renameCmd:
		name := strings.TrimSpace(s.About["args"])
		if name == "" {
			return api.Result{Error: "no new name"}, nil
		}
		params, _ := json.Marshal(map[string]string{"newName": name})
		return api.Result{Requests: []api.Request{{ID: renameCmd, Method: "textDocument/rename", Pos: cursor, End: -1, Params: string(params)}}}, nil
	case workspace:
		return api.Result{Pick: &api.Pick{ID: picked, Title: "symbols", Hint: "type to search the symbols", Query: s.About["args"], Live: true, Places: true}}, nil
	}
	rq := api.Request{ID: id, Method: methods[id], Pos: cursor, End: -1}
	switch id {
	case references:
		rq.Params = `{"context":{"includeDeclaration":true}}`
	case symbols:
		rq.Pos = -1
	}
	return api.Result{Requests: []api.Request{rq}}, nil
}

// Event takes the diagnostics a server publishes, drops a server's when it
// leaves a buffer, and forgets a closed buffer.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res api.Result
	event, server, _ := strings.Cut(name, " ")
	switch event {
	case "lsp.published":
		m.published(&res, s)
	case "lsp.detached":
		m.detached(&res, s, server)
	case "lsp.changed":
		// The panel of language servers shows the new state, while it is open.
		res.Show, _, _ = panel(s)
		res.Show.Update = true
	case "buffer.closed":
		delete(m.diags, s.About["buffer"])
		delete(m.notes, s.About["buffer"])
	case "window.moved", "option.set", "config.reloaded":
		// diagnostic_notes turned on or off, here or in the window moved to.
		if buf := s.About["buffer"]; m.diags[buf] != nil {
			if on, _ := s.Option("lsp.diagnostic_notes"); m.notes[buf] != (on == "true") {
				m.redraw(&res, s, buf)
			}
		}
	}
	return res, nil
}

// Callback shows what a server answered: hover in a float, one place as a
// jump and several as a list, the symbols found, or a rename applied.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res api.Result
	switch {
	case id == diagPick || id == ranReq:
		res.Error = s.About["error"]
		return res, nil
	case id == menuID:
		m.picked(&res, s)
		return res, nil
	case id == actionsReq:
		switch err := s.About["error"]; {
		case strings.HasPrefix(err, "no language server answered"):
			res.Error = "no code actions"
		case err != "":
			res.Error = offers(err, "code actions")
		default:
			m.offered(&res, s)
		}
		return res, nil
	case id >= resolveReq && id < actReq:
		m.resolved(&res, s, int(id-resolveReq))
		return res, nil
	case id >= actReq && id < searches:
		if i := int(id - actReq); s.About["output"] != "" && i < len(m.menu) {
			a := m.menu[i]
			a.raw = json.RawMessage(s.About["output"])
			m.act(&res, s, a, 0)
		} else if err := s.About["error"]; err != "" {
			res.Error = err
		}
		return res, nil
	}
	if id == picked {
		if query, live := s.About["query"]; live {
			if query == "" {
				return api.Result{Pick: &api.Pick{ID: picked}}, nil
			}
			m.search++
			params, _ := json.Marshal(map[string]string{"query": query})
			return api.Result{Requests: []api.Request{{ID: searches + m.search, Method: "workspace/symbol", Pos: -1, End: -1, Params: string(params)}}}, nil
		}
		return api.Result{}, nil
	}
	if id >= searches {
		if id != searches+m.search {
			return api.Result{}, nil
		}
		if err := s.About["error"]; err != "" {
			return api.Result{Error: offers(err, "workspace symbols"), Pick: &api.Pick{ID: picked}}, nil
		}
		items, err := symbolPlaces(s.About["output"])
		if err != nil {
			return api.Result{Error: err.Error(), Pick: &api.Pick{ID: picked}}, nil
		}
		return api.Result{Pick: &api.Pick{ID: picked, Items: items, Encoding: s.About["encoding"]}}, nil
	}
	if err := s.About["error"]; err != "" {
		if id == renameCmd {
			return api.Result{Error: offers(err, "rename")}, nil
		}
		if strings.HasPrefix(err, "no language server answered") {
			return api.Result{Error: "no " + what[id] + " found"}, nil
		}
		return api.Result{Error: offers(err, what[id])}, nil
	}
	out, enc := s.About["output"], s.About["encoding"]
	switch id {
	case hover:
		return hovered(out), nil
	case symbols:
		return listed(s, out, enc), nil
	case renameCmd:
		return api.Result{Apply: &api.WorkspaceEdit{Server: s.About["server"], Edit: out, Label: "rename"}}, nil
	}
	return located(id, out, enc), nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }

// offers rewrites the error of a request no server offers, naming what was
// asked in fude's words.
func offers(err, asked string) string {
	if strings.HasPrefix(err, "no language server offers") {
		return "no language server offers " + asked
	}
	return err
}

// word is the word under the cursor at c, or "".
func word(t api.Text, c int) string {
	start, end := c, c
	for start > 0 && isWord(t.Slice(start-1, start)[0]) {
		start--
	}
	for end < t.Len() && isWord(t.Slice(end, end+1)[0]) {
		end++
	}
	return t.Slice(start, end)
}

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// hovered shows a hover answer's text in a float, rendered from markdown.
func hovered(out string) api.Result {
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	json.Unmarshal([]byte(out), &h)
	// Contents is markup with a value, a string, or a list of either.
	var parts []json.RawMessage
	if json.Unmarshal(h.Contents, &parts) != nil {
		parts = []json.RawMessage{h.Contents}
	}
	var texts []string
	for _, p := range parts {
		if text := markupText(p); text != "" {
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		return api.Result{Error: "no hover information"}
	}
	return api.Result{Float: &api.Float{Text: strings.TrimSpace(strings.Join(texts, "\n\n"))}}
}

// markupText is the text of markup with a value or of a plain string, as
// servers send hover, or "" for anything else.
func markupText(raw json.RawMessage) string {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var markup struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &markup) != nil {
		return ""
	}
	return markup.Value
}

// located jumps to the one place an answer names, or lists them in a
// window when it names several, as references always are.
func located(id uint32, out, enc string) api.Result {
	type location struct {
		URI       string    `json:"uri"`
		Range     lsp.Range `json:"range"`
		TargetURI string    `json:"targetUri"`
		Target    lsp.Range `json:"targetSelectionRange"`
	}
	var locs []location
	if json.Unmarshal([]byte(out), &locs) != nil {
		var one location
		if json.Unmarshal([]byte(out), &one) == nil && (one.URI != "" || one.TargetURI != "") {
			locs = []location{one}
		}
	}
	var items []string
	var jump *api.Jump
	for _, l := range locs {
		if l.TargetURI != "" {
			l.URI, l.Range = l.TargetURI, l.Target
		}
		path, ok := lsp.Path(l.URI)
		if !ok {
			continue
		}
		jump = &api.Jump{Path: path, Line: l.Range.Start.Line + 1, Column: l.Range.Start.Character + 1, Encoding: enc}
		items = append(items, fmt.Sprintf("%s:%d:%d:", relative(path), jump.Line, jump.Column))
	}
	switch {
	case len(items) == 0:
		return api.Result{Error: "no " + what[id] + " found"}
	case len(items) == 1 && id != references:
		return api.Result{Jump: jump}
	}
	return api.Result{Places: &api.Places{Title: what[id], Items: items, Show: true, Encoding: enc}}
}

// relative returns path under the working directory relative to it, or
// path as it is.
func relative(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(wd, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return path
}

// listed lists the file's symbols a documentSymbol answer names, in a
// window where <CR> jumps to one. Nested symbols are indented under theirs.
func listed(s api.Scope, out, enc string) api.Result {
	type symbol struct {
		Name           string    `json:"name"`
		Kind           int       `json:"kind"`
		SelectionRange lsp.Range `json:"selectionRange"`
		Location       struct {
			Range lsp.Range `json:"range"`
		} `json:"location"`
		Children []json.RawMessage `json:"children"`
	}
	var items []string
	var add func(raw []json.RawMessage, depth int)
	add = func(raw []json.RawMessage, depth int) {
		for _, r := range raw {
			var sym symbol
			if json.Unmarshal(r, &sym) != nil {
				continue
			}
			// A symbol has a selectionRange, or a location when the answer is flat.
			pos := sym.SelectionRange.Start
			if sym.SelectionRange == (lsp.Range{}) {
				pos = sym.Location.Range.Start
			}
			items = append(items, fmt.Sprintf("%d:%d:%s[%s] %s", pos.Line+1, pos.Character+1, strings.Repeat("  ", depth), kind(sym.Kind), sym.Name))
			add(sym.Children, depth+1)
		}
	}
	var raw []json.RawMessage
	json.Unmarshal([]byte(out), &raw)
	add(raw, 0)
	if len(items) == 0 {
		return api.Result{Error: "no symbols found"}
	}
	return api.Result{Places: &api.Places{Title: "symbols", Items: items, Show: true, File: s.About["path"], Encoding: enc}}
}

// kind names a symbol kind, or is "" for one the protocol does not name.
func kind(k int) string {
	if k > 0 && k < len(kinds) {
		return kinds[k]
	}
	return ""
}

// symbolPlaces returns the symbols of a workspace/symbol answer as places,
// each its name and kind, grouped by file, at most limit of them.
func symbolPlaces(out string) ([]string, error) {
	var found []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			URI   string    `json:"uri"`
			Range lsp.Range `json:"range"`
		} `json:"location"`
	}
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		return nil, fmt.Errorf("workspace symbols: %w", err)
	}
	var items []string
	for _, sym := range found[:min(len(found), limit)] {
		path, ok := lsp.Path(sym.Location.URI)
		if !ok {
			continue
		}
		k := ""
		if kind(sym.Kind) != "" {
			k = "  " + kind(sym.Kind)
		}
		p := sym.Location.Range.Start
		items = append(items, fmt.Sprintf("%s:%d:%d:%s%s", relative(path), p.Line+1, p.Character+1, sym.Name, k))
	}
	// Places show under their file, so a file's come together.
	slices.SortStableFunc(items, func(a, b string) int {
		pa, _, _ := strings.Cut(a[len(filepath.VolumeName(a)):], ":")
		pb, _, _ := strings.Cut(b[len(filepath.VolumeName(b)):], ":")
		return strings.Compare(pa, pb)
	})
	return items, nil
}
