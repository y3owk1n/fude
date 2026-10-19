// Package links follows what is under the cursor: gx opens a link or a
// file with the system's opener, and gf opens a file in fude. In markdown,
// either takes a link's destination from anywhere on the link (B301).
package links

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// What links registers, by the ids its calls run them by.
const (
	open uint32 = iota + 1
	edit
)

// linkQuery finds a markdown link and its destination, in the inline text.
var linkQuery = map[string]string{"markdown_inline": `
(inline_link (link_destination) @to) @link
(image (link_destination) @to) @link
(uri_autolink) @link`}

// Module is links. It keeps what each job it ran opens, for its message.
type Module struct {
	mu     sync.Mutex
	jobs   uint32
	opened map[uint32]string // by job, what it opens
}

// New returns links.
func New() *Module { return &Module{opened: map[uint32]string{}} }

func (m *Module) Name() string { return "links" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Plug, Name: "open", Keys: "gx", Help: "opens the link or file under the cursor with the system's opener", ID: open, Host: true},
		{Kind: api.Plug, Name: "file", Keys: "gf", Help: "opens the file under the cursor, at the line after a colon", ID: edit},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs gx or gf at the cursor.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	target := under(s, args[0])
	if target == "" {
		return api.Result{Message: "no link or file under the cursor"}, nil
	}
	if id == open {
		if !strings.Contains(target, "://") && !strings.HasPrefix(target, "mailto:") {
			path, _ := split(target)
			found, ok := find(s, path)
			if !ok {
				return api.Result{Message: "no file named " + path}, nil
			}
			target = found
		}
		m.jobs++
		m.opened[m.jobs] = target
		return api.Result{Jobs: []api.Job{{ID: m.jobs, Argv: []string{opener(), target}}}}, nil
	}
	path, line := split(target)
	found, ok := find(s, path)
	if !ok {
		return api.Result{Message: "no file named " + path}, nil
	}
	j := &api.Jump{Path: found}
	if line > 0 {
		j.Line, j.Column = line, 1
	}
	return api.Result{Jump: j}, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback says what the opener did.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.opened[id]
	delete(m.opened, id)
	switch {
	case !ok:
		return api.Result{}, nil
	case s.About["error"] != "":
		return api.Result{Message: s.About["error"]}, nil
	}
	return api.Result{Message: "opened " + target}, nil
}

// Work runs the opener.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	out, err := exec.CommandContext(ctx, j.Argv[0], j.Argv[1:]...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", j.Argv[0], j.Argv[1], err, strings.TrimSpace(string(out)))
	}
	return "", nil
}

func (m *Module) Close(context.Context) error { return nil }

// opener is the program that opens a link or file as the system would.
func opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// under returns the link or path at pos. On a markdown link it is the
// link's destination. Elsewhere it is the run of text around pos up to a
// blank, without the brackets and marks around it.
func under(s api.Scope, pos int) string {
	spans, _ := s.Query(linkQuery)
	for i, sp := range spans {
		if sp.Name != "link" || pos < sp.Start || pos >= sp.End {
			continue
		}
		for _, to := range spans[i+1:] {
			if to.Name == "to" && to.Start >= sp.Start && to.End <= sp.End {
				return s.Text.Slice(to.Start, to.End)
			}
		}
		return strings.Trim(s.Text.Slice(sp.Start, sp.End), "<>")
	}
	t := s.Text
	start, end := t.LineStart(t.LineOf(pos)), t.Len()
	if l := t.LineOf(pos); l+1 < t.Lines() {
		end = t.LineStart(l+1) - 1
	}
	line := t.Slice(start, end)
	at := pos - start
	if at >= len(line) || isBlank(line[at]) {
		return ""
	}
	from, to := at, at
	for from > 0 && !isBlank(line[from-1]) {
		from--
	}
	for to < len(line) && !isBlank(line[to]) {
		to++
	}
	return strings.Trim(line[from:to], "<>()[]{}\"'`,;.!?")
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

// split cuts a :line off the end of target, and returns the path and the
// line, or 0 for none.
func split(target string) (string, int) {
	path, rest, ok := strings.Cut(target, ":")
	if !ok {
		return target, 0
	}
	n, _, _ := strings.Cut(rest, ":")
	line, err := strconv.Atoi(n)
	if err != nil {
		return target, 0
	}
	return path, line
}

// find returns path as a file that is there: as it is when absolute or
// under ~/, else in the folder of the buffer s is about, else in the
// working directory.
func find(s api.Scope, path string) (string, bool) {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, rest)
	}
	tries := []string{path}
	if !filepath.IsAbs(path) {
		tries = []string{filepath.Join(filepath.Dir(s.About["path"]), path), path}
	}
	for _, p := range tries {
		if _, err := os.Stat(p); err == nil {
			abs, err := filepath.Abs(p)
			return abs, err == nil
		}
	}
	return "", false
}
