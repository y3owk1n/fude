package editor

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/plugin"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// logMu keeps lines that plugin goroutines log whole.
var logMu sync.Mutex

// Load starts the plugins config.toml lists, each on its own goroutine, so
// none delays a frame (B56), and the language servers of the open buffers.
// Each plugin's init reads the current buffer, and its results reach the
// editor loop through Queue.
func (e *Session) Load() {
	e.started = true
	var failed []string
	for _, b := range e.bufs {
		if err := e.attach(b); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		e.message = strings.Join(failed, "  ")
	}
	e.plugged = len(e.setup)
	e.loadPlugins()
}

// Use runs m, a module built into the distribution, as it runs a plugin
// (D49). Call it before Load. :plugin reload leaves it as it is.
func (e *Session) Use(m api.Module) {
	b, t := e.win.buf, e.Text()
	scope := e.scope(m, b, t)
	res, err := m.Init(context.Background(), scope)
	scope.Done()
	e.loaded(m, err, b, t, res)
}

// loadPlugins starts the plugins config.toml lists.
func (e *Session) loadPlugins() {
	config, err1 := xdgDir("XDG_CONFIG_HOME", ".config")
	cache, err2 := xdgDir("XDG_CACHE_HOME", ".cache")
	if err := errors.Join(err1, err2); err != nil {
		e.message = fmt.Sprintf("load plugins: %v", err)
		return
	}
	for _, s := range e.cfg.Plugin {
		path := s.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(config, "fude", path)
		}
		settings, err := texts(s.Settings)
		if err != nil {
			e.message = fmt.Sprintf("load plugin %s: %v", s.Path, err)
			continue
		}
		b, t, scope := e.win.buf, e.Text(), e.scope(nil, e.win.buf, e.Text())
		go func() {
			defer scope.Done()
			ctx := context.Background()
			p, err := plugin.Load(ctx, path, plugin.Grants{Capabilities: s.Capabilities, Programs: s.Programs, Hosts: s.Hosts, Settings: settings}, filepath.Join(cache, "fude", "wasm"), logLine)
			if err != nil {
				e.queue <- func() { e.message = err.Error() }
				return
			}
			res, err := p.Init(ctx, scope)
			e.queue <- func() { e.loaded(p, err, b, t, res) }
		}()
	}
}

// texts returns a plugin's settings as the text its setting function gives
// it. Only numbers, strings and booleans read as text, so a table or a list
// is refused (B178).
func texts(settings map[string]any) (map[string]string, error) {
	out := map[string]string{}
	for name, v := range settings {
		switch v.(type) {
		case string, int64, float64, bool:
			out[name] = fmt.Sprint(v)
		default:
			return nil, fmt.Errorf("setting %s is not a number, string or boolean", name)
		}
	}
	return out, nil
}

// loaded keeps a plugin whose init returned, adds what it registered, and
// does what it asked for to b, whose text it saw as t. A plugin whose init
// failed is unloaded.
func (e *Session) loaded(p api.Module, err error, b *buffer, t kernel.Snapshot, res api.Result) {
	if err != nil {
		e.unload(p, err)
		return
	}
	trace.Record("plugin", "loaded "+p.Name(), 0)
	e.modules = append(e.modules, p)
	for _, r := range p.Registered() {
		e.register(p, r)
	}
	if len(p.Subscribed()) > 0 {
		l := e.listen(p)
		// p hears of the buffers that opened before it loaded. A module added
		// after the files opened then sees them as a later module does.
		if slices.Contains(p.Subscribed(), "buffer.opened") {
			for _, b := range e.bufs {
				l.put(event{name: "buffer.opened", buf: b}, func(ev, _ event) event { return ev })
			}
		}
		if slices.Contains(p.Subscribed(), "lsp.attached") {
			for _, b := range e.bufs {
				for _, s := range b.servers {
					if s.client.Encoding() != "" {
						l.put(event{name: "lsp.attached", detail: s.name, buf: b}, func(ev, _ event) event { return ev })
					}
				}
			}
		}
	}
	if b.text.Snapshot().Version() != t.Version() {
		e.message = fmt.Sprintf("plugin %s: the text changed while it loaded", p.Name())
		return
	}
	e.result(p, b, t, res)
}

