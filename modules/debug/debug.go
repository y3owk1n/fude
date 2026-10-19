// Package debug runs a program under a debug adapter: breakpoints,
// stepping, a panel of frames and variables, and evaluation (B261 to
// B265).
package debug

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/tomlerr"
	"github.com/y3owk1n/fude/protocol/dap"
)

// What debug registers, by the ids Call runs them by.
const (
	command uint32 = iota + 1
	keyBreak
	keyContinue
	keyNext
	keyIn
	keyOut
	keyStop
	keyPanel
	keyEval
	plugOpen
)

// The callbacks debug asks for, by id. A session's stream job has an id
// from streams up.
const (
	light   uint32 = iota + 1 // lights the stopped line once its file is open
	picked                    // the launch settings picked
	streams                   // the adapter's messages
)

// subcommands are :debug's.
var subcommands = []string{"start", "stop", "break", "continue", "next", "in", "out", "panel", "eval"}

// panelKeys are the panel's own keys: <CR> acts on the frame or the
// variable of its line, and q closes it (B264).
var panelKeys = map[string]string{"<CR>": "<Plug>(debug.open)", "q": ":close<CR>"}

// Module is debug.
type Module struct {
	mu        sync.Mutex
	run       *session            // the session running, or nil
	breaks    map[int]*brk        // by decoration id, the breakpoints
	ids       int                 // the last decoration id given
	launching []string            // the launch settings a pick offers
	panel     bool                // the panel is open
	rows      []row               // the panel's lines
	seq       int                 // the last request id given
	answers   map[int]answer      // by request id, what its answer does
	streams   map[uint32]*session // by job id, the session whose adapter it runs
	pending   chan *session       // the session whose Work has yet to start
}

// brk is a breakpoint, a decoration on a buffer's line.
type brk struct {
	buffer   int
	verified bool // the adapter took it
}

// session is one debug session (B262).
type session struct {
	name    string
	adapter string
	cmd     []string
	dir     string
	job     uint32                 // the stream job that runs its adapter
	todo    chan func(*dap.Client) // what Work runs on the client
	gone    chan struct{}          // closed once Work has returned
	emit    func(line)             // hands a line to the callback, set by Work
	thread  int                    // the stopped thread, 0 while the program runs
	frames  []frame
	frame   int // the chosen frame
	scopes  []variable
	members map[int][]variable // by reference, the members fetched
	open    map[int]bool       // the references that show their members
	output  []string           // the program's last lines
	lit     *lit               // the stopped line's decoration
	ended   bool
}

// lit is the decoration that lights the stopped line, and where.
type lit struct {
	id, buffer int
}

// answer does what a request's answer asks, on the result being built.
type answer func(res *api.Result, s api.Scope, body json.RawMessage, err error)

// launch is a [launch.{name}] table of .fude/launch.toml (B261).
type launch struct {
	Adapter string         `toml:"adapter"`
	Request string         `toml:"request"`
	Args    map[string]any `toml:"args"`
}

// frame is a frame of the stopped thread, as stackTrace answers it.
type frame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Source struct {
		Path string `json:"path"`
	} `json:"source"`
}

// variable is a scope or a variable of the chosen frame, as scopes and
// variables answer them. One with members has a reference to them.
type variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Ref   int    `json:"variablesReference"`
}

// row is a line of the panel, and what <CR> on it acts on: a frame, the
// members of a variable, or neither.
type row struct {
	text  string
	frame int // -1 for none
	ref   int // 0 for none
}

// line is one message Work hands the callback: an event of the adapter's,
// the answer to a request, or a line of its stderr for the log.
type line struct {
	Kind  string          `json:"kind"`
	Name  string          `json:"name,omitempty"`
	ID    int             `json:"id,omitempty"`
	Body  json.RawMessage `json:"body,omitempty"`
	Error string          `json:"error,omitempty"`
}

// New returns debug.
func New() *Module {
	return &Module{breaks: map[int]*brk{}, answers: map[int]answer{}, streams: map[uint32]*session{}, pending: make(chan *session, 1)}
}

