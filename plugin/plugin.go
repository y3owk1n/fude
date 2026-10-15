// Package plugin runs wasm plugins on wazero, each with the capabilities it
// declares and under a budget for every call.
package plugin

import (
	"cmp"
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

// Budgets are how long one call into a plugin may run.
const (
	InitBudget  = 200 * time.Millisecond
	InputBudget = 16 * time.Millisecond  // an operator, object or command, one frame
	EventBudget = 100 * time.Millisecond // on_event, off the input path
)

// Kind is what a plugin registers.
type Kind int32

// What a plugin registers during init, named as its register call names it.
const (
	Command  Kind = iota // an ex command, exported as func(cursor, count)
	Operator             // exported as func(start, end, linewise)
	Object               // exported as func(start, end) i64, start<<32|end or negative for none
	Key                  // keys in normal mode, exported as func(cursor, count)
)

// Decoration is a style a plugin puts on [Start, End) of the text, such as a
// highlight name. ID names it to the plugin.
type Decoration struct {
	ID         int
	Start, End int
	Style      string
}

// Result is what one call into a plugin asked for, and what it returned.
type Result struct {
	Edits      []fude.Edit
	Decorate   []Decoration
	Undecorate []int    // the ids of decorations to remove
	Emit       []string // the events to emit, named plugin.event
	Status     *string  // the plugin's statusline segment for the text it read, "" for none
	Float      *string  // markdown to show at the cursor, "" to close it
	Out        []uint64
}

// Registration is one thing a plugin's init registered, run by its export.
type Registration struct {
	Kind         Kind
	Name, Export string
}

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

// ErrBusy is what a call on the input path returns while another call
// runs, such as an event handler. The input path never waits for one.
var ErrBusy = errors.New("is busy")

// Plugin is one loaded wasm plugin. Calls into it take turns.
type Plugin struct {
	Name       string
	Registered []Registration // what its init registered
	Subscribed []string       // the events its init subscribed to
	caps       []string
	log        func(string)

	mu      sync.Mutex
	next    int // the id the next decoration gets
	cache   wazero.CompilationCache
	runtime wazero.Runtime
	mod     api.Module
	call    *call // the call running now
}

// call holds the text one call into a plugin reads and what it asks for.
type call struct {
	name string
	text fude.Text
	Result
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

// Call runs the plugin's export name with args under budget, reading t, and
// returns what it asked for and returned. It waits for a call already
// running. A call that traps or runs past its budget returns an error, and
// nothing it asked for happens.
func (p *Plugin) Call(ctx context.Context, name string, t fude.Text, budget time.Duration, args ...uint64) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.run(ctx, name, t, budget, args...)
}

// CallNow is Call for the input path. It returns ErrBusy instead of waiting
// for a call already running.
func (p *Plugin) CallNow(ctx context.Context, name string, t fude.Text, budget time.Duration, args ...uint64) (Result, error) {
	if !p.mu.TryLock() {
		return Result{}, fmt.Errorf("plugin %s %w", p.Name, ErrBusy)
	}
	defer p.mu.Unlock()
	return p.run(ctx, name, t, budget, args...)
}

// Event runs the plugin's on_event with the event's name, written into its
// memory, reading t, under EventBudget (B57).
func (p *Plugin) Event(ctx context.Context, name string, t fude.Text) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, EventBudget)
	defer cancel()
	out, err := p.mod.ExportedFunction("alloc").Call(ctx, uint64(len(name)))
	if err != nil || len(out) == 0 || !p.mod.Memory().WriteString(uint32(out[0]), name) {
		return Result{}, fmt.Errorf("plugin %s: write the event's name: %w", p.Name, cmp.Or(err, errors.New("no memory for it")))
	}
	return p.run(ctx, "on_event", t, EventBudget, out[0], uint64(len(name)))
}

// run makes one call while p.mu is held.
func (p *Plugin) run(ctx context.Context, name string, t fude.Text, budget time.Duration, args ...uint64) (Result, error) {
	f := p.mod.ExportedFunction(name)
	if f == nil {
		return Result{}, fmt.Errorf("plugin %s has no %s", p.Name, name)
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	p.call = &call{name: name, text: t}
	defer func() { p.call = nil }()
	out, err := f.Call(ctx, args...)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Result{}, fmt.Errorf("plugin %s %w", p.Name, errBudget)
	case err != nil:
		return Result{}, fmt.Errorf("plugin %s: %w", p.Name, err)
	}
	p.call.Out = out
	return p.call.Result, nil
}