// result applies the decorations and edits a call into p asked for on b,
// which read t.
func (e *Session) result(p api.Module, b *buffer, t kernel.Snapshot, res api.Result) {
	defer e.went()
	edits := e.decorate(p, b, res)
	if len(edits) == 0 {
		return
	}
	// A read-only session changes no text, through a module either (B247).
	if err := e.mayEdit(); err != nil {
		e.message = err.Error()
		return
	}
	tx := kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: "plugin " + p.Name(), Time: e.now()}
	for _, ed := range edits {
		tx.Edits = append(tx.Edits, kernel.Edit(ed))
	}
	if err := b.text.Apply(tx); err != nil {
		e.message = fmt.Sprintf("plugin %s: %v", p.Name(), err)
		return
	}
	b.head = b.text.Head()
	e.reveal()
}

// decoration is a plugin's style on a range of a buffer, kept as marks so
// it moves with edits (B55).
type decoration struct {
	id         int
	buf        *buffer
	start, end kernel.MarkID
	style      string
	note       string          // a diagnostic's message, or a plugin's text after the end of the line
	sign       string          // a plugin's text for the sign column
	under      bool            // any other sign on the line shows over it (B184)
	fold       bool            // a closed fold over its lines (B288)
	raw        json.RawMessage // a diagnostic as the server sent it, for code actions
}

// remove deletes the decoration's marks.
func (d decoration) remove() {
	d.buf.text.DeleteMark(d.start)
	d.buf.text.DeleteMark(d.end)
	if d.fold {
		d.buf.folds = slices.DeleteFunc(d.buf.folds, func(f decoration) bool { return f.start == d.start })
	}
	if d.sign != "" {
		d.buf.signs--
	}
}

// pluginRequest sends a plugin's request to those of b's servers that offer it,
// every one for a method outside textDocument/, and hands the plugin the
// first answer that is not null, as a job's output (B190).
func (e *Session) pluginRequest(p api.Module, b *buffer, rq api.Request, save *saving) {
	l := e.listen(p)
	reply := func(extra api.Context) {
		if slices.Contains(e.modules, p) {
			l.add(event{buf: b, id: rq.ID, extra: extra, save: save, from: e})
		}
	}
	var to []*server
	for _, s := range b.servers {
		if !strings.HasPrefix(rq.Method, "textDocument/") || s.offers(rq.Method) {
			to = append(to, s)
		}
	}
	var params map[string]any
	if rq.Params != "" {
		if err := json.Unmarshal([]byte(rq.Params), &params); err != nil {
			reply(api.Context{"error": fmt.Sprintf("the params of %s: %v", rq.Method, err)})
			return
		}
	}
	if len(to) == 0 {
		reply(api.Context{"error": "no language server offers " + rq.Method})
		return
	}
	t, uri := b.text.Snapshot(), lsp.URI(b.file.path)
	left, done := len(to), false
	for _, s := range to {
		s.client.Request(rq.Method, func(enc string) any {
			out := map[string]any{"textDocument": map[string]any{"uri": uri}}
			switch {
			case rq.Pos >= 0 && rq.End >= 0:
				out["range"] = lsp.Range{Start: lsp.Point(t, rq.Pos, enc), End: lsp.Point(t, rq.End, enc)}
			case rq.Pos >= 0:
				out["position"] = lsp.Point(t, rq.Pos, enc)
			}
			maps.Copy(out, params)
			return out
		}, func(res json.RawMessage) {
			switch left--; {
			case done:
			case len(res) > 0 && string(res) != "null":
				done = true
				reply(api.Context{"output": string(res), "server": s.name, "encoding": s.client.Encoding()})
			case left == 0:
				reply(api.Context{"error": "no language server answered " + rq.Method})
			}
		})
	}
}

// setOptions sets the options a result asks for on b (B283).
func (e *Session) setOptions(b *buffer, set map[string]string) {
	for name, value := range set {
		if name != "expand_tab" && name != "indent_width" && name != "tab_width" {
			e.message = "no option named " + name + " to set"
			continue
		}
		if b.set == nil {
			b.set = map[string]string{}
		}
		b.set[name] = value
		// Every window on b fits its options again at the next frame.
		for _, s := range e.sessions {
			for _, w := range s.root.windows() {
				if w.buf == b {
					w.fitted = nil
				}
			}
		}
	}
}

// jumpTo is where a plugin's call about b asked to move the cursor.
type jumpTo struct {
	b *buffer
	api.Jump
}

