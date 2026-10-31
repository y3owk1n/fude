// Package task runs a project's tasks in a terminal window: config.toml's
// [task.<name>] tables and .fude/tasks.toml's, the justfile's recipes, the
// Makefile's targets and package.json's scripts (B228, B230, B231, B233).
package task

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/tomlerr"
	"github.com/y3owk1n/fude/internal/trace"
)

// The command and source task registers, by the ids Call runs them by.
const (
	run uint32 = iota + 1
	pick
)

// picked is the id of the list of tasks, which runs the one picked.
const picked = 1

// The ids of the job that reads the start tasks, and of the question
// whether to run them, past those of the searches for the tasks (B605).
const (
	starts uint32 = 1<<31 + iota
	allow
)

// Module is task.
type Module struct {
	mu      sync.Mutex
	last    *task             // the task :task alone runs again
	running map[string]bool   // by name, the tasks whose programs run
	asks    map[uint32]ask    // by job, what the tasks it finds are for
	shown   []task            // the tasks the list shows
	vars    *strings.Replacer // the {file}, {dir}, {line} and {word} of the call the list is for
	jobs    uint32
	waiting []task // the start tasks the question asks to run
	hash    string // their hash, kept with the folder once allowed
}

// task is a command a project or config.toml names. Start runs it as
// fude starts (B605).
type task struct {
	Name, Source, Command, Doc string
	Start                      bool
}

// ask is what a search for the tasks is for, and the call that asked.
type ask struct {
	id   uint32 // run, pick, or 0 to complete :task
	name string // the task :task names
	vars *strings.Replacer
}

// New returns task.
func New() *Module { return &Module{running: map[string]bool{}, asks: map[uint32]ask{}} }

func (m *Module) Name() string         { return "task" }
func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "task", Help: "runs the task named in a terminal window below, or the last one run again", ID: run, Host: true, Complete: true},
		{Kind: api.Source, Name: "tasks", Help: "the project's tasks, and runs the one picked", ID: pick, Host: true},
	}
}

// Init reads the project's start tasks off the loop, unless fude runs with
// --clean (B605).
func (m *Module) Init(context.Context, api.Scope) (api.Result, error) {
	if keep.Clean != "" {
		return api.Result{}, nil
	}
	return api.Result{Jobs: []api.Job{{ID: starts, Argv: []string{"starts"}}}}, nil
}

// Call starts a job that finds the tasks for :task, :pick tasks or
// completing :task, as finding them may run just. :task alone runs the
// last task again.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := ask{id: id, name: s.About["args"], vars: vars(s, args[0])}
	if _, ok := s.About["complete"]; ok {
		a.id = 0
	} else if id == run && a.name == "" {
		if m.last == nil {
			return api.Result{Error: "no task has run"}, nil
		}
		return m.start(*m.last, a.vars), nil
	}
	config, _ := s.Config("task")
	m.jobs++
	m.asks[picked+m.jobs] = a
	return api.Result{Jobs: []api.Job{{ID: picked + m.jobs, Argv: []string{"tasks", config}}}}, nil
}

// Event tells of a task that ended, and makes the places its output names
// the last list.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, task, _ := strings.Cut(name, " ")
	delete(m.running, task)
	// Its end is worth a notification while the user is away (B564).
	res := api.Result{Message: fmt.Sprintf("the %s task ended in %s", task, s.About["took"]), Alert: true}
	if status := s.About["status"]; status != "0" {
		res.Message = fmt.Sprintf("the %s task failed with %s after %s", task, status, s.About["took"])
	}
	if items := places(s.Text); len(items) > 0 {
		res.Places = &api.Places{Title: "task " + task, Items: items}
		res.Message += fmt.Sprintf(". It named %d places, and ]q goes to the first", len(items))
	}
	return res, nil
}

