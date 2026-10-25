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
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/timeline"
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
	// Off the loop, as it reads a directory (B382).
	go func() {
		dir, err := timelineDir()
		if err == nil {
			err = timeline.Prune(dir, time.Now().Add(-timelineDays*24*time.Hour))
		}
		if err != nil {
			trace.Record("file", err.Error(), 0)
		}
	}()
	var failed []string
	for _, b := range e.bufs {
		if err := e.attach(b); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if err := e.moduleConfig(e.cfg); err != nil {
		failed = append(failed, err.Error())
	}
	if len(failed) > 0 {
		e.message = strings.Join(failed, "  ")
	}
	e.plugged = len(e.setup)
	e.loadPlugins()
	// This runs after the first frame, so a command such as :terminal knows
	// the screen's size.
	e.begin()
}

// Use runs m, a module built into the distribution, as it runs a plugin
// (D49). Call it before Load. :plugin reload leaves it as it is.
func (e *Session) Use(m api.Module) {
	// Its options start before its init, which may read them. Load reports
	// a wrong [module.name] table.
	start, _ := texts(e.cfg.Module[m.Name()])
	e.declare(m, start)
	b, t := e.win.buf, e.Text()
	scope := e.scope(m, b, t)
	res, err := m.Init(context.Background(), scope)
	scope.Done()
	e.loaded(m, err, b, t, res)
}

// loadPlugins starts the plugins config.toml lists.
func (e *Session) loadPlugins() {
	config, err1 := keep.Dir("XDG_CONFIG_HOME", ".config")
	cache, err2 := keep.Dir("XDG_CACHE_HOME", ".cache")
	if err := errors.Join(err1, err2); err != nil {
		e.message = fmt.Sprintf("load plugins: %v", err)
		return
	}
	for _, s := range e.cfg.Plugin {
		if s.Disabled {
			continue
		}
		path := s.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(config, "fude", path)
		}
		options, err := texts(s.Options)
		if err != nil {
			e.message = fmt.Sprintf("load plugin %s: %v", s.Path, err)
			continue
		}
		b, t, scope := e.win.buf, e.Text(), e.scope(nil, e.win.buf, e.Text())
		e.working++
		go func() {
			defer scope.Done()
			ctx, at := context.Background(), time.Now()
			p, err := plugin.Load(ctx, path, plugin.Grants{Capabilities: s.Capabilities, Programs: s.Programs, Hosts: s.Hosts, Options: options}, filepath.Join(cache, "fude", "wasm"), logLine, func(text string, took time.Duration) { trace.Record("plugin", text, took) })
			if err != nil {
				trace.Since("plugin", "load "+path, at, err)
				e.queue <- func() {
					e.working--
					e.message = err.Error()
				}
				return
			}
			res, err := p.Init(ctx, scope)
			trace.Since("plugin", "compiled and ran init of "+p.Name(), at, err)
			e.queue <- func() {
				e.working--
				e.declare(p, options)
				e.loaded(p, err, b, t, res)
			}
		}()
	}
}

