// Package plugin runs wasm plugins on wazero, each with the capabilities it
// declares and under a budget for every call.
package plugin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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

// What a plugin registers during init. fude runs each through the plugin's
// export call(id, a, b, c) i64, with the id it registered and these
// arguments.
const (
	Command  Kind = iota // an ex command, called with (cursor, count)
	Operator             // called with (start, end, linewise)
	Object               // called with (start, end), returns start<<32|end or negative for none
	Key                  // keys in normal mode, called with (cursor, count)
	Insert               // keys in insert mode, called with (cursor, 0)
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

// Registration is one thing a plugin's init registered: its name, what it
// does, and the id call runs it by.
type Registration struct {
	Kind       Kind
	Name, Help string
	ID         uint32
}

// memoryPages is the most memory a plugin may grow to, 64MB.
const memoryPages = 1024

// capabilities are the ones a plugin may declare.
var capabilities = []string{"buffer.read", "buffer.write", "ui", "fs.read", "process", "net"}

// Errors a host function returns to the plugin.
const (
	errDenied = -1 // the plugin did not declare the capability, or no call runs
	errRange  = -2 // a range outside the text
	errMemory = -3 // the plugin's memory could not take the result
	errFile   = -4 // a file outside the project, or one that cannot be read
	errRun    = -5 // a program the plugin did not declare, or one that failed
	errNet    = -6 // a host the plugin did not declare, or a request that failed
)

// ErrBusy is what a call on the input path returns while another call
// runs, such as an event handler. The input path never waits for one.
var ErrBusy = errors.New("is busy")

// Plugin is one loaded wasm plugin. Calls into it take turns.
type Plugin struct {
	Name       string
	Registered []Registration // what its init registered
	Subscribed []string       // the events its init subscribed to
	Grants
	log  func(string)
	root string // the project: the directory fude started in

	mu      sync.Mutex
	next    int // the id the next decoration gets
	cache   wazero.CompilationCache
	runtime wazero.Runtime
	mod     api.Module
	call    *call // the call running now
}

// call holds the text one call into a plugin reads, what the call is
// about, and what it asks for.
type call struct {
	name  string
	text  fude.Text
	about Context
	Result
}

// Context is what a call is about, by name, which the plugin reads through
// context. Every call has buffer, path and filetype, of the buffer it is
// about, and mode. In a command's call, args is the text typed after its
// name, and bang is true when ! followed the name. first and last are where
// its range's first and last lines start, and ranged is true when it was
// given a range.
type Context map[string]string

// errBudget is a call that ran past its budget. The plugin is closed.
var errBudget = errors.New("ran past its budget")

// Grants are what config.toml lets a plugin do: its capabilities, the
// programs process may run and the hosts net may reach. Settings are what
// it reads through setting, by name.
type Grants struct {
	Capabilities []string
	Programs     []string
	Hosts        []string
	Settings     map[string]string
}

// Load compiles the plugin at path and instantiates it with g. Compiled code
// is kept in cacheDir between runs. log takes what the plugin logs and the
// calls it was denied.
func Load(ctx context.Context, path string, g Grants, cacheDir string, log func(string)) (*Plugin, error) {
	for _, c := range g.Capabilities {
		if !slices.Contains(capabilities, c) {
			return nil, fmt.Errorf("plugin %s: no capability %q", path, c)
		}
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load plugin: %w", err)
	}
	root, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("load plugin: %w", err)
	}
	p := &Plugin{Name: strings.TrimSuffix(filepath.Base(path), ".wasm"), Grants: g, log: log, root: root}
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

// Init runs the plugin's _initialize, where a compiler puts its program's
// setup, then its init, each when it exports one, together under
// InitBudget. Both may register and subscribe.
func (p *Plugin) Init(ctx context.Context, t fude.Text, about Context) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, InitBudget)
	defer cancel()
	p.call = &call{name: "init", text: t, about: about}
	defer func() { p.call = nil }()
	for _, name := range []string{"_initialize", "init"} {
		f := p.mod.ExportedFunction(name)
		if f == nil {
			continue
		}
		_, err := f.Call(ctx)
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return Result{}, fmt.Errorf("plugin %s %w", p.Name, errBudget)
		case err != nil:
			return Result{}, fmt.Errorf("plugin %s: %s: %w", p.Name, name, err)
		}
	}
	return p.call.Result, nil
}

// Close frees the plugin.
func (p *Plugin) Close(ctx context.Context) error {
	return errors.Join(p.runtime.Close(ctx), p.cache.Close(ctx))
}

