package editor

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/dap"
	"github.com/y3owk1n/fude/vim"
)

// launch is a [launch.{name}] table of .fude/launch.toml (B261).
type launch struct {
	Adapter string         `toml:"adapter"`
	Request string         `toml:"request"`
	Args    map[string]any `toml:"args"`
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
		return nil, decodeError(path, err)
	}
	return f.Launch, nil
}

// breakpoint is a line a debug session stops on, which a mark at its start
// keeps as the text changes (B262).
type breakpoint struct {
	mark     kernel.MarkID
	verified bool // the adapter took it
}

// debugging is the editor's debug session (B262).
type debugging struct {
	client *dap.Client
	s      *Session // the session that started it, whose window shows where it stops
	name   string
	thread int // the stopped thread, 0 while the program runs
	frames []stackFrame
	frame  int // the chosen frame

	// The panel, and what it shows of the chosen frame (B264).
	panel   *buffer
	rows    []panelRow
	scopes  []variable
	members map[int][]variable // by reference, the members fetched
	open    map[int]bool       // the references that show their members
	output  []string           // the program's last lines
}

// stackFrame is a frame of the stopped thread, as stackTrace answers it.
type stackFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Source struct {
		Path string `json:"path"`
	} `json:"source"`
}

// stoppedLine owns the decoration that lights the line the program stopped
// on.
type stoppedLine struct{}

// debugCommands are :debug's subcommands.
var debugCommands = []string{"start", "stop", "break", "continue", "next", "in", "out", "panel", "eval"}

// completeDebug completes :debug's subcommand, and after start the names of
// the launch settings, which it reads off the loop (B262).
func completeDebug(e *Session, line string, start int, arg string, wait bool) (int, []string) {
	sub, rest, ok := strings.Cut(arg, " ")
	switch {
	case !ok:
		return start, match(debugCommands, sub)
	case sub != "start":
		return 0, nil
	}
	start += len(sub) + 1
	names := func() []string {
		// A file that fails to read gives no names, and :debug start says why.
		all, _ := launches()
		return match(slices.Sorted(maps.Keys(all)), rest)
	}
	if !wait {
		e.completeOff(line, start, names)
		return 0, nil
	}
	return start, names()
}

// debugCommand runs :debug's subcommand in arg (B262, B263).
func (e *Session) debugCommand(arg string) {
	sub, rest, _ := strings.Cut(arg, " ")
	switch sub {
	case "start":
		e.debugStart(strings.TrimSpace(rest))
	case "stop":
		e.debugStop()
	case "break":
		e.toggleBreak()
	case "continue", "next", "in", "out":
		e.debugStep(sub)
	case "panel":
		e.debugPanel()
	case "eval":
		e.debugEval(strings.TrimSpace(rest), false)
	default:
		e.message = "debug what? " + strings.Join(debugCommands, ", ")
	}
}

// debugStart starts the launch settings called name, or the only ones, or
// lists them to choose from (B262).
func (e *Session) debugStart(name string) {
	if e.debug != nil {
		e.message = "a debug session runs, and :debug stop ends it"
		return
	}
	all, err := launches()
	if err != nil {
		e.message = err.Error()
		return
	}
	names := slices.Sorted(maps.Keys(all))
	switch {
	case len(all) == 0:
		e.message = "no launch settings, so .fude/launch.toml should hold some"
	case name != "":
		l, ok := all[name]
		if !ok {
			e.message = "no launch settings called " + name
			return
		}
		e.launch(name, l)
	case len(all) == 1:
		e.launch(names[0], all[names[0]])
	default:
		e.pickFrom("launch", names, func(i int) { e.launch(names[i], all[names[i]]) })
	}
}