// went moves the cursor where a plugin's call last asked to, after the call
// and its edits: in a file it opens, or in the current window when it shows
// the buffer the call was about (B187).
func (e *Session) went() {
	g := e.jumpTo
	e.jumpTo = nil
	switch {
	case g == nil:
	case g.Path != "":
		if e.edit(g.Path); e.win.buf.is(g.Path) {
			e.setCursor(g.Pos)
		}
	case e.win.buf == g.b:
		e.setCursor(g.Pos)
	}
}

// decorate adds and removes the decorations res asks for on b, emits its
// events about b, arms its timers and starts its jobs, whose callbacks are
// about b. It returns res's edits, sorted, for the caller to apply next, so
// they move the new decorations.
func (e *Session) decorate(p api.Module, b *buffer, res api.Result) []vim.Edit {
	// A write that waits for this result waits for its callbacks too (B282).
	save := e.inSave
	wait := func() {
		if save != nil {
			save.pending++
		}
	}
	for _, name := range res.Emit {
		e.emit(name, "", b)
	}
	for _, rq := range res.Requests {
		wait()
		e.pluginRequest(p, b, rq, save)
	}
	for _, j := range res.Jobs {
		wait()
		l := e.listen(p)
		go func() {
			out, err := p.Work(l.ctx, j)
			extra := api.Context{"output": out}
			if err != nil {
				extra["error"] = err.Error()
			}
			e.queue <- func() {
				if slices.Contains(e.modules, p) {
					l.add(event{buf: b, id: j.ID, extra: extra, save: save, from: e})
				}
			}
		}()
	}
	for _, tm := range res.Timers {
		wait()
		time.AfterFunc(tm.After, func() {
			e.queue <- func() {
				if slices.Contains(e.modules, p) {
					e.listen(p).add(event{buf: b, id: tm.ID, save: save, from: e})
				}
			}
		})
	}
	switch {
	case res.Message == "":
	case save != nil:
		save.notes = append(save.notes, res.Message)
	default:
		e.message = res.Message
	}
	if res.Float != nil {
		e.float, e.floatSpans = e.markdown(*res.Float)
	}
	if s := res.Show; s != nil {
		e.panel(s.Title, s.Filetype, s.Text, s.Spans)
	}
	if j := res.Jump; j != nil {
		e.jumpTo = &jumpTo{b, *j}
	}
	if pk := res.Pick; pk != nil {
		e.showPick(p, b, pk, save)
	}
	if t := res.Terminal; t != nil {
		e.moduleTerminal(p, *t)
	}
	if pl := res.Places; pl != nil {
		e.lastList = placesOf(pl)
	}
	if a := e.asked; a != nil && len(res.Complete) > 0 {
		e.answered(a, res.Complete)
	}
	e.setOptions(b, res.Set)
	if res.Status != nil {
		if e.segments[p] == nil {
			e.segments[p] = map[*buffer]string{}
		}
		e.segments[p][b] = *res.Status
	}
	for _, d := range res.Decorate {
		// A sign shows its first two characters, as the column is two wide.
		sign := []rune(d.Sign)
		sign = sign[:min(len(sign), 2)]
		deco := decoration{id: d.ID, buf: b, style: d.Style, note: d.Note, sign: string(sign), under: d.Under, fold: d.Fold}
		// A fold's marks stay at their lines' starts through an insert there.
		gravity := kernel.Right
		if d.Fold {
			gravity = kernel.Left
		}
		deco.start, deco.end = b.text.SetMark(d.Start, gravity), b.text.SetMark(d.End, kernel.Left)
		e.decos[p] = append(e.decos[p], deco)
		if d.Fold {
			b.folds = append(b.folds, deco)
		}
		if len(sign) > 0 {
			b.signs++
		}
	}
	for _, id := range res.Undecorate {
		if i := slices.IndexFunc(e.decos[p], func(d decoration) bool { return d.id == id }); i >= 0 {
			e.decos[p][i].remove()
			e.decos[p] = slices.Delete(e.decos[p], i, i+1)
		}
	}
	slices.SortStableFunc(res.Edits, func(x, y api.Edit) int { return cmp.Compare(x.Start, y.Start) })
	edits := make([]vim.Edit, len(res.Edits))
	for i, ed := range res.Edits {
		edits[i] = vim.Edit(ed)
	}
	return edits
}

