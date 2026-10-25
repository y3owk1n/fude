// Package plugin runs wasm plugins on wazero, each with the capabilities it
// declares and under a budget for every call.
package plugin

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	fude "github.com/y3owk1n/fude/api"
)

// Budgets are how long one call into a plugin may run.
const (
	InitBudget  = 200 * time.Millisecond * slowdown
	InputBudget = 16 * time.Millisecond * slowdown  // an operator, object or command, one frame
	EventBudget = 100 * time.Millisecond * slowdown // on_event, off the input path
)

// maxJobs is how many of a plugin's jobs run at once. One more waits.
const maxJobs = 8

// maxFed is how many programs a plugin talks to run at once, outside its
// jobs. Work refuses one more, as none of them may end soon (B385).
const maxFed = 8

// memoryPages is the most memory a plugin may grow to, 64MB.
const memoryPages = 1024

// capabilities are the ones a plugin may declare.
var capabilities = []string{"buffer.read", "buffer.write", "ui", "fs.read", "fs.write", "process", "net", "lsp", "keys", "grammar"}

// Errors a host function returns to the plugin.
const (
	errDenied = -1 // the plugin did not declare the capability, or no call runs
	errRange  = -2 // a range outside the text
	errMemory = -3 // the plugin's memory could not take the result
	errFile   = -4 // a file outside the project and paths, or one that cannot be read or written
	errRun    = -5 // a program the plugin did not declare, or one that failed
	errNet    = -6 // a host the plugin did not declare, or a request that failed
)

// Plugin is one loaded wasm plugin. Calls into it take turns.
type Plugin struct {
	name       string
	registered []fude.Registration // what its init registered
	subscribed []string            // the events its init subscribed to
	Grants
	log   func(string)
	trace func(text string, took time.Duration)
	root  string   // the project: the directory fude started in
	roots []string // the folders of its paths, with ~ expanded (B585)

	turn    chan struct{} // held by the call running, as calls take turns
	cache   wazero.CompilationCache
	runtime wazero.Runtime
	mod     api.Module
	call    *call         // the call running now
	jobs    chan struct{} // a slot for each job running
	fed     chan struct{} // a slot for each program it talks to running
}

// call holds what one call into a plugin reads and what it asks for.
type call struct {
	name string
	fude.Scope
	fude.Result
}

// errBudget is a call that ran past its budget. The plugin is closed.
var errBudget = errors.New("ran past its budget")

// Grants are what config.toml lets a plugin do: its capabilities, the
// programs process may run, the hosts net may reach and the folders outside
// the project fs.read and fs.write reach (B585). Options are where its
// options start, by name (B377).
type Grants struct {
	Capabilities []string
	Programs     []string
	Hosts        []string
	Paths        []string
	Options      map[string]string
}

// Load compiles the plugin at path and instantiates it with g. Compiled code
// is kept in cacheDir between runs. log takes what the plugin logs and the
// calls it was denied, and trace each file it read, program it ran and URL
// it got, with how long it took.
func Load(ctx context.Context, path string, g Grants, cacheDir string, log func(string), trace func(text string, took time.Duration)) (*Plugin, error) {
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
	p := &Plugin{name: strings.TrimSuffix(filepath.Base(path), ".wasm"), Grants: g, log: log, trace: trace, root: root, turn: make(chan struct{}, 1), jobs: make(chan struct{}, maxJobs), fed: make(chan struct{}, maxFed)}
	for _, d := range g.Paths {
		home, err := expand(d)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", p.name, err)
		}
		if !filepath.IsAbs(home) {
			return nil, fmt.Errorf("plugin %s: path %q is not absolute and does not start with ~", p.name, d)
		}
		p.roots = append(p.roots, filepath.Clean(home))
	}
	if p.cache, err = wazero.NewCompilationCacheWithDir(cacheDir); err != nil {
		return nil, fmt.Errorf("plugin cache: %w", err)
	}
	p.runtime = wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCompilationCache(p.cache).WithMemoryLimitPages(memoryPages).WithCloseOnContextDone(true))
	if err := p.host(ctx); err != nil {
		p.Close(ctx)
		return nil, err
	}
	// The budget is for the plugin's code, so compiling runs before it.
	compiled, err := p.runtime.CompileModule(ctx, wasm)
	if err != nil {
		p.Close(ctx)
		return nil, fmt.Errorf("plugin %s: %w", p.name, err)
	}
	// A start function would run with no budget, so _start does not run,
	// and a start section runs under the one init has.
	ictx, cancel := context.WithTimeout(ctx, InitBudget)
	defer cancel()
	if p.mod, err = p.runtime.InstantiateModule(ictx, compiled, wazero.NewModuleConfig().WithStartFunctions()); err != nil {
		p.Close(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("plugin %s %w", p.name, errBudget)
		}
		return nil, fmt.Errorf("plugin %s: %w", p.name, err)
	}
	// The host writes events and answers into the plugin's memory through
	// its alloc, so a plugin without them cannot run.
	if p.mod.ExportedFunction("alloc") == nil || p.mod.ExportedMemory("memory") == nil {
		p.Close(ctx)
		return nil, fmt.Errorf("plugin %s exports no alloc or no memory", p.name)
	}
	return p, nil
}

