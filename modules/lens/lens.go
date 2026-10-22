// Package lens shows a language server's code lenses, such as a test's run
// or a function's count of references, in a row above the line each is
// about, and grx runs the one on the cursor's line (B462, B463).
package lens

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// The id of grx and of the callbacks lens asks for. A request for a
// buffer's lenses, or to resolve a lens, has an id from asks up.
const (
	run    uint32 = iota + 1
	picked        // the menu of the line's lenses
	ran           // a lens's command
	asks   uint32 = 1000
)

// lens is a code lens as its server sent it.
type lens struct {
	raw         json.RawMessage
	server, enc string
	at          lsp.Position
	title       string // its command's, "" until it is resolved
	resolving   bool
}

// Module is lens.
type Module struct {
	mu      sync.Mutex
	lenses  map[string][]*lens // by buffer, what its servers last sent
	shown   map[string][]int   // by buffer, the ids of its rows
	rows    map[int][]*lens    // by the id of a row, the lenses it shows
	asked   map[string]uint32  // by buffer, the request waiting for the server
	again   map[string]bool    // by buffer, the text changed while a request waited
	without map[string]bool    // by buffer, no server gives lenses, until one attaches
	bufs    map[uint32]string  // by request, the buffer it is about
	pending map[uint32]*lens   // by request, the lens being resolved
	menu    []*lens            // the lenses the menu offers
	reqs    uint32
	ids     int
}

// New returns lens.
func New() *Module {
	return &Module{
		lenses: map[string][]*lens{}, shown: map[string][]int{}, rows: map[int][]*lens{}, asked: map[string]uint32{},
		again: map[string]bool{}, without: map[string]bool{}, bufs: map[uint32]string{}, pending: map[uint32]*lens{}, reqs: asks,
	}
}

func (m *Module) Name() string { return "lens" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "buffer.closed", "window.moved", "lsp.attached", "lsp.refresh", "option.set", "config.reloaded"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Option, Name: "enabled", Default: "true", Help: "language servers' code lenses show in a row above the lines they are about"},
		{Kind: api.Plug, Name: "run", Keys: "grx", Help: "runs the code lens on the cursor's line", ID: run, Attached: true},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs the lens on the cursor's line, or shows a menu of them when
// there are several.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	line := s.Text.LineOf(args[0])
	var here []*lens
	for _, d := range s.Decorations() {
		if strconv.Itoa(d.Buffer) == s.About["buffer"] && s.Text.LineOf(d.Start) == line {
			here = m.rows[d.ID]
		}
	}
	switch {
	case len(here) == 0:
		return api.Result{Error: "no code lens on this line"}, nil
	case s.About["power"] != "host":
		return api.Result{Error: "a guest may not run a code lens"}, nil
	case len(here) == 1:
		return api.Result{Requests: []api.Request{command(here[0])}}, nil
	}
	m.menu = here
	items := make([]api.MenuItem, len(here))
	for i, l := range here {
		items[i] = api.MenuItem{Title: l.title, Kind: l.server}
	}
	return api.Result{Menu: &api.Menu{ID: picked, Items: items}}, nil
}

// Event asks the servers again after each change, once a server attaches
// or asks for it, and once lens.enabled turns on. Turning it off takes the
// lenses away. A move resolves the lenses that come into view.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	switch event, detail, _ := strings.Cut(name, " "); event {
	case "window.moved":
		return api.Result{Requests: m.resolve(s)}, nil
	case "option.set":
		if detail != "lens.enabled" {
			return api.Result{}, nil
		}
	case "lsp.refresh":
		if detail != "codeLens" {
			return api.Result{}, nil
		}
	case "lsp.attached":
		delete(m.without, buf)
	case "buffer.closed":
		delete(m.bufs, m.asked[buf])
		for _, id := range m.shown[buf] {
			delete(m.rows, id)
		}
		delete(m.lenses, buf)
		delete(m.shown, buf)
		delete(m.asked, buf)
		delete(m.again, buf)
		delete(m.without, buf)
		return api.Result{}, nil
	}
	if on, _ := s.Option("lens.enabled"); on != "true" {
		m.lenses[buf] = nil
		return m.draw(s), nil
	}
	if m.without[buf] {
		return api.Result{}, nil
	}
	if _, waiting := m.asked[buf]; waiting {
		m.again[buf] = true
		return api.Result{}, nil
	}
	return m.ask(s), nil
}