// unload closes p after err, removes its decorations and tells the user.
func (e *Session) unload(p api.Module, err error) {
	e.drop(p)
	e.message = fmt.Sprintf("%v, so it was unloaded", err)
	logLine(e.message)
}

// reloadPlugins reads the [[plugin]] tables of config.toml again, closes
// every plugin and what it added, and loads them anew (B191).
func (e *Session) reloadPlugins() {
	if !e.started {
		e.message = "the plugins have not loaded yet"
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		e.message = err.Error()
		return
	}
	for _, p := range slices.Clone(e.modules) {
		if _, ok := p.(*plugin.Plugin); ok {
			e.unregister(p)
			e.drop(p)
		}
	}
	e.cfg.Plugin = cfg.Plugin
	e.setup = e.setup[:e.plugged]
	e.message = fmt.Sprintf("reloading %d plugins", len(cfg.Plugin))
	e.loadPlugins()
}

// unregister removes the commands, operators, objects and mappings p
// added, so a reload that leaves p out leaves nothing of it.
func (e *Session) unregister(p api.Module) {
	for _, r := range p.Registered() {
		switch r.Kind {
		case api.Command:
			delete(e.commands, r.Name)
			delete(e.completers, r.Name)
			delete(e.pluginHelp, ":"+r.Name)
		case api.Source:
			delete(e.sources, r.Name)
			delete(e.pluginHelp, ":pick "+r.Name)
		case api.Operator, api.Object:
			e.pluginBindings = slices.DeleteFunc(e.pluginBindings, func(b vim.Binding) bool {
				return b.Kind == "operator" && b.Keys == r.Name+"{motion}" || b.Kind == "object" && b.Keys == r.Name
			})
		case api.Key, api.Plug:
			delete(e.pluginHelp, "<Plug>("+p.Name()+"."+r.Name+")")
		case api.Insert:
			delete(e.pluginHelp, "<Plug>("+p.Name()+".insert."+r.Name+")")
		}
		for _, s := range e.sessions {
			switch r.Kind {
			case api.Operator:
				s.vim.Operator(r.Name, nil)
			case api.Object:
				s.vim.Object(r.Name, nil)
			case api.Key:
				s.vim.Unmap(vim.Normal, withLeader(r.Name, s.leader))
				s.vim.Command(p.Name()+"."+r.Name, nil)
			case api.Plug:
				s.vim.Command(p.Name()+"."+r.Name, nil)
			case api.Insert:
				s.vim.Unmap(vim.Insert, r.Name)
				s.vim.Command(p.Name()+".insert."+r.Name, nil)
			}
		}
	}
}

// drop closes p and removes its listener, decorations and segments.
func (e *Session) drop(p api.Module) {
	if l := e.listeners[p]; l != nil {
		l.stop()
		delete(e.listeners, p)
	}
	p.Close(context.Background())
	e.modules = slices.DeleteFunc(e.modules, func(q api.Module) bool { return q == p })
	for _, d := range e.decos[p] {
		d.remove()
	}
	delete(e.decos, p)
	delete(e.segments, p)
}