// Name is the plugin's file name without .wasm.
func (p *Plugin) Name() string { return p.name }

// Registered is what its init registered.
func (p *Plugin) Registered() []fude.Registration { return p.registered }

// Subscribed is the events its init subscribed to.
func (p *Plugin) Subscribed() []string { return p.subscribed }

// Init runs the plugin's _initialize, where a compiler puts its program's
// setup, then its init, each when it exports one, together under
// InitBudget. Both may register and subscribe.
func (p *Plugin) Init(ctx context.Context, s fude.Scope) (fude.Result, error) {
	p.turn <- struct{}{}
	defer func() { <-p.turn }()
	ctx, cancel := context.WithTimeout(ctx, InitBudget)
	defer cancel()
	// The plugin's own options read as they start while its init defines
	// them, before the editor keeps them (B377).
	get := s.Option
	s.Option = func(name string) (string, bool) {
		short, own := strings.CutPrefix(name, p.name+".")
		i := slices.IndexFunc(p.registered, func(r fude.Registration) bool { return r.Kind == fude.Option && r.Name == short })
		switch v, set := p.Options[short]; {
		case own && i >= 0 && set:
			return v, true
		case own && i >= 0:
			return p.registered[i].Default, true
		case get == nil:
			return "", false
		}
		return get(name)
	}
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
			return fude.Result{}, fmt.Errorf("plugin %s %w", p.name, errBudget)
		case err != nil:
			return fude.Result{}, fmt.Errorf("plugin %s: %s: %w", p.name, name, err)
		}
	}
	return p.call.Result, nil
}

// Close frees the plugin.
func (p *Plugin) Close(ctx context.Context) error {
	return errors.Join(p.runtime.Close(ctx), p.cache.Close(ctx))
}

// Call runs what the plugin registered as id through its export call(id,
// a, b, c) i64, under InputBudget, reading s. The arguments a kind does not
// take are 0, and an object's export returns start<<32|end, or a negative
// number for none. It waits for a call already running for at most half
// its budget, runs in what is left, and returns api.ErrBusy once that half
// passes, so a key never waits past its budget (B57). A call that traps or
// runs past its budget returns an error, and nothing it asked for happens.
func (p *Plugin) Call(ctx context.Context, id uint32, s fude.Scope, args ...int) (fude.Result, error) {
	at := time.Now()
	select {
	case p.turn <- struct{}{}:
	default:
		p.trace(p.name+" waits for the call running", 0)
		wait := time.NewTimer(InputBudget / 2)
		defer wait.Stop()
		select {
		case p.turn <- struct{}{}:
		case <-wait.C:
			return fude.Result{}, fmt.Errorf("plugin %s %w", p.name, fude.ErrBusy)
		}
	}
	defer func() { <-p.turn }()
	full := make([]uint64, 4)
	full[0] = uint64(id)
	for i, a := range args {
		full[i+1] = uint64(a)
	}
	res, out, err := p.run(ctx, "call", s, InputBudget-time.Since(at), full...)
	if err == nil && len(out) > 0 && int64(out[0]) >= 0 && slices.ContainsFunc(p.registered, func(r fude.Registration) bool { return r.ID == id && r.Kind == fude.Object }) {
		res.Selected = &[2]int{int(out[0] >> 32), int(uint32(out[0]))}
	}
	return res, err
}

