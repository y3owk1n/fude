// Package grep is :pick grep, a live search of the working directory's
// files' lines (B134, B210).
package grep

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// limit is the most lines a search shows.
const limit = 1000

// pick is the id of the list, which every query answers.
const pick = 1

// Module is grep.
type Module struct {
	mu   sync.Mutex
	jobs uint32
}

// New returns grep.
func New() *Module { return &Module{} }

func (m *Module) Name() string         { return "grep" }
func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Source, Name: "grep", Help: "searches the files' lines for the text typed", ID: 1}}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call opens the live list, with the text after :pick grep typed.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	return api.Result{Pick: &api.Pick{ID: pick, Title: "grep", Hint: "type to search the files' lines", Query: s.About["args"], Live: true, Places: true}}, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback searches for each query in a job, and answers the list with
// what the job found.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if query, live := s.About["query"]; live {
		if query == "" {
			return api.Result{Pick: &api.Pick{ID: pick}}, nil
		}
		m.jobs++
		return api.Result{Jobs: []api.Job{{ID: pick + m.jobs, Argv: []string{"grep", query, s.About["all"]}}}}, nil
	}
	if id == pick {
		return api.Result{}, nil
	}
	if err := s.About["error"]; err != "" {
		return api.Result{Error: err, Pick: &api.Pick{ID: pick}}, nil
	}
	var items []string
	if out := s.About["output"]; out != "" {
		items = strings.Split(out, "\n")
	}
	return api.Result{Pick: &api.Pick{ID: pick, Items: items}}, nil
}

// Work runs the search of a job: rg when it is installed, and git grep in
// a repository otherwise, with the files git ignores too when the job's
// third argument says so.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	lines, err := search(ctx, j.Argv[1], j.Argv[2] == "true")
	return strings.Join(lines, "\n"), err
}

func (m *Module) Close(context.Context) error { return nil }

// search returns the lines of the working directory's files that hold
// query, as path:line:column:text, at most limit of them. A query without
// capitals ignores case. With all, it searches the files git ignores and
// hidden ones too.
func search(ctx context.Context, query string, all bool) ([]string, error) {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("rg"); err == nil {
		args := []string{"--line-number", "--column", "--no-heading", "--color=never", "--smart-case", "--fixed-strings"}
		if all {
			args = append(args, "--no-ignore", "--hidden", "--glob=!.git")
		}
		cmd = exec.CommandContext(ctx, "rg", append(args, "--", query)...)
	} else if _, err := exec.LookPath("git"); err == nil {
		ignores := "--exclude-standard"
		if all {
			ignores = "--no-exclude-standard"
		}
		args := []string{"grep", "-n", "--column", "-I", "--untracked", ignores, "-F"}
		if strings.ToLower(query) == query {
			args = append(args, "-i")
		}
		cmd = exec.CommandContext(ctx, "git", append(args, "-e", query)...)
	} else {
		return nil, errors.New("grep needs rg or git")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmd.Path, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", cmd.Path, err)
	}
	var lines []string
	in := bufio.NewReaderSize(out, 64<<10)
	for len(lines) < limit && err == nil {
		var line []byte
		var more bool
		if line, more, err = in.ReadLine(); err != nil {
			break
		}
		l := string(line)
		// git writes / between folders on Windows too, where rg writes \.
		if p, rest, ok := strings.Cut(l, ":"); ok && cmd.Args[0] == "git" {
			l = filepath.FromSlash(p) + ":" + rest
		}
		lines = append(lines, l)
		// A line longer than the reader's buffer shows its start.
		for more && err == nil {
			_, more, err = in.ReadLine()
		}
	}
	if len(lines) == limit || err != nil && !errors.Is(err, io.EOF) {
		// The rest are not shown, or cannot be read, so the search stops.
		cmd.Process.Kill()
		cmd.Wait()
		if len(lines) < limit {
			return nil, fmt.Errorf("%s: %w", cmd.Path, err)
		}
		return lines, nil
	}
	// Both exit with 1 when nothing matches.
	if err := cmd.Wait(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("%s: %w", cmd.Path, err)
		}
	}
	return lines, nil
}
