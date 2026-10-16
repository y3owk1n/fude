// Package plugin runs wasm plugins on wazero, each with the capabilities it
// declares and under a budget for every call.
package plugin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
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
	Sign, Note string // what shows on Start's line, in the sign column or after its end (B184)
}

// Result is what one call into a plugin asked for, and what it returned.
type Result struct {
	Edits      []fude.Edit
	Decorate   []Decoration
	Undecorate []int     // the ids of decorations to remove
	Emit       []string  // the events to emit, named plugin.event
	Status     *string   // the plugin's statusline segment for the text it read, "" for none
	Float      *string   // markdown to show at the cursor, "" to close it
	Show       *Panel    // text to show in a window of its own
	Jump       *Jump     // where to move the cursor
	Pick       *Pick     // a list to pick from, or a question to answer
	Timers     []Timer   // the callbacks to run later
	Jobs       []Job     // the work to do in the background
	Requests   []Request // the requests to send the buffer's language servers
	Out        []uint64
}

// Job is a program to run or a URL to get in the background. Once it ends,
// fude runs the plugin's callback with its id, with the output and any
// error in its context (B181).
type Job struct {
	ID   uint32
	Argv []string
	URL  string
}

// Request is a request a plugin sends the language servers of the buffer
// the call was about. fude fills in its textDocument, and its position at
// Pos unless Pos is negative, and adds Params, a JSON object. The plugin's
// callback with ID gets the first answer that is not null (B190).
type Request struct {
	ID     uint32
	Method string
	Pos    int
	Params string
}

// maxJobs is how many of a plugin's jobs run at once. One more waits.
const maxJobs = 8

// Pick is a list of items the plugin asks the user to pick from with
// Prompt, or with none a question whose answer is what they type. fude runs
// the plugin's callback with ID once they answer or cancel (B185).
type Pick struct {
	ID     uint32
	Prompt string
	Items  []string
}

// Jump is where a plugin moves the cursor: Pos in the buffer the call was
// about, or in the file at Path when it is not "", which fude opens
// (B187).
type Jump struct {
	Path string
	Pos  int
}

// Panel is text a plugin shows in a read-only window titled Title, coloured
// as Filetype. It replaces the text of a window open under that title
// (B186).
type Panel struct {
	Title, Filetype, Text string
}

// Timer is a callback the plugin asked fude to run after a while, through
// its callback export, by the id it gave (B180).
type Timer struct {
	ID    uint32
	After time.Duration
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
var capabilities = []string{"buffer.read", "buffer.write", "ui", "fs.read", "process", "net", "lsp"}

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
	call    *call         // the call running now
	jobs    chan struct{} // a slot for each job running
}

// call holds what one call into a plugin reads and what it asks for.
type call struct {
	name string
	Scope
	Result
}

// Scope is what one call into a plugin reads: the text of the buffer it is
// about, what it is about, and every open buffer, by id (B188).
type Scope struct {
	Text    fude.Text
	About   Context
	Buffers map[int]Buffer
	// Node finds a syntax node in Text, as syntax.Buffer.Nodes does. Free
	// frees what it reads once the call is done (B189).
	Node func(pos, up int) (kind string, start, end int, ok bool)
	Free func()
}

// Done frees what s holds, once the call reading it is done. The calls
// that take s call it.
func (s Scope) Done() {
	if s.Free != nil {
		s.Free()
	}
}

