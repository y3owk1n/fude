package editor

import (
	"bufio"
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
			for i, t := range found {
				items[i] = strings.TrimSpace(t.name + "  " + t.source + "  " + t.doc)
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
// running says so, and the window of its last run closes first.
func (e *Session) runTask(t task) {
	for _, w := range e.root.windows() {
		if term := w.buf.term; term != nil && term.task == t.name {
			if !term.exited {
				e.message = "the " + t.name + " task is running"
				return
			}
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