// host adds the functions a plugin imports from "fude".
func (p *Plugin) host(ctx context.Context) error {
	_, err := p.runtime.NewHostModuleBuilder("fude").
		NewFunctionBuilder().WithFunc(p.logText).Export("log").
		NewFunctionBuilder().WithFunc(p.textLen).Export("text_len").
		NewFunctionBuilder().WithFunc(p.text).Export("text").
		NewFunctionBuilder().WithFunc(p.edit).Export("edit").
		NewFunctionBuilder().WithFunc(p.register).Export("register").
		NewFunctionBuilder().WithFunc(p.subscribe).Export("subscribe").
		NewFunctionBuilder().WithFunc(p.emit).Export("emit").
		NewFunctionBuilder().WithFunc(p.status).Export("status").
		NewFunctionBuilder().WithFunc(p.float).Export("float").
		NewFunctionBuilder().WithFunc(p.decorate).Export("decorate").
		NewFunctionBuilder().WithFunc(p.undecorate).Export("undecorate").
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

// register records, during init, that the export at (eptr, en) runs the
// command, operator, object or key of kind named at (nptr, nn).
func (p *Plugin) register(_ context.Context, m api.Module, kind, nptr, nn, eptr, en uint32) int32 {
	name, ok1 := m.Memory().Read(nptr, nn)
	export, ok2 := m.Memory().Read(eptr, en)
	switch {
	case p.call == nil || p.call.name != "init" || Kind(kind) > Key:
		return errDenied
	case !ok1 || !ok2:
		return errRange
	}
	p.Registered = append(p.Registered, Registration{Kind(kind), string(name), string(export)})
	return 0
}

// subscribe records, during init, that the plugin hears the event named at
// (ptr, n) through on_event.
func (p *Plugin) subscribe(_ context.Context, m api.Module, ptr, n uint32) int32 {
	name, ok := m.Memory().Read(ptr, n)
	switch {
	case p.call == nil || p.call.name != "init":
		return errDenied
	case !ok:
		return errRange
	}
	p.Subscribed = append(p.Subscribed, string(name))
	return 0
}

// emit asks to emit the event named at (ptr, n), under the plugin's name.
func (p *Plugin) emit(_ context.Context, m api.Module, ptr, n uint32) int32 {
	name, ok := m.Memory().Read(ptr, n)
	switch {
	case p.call == nil:
		return errDenied
	case !ok:
		return errRange
	}
	p.call.Emit = append(p.call.Emit, p.Name+"."+string(name))
	return 0
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
	p.call.Edits = append(p.call.Edits, fude.Edit{Start: int(start), End: int(end), Text: string(b)})
	return 0
}

// decorate asks to put the style at (ptr, n) on [start, end), and returns
// the decoration's id.
func (p *Plugin) decorate(_ context.Context, m api.Module, start, end, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("ui", "decorate") {
		return errDenied
	}
	style, ok := m.Memory().Read(ptr, n)
	if start > end || int(end) > p.call.text.Len() || !ok {
		return errRange
	}
	p.next++
	p.call.Decorate = append(p.call.Decorate, Decoration{p.next, int(start), int(end), string(style)})
	return int32(p.next)
}

// status asks to show the text at (ptr, n) as the plugin's statusline
// segment for the buffer the call read, or none for "" (B58).
func (p *Plugin) status(_ context.Context, m api.Module, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("ui", "status") {
		return errDenied
	}
	text, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	s := string(text)
	p.call.Status = &s
	return 0
}

// float asks to show the markdown at (ptr, n) in a float at the cursor, or
// to close it for "" (B59).
func (p *Plugin) float(_ context.Context, m api.Module, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("ui", "float") {
		return errDenied
	}
	text, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	s := string(text)
	p.call.Float = &s
	return 0
}

// undecorate asks to remove the decoration id.
func (p *Plugin) undecorate(id uint32) int32 {
	if p.call == nil || !p.allowed("ui", "undecorate") {
		return errDenied
	}
	p.call.Undecorate = append(p.call.Undecorate, int(id))
	return 0
}
