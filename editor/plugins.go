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
			edits, err := p.Call(ctx, "init", t, plugin.InitBudget)
			e.queue <- func() { e.loaded(p, err, b, t, edits) }
		}()
	}
}

// loaded keeps a plugin whose init returned, and applies the edits it asked
// for to b, whose text it saw as t. A plugin whose init failed is unloaded.
func (e *Editor) loaded(p *plugin.Plugin, err error, b *buffer, t kernel.Snapshot, edits []api.Edit) {
	if err != nil {
		p.Close(context.Background())
		e.message = fmt.Sprintf("%v, so it was unloaded", err)
		logPlugin(e.message)
		return
	}
	e.plugins = append(e.plugins, p)
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
