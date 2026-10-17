package editor

import (
	"bufio"
	"bytes"
	"cmp"
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

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/fude/lsp"
	"github.com/y3owk1n/fude/vim"
)

// task is a command a project or config.toml names, which :task and
// :pick tasks run (B228).
type task struct {
	name, source, command, doc string
}

// makeTarget is a line of a Makefile that names a target.
var makeTarget = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_./-]*)\s*:([^=]|$)`)

// findTasks returns config.toml's tasks, then the working directory's just
// recipes, make targets and package.json scripts. A file that is not there
// gives none, and one that fails to read says why.
func findTasks(config map[string]taskConfig) ([]task, error) {
	var out []task
	for _, name := range slices.Sorted(maps.Keys(config)) {
		out = append(out, task{name: name, source: "config", command: config[name].Command})
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
		in := bufio.NewScanner(f)
		for in.Scan() {
			if m := makeTarget.FindStringSubmatch(in.Text()); m != nil {
				out = append(out, task{name: m[1], source: "make", command: "make " + m[1]})
			}
		}
		problems = append(problems, in.Err())
		f.Close()
	}
	if b, err := os.ReadFile("package.json"); err == nil {
		var p struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			problems = append(problems, fmt.Errorf("package.json: %w", err))
		}
		for _, name := range slices.Sorted(maps.Keys(p.Scripts)) {
			out = append(out, task{name: name, source: "npm", command: "npm run " + name, doc: p.Scripts[name]})
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		problems = append(problems, fmt.Errorf("package.json: %w", err))
	}
	return out, errors.Join(problems...)
}

// projectTasks returns the [task.<name>] tables of the working directory's
// .fude/tasks.toml, which holds tasks alone (B233).
func projectTasks() ([]task, error) {
	src, err := os.ReadFile(filepath.Join(".fude", "tasks.toml"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(".fude/tasks.toml: %w", err)
	}
	var f struct {
		Task map[string]taskConfig `toml:"task"`
	}
	d := toml.NewDecoder(bytes.NewReader(src))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, decodeError(".fude/tasks.toml", err)
	}
	var out []task
	for _, name := range slices.Sorted(maps.Keys(f.Task)) {
		out = append(out, task{name: name, source: "project", command: f.Task[name].Command})
	}
	return out, nil
}

// justTasks returns the justfile's public recipes, each with its doc
// comment.
func justTasks() ([]task, error) {
	b, err := exec.Command("just", "--dump", "--dump-format", "json").Output()
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
			out = append(out, task{name: name, source: "just", command: "just " + name, doc: r.Doc})
		}
	}
	return out, nil
}

// pickTasks opens the finder on the tasks, each with where it comes from and
// what it does, once they are read, and runs the one picked (B228).
func (e *Session) pickTasks() {
	var ts []task
	f := &finder{title: "tasks", chose: func(i int, _ string, ok bool) {
		if ok && i >= 0 {
			e.runTask(ts[i])
		}
	}}
	e.openFinder(f, "")
	go func() {
		found, err := findTasks(e.cfg.Task)
		e.queue <- func() {
			if err != nil {
				e.message = err.Error()
			}
			items := make([]string, len(found))
			running := e.runningTasks()
			for i, t := range found {
				source := t.source
				if slices.Contains(running, t.name) {
					source += ", running"
				}
				items[i] = strings.TrimSpace(t.name + "  " + source + "  " + t.doc)
			}
			ts, f.paths, f.stale = found, items, true
		}
	}()
}

// task runs the task named, or the last one run without a name.
func (e *Session) task(name string) {
	if name == "" {
		if e.lastTask == nil {
			e.message = "no task has run"
			return
		}
		e.runTask(*e.lastTask)
		return
	}
	go func() {
		ts, err := findTasks(e.cfg.Task)
		e.queue <- func() {
			if i := slices.IndexFunc(ts, func(t task) bool { return t.name == name }); i >= 0 {
				e.runTask(ts[i])
				return
			}
			e.message = "no task named " + name
			if err != nil {
				e.message = err.Error()
			}
		}
	}()
}

// runTask runs t in a terminal window below, with {file}, {dir}, {line} and
// {word} in its command standing for the current buffer's. A task already
// running shows its window again, and the window of its last run closes
// first (B228, B231).
func (e *Session) runTask(t task) {
	for _, b := range e.bufs {
		if term := b.term; term != nil && term.task == t.name && !term.exited {
			e.message = "the " + t.name + " task is running"
			if !slices.ContainsFunc(e.root.windows(), func(w *window) bool { return w.buf == b }) {
				from := e.win
				e.terminalWindow(b)
				e.win = from
			}
			return
		}
	}
	for _, w := range e.root.windows() {
		if term := w.buf.term; term != nil && term.task == t.name {
			if err := e.closeWindow(w); err != nil {
				e.message = err.Error()
				return
			}
		}
	}
	text, c := e.Text(), e.Cursor()
	start, end := c, c
	for start > 0 && isWord(text.Slice(start-1, start)[0]) {
		start--
	}
	for end < text.Len() && isWord(text.Slice(end, end+1)[0]) {
		end++
	}
	path := e.win.buf.file.path
	command := strings.NewReplacer(
		"{file}", path, "{dir}", filepath.Dir(path),
		"{line}", strconv.Itoa(text.LineOf(c)+1), "{word}", text.Slice(start, end),
	).Replace(t.command)
	e.lastTask = &t
	e.openTerminal(command, t.name)
}

// outputPlace is a line of a program's output that names a place, as
// path:line: or path:line:column:, or after Rust's -->, and what it says.
var outputPlace = regexp.MustCompile(`^\s*(?:-->\s*)?([^\s:]+):(\d+)(?::(\d+))?:?\s*(.*)$`)

// taskPlaces makes the places task's output in b names, of files that are
// there, the last list of places, without a window, so ]q goes to the first
// (B230).
func (e *Session) taskPlaces(b *buffer, task string) {
	t := b.text.Snapshot()
	var lines []string
	var spots []spot
	for line := range t.Lines() {
		start := t.LineStart(line)
		m := outputPlace.FindStringSubmatch(t.Slice(start, vim.LineEnd(t, start)))
		if m == nil {
			continue
		}
		if info, err := os.Stat(m[1]); err != nil || info.IsDir() {
			continue
		}
		row, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(cmp.Or(m[3], "1"))
		lines = append(lines, fmt.Sprintf("%s:%d:%d: %s", m[1], row, col, m[4]))
		spots = append(spots, spot{m[1], lsp.Position{Line: max(row-1, 0), Character: max(col-1, 0)}, "utf-8"})
	}
	if len(spots) == 0 {
		return
	}
	e.lastList = &places{title: "[task " + task + "]", lines: lines, spots: spots, at: -1}
	e.message += fmt.Sprintf(". It named %d places, and ]q goes to the first", len(spots))
}

// runningTasks returns the names of the tasks whose programs run.
func (e *Session) runningTasks() []string {
	var out []string
	for _, b := range e.bufs {
		if t := b.term; t != nil && t.task != "" && !t.exited {
			out = append(out, t.task)
		}
	}
	return out
}