func (m *Module) Name() string         { return "debug" }
func (m *Module) Subscribed() []string { return []string{"buffer.closed"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "debug", Help: "debugs the program the launch settings name, or acts on the debug session: start [name], stop, break, continue, next, in, out, panel or eval expr", ID: command, Host: true, Complete: true},
		{Kind: api.Plug, Name: "break", Keys: "<Leader>db", Help: "puts a breakpoint on the cursor's line, or takes it off", ID: keyBreak, Host: true},
		{Kind: api.Plug, Name: "continue", Keys: "<Leader>dc", Help: "lets the stopped program go on, or starts a debug session", ID: keyContinue, Host: true},
		{Kind: api.Plug, Name: "next", Keys: "<Leader>dn", Help: "steps over the stopped program's line", ID: keyNext, Host: true},
		{Kind: api.Plug, Name: "in", Keys: "<Leader>di", Help: "steps into the stopped program's line", ID: keyIn, Host: true},
		{Kind: api.Plug, Name: "out", Keys: "<Leader>do", Help: "steps out of the stopped program's function", ID: keyOut, Host: true},
		{Kind: api.Plug, Name: "stop", Keys: "<Leader>dq", Help: "ends the debug session", ID: keyStop, Host: true},
		{Kind: api.Plug, Name: "panel", Keys: "<Leader>dp", Help: "shows the debug session's frames, variables and output", ID: keyPanel, Host: true},
		{Kind: api.Plug, Name: "eval", Keys: "<Leader>de", Help: "shows the value of the selection, or the name under the cursor, at the chosen frame", ID: keyEval, Host: true, Visual: true},
		{Kind: api.Plug, Name: "open", Help: "chooses the debug panel's frame under the cursor, or shows or hides its variable's members", ID: plugOpen, Host: true},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs :debug, its keys and the panel's <CR>.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res api.Result
	switch id {
	case command:
		if arg, ok := s.About["complete"]; ok {
			res.Complete = complete(arg)
			return res, nil
		}
		sub, rest, _ := strings.Cut(s.About["args"], " ")
		switch sub {
		case "start":
			m.start(&res, s, strings.TrimSpace(rest))
		case "stop":
			m.stop(&res, s)
		case "break":
			m.toggle(&res, s, args[0])
		case "continue", "next", "in", "out":
			m.step(&res, s, sub)
		case "panel":
			m.showPanel(&res, s)
		case "eval":
			m.eval(&res, strings.TrimSpace(rest), false)
		default:
			res.Message = "debug what? " + strings.Join(subcommands, ", ")
		}
	case keyBreak:
		m.toggle(&res, s, args[0])
	case keyContinue:
		m.step(&res, s, "continue")
	case keyNext:
		m.step(&res, s, "next")
	case keyIn:
		m.step(&res, s, "in")
	case keyOut:
		m.step(&res, s, "out")
	case keyStop:
		m.stop(&res, s)
	case keyPanel:
		m.showPanel(&res, s)
	case keyEval:
		m.evalHere(&res, s, args)
	case plugOpen:
		m.openRow(&res, s, s.Text.LineOf(args[0]))
	}
	return res, nil
}

// Event notes the panel closing.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.About["title"] == "[debug]" {
		m.panel = false
	}
	return api.Result{}, nil
}

// Callback takes the adapter's messages, lights the stopped line once its
// file is open, and starts the launch settings picked.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res api.Result
	switch id {
	case picked:
		if i, _ := strconv.Atoi(s.About["picked"]); s.About["ok"] == "true" && i >= 0 && i < len(m.launching) {
			m.start(&res, s, m.launching[i])
		}
	case light:
		m.relight(&res, s)
	default:
		d := m.streams[id]
		if s.About["done"] == "true" {
			delete(m.streams, id)
			m.end(&res, s, d, "")
			return res, nil
		}
		var l line
		if err := json.Unmarshal([]byte(s.About["output"]), &l); err != nil {
			return res, fmt.Errorf("debug: %w", err)
		}
		switch l.Kind {
		case "log":
			res.Log = []string{l.Name}
		case "event":
			m.event(&res, s, d, l.Name, l.Body)
		case "answer":
			if then, ok := m.answers[l.ID]; ok && d == m.run {
				delete(m.answers, l.ID)
				var err error
				if l.Error != "" {
					err = errors.New(l.Error)
				}
				then(&res, s, l.Body, err)
			}
		}
	}
	return res, nil
}