// CallNow runs the plugin's export name with args under budget, on the
// input path, reading t, about what about says. It returns what the call
// asked for and returned, or ErrBusy instead of waiting for a call already
// running. A call that traps or runs past its budget returns an error, and
// nothing it asked for happens.
func (p *Plugin) CallNow(ctx context.Context, name string, t fude.Text, about Context, budget time.Duration, args ...uint64) (Result, error) {
	if !p.mu.TryLock() {
		return Result{}, fmt.Errorf("plugin %s %w", p.Name, ErrBusy)
	}
	defer p.mu.Unlock()
	return p.run(ctx, name, t, about, budget, args...)
}

// Event runs the plugin's on_event with the event's name, written into its
// memory, reading t, under EventBudget (B57).
func (p *Plugin) Event(ctx context.Context, name string, t fude.Text, about Context) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, EventBudget)
	defer cancel()
	out, err := p.mod.ExportedFunction("alloc").Call(ctx, uint64(len(name)))
	if err != nil || len(out) == 0 || !p.mod.Memory().WriteString(uint32(out[0]), name) {
		return Result{}, fmt.Errorf("plugin %s: write the event's name: %w", p.Name, cmp.Or(err, errors.New("no memory for it")))
	}
	return p.run(ctx, "on_event", t, about, EventBudget, out[0], uint64(len(name)))
}