// register adds a command, operator, object or key a plugin registered, as
// the machine's own, so it composes with every built-in (B52, B54).
func (e *Session) register(p api.Module, r api.Registration) {
	if e.pluginHelp == nil {
		e.pluginHelp = map[string]string{}
	}
	switch r.Kind {
	case api.Command:
		e.pluginHelp[":"+r.Name] = r.Help
		e.commands[r.Name] = func(s *Session, c call) {
			scope := s.scope(p, s.win.buf, s.Text())
			scope.About["args"], scope.About["bang"] = c.arg, strconv.FormatBool(c.force)
			scope.About["first"], scope.About["last"], scope.About["ranged"] = strconv.Itoa(c.first), strconv.Itoa(c.last), strconv.FormatBool(c.ranged)
			if edits, _, ok := s.run(p, r.ID, scope, s.Cursor(), 0); ok && len(edits) > 0 {
				if err := (host{s}).Apply(edits, ":"+r.Name); err != nil {
					s.message = err.Error()
					return
				}
				s.setCursor(edits[0].Start)
			}
		}
		if r.Complete {
			e.completers[r.Name] = func(s *Session, arg string) {
				if r.Host && s.power != Host {
					return
				}
				scope := s.scope(p, s.win.buf, s.Text())
				scope.About["complete"] = arg
				s.run(p, r.ID, scope, s.Cursor(), 0)
			}
		}
	case api.Source:
		e.pluginHelp[":pick "+r.Name] = r.Help
		e.sources[r.Name] = func(s *Session, text string) {
			scope := s.scope(p, s.win.buf, s.Text())
			scope.About["args"] = text
			s.run(p, r.ID, scope, s.Cursor(), 0)
		}
	case api.Operator:
		e.pluginBindings = append(e.pluginBindings, vim.Binding{Kind: "operator", Keys: r.Name + "{motion}", Help: r.Help})
		e.each(func(s *Session) {
			s.vim.Operator(r.Name, func(t vim.Text, start, end int, lines bool) []vim.Edit {
				linewise := 0
				if lines {
					linewise = 1
				}
				edits, _, _ := s.run(p, r.ID, s.scope(p, s.win.buf, t), start, end, linewise)
				return edits
			})
		})
	case api.Object:
		e.pluginBindings = append(e.pluginBindings, vim.Binding{Kind: "object", Keys: r.Name, Help: r.Help})
		e.each(func(s *Session) {
			s.vim.Object(r.Name, func(t vim.Text, start, end int) (int, int, bool) {
				_, sel, ok := s.run(p, r.ID, s.scope(p, s.win.buf, t), start, end)
				if !ok || sel == nil {
					return 0, 0, false
				}
				return sel[0], sel[1], true
			})
		})
	case api.Key, api.Plug:
		name := p.Name() + "." + r.Name
		e.pluginHelp["<Plug>("+name+")"] = r.Help
		e.each(func(s *Session) {
			s.vim.Command(name, func(t vim.Text, pos, count int) []vim.Edit {
				edits, _, _ := s.run(p, r.ID, s.scope(p, s.win.buf, t), pos, count)
				return edits
			})
			if r.Kind == api.Key {
				s.vim.Map(vim.Normal, withLeader(r.Name, s.leader), "<Plug>("+name+")")
			}
		})
	case api.Insert:
		name := p.Name() + ".insert." + r.Name
		e.pluginHelp["<Plug>("+name+")"] = r.Help
		e.each(func(s *Session) {
			s.vim.Command(name, func(t vim.Text, pos, _ int) []vim.Edit {
				edits, _, _ := s.run(p, r.ID, s.scope(p, s.win.buf, t), pos)
				return edits
			})
			s.vim.Map(vim.Insert, r.Name, "<Plug>("+name+")")
		})
	}
}

// scope is what a call of p about b reads: the text t, what the call is
// about, every open buffer (B188), b's syntax nodes (B189) and p's own
// decorations on b (B289).
func (e *Session) scope(p api.Module, b *buffer, t api.Text) api.Scope {
	bufs := map[int]api.Buffer{}
	for _, o := range e.bufs {
		bufs[o.id] = api.Buffer{Path: o.file.path, Filetype: e.filetype(o), Text: o.text.Snapshot()}
	}
	ft, opts, cfg := e.filetype(b), e.opts, &e.cfg
	if e.win.buf == b {
		opts = e.win.opts
	}
	s := api.Scope{Text: t, About: e.about(b), Buffers: bufs, Language: func(int) string { return ft }}
	s.Option = func(name string) (string, bool) { return option(opts, cfg, name) }
	s.Config = func(path string) (string, bool) { return configNode(cfg, path) }
	if b.syntax != nil {
		s.Node, s.Free = b.syntax.Nodes()
		regions := b.syntax.Regions()
		s.Language = func(pos int) string { return cmp.Or(syntax.LanguageIn(regions, pos), ft) }
		if snap, ok := t.(kernel.Snapshot); ok {
			queries := e.queries
			s.Spans = func(start, end int) []api.Span {
				var out []api.Span
				for _, sp := range b.hl.of(snap, ft, queries) {
					if sp.End > start && sp.Start < end {
						out = append(out, api.Span(sp))
					}
				}
				return out
			}
		}
	}
	language := s.Language
	s.Comment = func(pos int) (string, bool) { return syntax.Comment(language(pos)) }
	s.Decorations = func() []api.Decoration {
		var out []api.Decoration
		for _, d := range e.decos[p] {
			if d.buf == b {
				out = append(out, api.Decoration{ID: d.id, Start: b.text.Mark(d.start), End: b.text.Mark(d.end), Style: d.style, Sign: d.sign, Note: d.note, Under: d.under, Fold: d.fold})
			}
		}
		return out
	}
	return s
}