// Callback answers a search for the tasks, or runs the task picked.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch id {
	case starts:
		return m.readStarts(s), nil
	case allow:
		if s.About["yes"] != "true" {
			return api.Result{}, nil
		}
		if err := allowStarts(m.hash); err != nil {
			return api.Result{Error: err.Error()}, nil
		}
		return m.startAll(m.waiting, vars(s, 0)), nil
	}
	if id == picked {
		i, _ := strconv.Atoi(s.About["picked"])
		if s.About["ok"] != "true" || i < 0 || i >= len(m.shown) {
			return api.Result{}, nil
		}
		return m.start(m.shown[i], m.vars), nil
	}
	a, ok := m.asks[id]
	if !ok {
		return api.Result{}, nil
	}
	delete(m.asks, id)
	var ts []task
	if out := s.About["output"]; out != "" {
		if err := json.Unmarshal([]byte(out), &ts); err != nil {
			return api.Result{Error: "tasks: " + err.Error()}, nil
		}
	}
	res := api.Result{Error: s.About["error"]}
	switch a.id {
	case 0:
		for _, t := range ts {
			res.Complete = append(res.Complete, t.Name)
		}
		res.Error = ""
	case pick:
		if len(ts) == 0 {
			res.Error = cmp.Or(res.Error, "no tasks")
			return res, nil
		}
		items := make([]string, len(ts))
		for i, t := range ts {
			source := t.Source
			if m.running[t.Name] {
				source += ", running"
			}
			items[i] = strings.TrimSpace(t.Name + "  " + source + "  " + t.Doc)
		}
		m.shown, m.vars = ts, a.vars
		res.Pick = &api.Pick{ID: picked, Title: "tasks", Items: items}
	case run:
		i := slices.IndexFunc(ts, func(t task) bool { return t.Name == a.name })
		if i < 0 {
			res.Error = cmp.Or(res.Error, "no task named "+a.name)
			return res, nil
		}
		return m.start(ts[i], a.vars), nil
	}
	return res, nil
}

// Work finds the tasks, config.toml's given as JSON in the job's second
// argument, and returns them as JSON, with why any failed to read.
func (m *Module) Work(_ context.Context, j api.Job) (string, error) {
	if j.Argv[0] == "starts" {
		return startTasks()
	}
	var config map[string]struct{ Command string }
	if err := json.Unmarshal([]byte(cmp.Or(j.Argv[1], "null")), &config); err != nil {
		return "", fmt.Errorf("config.toml's tasks: %w", err)
	}
	ts, err := find(config)
	out, jerr := json.Marshal(ts)
	return string(out), errors.Join(err, jerr)
}

func (m *Module) Close(context.Context) error { return nil }

// start runs t in a terminal window, with vars in its command, or says it
// runs and shows its window again.
func (m *Module) start(t task, vars *strings.Replacer) api.Result {
	res := api.Result{Terminals: []api.Terminal{{Unnumbered: true, Name: t.Name, Command: vars.Replace(t.Command)}}}
	if m.running[t.Name] {
		res.Message = "the " + t.Name + " task is running"
	}
	m.last, m.running[t.Name] = &t, true
	return res
}

// readStarts runs the start tasks the job read, or asks whether to when
// the user has not allowed them in this folder (B605).
func (m *Module) readStarts(s api.Scope) api.Result {
	if s.About["error"] != "" {
		return api.Result{Error: s.About["error"]}
	}
	var r starting
	if err := json.Unmarshal([]byte(s.About["output"]), &r); err != nil {
		return api.Result{Error: "start tasks: " + err.Error()}
	}
	if len(r.Tasks) == 0 {
		return api.Result{}
	}
	if r.Allowed {
		return m.startAll(r.Tasks, vars(s, 0))
	}
	m.waiting, m.hash = r.Tasks, r.Hash
	runs := make([]string, len(r.Tasks))
	for i, t := range r.Tasks {
		runs[i] = t.Name + " runs " + t.Command
	}
	return api.Result{Confirm: &api.Confirm{ID: allow, Question: "run the start tasks of .fude/tasks.toml? " + strings.Join(runs, ", ")}}
}