// Work runs the adapter of the session until it ends, handing the callback
// each of its messages as a line.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	d := <-m.pending
	defer close(d.gone)
	d.emit = func(l line) {
		b, _ := json.Marshal(l)
		j.Emit(string(b))
	}
	emit := d.emit
	queue, exited := make(chan func(), 16), make(chan struct{})
	c, err := dap.Start(d.adapter, d.cmd, d.dir, queue,
		func(text string) { emit(line{Kind: "log", Name: text}) },
		func(string) {},
		func(name string, body json.RawMessage) { emit(line{Kind: "event", Name: name, Body: body}) },
		func(error) { close(exited) })
	if err != nil {
		return "", err
	}
	for {
		select {
		case f := <-queue:
			f()
		case f := <-d.todo:
			f(c)
		case <-exited:
			return "", nil
		case <-ctx.Done():
			c.Close()
			return "", nil
		}
	}
}

func (m *Module) Close(context.Context) error { return nil }

// complete returns what arg, the text after :debug, completes with: a
// subcommand, or after start the names of the launch settings. A file that
// fails to read gives none, and :debug start says why.
func complete(arg string) []string {
	sub, _, ok := strings.Cut(arg, " ")
	if !ok {
		return subcommands
	}
	if sub != "start" {
		return nil
	}
	all, _ := launches()
	var out []string
	for _, name := range slices.Sorted(maps.Keys(all)) {
		out = append(out, "start "+name)
	}
	return out
}

// launches reads .fude/launch.toml. A missing file has no launches.
func launches() (map[string]launch, error) {
	path := filepath.Join(".fude", "launch.toml")
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var f struct {
		Launch map[string]launch `toml:"launch"`
	}
	d := toml.NewDecoder(bytes.NewReader(src))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, tomlerr.Where(path, err)
	}
	return f.Launch, nil
}

// start starts the launch settings called name, or the only ones, or lists
// them to choose from (B262).
func (m *Module) start(res *api.Result, s api.Scope, name string) {
	if m.run != nil {
		res.Message = "a debug session runs, and :debug stop ends it"
		return
	}
	all, err := launches()
	if err != nil {
		res.Message = err.Error()
		return
	}
	names := slices.Sorted(maps.Keys(all))
	switch {
	case len(all) == 0:
		res.Message = "no launch settings, so .fude/launch.toml should hold some"
	case name != "":
		l, ok := all[name]
		if !ok {
			res.Message = "no launch settings called " + name
			return
		}
		m.launch(res, s, name, l)
	case len(all) == 1:
		m.launch(res, s, names[0], all[names[0]])
	default:
		m.launching = names
		res.Pick = &api.Pick{ID: picked, Title: "launch", Items: names}
	}
}