// about is what a call about b tells the plugin through context: the
// buffer's id, path and filetype, the mode (B176), and its indent and tab
// widths (B280).
func (e *Session) about(b *buffer) api.Context {
	return api.Context{
		"buffer": strconv.Itoa(b.id), "path": b.file.path, "filetype": e.filetype(b), "mode": modes[e.vim.Mode()],
		"indent": strconv.Itoa(e.indentWidth(b)), "tab": strconv.Itoa(e.tabWidth(b)),
	}
}

// run calls what p registered as id, on the input path, against the current
// buffer, reading scope. It adds the decorations the call asks for, and
// returns its edits, sorted, and what an object's call selected. A call that
// fails unloads p, and calls into an unloaded module do nothing.
func (e *Session) run(p api.Module, id uint32, scope api.Scope, args ...int) ([]vim.Edit, *[2]int, bool) {
	defer scope.Done()
	// A guest may not call a plugin granted process, net or fs.read (B246).
	if w, ok := p.(*plugin.Plugin); ok && e.power != Host && slices.ContainsFunc(w.Capabilities, func(c string) bool { return c == "process" || c == "net" || c == "fs.read" }) {
		e.message = "a guest may not use plugin " + p.Name()
		return nil, nil, false
	}
	if !slices.Contains(e.modules, p) {
		e.message = fmt.Sprintf("plugin %s is unloaded", p.Name())
		return nil, nil, false
	}
	name := strconv.Itoa(int(id))
	for _, r := range p.Registered() {
		if r.ID == id {
			name = r.Name
			if r.Host && e.power != Host {
				e.message = "a guest may not run " + name
				return nil, nil, false
			}
		}
	}
	at := time.Now()
	res, err := p.Call(context.Background(), id, scope, args...)
	trace.Record("plugin", p.Name()+" ran "+name, time.Since(at))
	switch {
	case errors.Is(err, api.ErrBusy):
		e.message = err.Error()
		return nil, nil, false
	case err != nil:
		e.unload(p, err)
		return nil, nil, false
	}
	return e.decorate(p, e.win.buf, res), res.Selected, true
}

// logLine appends a line to fude's log, which is where a plugin's own log
// lines, the calls it is denied and language servers' stderr go (B50). A line that cannot be written
// is dropped, as the log is where it would have been reported.
func logLine(line string) {
	trace.Record("log", line, 0)
	dir, err := xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	if err := os.MkdirAll(filepath.Join(dir, "fude"), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "fude", "log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.DateTime), line)
}

// showPick shows pk, a list or question of p about b, in the finder, or gives
// the live pick showing the items pk answers a query with (B185, B285).
func (e *Session) showPick(p api.Module, b *buffer, pk *api.Pick, save *saving) {
	if f := e.finder; f != nil && f.owner == p && f.pick == pk.ID && f.found != nil {
		found := f.found
		f.found = nil
		found(pk.Items, nil)
		return
	}
	if save != nil {
		save.pending++
	}
	tell := func(extra api.Context) {
		if slices.Contains(e.modules, p) {
			e.listen(p).add(event{buf: b, id: pk.ID, extra: extra, save: save, from: e})
		}
	}
	answer := func(picked int, item, query string, ok bool) {
		tell(api.Context{"picked": strconv.Itoa(picked), "item": item, "text": query, "ok": strconv.FormatBool(ok)})
	}
	// One list shows at a time, so a second is cancelled.
	if e.finder != nil {
		answer(-1, "", "", false)
		return
	}
	prompt := ""
	if pk.Prompt != "" {
		prompt = pk.Prompt + " "
	}
	// stale, so the first frame matches the items, which are all here.
	f := &finder{paths: pk.Items, stale: true, prompt: prompt, title: pk.Title, hint: pk.Hint, places: pk.Places, chose: answer, owner: p, pick: pk.ID}
	if pk.Live {
		f.lookup = func(q string, found func([]string, error)) {
			f.found = found
			tell(api.Context{"query": q, "all": strconv.FormatBool(f.all)})
		}
	}
	e.openFinder(f, pk.Query)
}
