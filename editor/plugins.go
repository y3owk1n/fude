package editor

import (
	"cmp"
	"context"
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
// none delays a frame (B56). Each plugin's init reads the current buffer, and
// its results reach the editor loop through Queue.
func (e *Editor) Load() {
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
			p, err := plugin.Load(ctx, path, s.Capabilities, filepath.Join(cache, "fude", "wasm"), logPlugin)
			if err != nil {
				e.queue <- func() { e.message = err.Error() }
				return
			}
			edits, _, err := p.Call(ctx, "init", t, plugin.InitBudget)
			e.queue <- func() { e.loaded(p, err, b, t, edits) }
		}()
	}
}

// loaded keeps a plugin whose init returned, adds what it registered, and
// applies the edits it asked for to b, whose text it saw as t. A plugin
// whose init failed is unloaded.
func (e *Editor) loaded(p *plugin.Plugin, err error, b *buffer, t kernel.Snapshot, edits []api.Edit) {
	if err != nil {
		e.unload(p, err)
		return
	}
	e.plugins = append(e.plugins, p)
	for _, r := range p.Registered {
		e.register(p, r)
	}
	if len(edits) == 0 {
		return
	}
	slices.SortStableFunc(edits, func(x, y api.Edit) int { return cmp.Compare(x.Start, y.Start) })
	tx := kernel.Transaction{Base: t.Version(), Origin: "user", Label: "plugin " + p.Name, Time: e.now()}
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

// unload closes p after err and tells the user.
func (e *Editor) unload(p *plugin.Plugin, err error) {
	p.Close(context.Background())
	e.plugins = slices.DeleteFunc(e.plugins, func(q *plugin.Plugin) bool { return q == p })
	e.message = fmt.Sprintf("%v, so it was unloaded", err)
	logPlugin(e.message)
}

// register adds a command, operator, object or key a plugin registered, as
// the machine's own, so it composes with every built-in (B52, B54).
func (e *Editor) register(p *plugin.Plugin, r plugin.Registration) {
	switch r.Kind {
	case plugin.Command:
		e.commands[r.Name] = func() {
			if edits, _, ok := e.run(p, r.Export, e.Text(), uint64(e.Cursor()), 0); ok && len(edits) > 0 {
				if err := (host{e}).Apply(edits, ":"+r.Name); err != nil {
					e.message = err.Error()
					return
				}
				e.setCursor(edits[0].Start)
			}
		}
	case plugin.Operator:
		e.vim.Operator(r.Name, func(t vim.Text, start, end int, lines bool) []vim.Edit {
			linewise := uint64(0)
			if lines {
				linewise = 1
			}
			edits, _, _ := e.run(p, r.Export, t, uint64(start), uint64(end), linewise)
			return edits
		})
	case plugin.Object:
		e.vim.Object(r.Name, func(t vim.Text, start, end int) (int, int, bool) {
			_, out, ok := e.run(p, r.Export, t, uint64(start), uint64(end))
			if !ok || len(out) == 0 || int64(out[0]) < 0 {
				return 0, 0, false
			}
			return int(out[0] >> 32), int(uint32(out[0])), true
		})
	case plugin.Key:
		name := p.Name + "." + r.Export
		e.vim.Command(name, func(t vim.Text, pos, count int) []vim.Edit {
			edits, _, _ := e.run(p, r.Export, t, uint64(pos), uint64(count))
			return edits
		})
		e.vim.Map(vim.Normal, r.Name, "<Plug>("+name+")")
	}
}

// run calls export of p on the input path, under its budget, and returns
// the edits it asks for, sorted, and what it returned. A call that fails
// unloads p, and calls into an unloaded plugin do nothing.
func (e *Editor) run(p *plugin.Plugin, export string, t vim.Text, args ...uint64) ([]vim.Edit, []uint64, bool) {
	if !slices.Contains(e.plugins, p) {
		e.message = fmt.Sprintf("plugin %s is unloaded", p.Name)
		return nil, nil, false
	}
	edits, out, err := p.Call(context.Background(), export, t, plugin.InputBudget, args...)
	if err != nil {
		e.unload(p, err)
		return nil, nil, false
	}
	slices.SortStableFunc(edits, func(x, y api.Edit) int { return cmp.Compare(x.Start, y.Start) })
	converted := make([]vim.Edit, len(edits))
	for i, ed := range edits {
		converted[i] = vim.Edit(ed)
	}
	return converted, out, true
}

// logPlugin appends a line to fude's log, which is where a plugin's own log
// lines and the calls it is denied go (B50). A line that cannot be written
// is dropped, as the log is where it would have been reported.
func logPlugin(line string) {
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