// run makes one call while p.mu is held.
func (p *Plugin) run(ctx context.Context, name string, t fude.Text, about Context, budget time.Duration, args ...uint64) (Result, error) {
	f := p.mod.ExportedFunction(name)
	if f == nil {
		return Result{}, fmt.Errorf("plugin %s has no %s", p.Name, name)
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	p.call = &call{name: name, text: t, about: about}
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
		NewFunctionBuilder().WithFunc(p.context).Export("context").
		NewFunctionBuilder().WithFunc(p.setting).Export("setting").
		NewFunctionBuilder().WithFunc(p.textLen).Export("text_len").
		NewFunctionBuilder().WithFunc(p.lines).Export("lines").
		NewFunctionBuilder().WithFunc(p.lineOf).Export("line_of").
		NewFunctionBuilder().WithFunc(p.lineStart).Export("line_start").
		NewFunctionBuilder().WithFunc(p.text).Export("text").
		NewFunctionBuilder().WithFunc(p.edit).Export("edit").
		NewFunctionBuilder().WithFunc(p.register).Export("register").
		NewFunctionBuilder().WithFunc(p.subscribe).Export("subscribe").
		NewFunctionBuilder().WithFunc(p.emit).Export("emit").
		NewFunctionBuilder().WithFunc(p.status).Export("status").
		NewFunctionBuilder().WithFunc(p.float).Export("float").
		NewFunctionBuilder().WithFunc(p.readFile).Export("read_file").
		NewFunctionBuilder().WithFunc(p.runProgram).Export("run").
		NewFunctionBuilder().WithFunc(p.httpGet).Export("http_get").
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
	if slices.Contains(p.Capabilities, c) {
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

// context writes the value named at (ptr, n) of what the call is about into
// memory the plugin allocates, or returns errRange for a name it has none
// of.
func (p *Plugin) context(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil {
		return errDenied
	}
	name, ok := m.Memory().Read(ptr, n)
	v, has := p.call.about[string(name)]
	if !ok || !has {
		return errRange
	}
	return give(ctx, m, v)
}

// setting writes the value of the plugin's setting named at (ptr, n) into
// memory the plugin allocates, or returns errRange for one it has none of
// (B178).
func (p *Plugin) setting(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil {
		return errDenied
	}
	name, ok := m.Memory().Read(ptr, n)
	v, has := p.Settings[string(name)]
	if !ok || !has {
		return errRange
	}
	return give(ctx, m, v)
}

func (p *Plugin) textLen() int32 {
	if p.call == nil || !p.allowed("buffer.read", "text_len") {
		return errDenied
	}
	return int32(p.call.text.Len())
}

// lines returns how many lines the text has.
func (p *Plugin) lines() int32 {
	if p.call == nil || !p.allowed("buffer.read", "lines") {
		return errDenied
	}
	return int32(p.call.text.Lines())
}

// lineOf returns the line that holds pos, from 0.
func (p *Plugin) lineOf(pos uint32) int32 {
	if p.call == nil || !p.allowed("buffer.read", "line_of") {
		return errDenied
	}
	if int(pos) > p.call.text.Len() {
		return errRange
	}
	return int32(p.call.text.LineOf(int(pos)))
}

// lineStart returns where line, from 0, starts.
func (p *Plugin) lineStart(line uint32) int32 {
	if p.call == nil || !p.allowed("buffer.read", "line_start") {
		return errDenied
	}
	if int(line) >= p.call.text.Lines() {
		return errRange
	}
	return int32(p.call.text.LineStart(int(line)))
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
	return give(ctx, m, t.Slice(int(start), int(end)))
}

// give writes s into memory the plugin allocates, and returns where, as
// ptr<<32|len.
func give(ctx context.Context, m api.Module, s string) int64 {
	out, err := m.ExportedFunction("alloc").Call(ctx, uint64(len(s)))
	if err != nil || len(out) == 0 || !m.Memory().WriteString(uint32(out[0]), s) {
		return errMemory
	}
	return int64(out[0])<<32 | int64(len(s))
}

// readFile writes the file at the path at (ptr, n), relative to the
// project, into memory the plugin allocates. A path that leads outside the
// project, through .. or a link, is refused (B47).
func (p *Plugin) readFile(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil || !p.allowed("fs.read", "read_file") {
		return errDenied
	}
	name, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	data, err := p.read(string(name))
	if err != nil {
		p.log(fmt.Sprintf("%s: read_file: %v", p.Name, err))
		return errFile
	}
	return give(ctx, m, string(data))
}

// runProgram runs the program and arguments at (ptr, n), separated by NUL
// bytes, in the project, and writes what it printed into memory the plugin
// allocates. The program must be one the plugin's table lists, and the
// call's budget stops it (B48).
func (p *Plugin) runProgram(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil || !p.allowed("process", "run") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	if !ok || n == 0 {
		return errRange
	}
	argv := strings.Split(string(b), "\x00")
	if !slices.Contains(p.Programs, argv[0]) {
		p.log(fmt.Sprintf("%s: run %s, which it did not declare", p.Name, argv[0]))
		return errRun
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = p.root
	out, err := cmd.Output()
	if err != nil {
		p.log(fmt.Sprintf("%s: run %s: %v", p.Name, argv[0], err))
		return errRun
	}
	return give(ctx, m, string(out))
}

// maxBody is the most of a response http_get reads, 16MB.
const maxBody = 16 << 20

// httpGet gets the http or https URL at (ptr, n) and writes the response's
// body into memory the plugin allocates. Its host must be one the plugin's
// table lists, and the call's budget stops the request (B49).
func (p *Plugin) httpGet(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil || !p.allowed("net", "http_get") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	body, err := p.get(ctx, string(b))
	if err != nil {
		p.log(fmt.Sprintf("%s: http_get: %v", p.Name, err))
		return errNet
	}
	return give(ctx, m, string(body))
}

// get gets rawURL if the plugin may reach its host.
func (p *Plugin) get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	switch {
	case err != nil:
		return nil, fmt.Errorf("get %s: %w", rawURL, err)
	case u.Scheme != "http" && u.Scheme != "https":
		return nil, fmt.Errorf("get %s: only http and https", rawURL)
	case !slices.Contains(p.Hosts, u.Hostname()):
		return nil, fmt.Errorf("get %s: %s is not a host it declared", rawURL, u.Hostname())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", rawURL, err)
	}
	// A redirect may not lead to a host the plugin did not declare.
	client := &http.Client{CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		if !slices.Contains(p.Hosts, r.URL.Hostname()) {
			return fmt.Errorf("redirect to %s, which it did not declare", r.URL.Hostname())
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("get %s: %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", rawURL, err)
	}
	return body, nil
}

// read reads the file at name, relative to the project, if it is in it.
func (p *Plugin) read(name string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(p.root)
	if err != nil {
		return nil, fmt.Errorf("find the project: %w", err)
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if rel, err := filepath.Rel(root, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%s is outside the project", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return data, nil
}

// register records, during init, the command, operator, object or key of
// kind named at (nptr, nn), with the help at (hptr, hn), which call runs by
// id.
func (p *Plugin) register(_ context.Context, m api.Module, kind, nptr, nn, hptr, hn, id uint32) int32 {
	name, ok1 := m.Memory().Read(nptr, nn)
	help, ok2 := m.Memory().Read(hptr, hn)
	switch {
	case p.call == nil || p.call.name != "init" || Kind(kind) > Insert:
		return errDenied
	case !ok1 || !ok2:
		return errRange
	}
	p.Registered = append(p.Registered, Registration{Kind(kind), string(name), string(help), id})
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
