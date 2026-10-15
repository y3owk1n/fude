package editor

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/plugin"
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
	for _, b := range e.bufs {
		e.attach(b)
	}
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
		b, t := e.win.buf, e.Text()
		go func() {
			ctx := context.Background()
			p, err := plugin.Load(ctx, path, plugin.Grants{Capabilities: s.Capabilities, Programs: s.Programs}, filepath.Join(cache, "fude", "wasm"), logLine)
			if err != nil {
				e.queue <- func() { e.message = err.Error() }
				return
			}
			res, err := p.Call(ctx, "init", t, plugin.InitBudget)
			e.queue <- func() { e.loaded(p, err, b, t, res) }
		}()
	}
}

// loaded keeps a plugin whose init returned, adds what it registered, and
// does what it asked for to b, whose text it saw as t. A plugin whose init
// failed is unloaded.
func (e *Session) loaded(p *plugin.Plugin, err error, b *buffer, t kernel.Snapshot, res plugin.Result) {
	if err != nil {
		e.unload(p, err)
		return
	}
	e.plugins = append(e.plugins, p)
	for _, r := range p.Registered {
		e.register(p, r)
	}
	if len(p.Subscribed) > 0 {
		e.listen(p)
	}
	if b.text.Snapshot().Version() != t.Version() {
		e.message = fmt.Sprintf("plugin %s: the text changed while it loaded", p.Name)
		return
	}
	e.result(p, b, t, res)
}

// result applies the decorations and edits a call into p asked for on b,
// which read t.
func (e *Session) result(p *plugin.Plugin, b *buffer, t kernel.Snapshot, res plugin.Result) {
	edits := e.decorate(p, b, res)
	if len(edits) == 0 {
		return
	}
	tx := kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: "plugin " + p.Name, Time: e.now()}
	for _, ed := range edits {
		tx.Edits = append(tx.Edits, kernel.Edit(ed))
	}
	if err := b.text.Apply(tx); err != nil {
		e.message = fmt.Sprintf("plugin %s: %v", p.Name, err)
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
	note       string          // a diagnostic's message
	raw        json.RawMessage // a diagnostic as the server sent it, for code actions
}

// remove deletes the decoration's marks.
func (d decoration) remove() {
	d.buf.text.DeleteMark(d.start)
	d.buf.text.DeleteMark(d.end)
}

// decorate adds and removes the decorations res asks for on b, and emits
// its events about b. It returns res's edits, sorted, for the caller to
// apply next, so they move the new decorations.
func (e *Session) decorate(p *plugin.Plugin, b *buffer, res plugin.Result) []vim.Edit {
	for _, name := range res.Emit {
		e.emit(name, "", b)
	}
	if res.Float != nil {
		e.float, e.floatSpans = e.markdown(*res.Float)
	}
	if res.Status != nil {
		if e.segments[p] == nil {
			e.segments[p] = map[*buffer]string{}
		}
		e.segments[p][b] = *res.Status
	}
	for _, d := range res.Decorate {
		e.decos[p] = append(e.decos[p], decoration{d.ID, b, b.text.SetMark(d.Start, kernel.Right), b.text.SetMark(d.End, kernel.Left), d.Style, "", nil})
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
func (e *Session) unload(p *plugin.Plugin, err error) {
	if l := e.listeners[p]; l != nil {
		close(l.done)
		delete(e.listeners, p)
	}
	p.Close(context.Background())
	e.plugins = slices.DeleteFunc(e.plugins, func(q *plugin.Plugin) bool { return q == p })
	for _, d := range e.decos[p] {
		d.remove()
	}
	delete(e.decos, p)
	delete(e.segments, p)
	e.message = fmt.Sprintf("%v, so it was unloaded", err)
	logLine(e.message)
}

// register adds a command, operator, object or key a plugin registered, as
// the machine's own, so it composes with every built-in (B52, B54).
func (e *Session) register(p *plugin.Plugin, r plugin.Registration) {
	switch r.Kind {
	case plugin.Command:
		e.commands[r.Name] = func(s *Session) {
			if edits, _, ok := s.run(p, r.Export, s.Text(), uint64(s.Cursor()), 0); ok && len(edits) > 0 {
				if err := (host{s}).Apply(edits, ":"+r.Name); err != nil {
					s.message = err.Error()
					return
				}
				s.setCursor(edits[0].Start)
			}
		}
	case plugin.Operator:
		e.each(func(s *Session) {
			s.vim.Operator(r.Name, func(t vim.Text, start, end int, lines bool) []vim.Edit {
				linewise := uint64(0)
				if lines {
					linewise = 1
				}
				edits, _, _ := s.run(p, r.Export, t, uint64(start), uint64(end), linewise)
				return edits
			})
		})
	case plugin.Object:
		e.each(func(s *Session) {
			s.vim.Object(r.Name, func(t vim.Text, start, end int) (int, int, bool) {
				_, out, ok := s.run(p, r.Export, t, uint64(start), uint64(end))
				if !ok || len(out) == 0 || int64(out[0]) < 0 {
					return 0, 0, false
				}
				return int(out[0] >> 32), int(uint32(out[0])), true
			})
		})
	case plugin.Key:
		name := p.Name + "." + r.Export
		e.each(func(s *Session) {
			s.vim.Command(name, func(t vim.Text, pos, count int) []vim.Edit {
				edits, _, _ := s.run(p, r.Export, t, uint64(pos), uint64(count))
				return edits
			})
			s.vim.Map(vim.Normal, r.Name, "<Plug>("+name+")")
		})
	}
}

// run calls export of p on the input path, under its budget, against the
// current buffer. It adds the decorations the call asks for, and returns
// its edits, sorted, and what it returned. A call that fails unloads p, and
// calls into an unloaded plugin do nothing.
func (e *Session) run(p *plugin.Plugin, export string, t vim.Text, args ...uint64) ([]vim.Edit, []uint64, bool) {
	if !slices.Contains(e.plugins, p) {
		e.message = fmt.Sprintf("plugin %s is unloaded", p.Name)
		return nil, nil, false
	}
	res, err := p.CallNow(context.Background(), export, t, plugin.InputBudget, args...)
	switch {
	case errors.Is(err, plugin.ErrBusy):
		e.message = err.Error()
		return nil, nil, false
	case err != nil:
		e.unload(p, err)
		return nil, nil, false
	}
	return e.decorate(p, e.win.buf, res), res.Out, true
}

// logLine appends a line to fude's log, which is where a plugin's own log
// lines, the calls it is denied and language servers' stderr go (B50). A line that cannot be written
// is dropped, as the log is where it would have been reported.
func logLine(line string) {
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