// startAll runs ts, each with its window hidden.
func (m *Module) startAll(ts []task, vars *strings.Replacer) api.Result {
	var res api.Result
	for _, t := range ts {
		if m.running[t.Name] {
			continue
		}
		m.running[t.Name] = true
		res.Terminals = append(res.Terminals, api.Terminal{Unnumbered: true, Hidden: true, Name: t.Name, Command: vars.Replace(t.Command)})
	}
	return res
}

// starting is the start tasks of the project, whether the user allowed
// them, and their hash.
type starting struct {
	Tasks   []task
	Allowed bool
	Hash    string
}

// startTasks returns as JSON the start tasks of .fude/tasks.toml, with
// whether the user allowed these in this folder.
func startTasks() (string, error) {
	ts, err := projectTasks()
	if err != nil {
		return "", err
	}
	ts = slices.DeleteFunc(ts, func(t task) bool { return !t.Start })
	h := sha256.New()
	for _, t := range ts {
		fmt.Fprintf(h, "%s\x00%s\n", t.Name, t.Command)
	}
	r := starting{Tasks: ts, Hash: hex.EncodeToString(h.Sum(nil))}
	if len(ts) > 0 {
		line, path, err := allowedLine(r.Hash)
		if err != nil {
			return "", err
		}
		b, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		r.Allowed = slices.Contains(strings.Split(string(b), "\n"), line)
	}
	out, err := json.Marshal(r)
	return string(out), err
}

// allowedLine returns the line of the file of allowed start tasks that
// allows those of hash in the working directory, and the file's path.
func allowedLine(hash string) (string, string, error) {
	wd, err1 := os.Getwd()
	state, err2 := keep.Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err := errors.Join(err1, err2); err != nil {
		return "", "", fmt.Errorf("start tasks: %w", err)
	}
	return wd + "\t" + hash, filepath.Join(state, "fude", "tasks"), nil
}

// allowStarts keeps that the user allowed the start tasks of hash in the
// working directory, in place of what it allowed there before.
func allowStarts(hash string) error {
	line, path, err := allowedLine(hash)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dir, _, _ := strings.Cut(line, "\t")
	lines := slices.DeleteFunc(strings.Split(strings.TrimSpace(string(b)), "\n"), func(l string) bool {
		d, _, _ := strings.Cut(l, "\t")
		return l == "" || d == dir
	})
	return keep.Write(path, []byte(strings.Join(append(lines, line), "\n")+"\n"))
}

// vars puts the buffer's file, its folder, the cursor's line and the word
// under it for {file}, {dir}, {line} and {word}.
func vars(s api.Scope, c int) *strings.Replacer {
	t := s.Text
	start, end := c, c
	for start > 0 && isWord(t.Slice(start-1, start)[0]) {
		start--
	}
	for end < t.Len() && isWord(t.Slice(end, end+1)[0]) {
		end++
	}
	path := s.About["path"]
	return strings.NewReplacer("{file}", path, "{dir}", filepath.Dir(path), "{line}", strconv.Itoa(t.LineOf(c)+1), "{word}", t.Slice(start, end))
}

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// outputPlace is a line of a program's output that names a place, as
// path:line: or path:line:column:, or after Rust's -->, and what it says.
var outputPlace = regexp.MustCompile(`^\s*(?:-->\s*)?([^\s:]+):(\d+)(?::(\d+))?:?\s*(.*)$`)