// Callback shows the lenses a server sent, in place of those showing, or
// one it resolved, and runs the lens picked from the menu.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	switch id {
	case ran:
		// A command's answer is often null, which is no error.
		if err := s.About["error"]; !strings.HasPrefix(err, "no language server answered") {
			return api.Result{Error: err}, nil
		}
		return api.Result{}, nil
	case picked:
		i, _ := strconv.Atoi(s.About["picked"])
		if s.About["ok"] != "true" || i < 0 || i >= len(m.menu) {
			return api.Result{}, nil
		}
		return api.Result{Requests: []api.Request{command(m.menu[i])}}, nil
	}
	if l, ok := m.pending[id]; ok {
		delete(m.pending, id)
		got := parse(json.RawMessage(s.About["output"]), l.server, l.enc)
		// The lenses may have come again since.
		i := slices.Index(m.lenses[buf], l)
		if got.title == "" || i < 0 {
			return api.Result{}, nil
		}
		m.lenses[buf][i] = got
		return m.draw(s), nil
	}
	if m.bufs[id] != buf || m.asked[buf] != id {
		return api.Result{}, nil
	}
	delete(m.bufs, id)
	delete(m.asked, buf)
	var res api.Result
	if m.again[buf] {
		delete(m.again, buf)
		res.Requests = m.ask(s).Requests
	}
	switch err := s.About["error"]; {
	case strings.HasPrefix(err, "no language server offers"):
		m.without[buf] = true
		return res, nil
	case strings.HasPrefix(err, "no language server answered"):
		// A server answers null for a text with no lenses.
	case err != "":
		res.Error = err
		return res, nil
	}
	var raws []json.RawMessage
	json.Unmarshal([]byte(s.About["output"]), &raws)
	m.lenses[buf] = nil
	for _, raw := range raws {
		m.lenses[buf] = append(m.lenses[buf], parse(raw, s.About["server"], s.About["encoding"]))
	}
	drawn := m.draw(s)
	res.Undecorate, res.Decorate = drawn.Undecorate, drawn.Decorate
	res.Requests = append(res.Requests, m.resolve(s)...)
	return res, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }

// ask asks the servers of the buffer s is about for its lenses.
func (m *Module) ask(s api.Scope) api.Result {
	buf := s.About["buffer"]
	m.reqs++
	m.asked[buf], m.bufs[m.reqs] = m.reqs, buf
	return api.Result{Requests: []api.Request{{ID: m.reqs, Method: "textDocument/codeLens", Pos: -1, End: -1}}}
}

// resolve asks the servers to resolve the lenses in the current window's
// view that came without a command.
func (m *Module) resolve(s api.Scope) []api.Request {
	view := s.About.View()
	if len(view) == 0 {
		return nil
	}
	first, last := s.Text.LineOf(view[0][0]), s.Text.LineOf(view[len(view)-1][1])
	var out []api.Request
	for _, l := range m.lenses[s.About["buffer"]] {
		if l.title == "" && !l.resolving && l.at.Line >= first && l.at.Line <= last {
			l.resolving = true
			m.reqs++
			m.pending[m.reqs] = l
			out = append(out, api.Request{ID: m.reqs, Method: "codeLens/resolve", Pos: -1, End: -1, Params: string(l.raw), Server: l.server})
		}
	}
	return out
}

// draw shows the buffer's lenses that have a title in place of those
// showing: the lenses of a line in one row above it, from its first
// lens's column, each lens's title apart from the next by a bar.
func (m *Module) draw(s api.Scope) api.Result {
	buf := s.About["buffer"]
	res := api.Result{Undecorate: m.shown[buf]}
	for _, id := range m.shown[buf] {
		delete(m.rows, id)
	}
	m.shown[buf] = nil
	lines := map[int][]*lens{}
	for _, l := range m.lenses[buf] {
		if l.title != "" && l.at.Line < s.Text.Lines() {
			lines[l.at.Line] = append(lines[l.at.Line], l)
		}
	}
	for line, ls := range lines {
		slices.SortStableFunc(ls, func(a, b *lens) int { return cmp.Compare(a.at.Character, b.at.Character) })
		titles := make([]string, len(ls))
		for i, l := range ls {
			titles[i] = l.title
		}
		// Tabs keep their width, and every other character is a space.
		start := s.Text.LineStart(line)
		indent := []rune(s.Text.Slice(start, lsp.Offset(s.Text, ls[0].at, ls[0].enc)))
		for i, r := range indent {
			if r != '\t' {
				indent[i] = ' '
			}
		}
		m.ids++
		m.shown[buf] = append(m.shown[buf], m.ids)
		m.rows[m.ids] = ls
		// An insert's rows show on the cursor's line too, so passing a lens
		// moves no text.
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: start, End: start, Style: "lens", Lines: []string{string(indent) + strings.Join(titles, " | ")}, Above: true, Insert: true})
	}
	return res
}

// parse reads a lens the server sent in encoding enc.
func parse(raw json.RawMessage, server, enc string) *lens {
	var l struct {
		Range   lsp.Range `json:"range"`
		Command *struct {
			Title string `json:"title"`
		} `json:"command"`
	}
	json.Unmarshal(raw, &l)
	out := &lens{raw: raw, server: server, enc: enc, at: l.Range.Start}
	if l.Command != nil {
		out.title = l.Command.Title
	}
	return out
}

// command is the request that runs l's command on its server.
func command(l *lens) api.Request {
	var c struct {
		Command json.RawMessage `json:"command"`
	}
	json.Unmarshal(l.raw, &c)
	return api.Request{ID: ran, Method: "workspace/executeCommand", Pos: -1, End: -1, Params: string(c.Command), Server: l.server}
}