// Callback runs the plugin's callback export with id, reading s, under
// EventBudget. It waits for a call already running.
func (p *Plugin) Callback(ctx context.Context, id uint32, s fude.Scope) (fude.Result, error) {
	p.turn <- struct{}{}
	defer func() { <-p.turn }()
	res, _, err := p.run(ctx, "callback", s, EventBudget, uint64(id))
	return res, err
}

// Event runs the plugin's on_event with the event's name, written into its
// memory, reading s, under EventBudget (B57).
func (p *Plugin) Event(ctx context.Context, name string, s fude.Scope) (fude.Result, error) {
	p.turn <- struct{}{}
	defer func() { <-p.turn }()
	ctx, cancel := context.WithTimeout(ctx, EventBudget)
	defer cancel()
	out, err := p.mod.ExportedFunction("alloc").Call(ctx, uint64(len(name)))
	if err != nil || len(out) == 0 || !p.mod.Memory().WriteString(uint32(out[0]), name) {
		return fude.Result{}, fmt.Errorf("plugin %s: write the event's name: %w", p.name, cmp.Or(err, errors.New("no memory for it")))
	}
	res, _, err := p.run(ctx, "on_event", s, EventBudget, out[0], uint64(len(name)))
	return res, err
}

// run makes one call while p.mu is held, and returns what the call asked
// for and what the export returned.
func (p *Plugin) run(ctx context.Context, name string, s fude.Scope, budget time.Duration, args ...uint64) (fude.Result, []uint64, error) {
	f := p.mod.ExportedFunction(name)
	if f == nil {
		return fude.Result{}, nil, fmt.Errorf("plugin %s has no %s", p.name, name)
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	p.call = &call{name: name, Scope: s}
	defer func() { p.call = nil }()
	out, err := f.Call(ctx, args...)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fude.Result{}, nil, fmt.Errorf("plugin %s %w", p.name, errBudget)
	case err != nil:
		return fude.Result{}, nil, fmt.Errorf("plugin %s: %w", p.name, err)
	}
	return p.call.Result, out, nil
}

// host adds the functions a plugin imports from "fude".
func (p *Plugin) host(ctx context.Context) error {
	_, err := p.runtime.NewHostModuleBuilder("fude").
		NewFunctionBuilder().WithFunc(p.logText).Export("log").
		NewFunctionBuilder().WithFunc(p.context).Export("context").
		NewFunctionBuilder().WithFunc(p.textLen).Export("text_len").
		NewFunctionBuilder().WithFunc(p.lines).Export("lines").
		NewFunctionBuilder().WithFunc(p.lenOf).Export("len_of").
		NewFunctionBuilder().WithFunc(p.textOf).Export("text_of").
		NewFunctionBuilder().WithFunc(p.lineOf).Export("line_of").
		NewFunctionBuilder().WithFunc(p.lineStart).Export("line_start").
		NewFunctionBuilder().WithFunc(p.text).Export("text").
		NewFunctionBuilder().WithFunc(p.readFile).Export("read_file").
		NewFunctionBuilder().WithFunc(p.listDir).Export("list_dir").
		NewFunctionBuilder().WithFunc(p.stat).Export("stat").
		NewFunctionBuilder().WithFunc(p.writeFile).Export("write_file").
		NewFunctionBuilder().WithFunc(p.makeDir).Export("make_dir").
		NewFunctionBuilder().WithFunc(p.rename).Export("rename").
		NewFunctionBuilder().WithFunc(p.remove).Export("remove").
		NewFunctionBuilder().WithFunc(p.runProgram).Export("run").
		NewFunctionBuilder().WithFunc(p.httpGet).Export("http_get").
		NewFunctionBuilder().WithFunc(p.register).Export("register").
		NewFunctionBuilder().WithFunc(p.subscribe).Export("subscribe").
		NewFunctionBuilder().WithFunc(p.result).Export("result").
		NewFunctionBuilder().WithFunc(p.readScope).Export("read").
		NewFunctionBuilder().WithFunc(now).Export("now").
		Instantiate(ctx)
	if err != nil {
		return fmt.Errorf("plugin %s: %w", p.name, err)
	}
	return nil
}