// texts returns a plugin's options table, or a [module.name] table, as the
// text its options start at. Only numbers, strings and booleans read as text, so
// a table or a list is refused (B178, B377).
func texts(settings map[string]any) (map[string]string, error) {
	out := map[string]string{}
	for name, v := range settings {
		switch v.(type) {
		case string, int64, float64, bool:
			out[name] = fmt.Sprint(v)
		default:
			return nil, fmt.Errorf("option %s is not a number, string or boolean", name)
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
	trace.Record(kind(p), "loaded "+p.Name(), 0)
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
	note       string   // a module's text after the end of the line
	sign       string   // a plugin's text for the sign column
	layer      int      // orders its sign among a line's, the highest showing (B184)
	fold       bool     // a closed fold over its lines (B288)
	replace    bool     // note shows in place of its text (B295)
	insert     bool     // note shows among the text at its start (B216)
	cover      bool     // note shows over its text, on the cursor's line too (B315)
	icon       bool     // sign shows in the icon column (B337)
	line       bool     // its style's ground shades its lines' rows (B296)
	lines      []string // rows drawn below its line, or above it (B299)
	above      bool
	block      bool   // its lines show their text as it is together (B300)
	image      string // the image file it shows below its line (B303)
}

// inline reports whether d shows text in or between the lines, or decides
// which lines show theirs as it is: an inlay hint, a proposal's text, a
// replacement, rows of its own or a block (B71, B216, B295, B299, B300).
func (d decoration) inline() bool {
	return d.replace || d.insert || d.cover || len(d.lines) > 0 || d.block || d.image != "" || strings.HasPrefix(d.style, "proposal") && d.note != ""
}

// remove deletes the decoration's marks.
func (d decoration) remove() {
	d.buf.text.DeleteMark(d.start)
	d.buf.text.DeleteMark(d.end)
	if d.fold {
		d.buf.folds = slices.DeleteFunc(d.buf.folds, func(f decoration) bool { return f.start == d.start })
	}
	if d.sign != "" && !d.icon {
		d.buf.signs--
	}
	if d.inline() {
		d.buf.inlines--
	}
}

// pluginRequest sends a plugin's request to those of b's servers that offer it,
// every one for a method outside textDocument/, and hands the plugin the
// first answer that is not null, as a job's output (B190).
// clientCommand runs command, which from named, through the module that
// registered it, and reports whether one did (B539).
func (e *Session) clientCommand(from *server, command, args any) bool {
	name, _ := command.(string)
	run := e.clientCommands[name]
	if run == nil {
		return false
	}
	raw, err := json.Marshal(args)
	if err != nil {
		logLine(fmt.Sprintf("the arguments of %s: %v", name, err))
		return true
	}
	run(e, from, string(raw))
	return true
}

// pluginNotify sends n to the language server n names on b (B532).
func (e *Session) pluginNotify(p api.Module, b *buffer, n api.Notify) {
	i := slices.IndexFunc(b.servers, func(s *server) bool { return s.name == n.Server })
	switch {
	case i < 0:
		logLine(fmt.Sprintf("%s: %s to %s, which is not on %s", p.Name(), n.Method, n.Server, cmp.Or(b.file.path, "the buffer")))
	case n.Params != "" && !json.Valid([]byte(n.Params)):
		logLine(fmt.Sprintf("%s: the params of %s are not JSON: %s", p.Name(), n.Method, n.Params))
	default:
		b.servers[i].client.Notify(n.Method, json.RawMessage(cmp.Or(n.Params, "null")))
	}
}

func (e *Session) pluginRequest(p api.Module, b *buffer, rq api.Request, save *saving) {
	l := e.listen(p)
	reply := func(extra api.Context) {
		if slices.Contains(e.modules, p) {
			l.add(event{buf: b, id: rq.ID, extra: extra, save: save, from: e})
		}
	}
	document := strings.HasPrefix(rq.Method, "textDocument/")
	var to []*server
	for _, s := range b.servers {
		if (rq.Server == "" || s.name == rq.Server) && (!document || s.offers(rq.Method)) {
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
	if rq.Method == "workspace/executeCommand" && e.clientCommand(to[0], params["command"], params["arguments"]) {
		reply(api.Context{})
		return
	}
	t, uri := b.text.Snapshot(), lsp.URI(b.file.path)
	left, done := len(to), false
	e.working++
	for _, s := range to {
		s.client.Request(rq.Method, func(enc string) any {
			out := map[string]any{}
			if document {
				out["textDocument"] = map[string]any{"uri": uri}
			}
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
				e.working--
				reply(api.Context{"output": string(res), "server": s.name, "encoding": s.client.Encoding()})
			case left == 0:
				e.working--
				reply(api.Context{"error": "no language server answered " + rq.Method})
			}
		})
	}
}

// setOptions sets the options a result asks for on b (B283).
func (e *Session) setOptions(b *buffer, set map[string]string) {
	for name, value := range set {
		if name != "expand_tab" && name != "indent_width" && name != "tab_width" {
			e.report(e.set(name + " " + value))
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
		e.emit("option.set", name, b)
	}
}

// jumpTo is where a plugin's call about b asked to move the cursor.
type jumpTo struct {
	b *buffer
	p api.Module
	api.Jump
}

// went moves the cursor where a plugin's call last asked to, after the call
// and its edits: in a file it opens, or in the current window when it shows
// the buffer the call was about (B187). Then it does what the calls' results
// asked of buffers and windows. A command line a result ran may ask for
// more, so it repeats, at most 8 times, and a call that runs itself cannot
// stall the editor.
func (e *Session) went() {
	for range 8 {
		if e.jumpTo == nil && len(e.actions) == 0 {
			break
		}
		e.arrive()
	}
	e.notice()
}

// arrive moves the cursor as went does, then does the actions waiting.
func (e *Session) arrive() {
	g := e.jumpTo
	e.jumpTo = nil
	switch {
	case g == nil:
	case g.Path != "":
		// A file shows in a window that shows a file or an explorer, not in a
		// panel's.
		onFile := func(w *window) bool { return w.buf.title == "" || w.buf.listed != "" }
		if !onFile(e.win) {
			if i := slices.IndexFunc(e.root.windows(), onFile); i >= 0 {
				e.win = e.root.windows()[i]
			}
		}
		if where := e.placement(g.p, g.Where, g.Lock, "here"); where != "here" {
			e.openIn(e.win.buf, where)
			e.win.locked = g.Lock
		}
		// Moving to another buffer is a jump of its own.
		if e.win.buf.is(g.Path) {
			e.pushJump(e.Cursor())
		} else {
			e.report(e.edit(g.Path))
		}
		if e.win.buf.is(g.Path) {
			e.setCursor(e.jumpPos(g))
			e.reveal()
		}
	case g.Buffer != 0:
		to, err := e.buffer(g.Buffer)
		if err != nil {
			e.message = err.Error()
			break
		}
		if where := e.placement(g.p, g.Where, g.Lock, "here"); where != "here" {
			e.openIn(e.win.buf, where)
			e.win.locked = g.Lock
		}
		e.show(to)
		if e.win.buf == to && g.Line != 0 {
			e.setCursor(e.jumpPos(g))
			e.reveal()
		}
	case e.win.buf == g.b:
		e.pushJump(e.Cursor())
		e.setCursor(e.jumpPos(g))
	}
	actions := e.actions
	e.actions = nil
	for _, f := range actions {
		f()
	}
}

// noticed is what the modules last heard of the session's windows, the
// search, the macro, the selection and the theme.
type noticed struct {
	started   bool
	wins      map[int]*buffer // by id, each window's buffer
	win       int
	search    string
	recording rune
	selection string
	theme     string
}

// notice tells the modules what changed of the windows, the search, the
// macro being recorded, the selection and the theme (B346).
func (e *Session) notice() {
	n := &e.noticed
	wins := map[int]*buffer{}
	for _, w := range e.windows() {
		wins[w.id] = w.buf
	}
	search, _ := e.vim.Search()
	selection := ""
	if s, ok := e.vim.Selection(); ok {
		selection = fmt.Sprintf("%d %d", s.Start, s.End)
	}
	if n.started {
		for _, id := range slices.Sorted(maps.Keys(wins)) {
			if _, ok := n.wins[id]; !ok {
				e.emit("window.opened", strconv.Itoa(id), wins[id])
			}
		}
		for _, id := range slices.Sorted(maps.Keys(n.wins)) {
			if _, ok := wins[id]; !ok {
				e.emit("window.closed", strconv.Itoa(id), n.wins[id])
			}
		}
		if e.win.id != n.win {
			e.emit("window.focused", strconv.Itoa(e.win.id), e.win.buf)
		}
		if search != n.search {
			e.emit("search.changed", search, e.win.buf)
		}
		if r := e.vim.Recording(); r == 0 && n.recording != 0 {
			e.emit("macro.recorded", string(n.recording), e.win.buf)
		}
		if selection != n.selection {
			e.emit("selection.changed", selection, e.win.buf)
		}
		if e.theme != n.theme {
			e.emit("theme.changed", e.theme, e.win.buf)
		}
	}
	*n = noticed{true, wins, e.win.id, search, e.vim.Recording(), selection, e.theme}
}

// act does what res asks of buffers, windows and the vim machine, once the
// call's edits are made (B343, B344, B345).
func (e *Session) act(p api.Module, res api.Result) {
	for i, id := range slices.Concat(res.Close, res.Drop) {
		if b, err := e.buffer(id); err != nil {
			e.message = err.Error()
		} else {
			e.close(b, i >= len(res.Close))
		}
	}
	for _, id := range res.Write {
		if b, err := e.buffer(id); err != nil {
			e.message = err.Error()
		} else {
			e.report(e.write(b, false, nil))
		}
	}
	for _, a := range res.Windows {
		if err := e.windowAction(a); err != nil {
			e.message = err.Error()
		}
	}
	if s := res.Scratch; s != nil {
		e.openScratch(p, *s)
	}
	for _, m := range res.Marks {
		if err := e.setMark(m); err != nil {
			e.message = err.Error()
		}
	}
	for _, r := range res.Registers {
		name := []rune(r.Name)
		if len(name) != 1 {
			e.message = "no register named " + r.Name
			continue
		}
		if err := e.vim.SetRegister(name[0], vim.Saved{Text: r.Text, Linewise: r.Linewise, Block: r.Block}); err != nil {
			e.message = err.Error()
		}
	}
	if len(res.Styles) > 0 {
		e.restyle(res.Styles)
	}
	if a := res.Each; a != nil {
		e.runAt(*a)
	}
	if s := res.Search; s != nil {
		e.vim.SetSearch(s.Pattern, s.Forward)
	}
	if len(res.Ex) > 0 && res.Place != "" {
		e.placing = res.Place
	}
	for _, line := range res.Ex {
		if err := e.vim.Ex(line); err != nil {
			e.message = err.Error()
		}
	}
}

// restyle defines the styles a module's result names (B345).
func (e *Session) restyle(styles map[string]api.Style) {
	if e.styles == nil {
		e.styles = map[string]wire.Style{}
	}
	for name, s := range styles {
		out := wire.Style{Bold: s.Bold, Italic: s.Italic, Underline: s.Underline, Strike: s.Strike}
		if err := errors.Join(out.Fg.UnmarshalText([]byte(s.Fg)), out.Bg.UnmarshalText([]byte(s.Bg))); err != nil {
			e.message = fmt.Sprintf("style %s: %v", name, err)
			continue
		}
		e.styles[name] = out
	}
	e.restyled++
}

// setMark sets the mark m names in its buffer (B344).
func (e *Session) setMark(m api.Mark) error {
	b, err := e.buffer(m.Buffer)
	name := []rune(m.Name)
	switch {
	case err != nil:
		return err
	case len(name) != 1:
		return fmt.Errorf("no mark named %q", m.Name)
	case m.Pos < 0 || m.Pos > b.text.Snapshot().Len():
		return fmt.Errorf("%d is outside buffer %d", m.Pos, b.id)
	}
	b.setMark(name[0], m.Pos)
	return nil
}

// windowAction does a to its window (B343).
func (e *Session) windowAction(a api.WindowAction) error {
	w := e.win
	if a.ID != 0 {
		i := slices.IndexFunc(e.windows(), func(w *window) bool { return w.id == a.ID })
		if i < 0 {
			return fmt.Errorf("no window %d", a.ID)
		}
		w = e.windows()[i]
	}
	t := w.buf.text.Snapshot()
	switch a.Do {
	case "close":
		return e.closeWindow(w)
	case "only":
		e.win = w
		return e.only()
	case "float", "above", "below", "left", "right":
		return e.move(w, a.Do)
	case "zoom":
		return e.zoom(w)
	case "hide":
		if w.locked {
			return fmt.Errorf("%s cannot move", w.buf.name())
		}
		return e.hide(w)
	case "focus":
		e.win = w
	case "height", "width", "taller", "wider":
		if w.anchor != "" {
			return fmt.Errorf("window %d is a float, which sizes itself", w.id)
		}
		cur := e.win
		e.win = w
		e.resize(a.Do == "width" || a.Do == "wider", func(now, room int) int {
			switch {
			case a.Do == "taller" || a.Do == "wider":
				return now + a.Size
			case a.Size == 0:
				return room
			}
			return a.Size
		})
		e.win = cur
	case "cursor", "top":
		if a.Pos < 0 || a.Pos > t.Len() {
			return fmt.Errorf("%d is outside window %d's text", a.Pos, w.id)
		}
		if a.Do == "top" {
			w.buf.text.MoveMark(w.top, t.LineStart(t.LineOf(a.Pos)))
		} else {
			w.buf.text.MoveMark(w.cursor, a.Pos)
		}
	case "bind":
		if !w.bound {
			w.bound, w.wrapped, w.opts.Wrap = true, w.opts.Wrap, false
		}
		e.syncKeys()
	case "unbind":
		if w.bound {
			w.bound, w.opts.Wrap, w.fill = false, w.wrapped, 0
		}
		e.syncKeys()
	default:
		return fmt.Errorf("no window action %q", a.Do)
	}
	return nil
}

// openScratch shows a new buffer with no file holding s's text, coloured
// as its filetype, for the user to edit, in a window placed for p (B343).
func (e *Session) openScratch(p api.Module, s api.Scratch) {
	if _, err := e.addAs("", !s.Unnumbered); err != nil {
		e.message = err.Error()
		return
	}
	b := e.bufs[len(e.bufs)-1]
	if s.Text != "" {
		if err := b.text.Apply(kernel.Transaction{Edits: []kernel.Edit{{Text: s.Text}}}); err != nil {
			e.message = err.Error()
			return
		}
		b.head, b.saved = b.text.Head(), b.text.Head()
	}
	b.panelKind = s.Kind
	e.openIn(b, e.placement(p, s.Where, s.Lock, "here"))
	e.win.locked = s.Lock
	if b.ft = s.Filetype; b.ft == "" {
		return
	}
	if _, err := e.attachSyntax(b); err != nil {
		e.message = err.Error()
	}
}

// jumpPos is where g moves the cursor in the current buffer: its line and
// column when it has them, or its position.
func (e *Session) jumpPos(g *jumpTo) int {
	if g.Line == 0 {
		return g.Pos
	}
	t := e.Text()
	if g.Encoding != "" {
		return lsp.Offset(t, lsp.Position{Line: g.Line - 1, Character: g.Column - 1}, g.Encoding)
	}
	start := t.LineStart(min(max(g.Line-1, 0), t.Lines()-1))
	return min(start+max(g.Column-1, 0), vim.LineEnd(t, start))
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
	for _, n := range res.Notify {
		e.pluginNotify(p, b, n)
	}
	for _, a := range res.Answers {
		answer, ok := e.waiting[a.Request]
		switch {
		case !ok:
			logLine(fmt.Sprintf("%s: an answer to request %s, which no server waits for", p.Name(), a.Request))
		case a.Result != "" && !json.Valid([]byte(a.Result)):
			logLine(fmt.Sprintf("%s: the answer to request %s is not JSON: %s", p.Name(), a.Request, a.Result))
		default:
			delete(e.waiting, a.Request)
			answer(json.RawMessage(cmp.Or(a.Result, "null")))
		}
	}
	for _, j := range res.Jobs {
		wait()
		l := e.listen(p)
		// A stream's callbacks are about the buffer current as each line
		// comes, and only its end counts for a write that waits (B291).
		ctx := l.ctx
		var st *stream
		if j.Stream {
			j.Emit = func(line string) {
				e.queue <- func() {
					if slices.Contains(e.modules, p) {
						l.add(event{buf: e.streamBuf(), id: j.ID, extra: api.Context{"output": line}, from: e})
					}
				}
			}
			st = &stream{}
			st.ctx, st.stop = context.WithCancel(l.ctx)
			ctx = st.ctx
			if j.Fed {
				st.feed = make(chan string)
				j.Feed = st.feed
			}
			l.streams[j.ID] = st
		}
		e.working++
		go func() {
			// The start line shows a job that never ends. Its id is its callback's.
			task := fmt.Sprintf("%s #%d", cmp.Or(j.URL, strings.Join(j.Argv, " ")), j.ID)
			trace.Record("job", p.Name()+" started "+task, 0)
			at := time.Now()
			out, err := p.Work(ctx, j)
			if st != nil {
				st.stop()
			}
			what := fmt.Sprintf("%s ran %s, %d bytes", p.Name(), task, len(out))
			switch {
			case j.URL != "":
				what = fmt.Sprintf("%s got %s, %d bytes", p.Name(), task, len(out))
			case j.Stream:
				what = p.Name() + " streamed " + task
			}
			trace.Since("job", what, at, err)
			extra := api.Context{"output": out}
			if err != nil {
				extra["error"] = err.Error()
			}
			if j.Stream {
				extra["done"] = "true"
			}
			e.queue <- func() {
				e.working--
				about := b
				if j.Stream {
					about = e.streamBuf()
					if l.streams[j.ID] == st {
						delete(l.streams, j.ID)
					}
				}
				if slices.Contains(e.modules, p) {
					l.add(event{buf: about, id: j.ID, extra: extra, save: save, from: e})
				}
			}
		}()
	}
	for _, sd := range res.Send {
		st := e.listen(p).streams[sd.ID]
		switch {
		case st == nil:
			logLine(fmt.Sprintf("%s: send to job #%d, which is not running", p.Name(), sd.ID))
		case sd.Stop:
			st.stop()
		case st.feed == nil:
			logLine(fmt.Sprintf("%s: send to job #%d, which takes no input", p.Name(), sd.ID))
		default:
			st.send(sd.Text)
		}
	}
	for _, line := range res.Log {
		logLine(line)
	}
	for _, tm := range res.Timers {
		wait()
		e.working++
		time.AfterFunc(tm.After, func() {
			e.queue <- func() {
				e.working--
				if slices.Contains(e.modules, p) {
					e.listen(p).add(event{buf: b, id: tm.ID, save: save, from: e})
				}
			}
		})
	}
	// A failure's reason shows in place of the message, but not one of a
	// hover the mouse asked for (B562).
	pointed := e.pointing != nil && e.pointing.p == p
	switch msg := cmp.Or(res.Error, res.Message); {
	case msg == "", pointed && res.Error != "":
	case save != nil:
		save.notes = append(save.notes, msg)
	default:
		e.message = msg
		if res.Alert && res.Error == "" {
			e.alert = &msg
		}
	}
	if f := res.Float; f != nil && f.Keys {
		// Key hints for a command no longer typed are too late.
		if e.keyFloat = f; f.Text == "" || e.vim.Shown() == "" {
			e.keyFloat = nil
		}
	} else if f != nil && f.Spans == nil {
		text, spans := e.markdown(f.Text)
		e.popup(text, spans, markdownImage(f.Text, b.file.path), pointed)
	} else if f != nil {
		e.popup(f.Text, styled(f.Spans), "", pointed)
	}
	if a := res.Apply; a != nil {
		e.applyWorkspace(b, a)
	}
	// A command line starts with its text typed, nothing run (B292).
	if res.Cmdline != "" {
		e.later = append(e.later, ":")
		for _, r := range strings.ReplaceAll(res.Cmdline, "\n", "") {
			e.later = append(e.later, vim.Key(string(r)))
		}
	}
	e.serverActions(res.Servers)
	for _, m := range res.Moved {
		e.moved(m[0], m[1])
		// Each move is an event of its own, so a listener never merges two (B346).
		for _, l := range e.listeners {
			if slices.Contains(l.p.Subscribed(), "explorer.moved") {
				l.add(event{name: "explorer.moved", buf: b, extra: api.Context{"from": m[0], "to": m[1]}, from: e})
			}
		}
	}
	if res.Tabs != nil {
		e.tabs = *res.Tabs
	}
	if res.TabLine != nil {
		e.tabNames = map[int]string{}
		for _, t := range *res.TabLine {
			e.tabNames[t.Page] = t.Name
		}
	}
	for _, s := range e.sessions {
		for _, w := range s.windows() {
			if starts, ok := res.Pins[w.id]; ok {
				w.pins.buf, w.pins.starts = w.buf, starts
			}
			if lines, ok := res.Guides[w.id]; ok {
				w.guides.buf, w.guides.lines = w.buf, lines
			}
		}
	}
	if res.Follow != "" {
		e.startFollow(res.Follow)
	}
	if len(res.Build) > 0 {
		e.build(res.Build)
	}
	if res.Kick != "" {
		e.kick(res.Kick)
	}
	if len(res.Window) == 1 {
		e.moveTo(res.Window[0])
	}
	if res.Copy != "" {
		if err := (host{e}).SetClipboard(res.Copy); err != nil {
			e.message = err.Error()
		}
	}
	// A Completer's items that came later fill the menu it held (B336).
	if w := e.completion.waiting; w != nil && w.p == p && res.Completion != nil {
		e.completion.waiting = nil
		e.offer(w.keys, b.text.Snapshot(), res.Completion.Start, []string{w.name}, map[string][]item{w.name: items(res.Completion.Items)})
	}
	if tr := res.Travel; tr != nil {
		on, err := e.buffer(tr.Buffer)
		if tr.Buffer == 0 {
			on = b
		}
		if err != nil {
			e.message = err.Error()
		} else {
			e.travel(on, *tr)
		}
	}
	if res.Restore != "" {
		e.restoreFrom(res.Restore)
	}
	switch s := res.Show; {
	case s != nil && s.Screen:
		e.screenPanel(b, *s)
	case s != nil && e.panel(p, *s) && s.Line > 0:
		e.panelAt(s.Title, s.Line-1)
	}
	e.later = append(e.later, vim.Keys(res.Keys)...)
	if j := res.Jump; j != nil {
		e.jumpTo = &jumpTo{b, p, *j}
	}
	e.actions = append(e.actions, func() { e.act(p, res) })
	if pk := res.Pick; pk != nil {
		e.showPick(p, b, pk, save)
	}
	if c := res.Confirm; c != nil && c.ID != 0 {
		e.moduleConfirm(p, b, c, save)
	}
	if m := res.Menu; m != nil {
		e.offerMenu(p, b, m)
	}
	if t := res.Terminal; t != nil {
		e.moduleTerminal(p, *t)
	}
	if pl := res.Places; pl != nil {
		e.setPlaces(pl)
	}
	if a := e.cmdline.asked; a != nil && len(res.Complete) > 0 {
		e.answered(a, res.Complete)
	}
	e.setOptions(b, res.Set)
	if res.Status != nil {
		if e.segments[p] == nil {
			e.segments[p] = map[*buffer]string{}
		}
		e.segments[p][b] = *res.Status
	}
	if res.StatusLeft != nil {
		if e.leftSegments[p] == nil {
			e.leftSegments[p] = map[*buffer]string{}
		}
		e.leftSegments[p][b] = *res.StatusLeft
	}
	for id, s := range res.SessionLeft {
		on, err := e.buffer(id)
		if err != nil {
			e.message = err.Error()
			continue
		}
		if e.ownLeft == nil {
			e.ownLeft = map[api.Module]map[*buffer]string{}
		}
		if e.ownLeft[p] == nil {
			e.ownLeft[p] = map[*buffer]string{}
		}
		e.ownLeft[p][on] = s
	}
	for _, d := range res.Decorate {
		on, err := e.bufferFor(b, d)
		if err != nil {
			e.message = err.Error()
			continue
		}
		// A sign shows its first two characters, as the column is two wide.
		sign := []rune(d.Sign)
		sign = sign[:min(len(sign), 2)]
		deco := decoration{id: d.ID, buf: on, style: d.Style, note: d.Note, sign: string(sign), layer: d.Layer, fold: d.Fold, replace: d.Replace, insert: d.Insert, cover: d.Cover, icon: d.Icon, line: d.Line, lines: d.Lines, above: d.Above, block: d.Block, image: d.Image}
		// A fold's marks stay at their lines' starts through an insert there.
		gravity := kernel.Right
		if d.Fold {
			gravity = kernel.Left
		}
		deco.start, deco.end = on.text.SetMark(d.Start, gravity), on.text.SetMark(d.End, kernel.Left)
		e.decos[p] = append(e.decos[p], deco)
		if d.Fold {
			on.folds = append(on.folds, deco)
		}
		if len(sign) > 0 && !d.Icon {
			on.signs++
		}
		if deco.inline() {
			on.inlines++
		}
	}
	for _, id := range res.Undecorate {
		if i := slices.IndexFunc(e.decos[p], func(d decoration) bool { return d.id == id }); i >= 0 {
			e.decos[p][i].remove()
			e.decos[p] = slices.Delete(e.decos[p], i, i+1)
		}
	}
	slices.SortStableFunc(res.Edits, func(x, y api.Edit) int { return cmp.Compare(x.Start, y.Start) })
	var edits []vim.Edit
	others := map[int][]kernel.Edit{}
	for _, ed := range res.Edits {
		if ed.Buffer == 0 || ed.Buffer == b.id {
			edits = append(edits, vim.Edit{Start: ed.Start, End: ed.End, Text: ed.Text})
		} else {
			others[ed.Buffer] = append(others[ed.Buffer], kernel.Edit{Start: ed.Start, End: ed.End, Text: ed.Text})
		}
	}
	for _, id := range slices.Sorted(maps.Keys(others)) {
		if err := e.editBuffer(p, id, others[id]); err != nil {
			e.message = fmt.Sprintf("plugin %s: %v", p.Name(), err)
		}
	}
	return edits
}

// editBuffer makes p's edits to the buffer id, other than the one its call
// was about, as one change (B342).
func (e *Session) editBuffer(p api.Module, id int, edits []kernel.Edit) error {
	if err := e.mayEdit(); err != nil {
		return err
	}
	b, err := e.buffer(id)
	switch {
	case err != nil:
		return err
	case b.written():
		return fmt.Errorf("buffer %d is text fude writes", id)
	case b.file.readonly:
		return fmt.Errorf("buffer %d is read-only", id)
	}
	t := b.text.Snapshot()
	for _, ed := range edits {
		if ed.Start < 0 || ed.Start > ed.End || ed.End > t.Len() {
			return fmt.Errorf("edit %d-%d is outside buffer %d", ed.Start, ed.End, id)
		}
	}
	tx := kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: "plugin " + p.Name(), Time: e.now(), Edits: edits}
	if err := b.text.Apply(tx); err != nil {
		return fmt.Errorf("edit buffer %d: %w", id, err)
	}
	b.head = b.text.Head()
	return nil
}

// applyWorkspace applies a module's workspace edit from the server it
// names, about b, and says how many files changed or what went wrong
// (B292).
func (e *Session) applyWorkspace(b *buffer, a *api.WorkspaceEdit) {
	i := slices.IndexFunc(b.servers, func(s *server) bool { return s.name == a.Server })
	if i < 0 {
		e.message = "no language server named " + a.Server + " is attached"
		return
	}
	changed, problems := e.workspaceEdit(b.servers[i], b, b.text.Snapshot(), json.RawMessage(a.Edit), a.Label)
	e.message = fmt.Sprintf("%s changed %d files", a.Label, changed)
	if len(problems) > 0 {
		e.message = strings.Join(problems, "  ")
	}
}

// styled turns a module's spans into the editor's.
func styled(in []api.Span) []wire.Span {
	out := make([]wire.Span, len(in))
	for i, sp := range in {
		out[i] = wire.Span(sp)
	}
	return out
}

// serverNames are the names of servers.
func serverNames(servers []*server) []string {
	names := make([]string, len(servers))
	for i, s := range servers {
		names[i] = s.name
	}
	return names
}

// bufferFor returns the buffer d goes on: the one its Buffer names, or
// else b (B291).
func (e *Session) bufferFor(b *buffer, d api.Decoration) (*buffer, error) {
	if d.Buffer == 0 {
		return b, nil
	}
	if i := slices.IndexFunc(e.bufs, func(o *buffer) bool { return o.id == d.Buffer }); i >= 0 {
		return e.bufs[i], nil
	}
	return nil, fmt.Errorf("no buffer %d to decorate", d.Buffer)
}

// unload closes p after err, removes its decorations and tells the user.
func (e *Session) unload(p api.Module, err error) {
	e.drop(p)
	e.message = fmt.Sprintf("%v, so it was unloaded", err)
	logLine(e.message)
}

// reloadPlugins reads the [[plugin]] tables of config.toml again, closes
// every plugin and what it added, and loads them anew (B191).
func (e *Session) reloadPlugins() error {
	if !e.started {
		return errors.New("the plugins have not loaded yet")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
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
	return nil
}

// unregister removes the commands, operators, objects and mappings p
// added, so a reload that leaves p out leaves nothing of it.
func (e *Session) unregister(p api.Module) {
	for _, r := range p.Registered() {
		switch r.Kind {
		case api.Command:
			for _, name := range append([]string{r.Name}, r.Short...) {
				delete(e.commands, name)
				delete(e.completers, name)
				delete(e.shortOf, name)
				delete(e.pluginArgs, name)
				delete(e.pathArgs, name)
			}
			delete(e.pluginHelp, ":"+r.Name)
		case api.Source:
			delete(e.sources, r.Name)
			delete(e.pluginHelp, ":pick "+r.Name)
		case api.ClientCommand:
			delete(e.clientCommands, r.Name)
		case api.Operator, api.Object:
			e.pluginBindings = slices.DeleteFunc(e.pluginBindings, func(b vim.Binding) bool {
				return b.Kind == "operator" && b.Keys == bindingKeys(r) || b.Kind == "object" && b.Keys == r.Name
			})
		case api.Key, api.Plug:
			delete(e.pluginHelp, "<Plug>("+p.Name()+"."+r.Name+")")
			e.pluginBindings = slices.DeleteFunc(e.pluginBindings, func(b vim.Binding) bool { return r.Chars > 0 && b.Keys == bindingKeys(r) })
		case api.Typed:
			e.pluginBindings = slices.DeleteFunc(e.pluginBindings, func(b vim.Binding) bool { return b.Kind == "insert" && b.Keys == r.Name })
		case api.Write:
			delete(e.writers, r.Name)
		case api.Completer:
			delete(e.completionSources, r.Name)
		case api.Insert:
			delete(e.pluginHelp, "<Plug>("+p.Name()+".insert."+r.Name+")")
		}
		for _, s := range e.sessions {
			switch r.Kind {
			case api.Operator:
				s.vim.Operator(r.Name, 0, nil)
			case api.Object:
				s.vim.Object(r.Name, nil)
			case api.Key, api.Plug:
				if r.Chars > 0 {
					s.vim.Keyed(keyedMode(r), r.Name, 0, nil)
					break
				}
				for _, k := range keysOf(p, r) {
					s.mapKey(k, false)
				}
				s.vim.Command(p.Name()+"."+r.Name, nil)
			case api.Typed:
				s.vim.Keyed(vim.Insert, r.Name, 0, nil)
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
	if e.capture != nil && e.capture.p == p {
		e.capture = nil
	}
	e.modules = slices.DeleteFunc(e.modules, func(q api.Module) bool { return q == p })
	for _, d := range e.decos[p] {
		d.remove()
	}
	delete(e.decos, p)
	delete(e.segments, p)
	for name, d := range e.declared {
		if d.owner == p {
			delete(e.declared, name)
			delete(e.values, name)
		}
	}
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
		if e.pluginArgs == nil {
			e.pluginArgs = map[string]string{}
		}
		if e.pathArgs == nil {
			e.pathArgs = map[string]bool{}
		}
		for _, name := range append([]string{r.Name}, r.Short...) {
			e.pluginArgs[name], e.pathArgs[name] = r.Args, r.Paths
		}
		run := func(s *Session, c call) error {
			scope := s.scope(p, s.win.buf, s.Text())
			scope.About["command"], scope.About["args"], scope.About["bang"] = c.name, c.arg, strconv.FormatBool(c.force)
			scope.About["first"], scope.About["last"], scope.About["ranged"] = strconv.Itoa(c.first), strconv.Itoa(c.last), strconv.FormatBool(c.ranged)
			edits, _, err := s.run(p, r.ID, scope, s.Cursor(), 0)
			if err != nil || len(edits) == 0 {
				return err
			}
			if err := (host{s}).Apply(edits, ":"+r.Name); err != nil {
				return err
			}
			s.setCursor(edits[0].Start)
			return nil
		}
		complete := func(s *Session, arg string) {
			if r.Host && s.power != Host {
				return
			}
			scope := s.scope(p, s.win.buf, s.Text())
			scope.About["complete"] = arg
			s.run(p, r.ID, scope, s.Cursor(), 0)
		}
		for _, name := range append([]string{r.Name}, r.Short...) {
			e.commands[name] = run
			if r.Complete {
				e.completers[name] = complete
			}
		}
		for _, short := range r.Short {
			e.shortOf[short] = r.Name
		}
	case api.ClientCommand:
		e.clientCommands[r.Name] = func(s *Session, from *server, args string) {
			scope := s.scope(p, s.win.buf, s.Text())
			scope.About["arguments"], scope.About["server"] = args, from.name
			s.run(p, r.ID, scope, s.Cursor())
		}
	case api.Source:
		e.pluginHelp[":pick "+r.Name] = r.Help
		e.sources[r.Name] = func(s *Session, text string) {
			scope := s.scope(p, s.win.buf, s.Text())
			scope.About["args"] = text
			s.run(p, r.ID, scope, s.Cursor(), 0)
		}
	case api.Operator:
		e.pluginBindings = append(e.pluginBindings, vim.Binding{Kind: "operator", Keys: bindingKeys(r), Help: r.Help})
		e.each(func(s *Session) {
			s.vim.Operator(r.Name, r.Chars, func(t vim.Text, start, end int, lines bool, chars []string) []vim.Edit {
				return s.runChars(p, r, t, start, end, lines, chars)
			})
		})
	case api.Object:
		e.pluginBindings = append(e.pluginBindings, vim.Binding{Kind: "object", Keys: r.Name, Help: r.Help})
		e.each(func(s *Session) {
			s.vim.Object(r.Name, func(t vim.Text, start, end int) (int, int, bool) {
				_, res, err := s.run(p, r.ID, s.scope(p, s.win.buf, t), start, end)
				sel := res.Selected
				if err != nil || sel == nil {
					return 0, 0, false
				}
				return sel[0], sel[1], true
			})
		})
	case api.Write:
		e.writers[r.Name] = writer{p, r.ID}
	case api.Completer:
		e.completionSources[r.Name] = completer{p, r.ID, r.Keys}
	case api.Key, api.Plug:
		if r.Kind == api.Key && r.Chars > 0 {
			e.pluginBindings = append(e.pluginBindings, vim.Binding{Kind: modes[keyedMode(r)], Keys: bindingKeys(r), Help: r.Help})
			e.each(func(s *Session) {
				s.vim.Keyed(keyedMode(r), r.Name, r.Chars, func(t vim.Text, start, end int, lines bool, chars []string) []vim.Edit {
					return s.runChars(p, r, t, start, end, lines, chars)
				})
			})
			return
		}
		name := p.Name() + "." + r.Name
		e.pluginHelp["<Plug>("+name+")"] = r.Help
		e.each(func(s *Session) {
			s.vim.Command(name, func(t vim.Text, pos, count int) ([]vim.Edit, string) {
				if r.Enters && s.enter() {
					return nil, ""
				}
				args := []int{pos, count}
				if sel, ok := s.Selection(); r.Visual && ok {
					args = []int{sel.Start, min(sel.End, t.Len())}
				}
				edits, res, err := s.run(p, r.ID, s.scope(p, s.win.buf, t), args...)
				if r.Filetype != "" && err == nil && reflect.ValueOf(res).IsZero() {
					return nil, s.beneath(p, r)
				}
				return edits, ""
			})
			for _, k := range keysOf(p, r) {
				s.mapKey(k, true)
			}
		})
	case api.Typed:
		e.pluginBindings = append(e.pluginBindings, vim.Binding{Kind: "insert", Keys: r.Name, Help: r.Help})
		e.each(func(s *Session) {
			s.vim.Keyed(vim.Insert, r.Name, 0, func(t vim.Text, pos, _ int, _ bool, _ []string) []vim.Edit {
				edits, _, _ := s.run(p, r.ID, s.scope(p, s.win.buf, t), pos)
				return edits
			})
		})
	case api.Insert:
		name := p.Name() + ".insert." + r.Name
		e.pluginHelp["<Plug>("+name+")"] = r.Help
		e.each(func(s *Session) {
			s.vim.Command(name, func(t vim.Text, pos, _ int) ([]vim.Edit, string) {
				edits, _, _ := s.run(p, r.ID, s.scope(p, s.win.buf, t), pos)
				return edits, ""
			})
			s.vim.Map(vim.Insert, r.Name, "<Plug>("+name+")")
		})
	}
}

// runChars calls what p registered as r on the range from start to end,
// linewise or not, with the characters typed after its keys (B322).
func (s *Session) runChars(p api.Module, r api.Registration, t vim.Text, start, end int, lines bool, chars []string) []vim.Edit {
	linewise := 0
	if lines {
		linewise = 1
	}
	scope := s.scope(p, s.win.buf, t)
	if len(chars) > 0 {
		scope.About["chars"] = strings.Join(chars, "")
	}
	edits, _, _ := s.run(p, r.ID, scope, start, end, linewise)
	return edits
}

// keyedMode is the mode a Key with Chars is read in (B322).
func keyedMode(r api.Registration) vim.Mode {
	if r.Visual {
		return vim.Visual
	}
	return vim.Normal
}

// bindingKeys are r's keys as help shows them, with what follows them.
func bindingKeys(r api.Registration) string {
	keys := r.Name
	if r.Kind == api.Operator {
		keys += "{motion}"
	}
	return keys + strings.Repeat("{char}", r.Chars)
}

// scope is what a call of p about b reads: the text t, what the call is
// about, every open buffer (B188), b's syntax nodes (B189) and p's own
// decorations on b (B289).
func (e *Session) scope(p api.Module, b *buffer, t api.Text) api.Scope {
	bufs := map[int]api.Buffer{}
	for _, o := range e.bufs {
		bufs[o.id] = api.Buffer{Barred: e.barred(o) != nil, Path: o.file.path, Filetype: e.filetype(o), Name: o.name(), Modified: o.modified(), Kind: o.kind(), Unnumbered: !o.numbered(), Text: o.text.Snapshot()}
	}
	ft, opts, cfg := e.filetype(b), e.opts, &e.cfg
	if e.win.buf == b {
		opts = e.win.opts
	}
	s := api.Scope{Text: t, About: e.about(b), Buffers: bufs, Language: func(int) string { return ft }}
	if e.win.buf == b {
		// The text the current window shows, as start:end ranges.
		ranges, _ := e.view(e.win, e.win.buf.text.Snapshot(), e.Top(), e.tabWidth(b))
		var view []string
		for _, r := range ranges {
			view = append(view, fmt.Sprintf("%d:%d", r.Start, r.End))
		}
		s.About["view"] = strings.Join(view, " ")
	}
	s.About["columns"] = strconv.Itoa(e.width)
	s.Object = func(keys string, pos int) (int, int, bool) {
		obj := strings.TrimLeft(keys, "0123456789")
		count, _ := strconv.Atoi(keys[:len(keys)-len(obj)])
		return vim.TextObject(t, pos, count, obj)
	}
	// Only a call reads the registry and the session, on the loop, so run
	// sets these.
	s.Keys = func() []api.Binding { return nil }
	s.Commands = func() []api.ExCommand { return nil }
	s.HelpTags = func() []string { return nil }
	s.Session = func() string { return "" }
	s.Entries = func() []api.Entry { return nil }
	s.States = func(int) []api.State { return nil }
	s.Marks, s.Jumps, s.Changes = none, none, none
	s.Window = func(string) bool { return false }
	s.Sessions = func() []api.Peer { return nil }
	s.Grammars = func() []api.Grammar { return nil }
	s.HelpAt = func(string) (string, int, bool) { return "", 0, false }
	s.Messages = func() []string { return nil }
	s.Register = func(rune) (string, bool, error) { return "", false, nil }
	s.StateText = func(int, int) (string, bool) { return "", false }
	s.Registers = func() []api.RegisterText { return nil }
	s.Search = func() (string, bool) { return "", true }
	s.Recording = func() string { return "" }
	s.History = func(string) []string { return nil }
	servers := e.serverList()
	s.Servers = func() []api.Server { return servers }
	// The theme and center are as :theme and :center left them, not as
	// config.toml set them.
	theme, centered, values := e.theme, e.centered, maps.Clone(e.values)
	s.Option = func(name string) (string, bool) {
		switch name {
		case "theme":
			return theme, true
		case "center":
			return strconv.FormatBool(centered), true
		}
		if _, ok := values[name]; ok {
			return declaredValue(opts, values, name), true
		}
		return option(opts, cfg, name)
	}
	s.Config = func(path string) (string, bool) { return configNode(cfg, path) }
	if b.syntax != nil {
		s.Node, s.Context, s.Free = b.syntax.Nodes()
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
			s.Query = func(qs map[string]string) ([]api.Span, error) {
				spans, err := syntax.Query(ft, queries, snap.Slice(0, snap.Len()), qs)
				out := make([]api.Span, len(spans))
				for i, sp := range spans {
					out[i] = api.Span(sp)
				}
				return out, err
			}
		}
	}
	if s.Query == nil {
		s.Query = func(map[string]string) ([]api.Span, error) { return nil, nil }
	}
	language := s.Language
	s.Comment = func(pos int) (string, bool) { return syntax.Comment(language(pos)) }
	if b.title != "" {
		s.About["title"] = b.title
	}
	if b.listed != "" {
		s.About["explorer"] = b.listed
	}
	// The decorations are listed now, on the loop, as a module reads its
	// scope off it.
	var decos []api.Decoration
	for _, d := range e.decos[p] {
		decos = append(decos, api.Decoration{ID: d.id, Start: d.buf.text.Mark(d.start), End: d.buf.text.Mark(d.end), Style: d.style, Sign: d.sign, Note: d.note, Layer: d.layer, Fold: d.fold, Replace: d.replace, Insert: d.insert, Cover: d.cover, Icon: d.icon, Line: d.line, Lines: d.lines, Above: d.above, Block: d.block, Image: d.image, Buffer: d.buf.id})
	}
	s.Decorations = func() []api.Decoration { return decos }
	// The windows and the selection are listed now too, for the same
	// reason.
	var wins []api.Window
	for _, w := range e.windows() {
		alt := 0
		if w.alt != nil {
			alt = w.alt.id
		}
		wins = append(wins, api.Window{ID: w.id, Buffer: w.buf.id, Alt: alt, Cursor: w.pos(), Top: w.buf.text.Mark(w.top), Row: w.y, Column: w.x, Width: w.w, Height: w.h, Current: w == e.win, Float: w.anchor, Zoomed: w == e.zoomed})
		if w.buf == b && (w == e.win || s.About["cursor"] == "") {
			s.About["cursor"] = strconv.Itoa(w.pos())
		}
	}
	s.Windows = func() []api.Window { return wins }
	pages := e.scopePages()
	s.Pages = func() []api.TabPage { return pages }
	sel, selected := e.vim.Selection()
	s.Selection = func() (api.Selection, bool) {
		return api.Selection{Kind: string(sel.Kind), Start: sel.Start, End: sel.End, Block: sel.Block, Left: sel.Left, Right: sel.Right, Regions: sel.Regions}, selected
	}
	cursors := e.Cursors()
	s.Cursors = func() []int { return cursors }
	var proposals []api.Proposal
	for o, ds := range e.decos {
		if by, ok := o.(*Session); ok {
			for _, d := range ds {
				proposals = append(proposals, api.Proposal{ID: d.id, Buffer: d.buf.id, Start: d.buf.text.Mark(d.start), End: d.buf.text.Mark(d.end), Text: d.note, Stale: d.style == "proposal.stale", By: by.label()})
			}
		}
	}
	slices.SortFunc(proposals, func(x, y api.Proposal) int { return cmp.Compare(x.ID, y.ID) })
	s.Proposals = func() []api.Proposal { return proposals }
	last := e.lastList
	s.Places = func() (api.Places, bool) {
		if last == nil {
			return api.Places{}, false
		}
		return *last, true
	}
	return s
}

// about is what a call about b tells the plugin through context: the
// buffer's id, path and filetype, the mode (B176), its indent and tab
// widths (B280), the session by its id (B332), and fude's version (B349).
func (e *Session) about(b *buffer) api.Context {
	return api.Context{
		"buffer": strconv.Itoa(b.id), "path": b.file.path, "filetype": e.filetype(b), "mode": modes[e.vim.Mode()],
		"power": powers[e.power], "session": strconv.Itoa(e.id),
		"servers": strings.Join(serverNames(b.servers), ","),
		"indent":  strconv.Itoa(e.indentWidth(b)), "tab": strconv.Itoa(e.tabWidth(b)),
		"cwd": e.wd, "project": e.project, "version": e.version,
	}
}

// run calls what p registered as id, on the input path, against the current
// buffer, reading scope. It adds the decorations the call asks for, and
// returns its edits, sorted, and what an object's call selected. A call that
// fails unloads p, and calls into an unloaded module do nothing.
func (e *Session) run(p api.Module, id uint32, scope api.Scope, args ...int) ([]vim.Edit, api.Result, error) {
	defer scope.Done()
	// A guest may not call a plugin granted process, net, fs.read or keys (B246).
	if w, ok := p.(*plugin.Plugin); ok && e.power != Host && slices.ContainsFunc(w.Capabilities, func(c string) bool { return c == "process" || c == "net" || c == "fs.read" || c == "keys" }) {
		return nil, api.Result{}, e.fail(errors.New("a guest may not use plugin " + p.Name()))
	}
	if !slices.Contains(e.modules, p) {
		return nil, api.Result{}, e.fail(fmt.Errorf("plugin %s is unloaded", p.Name()))
	}
	name := strconv.Itoa(int(id))
	for _, r := range p.Registered() {
		if r.ID == id {
			switch {
			case r.Kind == api.Command:
				name = ":" + r.Name
			case r.Kind == api.Plug:
				name = "<Plug>(" + p.Name() + "." + r.Name + ")"
			default:
				name = r.Name
			}
			if r.Host && e.power != Host {
				return nil, api.Result{}, e.fail(errors.New("a guest may not run " + name))
			}
		}
	}
	// What the registry holds is read on the loop, as a call runs (B324).
	e.onLoop(&scope)
	at := time.Now()
	res, err := p.Call(context.Background(), id, scope, args...)
	trace.Since(kind(p), p.Name()+" ran "+name, at, err)
	switch {
	case errors.Is(err, api.ErrBusy):
		return nil, api.Result{}, e.fail(err)
	case err != nil:
		e.unload(p, err)
		return nil, api.Result{}, err
	}
	// A result that captures keys takes those typed next, and one without
	// it lets them go (B316).
	switch {
	case res.Capture:
		e.capture = &captured{p, id}
	case e.capture != nil && e.capture.p == p && e.capture.id == id:
		e.capture = nil
	}
	edits := e.decorate(p, e.win.buf, res)
	if res.Error != "" {
		return edits, res, errors.New(res.Error)
	}
	return edits, res, nil
}

// report shows err as the message, for a failure no caller takes back,
// such as one in a write that finishes later.
func (e *Session) report(err error) {
	if err != nil {
		e.message = err.Error()
	}
}

// fail shows err as the message and returns it, for a call that failed to
// tell the user and its caller both.
func (e *Session) fail(err error) error {
	e.message = err.Error()
	return err
}

// captured is the module call the keys typed next go to (B316).
type captured struct {
	p  api.Module
	id uint32
}

// key sends k to the module call that captured the keys, and applies what
// it asks, as the vim machine does after a key.
func (c *captured) key(e *Session, k vim.Key) bool {
	if c == nil {
		return false
	}
	scope := e.scope(c.p, e.win.buf, e.Text())
	scope.About["key"] = string(k)
	edits, _, err := e.run(c.p, c.id, scope, e.Cursor(), 1)
	if err != nil {
		// A call that failed, or a module gone, lets the keys go.
		e.capture = nil
	}
	if len(edits) > 0 {
		if err := (host{e}).Apply(edits, string(k)); err != nil {
			e.message = err.Error()
		}
	}
	e.settle(nil)
	return true
}

// logLine appends a line to fude's log, which is where a plugin's own log
// lines, the calls it is denied and language servers' stderr go (B50). A line that cannot be written
// is dropped, as the log is where it would have been reported.
func logLine(line string) {
	trace.Record("log", line, 0)
	dir, err := keep.Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
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

// moduleConfirm asks p's question c and runs its callback with the answer,
// or with no while another question shows (B363).
func (e *Session) moduleConfirm(p api.Module, b *buffer, c *api.Confirm, save *saving) {
	if save != nil {
		save.pending++
	}
	tell := func(yes bool) {
		if slices.Contains(e.modules, p) {
			e.listen(p).add(event{buf: b, id: c.ID, extra: api.Context{"yes": strconv.FormatBool(yes)}, save: save, from: e})
		}
	}
	if e.ask != nil {
		tell(false)
		return
	}
	e.confirm(c.Question, tell)
}

// streamBuf is the buffer a stream's callback is about, the current one.
// The finder's query is no buffer of the session, so while it shows the
// callback is about the buffer under it (B291).
func (e *Session) streamBuf() *buffer {
	if b := e.win.buf; slices.Contains(e.bufs, b) {
		return b
	}
	return e.under().buf
}

// showPick shows pk, a list or question of p about b, in the finder, or gives
// the live pick showing the items pk answers a query with (B185, B285).
func (e *Session) showPick(p api.Module, b *buffer, pk *api.Pick, save *saving) {
	f := e.finder
	if pk.Update {
		if f != nil && f.owner == p && f.pick == pk.ID {
			f.paths, f.stale = append([]string{}, pk.Items...), true
		}
		return
	}
	if f != nil && f.owner == p && f.pick == pk.ID && f.found != nil {
		if pk.Encoding != "" {
			f.enc = pk.Encoding
		}
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
	f = &finder{paths: pk.Items, stale: true, prompt: prompt, title: pk.Title, hint: pk.Hint, places: pk.Places, asFiles: pk.Files, enc: pk.Encoding, chose: answer, owner: p, pick: pk.ID, tell: tell}
	if pk.Live {
		f.lookup = func(q string, found func([]string, error)) {
			f.found = found
			tell(api.Context{"query": q, "all": strconv.FormatBool(f.all)})
		}
	}
	e.openFinder(f, pk.Query)
}

// moduleKey is a key a module maps to one of its plugs, in a [keymap]
// table: normal, visual or a kind of buffer's. An attached one maps only
// while the current buffer has a language server (B327).
type moduleKey struct {
	table, keys, plug string
	attached, bound   bool
	filetype          string
}

// keysOf are the keys r maps to p's plug, a Key's own or a Plug's Keys.
// They map in normal mode, in visual mode too with Visual, or among a
// kind's keys with In. A Key with Chars is the machine's own and has none.
func keysOf(p api.Module, r api.Registration) []moduleKey {
	keys := r.Keys
	switch {
	case r.Kind == api.Key && r.Chars == 0:
		keys = r.Name
	case r.Kind != api.Plug || keys == "":
		return nil
	}
	plug := "<Plug>(" + p.Name() + "." + r.Name + ")"
	if r.In != "" {
		return []moduleKey{{r.In, keys, plug, r.Attached, r.Bound, r.Filetype}}
	}
	out := []moduleKey{{"normal", keys, plug, r.Attached, r.Bound, r.Filetype}}
	if r.Visual {
		out = append(out, moduleKey{"visual", keys, plug, r.Attached, r.Bound, r.Filetype})
	}
	return out
}

// moduleKeys are the keys every module maps (B327).
func (e *Session) moduleKeys() []moduleKey {
	var out []moduleKey
	for _, p := range e.modules {
		for _, r := range p.Registered() {
			out = append(out, keysOf(p, r)...)
		}
	}
	return out
}

// mapKey maps k, or takes it away with on false or while it is off. A key
// the session's [keymap] table maps stays as the table has it (B327).
func (e *Session) mapKey(k moduleKey, on bool) {
	if e.configures(k.table, k.keys) {
		return
	}
	m, mode := e.table(k.table)
	keys := withLeader(k.keys, e.leader)
	if on && e.keyOn(k) {
		m.Map(mode, keys, k.plug)
		return
	}
	// Another module's key may map them in its place, as one that names no
	// filetype does once the filetype's key is off (B588). A module going
	// away maps none of its own.
	owner, _, _ := strings.Cut(k.plug, ".")
	for _, o := range e.moduleKeys() {
		if o != k && o.table == k.table && o.keys == k.keys && e.keyOn(o) && (on || !strings.HasPrefix(o.plug, owner+".")) {
			m.Map(mode, keys, o.plug)
			return
		}
	}
	m.Unmap(mode, keys)
}

// keyOn reports whether k is mapped now: an attached key while the buffer
// has a server, a bound key while the window is bound, and a filetype's key
// in a buffer of it, over a key that names none (B588).
func (e *Session) keyOn(k moduleKey) bool {
	if !e.holds(k) {
		return false
	}
	// A session joining has no window yet.
	ft := e.keyFiletype
	if e.win != nil {
		ft = e.filetype(e.win.buf)
	}
	if k.filetype != "" {
		return k.filetype == ft
	}
	return !slices.ContainsFunc(e.moduleKeys(), func(o moduleKey) bool {
		return o.filetype != "" && o.filetype == ft && o.table == k.table && o.keys == k.keys && e.holds(o)
	})
}

// holds reports whether k's server and window allow it: an attached key
// while the buffer has a server, and a bound key while the window is bound.
func (e *Session) holds(k moduleKey) bool {
	return (!k.attached || e.lspKeys) && (!k.bound || e.boundKeys)
}

// beneath returns what r's keys do without p's mapping of them in the
// filetype: the keys of a module that names no filetype, or else the keys
// themselves, which run through no mapping (B589).
func (e *Session) beneath(p api.Module, r api.Registration) string {
	table := "normal"
	if e.vim.Mode() == vim.Visual {
		table = "visual"
	}
	for _, k := range keysOf(p, r) {
		if k.table != table {
			continue
		}
		for _, o := range e.moduleKeys() {
			if o.filetype == "" && o.table == k.table && o.keys == k.keys && e.holds(o) {
				return o.plug
			}
		}
		return withLeader(k.keys, e.leader)
	}
	return ""
}

// none is no marks, as a scope off the loop has (B329).
func none() []api.Mark { return nil }