// Buffer is an open buffer as a plugin reads it.
type Buffer struct {
	Path, Filetype string
	Text           fude.Text
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
	p := &Plugin{Name: strings.TrimSuffix(filepath.Base(path), ".wasm"), Grants: g, log: log, root: root, jobs: make(chan struct{}, maxJobs)}
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
func (p *Plugin) Init(ctx context.Context, s Scope) (Result, error) {
	defer s.Done()
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, InitBudget)
	defer cancel()
	p.call = &call{name: "init", Scope: s}
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
// input path, reading s. It returns what the call
// asked for and returned, or ErrBusy instead of waiting for a call already
// running. A call that traps or runs past its budget returns an error, and
// nothing it asked for happens.
func (p *Plugin) CallNow(ctx context.Context, name string, s Scope, budget time.Duration, args ...uint64) (Result, error) {
	defer s.Done()
	if !p.mu.TryLock() {
		return Result{}, fmt.Errorf("plugin %s %w", p.Name, ErrBusy)
	}
	defer p.mu.Unlock()
	return p.run(ctx, name, s, budget, args...)
}

// Callback runs the plugin's callback export with id, reading s, under
// EventBudget. It waits for a call already running.
func (p *Plugin) Callback(ctx context.Context, id uint32, s Scope) (Result, error) {
	defer s.Done()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.run(ctx, "callback", s, EventBudget, uint64(id))
}

// Event runs the plugin's on_event with the event's name, written into its
// memory, reading s, under EventBudget (B57).
func (p *Plugin) Event(ctx context.Context, name string, s Scope) (Result, error) {
	defer s.Done()
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, EventBudget)
	defer cancel()
	out, err := p.mod.ExportedFunction("alloc").Call(ctx, uint64(len(name)))
	if err != nil || len(out) == 0 || !p.mod.Memory().WriteString(uint32(out[0]), name) {
		return Result{}, fmt.Errorf("plugin %s: write the event's name: %w", p.Name, cmp.Or(err, errors.New("no memory for it")))
	}
	return p.run(ctx, "on_event", s, EventBudget, out[0], uint64(len(name)))
}