// allowed reports whether the plugin declared capability c, and logs the
// call it denies.
func (p *Plugin) allowed(c, fn string) bool {
	if slices.Contains(p.Capabilities, c) {
		return true
	}
	p.log(fmt.Sprintf("%s: %s needs %s, which it did not declare", p.name, fn, c))
	return false
}

func (p *Plugin) logText(_ context.Context, m api.Module, ptr, n uint32) {
	if b, ok := m.Memory().Read(ptr, n); ok {
		p.log(p.name + ": " + string(b))
	}
}

// now returns the time of day, in nanoseconds since 1970 (B386).
func now() int64 { return time.Now().UnixNano() }

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

func (p *Plugin) textLen() int32 {
	if p.call == nil || !p.allowed("buffer.read", "text_len") {
		return errDenied
	}
	return int32(p.call.Text.Len())
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
// project, into memory the plugin allocates. It refuses a path that leads
// outside the project and the plugin's paths, through .. or a link (B47).
func (p *Plugin) readFile(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil || !p.allowed("fs.read", "read_file") {
		return errDenied
	}
	name, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	at := time.Now()
	data, err := p.read(string(name))
	p.traced(fmt.Sprintf("%s read %s, %d bytes", p.name, name, len(data)), at, err)
	if err != nil {
		p.log(fmt.Sprintf("%s: read_file: %v", p.name, err))
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
	argv, at := strings.Split(string(b), "\x00"), time.Now()
	out, err := p.program(ctx, argv, "")
	p.traced(fmt.Sprintf("%s ran %s, %d bytes", p.name, strings.Join(argv, " "), len(out)), at, err)
	if err != nil {
		p.log(fmt.Sprintf("%s: %v", p.name, err))
		return errRun
	}
	return give(ctx, m, out)
}

// program runs argv in the project and returns what it prints, if the
// plugin declared the program.
func (p *Plugin) program(ctx context.Context, argv []string, input string) (string, error) {
	cmd, err := p.command(ctx, argv, input)
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run %s: %w", argv[0], err)
	}
	return string(out), nil
}

// stream runs argv as program does, and hands emit each line it prints as
// it prints it (B339).
func (p *Plugin) stream(ctx context.Context, argv []string, input string, emit func(string), feed <-chan string) error {
	cmd, err := p.command(ctx, argv, input)
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	var in io.WriteCloser
	if feed != nil {
		// feed writes input first, on the pipe that stays open.
		cmd.Stdin = nil
		if in, err = cmd.StdinPipe(); err != nil {
			return fmt.Errorf("run %s: %w", argv[0], err)
		}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	ended := make(chan struct{})
	if in != nil {
		go p.feed(argv[0], in, input, feed, ended)
	}
	lines := bufio.NewScanner(out)
	lines.Buffer(nil, maxBody)
	for lines.Scan() {
		emit(lines.Text())
	}
	close(ended)
	if err := errors.Join(lines.Err(), cmd.Wait()); err != nil {
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	return nil
}

// feed writes input, then each text that comes on feed, on the input of
// the program name until it ends (B385).
func (p *Plugin) feed(name string, in io.Writer, input string, feed <-chan string, ended <-chan struct{}) {
	text := input
	for {
		if _, err := io.WriteString(in, text); err != nil {
			p.log(fmt.Sprintf("%s: write to %s: %v", p.name, name, err))
			return
		}
		select {
		case text = <-feed:
		case <-ended:
			return
		}
	}
}

// command is argv to run in the project, if the plugin declared the
// program, with input on its stdin.
func (p *Plugin) command(ctx context.Context, argv []string, input string) (*exec.Cmd, error) {
	if !slices.Contains(p.Programs, argv[0]) {
		return nil, fmt.Errorf("run %s, which it did not declare", argv[0])
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// A child the program leaves holding its output does not hold the call.
	// 10ms lost a program's own output on a loaded machine.
	cmd.Dir, cmd.WaitDelay = p.root, 100*time.Millisecond
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	return cmd, nil
}

// Work does j, a program or a get, as run and http_get do but with no
// budget, until it ends or ctx does. It calls j.Emit with each line a
// stream's program prints. At most maxJobs run at once, and maxFed
// programs it talks to besides.
func (p *Plugin) Work(ctx context.Context, j fude.Job) (string, error) {
	if j.Fed {
		select {
		case p.fed <- struct{}{}:
		default:
			return "", fmt.Errorf("run %s: %d programs it talks to run already", j.Argv[0], maxFed)
		}
		defer func() { <-p.fed }()
		return "", p.stream(ctx, j.Argv, j.Input, j.Emit, j.Feed)
	}
	select {
	case p.jobs <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-p.jobs }()
	switch {
	case j.Stream:
		return "", p.stream(ctx, j.Argv, j.Input, j.Emit, j.Feed)
	case j.URL != "":
		body, err := p.get(ctx, j.URL)
		return string(body), err
	}
	return p.program(ctx, j.Argv, j.Input)
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
	at := time.Now()
	body, err := p.get(ctx, string(b))
	p.traced(fmt.Sprintf("%s got %s, %d bytes", p.name, b, len(body)), at, err)
	if err != nil {
		p.log(fmt.Sprintf("%s: http_get: %v", p.name, err))
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
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		switch {
		case !slices.Contains(p.Hosts, r.URL.Hostname()):
			return fmt.Errorf("redirect to %s, which it did not declare", r.URL.Hostname())
		case len(via) >= 10:
			return errors.New("stopped after 10 redirects")
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

// inside returns the absolute path of the file at name, relative to the
// project or to ~, or an error when it is outside the project and the
// plugin's paths, through .. or a link. The file need not exist. The path
// leaves its links unresolved, so the editor matches it to a buffer opened
// by the same path (B585).
func (p *Plugin) inside(name string) (string, error) {
	path, err := expand(name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.root, path)
	}
	real, err := resolve(path)
	if err != nil {
		return "", fmt.Errorf("find %s: %w", name, err)
	}
	for _, dir := range append([]string{p.root}, p.roots...) {
		root, err := resolve(dir)
		if err != nil {
			return "", fmt.Errorf("find %s: %w", dir, err)
		}
		if rel, err := filepath.Rel(root, real); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Clean(path), nil
		}
	}
	return "", fmt.Errorf("%s is outside the project and the plugin's paths", name)
}

// expand puts the home folder in place of a leading ~.
func expand(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, "~")
	if !ok || rest != "" && rest[0] != '/' && rest[0] != filepath.Separator {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %s: %w", path, err)
	}
	return filepath.Join(home, rest), nil
}

// resolve returns path with its links resolved, as far as it exists.
func resolve(path string) (string, error) {
	rest := ""
	for {
		real, err := filepath.EvalSymlinks(path)
		switch {
		case err == nil:
			return filepath.Join(real, rest), nil
		case !errors.Is(err, fs.ErrNotExist) || filepath.Dir(path) == path:
			return "", err
		}
		rest, path = filepath.Join(filepath.Base(path), rest), filepath.Dir(path)
	}
}

// traced traces what a host call did, the time since at and any err.
func (p *Plugin) traced(what string, at time.Time, err error) {
	if err != nil {
		what += ": " + err.Error()
	}
	p.trace(what, time.Since(at))
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
	p.subscribed = append(p.subscribed, string(name))
	return 0
}