// launch starts a debug session of l, called name, with the adapter it
// names or the first of the current file's filetype (B261).
func (e *Session) launch(name string, l launch) {
	b := e.win.buf
	adapter := l.Adapter
	if adapter == "" {
		if names := e.cfg.Filetype[e.filetype(b)].Debug; len(names) > 0 {
			adapter = names[0]
		}
	}
	cfg, ok := e.cfg.Debug[adapter]
	switch {
	case adapter == "":
		e.message = fmt.Sprintf("no debug adapter for %s, so its filetype table's debug should name one", cmp.Or(e.filetype(b), "this file"))
		return
	case !ok:
		e.message = "no [debug." + adapter + "] table in config.toml"
		return
	}
	file, _ := filepath.Abs(b.file.path)
	args := expandVars(l.Args, map[string]string{"{file}": file, "{dir}": filepath.Dir(file), "{project}": e.project})
	d := &debugging{s: e, name: name, members: map[int][]variable{}, open: map[int]bool{}}
	c, err := dap.Start(adapter, cfg.Command, e.project, e.queue, logLine,
		func(text string) { api.Trace("dap", text, 0) },
		func(event string, body json.RawMessage) { e.debugEvent(d, event, body) },
		func(error) { e.debugEnd(d, "the debug adapter "+adapter+" ended") })
	if err != nil {
		e.message = err.Error()
		return
	}
	d.client, e.debug = c, d
	c.Request("initialize", map[string]any{
		"clientID": "fude", "clientName": "fude", "adapterID": adapter,
		"linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path",
	}, func(_ json.RawMessage, err error) {
		if err != nil {
			e.debugEnd(d, "the debug adapter "+adapter+" did not start: "+err.Error())
		}
	})
	// The answer may come only once configurationDone has, so nothing waits
	// on it.
	c.Request(cmp.Or(l.Request, "launch"), args, func(_ json.RawMessage, err error) {
		if err != nil {
			e.debugEnd(d, name+" did not start: "+err.Error())
		}
	})
	e.message = "starting " + name
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

// debugEvent handles an event of d's adapter.
func (e *Session) debugEvent(d *debugging, event string, body json.RawMessage) {
	if e.debug != d {
		return
	}
	read := func(v any) bool {
		if err := json.Unmarshal(body, v); err != nil {
			e.message = fmt.Sprintf("the debug adapter sent a bad %s: %v", event, err)
			return false
		}
		return true
	}
	switch event {
	case "initialized":
		// Breakpoints, then exception breakpoints with no filters, which some
		// adapters will not run without, then configurationDone.
		for _, b := range e.bufs {
			if len(b.breaks) > 0 {
				e.sendBreaks(b)
			}
		}
		// An adapter with no exception breakpoints may refuse, and runs on.
		d.client.Request("setExceptionBreakpoints", map[string]any{"filters": []string{}}, func(json.RawMessage, error) {})
		d.client.Request("configurationDone", nil, func(_ json.RawMessage, err error) {
			if err != nil {
				e.message = err.Error()
			}
		})
	case "stopped":
		var s struct {
			Reason      string `json:"reason"`
			Description string `json:"description"`
			ThreadID    int    `json:"threadId"`
		}
		if !read(&s) {
			return
		}
		why := fmt.Sprintf("%s stopped: %s", d.name, cmp.Or(s.Description, s.Reason))
		if s.ThreadID != 0 {
			e.stoppedAt(d, s.ThreadID, why)
			return
		}
		d.client.Request("threads", nil, func(body json.RawMessage, err error) {
			var t struct {
				Threads []struct {
					ID int `json:"id"`
				} `json:"threads"`
			}
			if err == nil {
				err = json.Unmarshal(body, &t)
			}
			if err != nil || len(t.Threads) == 0 {
				e.message = why + ", in no thread fude could find"
				return
			}
			e.stoppedAt(d, t.Threads[0].ID, why)
		})
	case "continued":
		e.running(d)
	case "output":
		e.debugOutput(d, body)
	case "terminated":
		e.debugEnd(d, d.name+" ended")
	case "exited":
		var x struct {
			Code int `json:"exitCode"`
		}
		if read(&x) {
			e.debugEnd(d, fmt.Sprintf("%s exited with code %d", d.name, x.Code))
		}
	}
}

// stoppedAt takes thread as the stopped one, and shows its top frame.
func (e *Session) stoppedAt(d *debugging, thread int, why string) {
	d.thread = thread
	d.client.Request("stackTrace", map[string]any{"threadId": thread, "startFrame": 0, "levels": 64}, func(body json.RawMessage, err error) {
		var st struct {
			Frames []stackFrame `json:"stackFrames"`
		}
		if err == nil {
			err = json.Unmarshal(body, &st)
		}
		if e.debug != d || d.thread != thread {
			return
		}
		if err != nil {
			e.message = why + ", with no stack: " + err.Error()
			return
		}
		d.frames, d.frame, d.scopes = st.Frames, 0, nil
		clear(d.open)
		clear(d.members)
		e.showFrame(d)
		e.fetchScopes(d)
		e.drawPanel(d)
		d.s.message = why
	})
}

// showFrame shows the chosen frame's line in d's session's window, lit
// and with → in the sign column (B262).
func (e *Session) showFrame(d *debugging) {
	e.clearStopped()
	if d.frame >= len(d.frames) {
		return
	}
	f := d.frames[d.frame]
	s := d.s
	// The line shows in a window other than the panel's.
	if s.win.buf == d.panel {
		if i := slices.IndexFunc(s.root.windows(), func(w *window) bool { return w.buf != d.panel }); i >= 0 {
			s.win = s.root.windows()[i]
		}
	}
	if f.Source.Path == "" {
		s.message = "the frame " + f.Name + " has no source"
		return
	}
	if s.edit(f.Source.Path); !s.win.buf.is(f.Source.Path) {
		return
	}
	b, t := s.win.buf, s.Text()
	start := t.LineStart(min(max(f.Line-1, 0), t.Lines()-1))
	end := vim.LineEnd(t, start)
	s.setCursor(min(start+max(f.Column-1, 0), end))
	s.reveal()
	b.stopped = true
	e.decos[stoppedLine{}] = []decoration{{buf: b, start: b.text.SetMark(start, kernel.Left), end: b.text.SetMark(end, kernel.Right), style: "debug.line"}}
}

// clearStopped takes the light and the arrow off the stopped line.
func (e *Session) clearStopped() {
	for _, d := range e.decos[stoppedLine{}] {
		d.buf.stopped = false
		d.remove()
	}
	delete(e.decos, stoppedLine{})
}

// running notes that d's program runs again.
func (e *Session) running(d *debugging) {
	d.thread, d.frames, d.scopes = 0, nil, nil
	e.clearStopped()
	e.drawPanel(d)
}

// debugEnd ends d, saying why, when it is still the editor's session.
func (e *Session) debugEnd(d *debugging, why string) {
	if e.debug != d {
		return
	}
	e.debug = nil
	e.running(d)
	for _, b := range e.bufs {
		for i := range b.breaks {
			b.breaks[i].verified = false
		}
	}
	d.s.message = why
	// fude ends an adapter's process that still runs a second later.
	go func() {
		time.Sleep(time.Second)
		d.client.Close()
	}()
}

// debugStop ends the debug session, as :debug stop does (B262).
func (e *Session) debugStop() {
	d := e.debug
	if d == nil {
		e.message = "no debug session runs"
		return
	}
	// The session ends whatever the adapter answers.
	d.client.Request("disconnect", map[string]any{"terminateDebuggee": true}, func(json.RawMessage, error) {})
	e.debugEnd(d, "stopped debugging "+d.name)
}

// debugStep lets the stopped thread go on, or steps over, into or out of
// its line (B263).
func (e *Session) debugStep(how string) {
	d := e.debug
	switch {
	case d == nil && how == "continue":
		e.debugStart("")
		return
	case d == nil:
		e.message = "no debug session runs"
		return
	case d.thread == 0:
		e.message = d.name + " is not stopped"
		return
	}
	command := map[string]string{"continue": "continue", "next": "next", "in": "stepIn", "out": "stepOut"}[how]
	d.client.Request(command, map[string]any{"threadId": d.thread}, func(_ json.RawMessage, err error) {
		if err != nil {
			e.message = err.Error()
		}
	})
	e.running(d)
}

// toggleBreak puts a breakpoint on the cursor's line or takes one off, and
// tells a running session (B262).
func (e *Session) toggleBreak() {
	b, t := e.win.buf, e.Text()
	if b.file.path == "" {
		e.message = "a breakpoint needs a file"
		return
	}
	line := t.LineOf(e.Cursor())
	i := slices.IndexFunc(b.breaks, func(br breakpoint) bool { return t.LineOf(b.text.Mark(br.mark)) == line })
	if i >= 0 {
		b.text.DeleteMark(b.breaks[i].mark)
		b.breaks = slices.Delete(b.breaks, i, i+1)
	} else {
		b.breaks = append(b.breaks, breakpoint{mark: b.text.SetMark(t.LineStart(line), kernel.Left)})
	}
	if e.debug != nil {
		e.sendBreaks(b)
	}
}

// sendBreaks gives the session b's breakpoints, and marks those the
// adapter took.
func (e *Session) sendBreaks(b *buffer) {
	d, t := e.debug, b.text.Snapshot()
	path, _ := filepath.Abs(b.file.path)
	lines := make([]map[string]int, len(b.breaks))
	for i, br := range b.breaks {
		lines[i] = map[string]int{"line": t.LineOf(b.text.Mark(br.mark)) + 1}
	}
	sent := slices.Clone(b.breaks)
	d.client.Request("setBreakpoints", map[string]any{"source": map[string]string{"path": path}, "breakpoints": lines}, func(body json.RawMessage, err error) {
		var r struct {
			Breakpoints []struct {
				Verified bool `json:"verified"`
			} `json:"breakpoints"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		if err != nil {
			e.message = "the debug adapter took no breakpoints: " + err.Error()
			return
		}
		for i, br := range r.Breakpoints {
			if i < len(sent) {
				if j := slices.IndexFunc(b.breaks, func(x breakpoint) bool { return x.mark == sent[i].mark }); j >= 0 {
					b.breaks[j].verified = br.Verified
				}
			}
		}
	})
}

// debugEval shows the value of expr at the chosen frame, in the message, or
// with float in a float at the cursor, as :debug eval and <Leader>de do
// (B265).
func (e *Session) debugEval(expr string, float bool) {
	d := e.debug
	switch {
	case d == nil:
		e.message = "no debug session runs"
		return
	case expr == "":
		e.message = "evaluate what? :debug eval expr"
		return
	}
	args := map[string]any{"expression": expr, "context": map[bool]string{true: "hover", false: "repl"}[float]}
	if d.frame < len(d.frames) {
		args["frameId"] = d.frames[d.frame].ID
	}
	d.client.Request("evaluate", args, func(body json.RawMessage, err error) {
		var r struct {
			Result string `json:"result"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		switch {
		case err != nil:
			e.message = expr + ": " + err.Error()
		case float:
			e.float, e.floatSpans = r.Result, nil
		default:
			e.message = expr + " = " + r.Result
		}
	})
}

// evalHere shows the value of the selection, or of the name under the
// cursor with its dots, in a float, as <Leader>de does (B265).
func (e *Session) evalHere() {
	t := e.Text()
	if sel, ok := e.Selection(); ok {
		e.debugEval(t.Slice(sel.Start, min(sel.End, t.Len())), true)
		return
	}
	name := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' }
	start, end := vim.LineStart(t, e.Cursor()), vim.LineEnd(t, e.Cursor())
	line, at := t.Slice(start, end), e.Cursor()-start
	from := strings.LastIndexFunc(line[:at], func(r rune) bool { return !name(r) }) + 1
	to := strings.IndexFunc(line[at:], func(r rune) bool { return !name(r) })
	if to < 0 {
		to = len(line) - at
	}
	e.debugEval(line[from:at+to], true)
}
