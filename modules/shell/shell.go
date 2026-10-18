// Package shell runs commands in the user's shell: :!cmd shows what one
// writes, :{range}!cmd puts the range's lines through it, and :r !cmd puts
// what it writes below the line, as :r file puts a file's text (B156).
package shell

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// What shell registers, by the ids Call runs them by.
const (
	shell uint32 = iota + 1
	read
)

// Module is shell. It keeps what each command that runs is for.
type Module struct {
	mu   sync.Mutex
	jobs uint32
	runs map[uint32]run // by job
}

// run is a command that runs for the buffer buf. It reads the text from
// start to end, or none. Its output replaces that text, goes below it, or
// shows.
type run struct {
	buf, cmd, text string
	start, end     int
	filter, below  bool
}

// New returns shell.
func New() *Module { return &Module{runs: map[uint32]run{}} }

func (m *Module) Name() string { return "shell" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "!", Help: "runs cmd in the shell, or with a range puts the lines through it", ID: shell, Host: true},
		{Kind: api.Command, Name: "read", Short: []string{"r"}, Args: "{file} | !{cmd}", Paths: true, Help: "puts a file, or what cmd writes, below the line", ID: read},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs :! or :r.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == read {
		return m.read(s), nil
	}
	return m.shell(s), nil
}

// read puts the text of the file :r names below the range's last line.
// With !cmd it puts what the command writes there, as :! with a bang
// does, which a guest may not run (B156, B244).
func (m *Module) read(s api.Scope) api.Result {
	arg := strings.TrimSpace(s.About["args"])
	last, _ := strconv.Atoi(s.About["last"])
	t := s.Text
	switch cmd, run := strings.CutPrefix(arg, "!"); {
	case arg == "":
		return api.Result{Message: "no file name"}
	case run && s.About["power"] != "host":
		return api.Result{Message: "a guest may not run :!"}
	case run:
		s.About["args"], s.About["bang"] = cmd, "true"
		s.About["first"] = strconv.Itoa(t.LineStart(t.LineOf(last)))
		return m.shell(s)
	}
	text, err := os.ReadFile(arg)
	if err != nil {
		return api.Result{Message: err.Error()}
	}
	if out := strings.TrimSuffix(string(text), "\n"); out != "" {
		at := lineEnd(t, last)
		return api.Result{Edits: []api.Edit{{Start: at, End: at, Text: "\n" + out}}}
	}
	return api.Result{}
}

// lineEnd is where the line holding pos ends, before its line break.
func lineEnd(t api.Text, pos int) int {
	if l := t.LineOf(pos); l+1 < t.Lines() {
		return t.LineStart(l+1) - 1
	}
	return t.Len()
}

// shell starts the command typed after :!, off the loop. With a range it
// reads those lines. With a bang, as :r !cmd runs it, what it writes goes
// below the range's last line.
func (m *Module) shell(s api.Scope) api.Result {
	cmd := strings.TrimSpace(s.About["args"])
	if cmd == "" {
		return api.Result{Message: "no command"}
	}
	t := s.Text
	first, _ := strconv.Atoi(s.About["first"])
	last, _ := strconv.Atoi(s.About["last"])
	end := lineEnd(t, last)
	r := run{buf: s.About["buffer"], cmd: cmd, start: first, end: end}
	input := ""
	switch {
	case s.About["bang"] == "true":
		r.start, r.below = end, true
	case s.About["ranged"] == "true":
		r.text, r.filter = t.Slice(first, end), true
		input = r.text + "\n"
	}
	m.jobs++
	m.runs[m.jobs] = r
	return api.Result{Jobs: []api.Job{{ID: m.jobs, Argv: []string{cmd}, Input: input}}}
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback puts what the command wrote where its run says. A filter's
// output replaces the lines only while they are as it read them.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	delete(m.runs, id)
	if !ok || r.buf != s.About["buffer"] {
		return api.Result{}, nil
	}
	if err := s.About["error"]; err != "" {
		return api.Result{Message: err}, nil
	}
	out := strings.TrimSuffix(s.About["output"], "\n")
	switch {
	case r.filter && (r.end > s.Text.Len() || s.Text.Slice(r.start, r.end) != r.text):
		return api.Result{Message: "the text changed while " + r.cmd + " ran"}, nil
	case r.filter:
		return api.Result{Edits: []api.Edit{{Start: r.start, End: r.end, Text: out}}}, nil
	case r.below && out == "":
		return api.Result{}, nil
	case r.below:
		return api.Result{Edits: []api.Edit{{Start: min(r.start, s.Text.Len()), End: min(r.start, s.Text.Len()), Text: "\n" + out}}}, nil
	case strings.Count(out, "\n") > 10:
		return api.Result{Show: &api.Panel{Title: "[!" + r.cmd + "]", Text: out}}, nil
	}
	return api.Result{Message: out}, nil
}

// Work runs the command in the user's shell, with its input on stdin.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	c := exec.CommandContext(ctx, cmp.Or(os.Getenv("SHELL"), "sh"), "-c", j.Argv[0])
	c.Stdin = strings.NewReader(j.Input)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", j.Argv[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func (m *Module) Close(context.Context) error { return nil }