// launch starts a debug session of l, called name, with the adapter it
// names or the first of the current file's filetype (B261).
func (m *Module) launch(res *api.Result, s api.Scope, name string, l launch) {
	ft := s.About["filetype"]
	adapter := l.Adapter
	if adapter == "" {
		var names []string
		if v, ok := s.Config("filetype." + ft + ".debug"); ok {
			json.Unmarshal([]byte(v), &names)
		}
		if len(names) > 0 {
			adapter = names[0]
		}
	}
	var cmd []string
	if v, ok := s.Config("debug." + adapter + ".command"); ok {
		json.Unmarshal([]byte(v), &cmd)
	}
	switch {
	case adapter == "":
		res.Message = fmt.Sprintf("no debug adapter for %s, so its filetype table's debug should name one", cmp.Or(ft, "this file"))
		return
	case cmd == nil:
		res.Message = "no [debug." + adapter + "] table in config.toml"
		return
	}
	file, _ := filepath.Abs(s.About["path"])
	project, _ := os.Getwd()
	args := expandVars(l.Args, map[string]string{"{file}": file, "{dir}": filepath.Dir(file), "{project}": project})
	d := &session{name: name, adapter: adapter, cmd: cmd, dir: project, job: streams + uint32(len(m.streams)), todo: make(chan func(*dap.Client), 16), gone: make(chan struct{}), members: map[int][]variable{}, open: map[int]bool{}}
	for m.streams[d.job] != nil {
		d.job++
	}
	m.run, m.streams[d.job] = d, d
	m.pending <- d
	res.Jobs = []api.Job{{ID: d.job, Stream: true}}
	m.request(d, "initialize", map[string]any{
		"clientID": "fude", "clientName": "fude", "adapterID": adapter,
		"linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path",
	}, func(res *api.Result, s api.Scope, _ json.RawMessage, err error) {
		if err != nil {
			m.end(res, s, d, "the debug adapter "+adapter+" did not start: "+err.Error())
		}
	})
	// The answer may come only once configurationDone has, so nothing waits
	// on it.
	m.request(d, cmp.Or(l.Request, "launch"), args, func(res *api.Result, s api.Scope, _ json.RawMessage, err error) {
		if err != nil {
			m.end(res, s, d, name+" did not start: "+err.Error())
		}
	})
	res.Message = "starting " + name
}

// expandVars returns args with each of vars in its strings, and in the
// strings of the maps and lists in it, made its value.
func expandVars(args map[string]any, vars map[string]string) map[string]any {
	var walk func(any) any
	walk = func(v any) any {
		switch v := v.(type) {
		case string:
			for from, to := range vars {
				v = strings.ReplaceAll(v, from, to)
			}
			return v
		case map[string]any:
			out := map[string]any{}
			for k, x := range v {
				out[k] = walk(x)
			}
			return out
		case []any:
			out := make([]any, len(v))
			for i, x := range v {
				out[i] = walk(x)
			}
			return out
		}
		return v
	}
	out, _ := walk(args).(map[string]any)
	return out
}

// request sends command with args to d's adapter, and then runs with its
// answer, in a later callback.
func (m *Module) request(d *session, command string, args any, then answer) {
	m.seq++
	id := m.seq
	m.answers[id] = then
	post(d, func(c *dap.Client) {
		c.Request(command, args, func(body json.RawMessage, err error) {
			l := line{Kind: "answer", ID: id, Body: body}
			if err != nil {
				l.Error = err.Error()
			}
			d.emit(l)
		})
	})
}

// post hands f to Work to run on the client, unless Work has returned.
func post(d *session, f func(*dap.Client)) {
	select {
	case d.todo <- f:
	case <-d.gone:
	}
}