// places returns the places a task's output names in files that are there,
// as path:line:column:text (B230).
func places(t api.Text) []string {
	var out []string
	for line := range t.Lines() {
		end := t.Len()
		if line+1 < t.Lines() {
			end = t.LineStart(line+1) - 1
		}
		m := outputPlace.FindStringSubmatch(t.Slice(t.LineStart(line), end))
		if m == nil {
			continue
		}
		if info, err := os.Stat(m[1]); err != nil || info.IsDir() {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s:%s:%s", m[1], m[2], cmp.Or(m[3], "1"), m[4]))
	}
	return out
}

// makeTarget is a line of a Makefile that names a target.
var makeTarget = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_./-]*)\s*:([^=]|$)`)

// find returns config's tasks, then the working directory's own, just
// recipes, make targets and package.json scripts. A file that is not there
// gives none, and one that fails to read says why.
func find(config map[string]struct{ Command string }) ([]task, error) {
	var out []task
	for _, name := range slices.Sorted(maps.Keys(config)) {
		out = append(out, task{Name: name, Source: "config", Command: config[name].Command})
	}
	var problems []error
	project, err := projectTasks()
	out, problems = append(out, project...), append(problems, err)
	if _, err := os.Stat("justfile"); err == nil {
		if _, err := exec.LookPath("just"); err == nil {
			ts, err := justTasks()
			out, problems = append(out, ts...), append(problems, err)
		}
	}
	if f, err := os.Open("Makefile"); err == nil {
		trace.Record("file", `read "Makefile"`, 0)
		in := bufio.NewScanner(f)
		for in.Scan() {
			if m := makeTarget.FindStringSubmatch(in.Text()); m != nil {
				out = append(out, task{Name: m[1], Source: "make", Command: "make " + m[1]})
			}
		}
		problems = append(problems, in.Err())
		f.Close()
	}
	at := time.Now()
	b, err := os.ReadFile("package.json")
	if !errors.Is(err, fs.ErrNotExist) {
		trace.Since("file", fmt.Sprintf(`read "package.json", %d bytes`, len(b)), at, err)
	}
	if err == nil {
		var p struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			problems = append(problems, fmt.Errorf("package.json: %w", err))
		}
		for _, name := range slices.Sorted(maps.Keys(p.Scripts)) {
			out = append(out, task{Name: name, Source: "npm", Command: "npm run " + name, Doc: p.Scripts[name]})
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		problems = append(problems, fmt.Errorf("package.json: %w", err))
	}
	return out, errors.Join(problems...)
}

// projectTasks returns the [task.<name>] tables of the working directory's
// .fude/tasks.toml, which holds tasks alone (B233).
func projectTasks() ([]task, error) {
	at := time.Now()
	src, err := os.ReadFile(filepath.Join(".fude", "tasks.toml"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	trace.Since("file", fmt.Sprintf(`read ".fude/tasks.toml", %d bytes`, len(src)), at, err)
	if err != nil {
		return nil, fmt.Errorf(".fude/tasks.toml: %w", err)
	}
	var f struct {
		Task map[string]struct {
			Command string `toml:"command"`
			Start   bool   `toml:"start"`
		} `toml:"task"`
	}
	d := toml.NewDecoder(bytes.NewReader(src))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, tomlerr.Where(".fude/tasks.toml", err)
	}
	var out []task
	for _, name := range slices.Sorted(maps.Keys(f.Task)) {
		out = append(out, task{Name: name, Source: "project", Command: f.Task[name].Command, Start: f.Task[name].Start})
	}
	return out, nil
}

// justTasks returns the justfile's public recipes, each with its doc
// comment.
func justTasks() ([]task, error) {
	at := time.Now()
	b, err := exec.Command("just", "--dump", "--dump-format", "json").Output()
	trace.Since("module", fmt.Sprintf("task ran just --dump --dump-format json, %d bytes", len(b)), at, err)
	if err != nil {
		return nil, fmt.Errorf("just --dump: %w", err)
	}
	var j struct {
		Recipes map[string]struct {
			Doc     string `json:"doc"`
			Private bool   `json:"private"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("just --dump: %w", err)
	}
	var out []task
	for _, name := range slices.Sorted(maps.Keys(j.Recipes)) {
		if r := j.Recipes[name]; !r.Private {
			out = append(out, task{Name: name, Source: "just", Command: "just " + name, Doc: r.Doc})
		}
	}
	return out, nil
}