// run makes one call while p.mu is held.
func (p *Plugin) run(ctx context.Context, name string, s Scope, budget time.Duration, args ...uint64) (Result, error) {
	f := p.mod.ExportedFunction(name)
	if f == nil {
		return Result{}, fmt.Errorf("plugin %s has no %s", p.Name, name)
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	p.call = &call{name: name, Scope: s}
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
		NewFunctionBuilder().WithFunc(p.after).Export("after").
		NewFunctionBuilder().WithFunc(p.start).Export("start").
		NewFunctionBuilder().WithFunc(p.fetch).Export("fetch").
		NewFunctionBuilder().WithFunc(p.request).Export("request").
		NewFunctionBuilder().WithFunc(p.textLen).Export("text_len").
		NewFunctionBuilder().WithFunc(p.lines).Export("lines").
		NewFunctionBuilder().WithFunc(p.node).Export("node").
		NewFunctionBuilder().WithFunc(p.buffers).Export("buffers").
		NewFunctionBuilder().WithFunc(p.lenOf).Export("len_of").
		NewFunctionBuilder().WithFunc(p.textOf).Export("text_of").
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
		NewFunctionBuilder().WithFunc(p.sign).Export("sign").
		NewFunctionBuilder().WithFunc(p.note).Export("note").
		NewFunctionBuilder().WithFunc(p.show).Export("show").
		NewFunctionBuilder().WithFunc(p.jump).Export("jump").
		NewFunctionBuilder().WithFunc(p.pick).Export("pick").
		NewFunctionBuilder().WithFunc(p.open).Export("open").
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
	v, has := p.call.About[string(name)]
	if !ok || !has {
		return errRange
	}
	return give(ctx, m, v)
}

// after asks fude to run the plugin's callback with id, ms milliseconds
// after the call returns.
func (p *Plugin) after(ms, id uint32) int32 {
	if p.call == nil {
		return errDenied
	}
	p.call.Timers = append(p.call.Timers, Timer{id, time.Duration(ms) * time.Millisecond})
	return 0
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
	return int32(p.call.Text.Len())
}

// node writes the kind, start and end of the named syntax node that holds
// pos, or its up'th named parent, NUL-separated, into memory the plugin
// allocates, or returns errRange for none (B189).
func (p *Plugin) node(ctx context.Context, m api.Module, pos, up uint32) int64 {
	if p.call == nil || !p.allowed("buffer.read", "node") {
		return errDenied
	}
	if p.call.Node == nil || int(pos) > p.call.Text.Len() {
		return errRange
	}
	kind, start, end, ok := p.call.Node(int(pos), int(up))
	if !ok {
		return errRange
	}
	return give(ctx, m, fmt.Sprintf("%s\x00%d\x00%d", kind, start, end))
}

// buffers writes the id, path and filetype of each open buffer, each
// followed by a NUL, into memory the plugin allocates (B188).
func (p *Plugin) buffers(ctx context.Context, m api.Module) int64 {
	if p.call == nil || !p.allowed("buffer.read", "buffers") {
		return errDenied
	}
	var b strings.Builder
	for _, id := range slices.Sorted(maps.Keys(p.call.Buffers)) {
		o := p.call.Buffers[id]
		fmt.Fprintf(&b, "%d\x00%s\x00%s\x00", id, o.Path, o.Filetype)
	}
	return give(ctx, m, b.String())
}

// lenOf returns the length of the text of the buffer id.
func (p *Plugin) lenOf(id uint32) int32 {
	if p.call == nil || !p.allowed("buffer.read", "len_of") {
		return errDenied
	}
	o, ok := p.call.Buffers[int(id)]
	if !ok {
		return errRange
	}
	return int32(o.Text.Len())
}

// textOf writes [start, end) of the text of the buffer id into memory the
// plugin allocates.
func (p *Plugin) textOf(ctx context.Context, m api.Module, id, start, end uint32) int64 {
	if p.call == nil || !p.allowed("buffer.read", "text_of") {
		return errDenied
	}
	o, ok := p.call.Buffers[int(id)]
	if !ok || start > end || int(end) > o.Text.Len() {
		return errRange
	}
	return give(ctx, m, o.Text.Slice(int(start), int(end)))
}

// lines returns how many lines the text has.
func (p *Plugin) lines() int32 {
	if p.call == nil || !p.allowed("buffer.read", "lines") {
		return errDenied
	}
	return int32(p.call.Text.Lines())
}

// lineOf returns the line that holds pos, from 0.
func (p *Plugin) lineOf(pos uint32) int32 {
	if p.call == nil || !p.allowed("buffer.read", "line_of") {
		return errDenied
	}
	if int(pos) > p.call.Text.Len() {
		return errRange
	}
	return int32(p.call.Text.LineOf(int(pos)))
}

// lineStart returns where line, from 0, starts.
func (p *Plugin) lineStart(line uint32) int32 {
	if p.call == nil || !p.allowed("buffer.read", "line_start") {
		return errDenied
	}
	if int(line) >= p.call.Text.Lines() {
		return errRange
	}
	return int32(p.call.Text.LineStart(int(line)))
}

// text writes [start, end) of the text into memory the plugin allocates,
// and returns where, as ptr<<32|len.
func (p *Plugin) text(ctx context.Context, m api.Module, start, end uint32) int64 {
	if p.call == nil || !p.allowed("buffer.read", "text") {
		return errDenied
	}
	t := p.call.Text
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
	out, err := p.program(ctx, strings.Split(string(b), "\x00"))
	if err != nil {
		p.log(fmt.Sprintf("%s: %v", p.Name, err))
		return errRun
	}
	return give(ctx, m, out)
}

// program runs argv in the project and returns what it prints, if the
// plugin declared the program.
func (p *Plugin) program(ctx context.Context, argv []string) (string, error) {
	if !slices.Contains(p.Programs, argv[0]) {
		return "", fmt.Errorf("run %s, which it did not declare", argv[0])
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = p.root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run %s: %w", argv[0], err)
	}
	return string(out), nil
}

// start asks fude to run the program and arguments at (ptr, n), NUL
// separated, in the background, and to run the plugin's callback with id
// once it exits (B181).
func (p *Plugin) start(_ context.Context, m api.Module, ptr, n, id uint32) int32 {
	if p.call == nil || !p.allowed("process", "start") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	if !ok || n == 0 {
		return errRange
	}
	argv := strings.Split(string(b), "\x00")
	if !slices.Contains(p.Programs, argv[0]) {
		p.log(fmt.Sprintf("%s: start %s, which it did not declare", p.Name, argv[0]))
		return errRun
	}
	p.call.Jobs = append(p.call.Jobs, Job{ID: id, Argv: argv})
	return 0
}

// fetch asks fude to get the URL at (ptr, n) in the background, and to run
// the plugin's callback with id once it has the body (B181).
func (p *Plugin) fetch(_ context.Context, m api.Module, ptr, n, id uint32) int32 {
	if p.call == nil || !p.allowed("net", "fetch") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	p.call.Jobs = append(p.call.Jobs, Job{ID: id, URL: string(b)})
	return 0
}

// request asks fude to send the method and params at (ptr, n), NUL
// separated, to the language servers of the buffer the call is about, at
// pos, or with no position for a negative pos, and to run the plugin's
// callback with id with the answer (B190).
func (p *Plugin) request(_ context.Context, m api.Module, ptr, n, pos, id uint32) int32 {
	if p.call == nil || !p.allowed("lsp", "request") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	method, params, _ := strings.Cut(string(b), "\x00")
	if !ok || method == "" || int(int32(pos)) > p.call.Text.Len() {
		return errRange
	}
	p.call.Requests = append(p.call.Requests, Request{id, method, int(int32(pos)), params})
	return 0
}

// Work does j, a program or a get, as run and http_get do but with no
// budget, until it ends or ctx does. At most maxJobs run at once.
func (p *Plugin) Work(ctx context.Context, j Job) (string, error) {
	select {
	case p.jobs <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-p.jobs }()
	if j.URL != "" {
		body, err := p.get(ctx, j.URL)
		return string(body), err
	}
	return p.program(ctx, j.Argv)
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

// inside returns where the file at name, relative to the project, is, or an
// error when it is not in the project, through .. or a link.
func (p *Plugin) inside(name string) (string, error) {
	root, err := filepath.EvalSymlinks(p.root)
	if err != nil {
		return "", fmt.Errorf("find the project: %w", err)
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", fmt.Errorf("find %s: %w", name, err)
	}
	if rel, err := filepath.Rel(root, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the project", name)
	}
	return path, nil
}

// read reads the file at name, relative to the project, if it is in it.
func (p *Plugin) read(name string) ([]byte, error) {
	path, err := p.inside(name)
	if err != nil {
		return nil, err
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
	t := p.call.Text
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
	if start > end || int(end) > p.call.Text.Len() || !ok {
		return errRange
	}
	p.next++
	p.call.Decorate = append(p.call.Decorate, Decoration{ID: p.next, Start: int(start), End: int(end), Style: string(style)})
	return int32(p.next)
}

// sign puts the text at (ptr, n) in the sign column on pos's line, in the
// style after a NUL, and returns its id, which undecorate removes (B184).
func (p *Plugin) sign(_ context.Context, m api.Module, pos, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("ui", "sign") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	if int(pos) > p.call.Text.Len() || !ok {
		return errRange
	}
	text, style, _ := strings.Cut(string(b), "\x00")
	p.next++
	p.call.Decorate = append(p.call.Decorate, Decoration{ID: p.next, Start: int(pos), End: int(pos), Style: style, Sign: text})
	return int32(p.next)
}

// pick asks the user to pick from the items at (ptr, n), after a prompt,
// all NUL-separated, or with no items to answer the prompt, once the call
// returns. The plugin's callback with id runs with the answer (B185).
func (p *Plugin) pick(_ context.Context, m api.Module, ptr, n, id uint32) int32 {
	if p.call == nil || !p.allowed("ui", "pick") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	parts := strings.Split(string(b), "\x00")
	p.call.Pick = &Pick{id, parts[0], parts[1:]}
	return 0
}

// jump moves the cursor to pos once the call returns (B187).
func (p *Plugin) jump(pos uint32) int32 {
	if p.call == nil || !p.allowed("ui", "jump") {
		return errDenied
	}
	if int(pos) > p.call.Text.Len() {
		return errRange
	}
	p.call.Jump = &Jump{Pos: int(pos)}
	return 0
}

// open opens the file at the path at (ptr, n), relative to the project, and
// puts the cursor at pos in it, once the call returns. A path that leads
// outside the project is refused, as for read_file (B187).
func (p *Plugin) open(_ context.Context, m api.Module, pos, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("ui", "open") {
		return errDenied
	}
	name, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	path, err := p.inside(string(name))
	if err != nil {
		p.log(fmt.Sprintf("%s: open: %v", p.Name, err))
		return errFile
	}
	p.call.Jump = &Jump{path, int(pos)}
	return 0
}

// show shows, in a window of its own, the title, filetype and text at
// (ptr, n), separated by NULs (B186).
func (p *Plugin) show(_ context.Context, m api.Module, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("ui", "show") {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	parts := strings.SplitN(string(b), "\x00", 3)
	if !ok || len(parts) != 3 || parts[0] == "" {
		return errRange
	}
	p.call.Show = &Panel{parts[0], parts[1], parts[2]}
	return 0
}

// note shows the text at (ptr, n) after the end of pos's line, and returns
// its id, which undecorate removes (B184).
func (p *Plugin) note(_ context.Context, m api.Module, pos, ptr, n uint32) int32 {
	if p.call == nil || !p.allowed("ui", "note") {
		return errDenied
	}
	text, ok := m.Memory().Read(ptr, n)
	if int(pos) > p.call.Text.Len() || !ok {
		return errRange
	}
	p.next++
	p.call.Decorate = append(p.call.Decorate, Decoration{ID: p.next, Start: int(pos), End: int(pos), Note: string(text)})
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