// event handles an event of d's adapter.
func (m *Module) event(res *api.Result, s api.Scope, d *session, name string, body json.RawMessage) {
	if d != m.run || d.ended {
		return
	}
	read := func(v any) bool {
		if err := json.Unmarshal(body, v); err != nil {
			res.Message = fmt.Sprintf("the debug adapter sent a bad %s: %v", name, err)
			return false
		}
		return true
	}
	switch name {
	case "initialized":
		// Breakpoints, then exception breakpoints with no filters, which some
		// adapters will not run without, then configurationDone.
		sent := map[int]bool{}
		for _, b := range m.breaks {
			if !sent[b.buffer] {
				sent[b.buffer] = true
				m.sendBreaks(s, b.buffer)
			}
		}
		m.request(d, "setExceptionBreakpoints", map[string]any{"filters": []string{}}, func(*api.Result, api.Scope, json.RawMessage, error) {})
		m.request(d, "configurationDone", nil, func(res *api.Result, _ api.Scope, _ json.RawMessage, err error) {
			if err != nil {
				res.Message = err.Error()
			}
		})
	case "stopped":
		var st struct {
			Reason      string `json:"reason"`
			Description string `json:"description"`
			ThreadID    int    `json:"threadId"`
		}
		if !read(&st) {
			return
		}
		why := fmt.Sprintf("%s stopped: %s", d.name, cmp.Or(st.Description, st.Reason))
		if st.ThreadID != 0 {
			m.stoppedAt(d, st.ThreadID, why)
			return
		}
		m.request(d, "threads", nil, func(res *api.Result, _ api.Scope, body json.RawMessage, err error) {
			var t struct {
				Threads []struct {
					ID int `json:"id"`
				} `json:"threads"`
			}
			if err == nil {
				err = json.Unmarshal(body, &t)
			}
			if err != nil || len(t.Threads) == 0 {
				res.Message = why + ", in no thread fude could find"
				return
			}
			m.stoppedAt(d, t.Threads[0].ID, why)
		})
	case "continued":
		m.running(res, s, d)
	case "output":
		var o struct {
			Category string `json:"category"`
			Output   string `json:"output"`
		}
		if !read(&o) || o.Category == "telemetry" {
			return
		}
		d.output = append(d.output, strings.Split(strings.TrimSuffix(o.Output, "\n"), "\n")...)
		d.output = d.output[max(len(d.output)-100, 0):]
		m.draw(res, s)
	case "terminated":
		m.end(res, s, d, d.name+" ended")
	case "exited":
		var x struct {
			Code int `json:"exitCode"`
		}
		if read(&x) {
			m.end(res, s, d, fmt.Sprintf("%s exited with code %d", d.name, x.Code))
		}
	}
}

// stoppedAt takes thread as the stopped one, and asks for its stack, whose
// top frame then shows.
func (m *Module) stoppedAt(d *session, thread int, why string) {
	d.thread = thread
	m.request(d, "stackTrace", map[string]any{"threadId": thread, "startFrame": 0, "levels": 64}, func(res *api.Result, s api.Scope, body json.RawMessage, err error) {
		var st struct {
			Frames []frame `json:"stackFrames"`
		}
		if err == nil {
			err = json.Unmarshal(body, &st)
		}
		if m.run != d || d.thread != thread {
			return
		}
		if err != nil {
			res.Message = why + ", with no stack: " + err.Error()
			return
		}
		d.frames, d.frame, d.scopes = st.Frames, 0, nil
		clear(d.open)
		clear(d.members)
		m.showFrame(res, s, d)
		m.fetchScopes(d)
		m.draw(res, s)
		res.Message = why
	})
}

// showFrame moves the cursor to the chosen frame's line, in a window that
// shows a file, and lights it. A file not open yet is lit once it is
// (B262).
func (m *Module) showFrame(res *api.Result, s api.Scope, d *session) {
	m.unlight(res, d)
	if d.frame >= len(d.frames) {
		return
	}
	f := d.frames[d.frame]
	if f.Source.Path == "" {
		res.Message = "the frame " + f.Name + " has no source"
		return
	}
	res.Jump = &api.Jump{Path: f.Source.Path, Line: f.Line, Column: max(f.Column, 1)}
	if !m.relight(res, s) {
		res.Timers = append(res.Timers, api.Timer{ID: light})
	}
}

// relight lights the chosen frame's line, with → in the sign column over
// any breakpoint there, and reports whether its file is open.
func (m *Module) relight(res *api.Result, s api.Scope) bool {
	d := m.run
	if d == nil || d.frame >= len(d.frames) {
		return false
	}
	m.unlight(res, d)
	f := d.frames[d.frame]
	for id, b := range s.Buffers {
		if !samePath(b.Path, f.Source.Path) {
			continue
		}
		t := b.Text
		start := t.LineStart(min(max(f.Line-1, 0), t.Lines()-1))
		end := t.Len()
		if l := t.LineOf(start); l+1 < t.Lines() {
			end = t.LineStart(l+1) - 1
		}
		m.ids++
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Buffer: id, Start: start, End: end, Style: "debug.line", Sign: "→", Layer: 2, Line: true})
		d.lit = &lit{m.ids, id}
		return true
	}
	return false
}

