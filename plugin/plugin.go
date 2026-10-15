// Package plugin runs wasm plugins on wazero, each with the capabilities it
// declares and under a budget for every call.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	fude "github.com/y3owk1n/fude/api"
)

// InitBudget is how long a plugin's init may run.
const InitBudget = 200 * time.Millisecond

// memoryPages is the most memory a plugin may grow to, 64MB.
const memoryPages = 1024

// capabilities are the ones a plugin may declare. fs.read, process and net
// grant nothing yet.
var capabilities = []string{"buffer.read", "buffer.write", "ui", "fs.read", "process", "net"}

// Errors a host function returns to the plugin.
const (
	errDenied = -1 // the plugin did not declare the capability, or no call runs
	errRange  = -2 // a range outside the text
	errMemory = -3 // the plugin's memory could not take the result
)

// Plugin is one loaded wasm plugin. Calls into it take turns.
type Plugin struct {
	Name string
	caps []string
	log  func(string)

	mu      sync.Mutex
	cache   wazero.CompilationCache
	runtime wazero.Runtime
	mod     api.Module
	call    *call // the call running now
}

// call holds the text one call into a plugin reads and the edits it asks
// for.
type call struct {
	text  fude.Text
	edits []fude.Edit
}

// errBudget is a call that ran past its budget. The plugin is closed.
var errBudget = errors.New("ran past its budget")

// Load compiles the plugin at path and instantiates it with caps. Compiled
// code is kept in cacheDir between runs. log takes what the plugin logs and
// the calls it was denied.
func Load(ctx context.Context, path string, caps []string, cacheDir string, log func(string)) (*Plugin, error) {
	for _, c := range caps {
		if !slices.Contains(capabilities, c) {
			return nil, fmt.Errorf("plugin %s: no capability %q", path, c)
		}
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load plugin: %w", err)
	}
	p := &Plugin{Name: strings.TrimSuffix(filepath.Base(path), ".wasm"), caps: caps, log: log}
	if p.cache, err = wazero.NewCompilationCacheWithDir(cacheDir); err != nil {
		return nil, fmt.Errorf("plugin cache: %w", err)
	}
	p.runtime = wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCompilationCache(p.cache).WithMemoryLimitPages(memoryPages).WithCloseOnContextDone(true))
	if err := p.host(ctx); err != nil {
		p.Close(ctx)
		return nil, err
	}
	if p.mod, err = p.runtime.Instantiate(ctx, wasm); err != nil {
		p.Close(ctx)
		return nil, fmt.Errorf("plugin %s: %w", p.Name, err)
	}
	return p, nil
}

// Close frees the plugin.
func (p *Plugin) Close(ctx context.Context) error {
	return errors.Join(p.runtime.Close(ctx), p.cache.Close(ctx))
}

// Call runs the plugin's export name under budget, reading t, and returns
// the edits it asked for. A call that traps or runs past its budget returns
// an error, and its edits are dropped.
func (p *Plugin) Call(ctx context.Context, name string, t fude.Text, budget time.Duration) ([]fude.Edit, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.mod.ExportedFunction(name)
	if f == nil {
		return nil, fmt.Errorf("plugin %s has no %s", p.Name, name)
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	p.call = &call{text: t}
	defer func() { p.call = nil }()
	_, err := f.Call(ctx)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return nil, fmt.Errorf("plugin %s %w", p.Name, errBudget)
	case err != nil:
		return nil, fmt.Errorf("plugin %s: %w", p.Name, err)
	}
	return p.call.edits, nil
}

// host adds the functions a plugin imports from "fude".
func (p *Plugin) host(ctx context.Context) error {
	_, err := p.runtime.NewHostModuleBuilder("fude").
		NewFunctionBuilder().WithFunc(p.logText).Export("log").
		NewFunctionBuilder().WithFunc(p.textLen).Export("text_len").
		NewFunctionBuilder().WithFunc(p.text).Export("text").
		NewFunctionBuilder().WithFunc(p.edit).Export("edit").
		Instantiate(ctx)
	if err != nil {
		return fmt.Errorf("plugin %s: %w", p.Name, err)
	}
	return nil
}

// allowed reports whether the plugin declared capability c, and logs the
// call it denies.
func (p *Plugin) allowed(c, fn string) bool {
	if slices.Contains(p.caps, c) {
		return true
	}
	p.log(fmt.Sprintf("%s: %s needs %s, which it did not declare", p.Name, fn, c))
	return false
}

func (p *Plugin) logText(_ context.Context, m api.Module, ptr, n uint32) {
	if b, ok := m.Memory().Read(ptr, n); ok {
		p.log(p.Name + ": " + string(b))
	}
}

func (p *Plugin) textLen() int32 {
	if p.call == nil || !p.allowed("buffer.read", "text_len") {
		return errDenied
	}
	return int32(p.call.text.Len())
}

// text writes [start, end) of the text into memory the plugin allocates,
// and returns where, as ptr<<32|len.
func (p *Plugin) text(ctx context.Context, m api.Module, start, end uint32) int64 {
	if p.call == nil || !p.allowed("buffer.read", "text") {
		return errDenied
	}
	t := p.call.text
	if start > end || int(end) > t.Len() {
		return errRange
	}
	s := t.Slice(int(start), int(end))
	out, err := m.ExportedFunction("alloc").Call(ctx, uint64(len(s)))
	if err != nil || len(out) == 0 || !m.Memory().WriteString(uint32(out[0]), s) {
		return errMemory
	}
	return int64(out[0])<<32 | int64(len(s))
}

// edit asks to replace [start, end) with the text at ptr. The edits a call
// asks for apply together once it returns.
func (p *Plugin) edit(_ context.Context, m api.Module, start, end, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("buffer.write", "edit") {
		return errDenied
	}
	t := p.call.text
	if start > end || int(end) > t.Len() {
		return errRange
	}
	b, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	p.call.edits = append(p.call.edits, fude.Edit{Start: int(start), End: int(end), Text: string(b)})
	return 0
}