// unlight takes the light and the arrow off the stopped line.
func (m *Module) unlight(res *api.Result, d *session) {
	if d.lit != nil {
		res.Undecorate = append(res.Undecorate, d.lit.id)
		d.lit = nil
	}
}

// samePath reports whether a and b name the same file.
func samePath(a, b string) bool {
	x, _ := filepath.Abs(a)
	y, _ := filepath.Abs(b)
	return x == y
}

// running notes that d's program runs again.
func (m *Module) running(res *api.Result, s api.Scope, d *session) {
	d.thread, d.frames, d.scopes = 0, nil, nil
	m.unlight(res, d)
	m.draw(res, s)
}

// end ends d, saying why, when it is the session running, and lets its
// adapter's process go a second later. Its breakpoints show as not taken
// again.
func (m *Module) end(res *api.Result, s api.Scope, d *session, why string) {
	if d == nil || d != m.run || d.ended {
		return
	}
	d.ended = true
	m.running(res, s, d)
	for id, b := range m.breaks {
		if b.verified {
			b.verified = false
			m.redraw(res, s, id)
		}
	}
	if why != "" {
		res.Message = why
	}
	m.run = nil
	post(d, func(c *dap.Client) { time.AfterFunc(time.Second, c.Close) })
}

// stop ends the debug session, as :debug stop does (B262).
func (m *Module) stop(res *api.Result, s api.Scope) {
	d := m.run
	if d == nil {
		res.Message = "no debug session runs"
		return
	}
	// The session ends whatever the adapter answers.
	m.request(d, "disconnect", map[string]any{"terminateDebuggee": true}, func(*api.Result, api.Scope, json.RawMessage, error) {})
	m.end(res, s, d, "stopped debugging "+d.name)
}

// step lets the stopped thread go on, or steps over, into or out of its
// line (B263).
func (m *Module) step(res *api.Result, s api.Scope, how string) {
	d := m.run
	switch {
	case d == nil && how == "continue":
		m.start(res, s, "")
		return
	case d == nil:
		res.Message = "no debug session runs"
		return
	case d.thread == 0:
		res.Message = d.name + " is not stopped"
		return
	}
	cmd := map[string]string{"continue": "continue", "next": "next", "in": "stepIn", "out": "stepOut"}[how]
	m.request(d, cmd, map[string]any{"threadId": d.thread}, func(res *api.Result, _ api.Scope, _ json.RawMessage, err error) {
		if err != nil {
			res.Message = err.Error()
		}
	})
	m.running(res, s, d)
}

// toggle puts a breakpoint on the cursor's line or takes one off, and
// tells a running session (B262).
func (m *Module) toggle(res *api.Result, s api.Scope, cursor int) {
	if s.About["path"] == "" {
		res.Message = "a breakpoint needs a file"
		return
	}
	buffer, _ := strconv.Atoi(s.About["buffer"])
	line := s.Text.LineOf(cursor)
	for _, d := range s.Decorations() {
		if _, ok := m.breaks[d.ID]; ok && d.Buffer == buffer && s.Text.LineOf(d.Start) == line {
			res.Undecorate = append(res.Undecorate, d.ID)
			delete(m.breaks, d.ID)
			m.sendBreaks(s, buffer)
			return
		}
	}
	m.ids++
	m.breaks[m.ids] = &brk{buffer: buffer}
	res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Buffer: buffer, Start: s.Text.LineStart(line), End: s.Text.LineStart(line), Style: "debug.pending", Sign: "○", Layer: 2})
	m.sendBreaks(s, buffer)
}

// redraw shows the breakpoint id as taken or not, where it is now.
func (m *Module) redraw(res *api.Result, s api.Scope, id int) {
	b := m.breaks[id]
	i := slices.IndexFunc(s.Decorations(), func(d api.Decoration) bool { return d.ID == id })
	if i < 0 {
		return
	}
	at := s.Decorations()[i].Start
	res.Undecorate = append(res.Undecorate, id)
	delete(m.breaks, id)
	m.ids++
	m.breaks[m.ids] = b
	sign, style := "○", "debug.pending"
	if b.verified {
		sign, style = "●", "debug.break"
	}
	res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Buffer: b.buffer, Start: at, End: at, Style: style, Sign: sign, Layer: 2})
}

// sendBreaks gives a running session the breakpoints of buffer, and marks
// those the adapter took.
func (m *Module) sendBreaks(s api.Scope, buffer int) {
	d := m.run
	if d == nil {
		return
	}
	b, ok := s.Buffers[buffer]
	if !ok {
		return
	}
	path, _ := filepath.Abs(b.Path)
	var lines []map[string]int
	var sent []int
	for _, dc := range s.Decorations() {
		if _, ok := m.breaks[dc.ID]; ok && dc.Buffer == buffer {
			lines = append(lines, map[string]int{"line": b.Text.LineOf(dc.Start) + 1})
			sent = append(sent, dc.ID)
		}
	}
	m.request(d, "setBreakpoints", map[string]any{"source": map[string]string{"path": path}, "breakpoints": lines}, func(res *api.Result, s api.Scope, body json.RawMessage, err error) {
		var r struct {
			Breakpoints []struct {
				Verified bool `json:"verified"`
			} `json:"breakpoints"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		if err != nil {
			res.Message = "the debug adapter took no breakpoints: " + err.Error()
			return
		}
		for i, taken := range r.Breakpoints {
			if i < len(sent) {
				if b, ok := m.breaks[sent[i]]; ok && b.verified != taken.Verified {
					b.verified = taken.Verified
					m.redraw(res, s, sent[i])
				}
			}
		}
		// The stopped line's sign stays over a breakpoint drawn anew.
		if d.lit != nil && len(res.Decorate) > 0 {
			m.relight(res, s)
		}
	})
}

// eval shows the value of expr at the chosen frame, in the message, or
// with float in a float at the cursor (B265).
func (m *Module) eval(res *api.Result, expr string, float bool) {
	d := m.run
	switch {
	case d == nil:
		res.Message = "no debug session runs"
		return
	case expr == "":
		res.Message = "evaluate what? :debug eval expr"
		return
	}
	args := map[string]any{"expression": expr, "context": map[bool]string{true: "hover", false: "repl"}[float]}
	if d.frame < len(d.frames) {
		args["frameId"] = d.frames[d.frame].ID
	}
	m.request(d, "evaluate", args, func(res *api.Result, _ api.Scope, body json.RawMessage, err error) {
		var r struct {
			Result string `json:"result"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		switch {
		case err != nil:
			res.Message = expr + ": " + err.Error()
		case float:
			res.Float = &api.Float{Text: r.Result}
		default:
			res.Message = expr + " = " + r.Result
		}
	})
}

// evalHere shows the value of the selection, or of the name under the
// cursor with its dots, in a float, as <Leader>de does (B265).
func (m *Module) evalHere(res *api.Result, s api.Scope, args []int) {
	t := s.Text
	if s.About["mode"] == "visual" {
		m.eval(res, t.Slice(args[0], args[1]), true)
		return
	}
	c := args[0]
	ls := t.LineStart(t.LineOf(c))
	le := t.Len()
	if l := t.LineOf(c); l+1 < t.Lines() {
		le = t.LineStart(l+1) - 1
	}
	line, at := t.Slice(ls, le), c-ls
	name := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' }
	from := strings.LastIndexFunc(line[:at], func(r rune) bool { return !name(r) }) + 1
	to := strings.IndexFunc(line[at:], func(r rune) bool { return !name(r) })
	if to < 0 {
		to = len(line) - at
	}
	m.eval(res, line[from:at+to], true)
}

// showPanel opens the panel of the session's frames, variables and
// output, as :debug panel does (B264).
func (m *Module) showPanel(res *api.Result, s api.Scope) {
	d := m.run
	if d == nil {
		res.Message = "no debug session runs"
		return
	}
	m.panel = true
	m.draw(res, s)
	if d.thread != 0 {
		m.fetchScopes(d)
	}
}

// fetchScopes asks for the chosen frame's scopes, the first of which shows
// open, and then for the members of each that shows open.
func (m *Module) fetchScopes(d *session) {
	if d.frame >= len(d.frames) || !m.panel {
		return
	}
	id := d.frames[d.frame].ID
	m.request(d, "scopes", map[string]any{"frameId": id}, func(res *api.Result, s api.Scope, body json.RawMessage, err error) {
		var r struct {
			Scopes []variable `json:"scopes"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		if m.run != d || d.frame >= len(d.frames) || d.frames[d.frame].ID != id {
			return
		}
		if err != nil {
			res.Message = "the frame's scopes: " + err.Error()
			return
		}
		d.scopes = r.Scopes
		if len(d.scopes) > 0 && len(d.open) == 0 {
			d.open[d.scopes[0].Ref] = true
		}
		for ref := range d.open {
			m.fetchMembers(d, ref)
		}
		m.draw(res, s)
	})
}

// fetchMembers asks for the members of the variable or scope ref names.
func (m *Module) fetchMembers(d *session, ref int) {
	m.request(d, "variables", map[string]any{"variablesReference": ref}, func(res *api.Result, s api.Scope, body json.RawMessage, err error) {
		var r struct {
			Variables []variable `json:"variables"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		if m.run != d {
			return
		}
		if err != nil {
			res.Message = "the variables: " + err.Error()
			return
		}
		d.members[ref] = r.Variables
		m.draw(res, s)
	})
}

// draw shows the panel, when it is open: the frames, the chosen one
// marked, the chosen frame's scopes and variables, open ones with their
// members under them, and the program's output.
func (m *Module) draw(res *api.Result, s api.Scope) {
	d := m.run
	if !m.panel || d == nil {
		return
	}
	rows := []row{{"Frames", -1, 0}}
	for i, f := range d.frames {
		mark := "  "
		if i == d.frame {
			mark = "→ "
		}
		rows = append(rows, row{fmt.Sprintf("%s%s  %s:%d", mark, f.Name, filepath.Base(f.Source.Path), f.Line), i, 0})
	}
	if d.thread == 0 {
		rows = append(rows, row{"  not stopped", -1, 0})
	}
	rows = append(rows, row{"Variables", -1, 0})
	var add func(vs []variable, depth int)
	add = func(vs []variable, depth int) {
		for _, v := range vs {
			sign := "  "
			if v.Ref > 0 {
				sign = map[bool]string{true: "▾ ", false: "▸ "}[d.open[v.Ref]]
			}
			text := strings.Repeat("  ", depth) + sign + v.Name
			if value, _, _ := strings.Cut(v.Value, "\n"); value != "" {
				text += " = " + value
			}
			rows = append(rows, row{text, -1, v.Ref})
			if v.Ref > 0 && d.open[v.Ref] {
				add(d.members[v.Ref], depth+1)
			}
		}
	}
	add(d.scopes, 0)
	rows = append(rows, row{"Output", -1, 0})
	for _, l := range d.output {
		rows = append(rows, row{"  " + l, -1, 0})
	}
	m.rows = rows
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r.text
	}
	res.Show = &api.Panel{Title: "[debug]", Text: strings.Join(lines, "\n"), Keys: panelKeys}
}

// openRow acts on the panel's line: a frame is chosen and shows its line,
// and a variable with members shows or hides them (B264).
func (m *Module) openRow(res *api.Result, s api.Scope, line int) {
	d := m.run
	if d == nil || s.About["title"] != "[debug]" || line >= len(m.rows) {
		return
	}
	switch r := m.rows[line]; {
	case r.frame >= 0:
		d.frame, d.scopes = r.frame, nil
		clear(d.open)
		clear(d.members)
		m.showFrame(res, s, d)
		m.fetchScopes(d)
		m.draw(res, s)
	case r.ref > 0:
		d.open[r.ref] = !d.open[r.ref]
		if d.open[r.ref] && d.members[r.ref] == nil {
			m.fetchMembers(d, r.ref)
		}
		m.draw(res, s)
	}
}
