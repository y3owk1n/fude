// Package blame shows, dim after the cursor's line, who changed it last,
// how long ago and the commit's summary (B220).
package blame

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
)

// Module is :blame. It asks git about the cursor's line each time the
// cursor or the text moves, and shows only the newest answer.
type Module struct {
	mu     sync.Mutex
	on     bool
	buffer string // the buffer the cursor is in, as a call's context names it
	cursor int
	asked  uint32 // the job of the newest question
	line   int    // the line it asked about
	jobs   uint32
	note   int // the id of the note showing, 0 for none
	notes  int
}

// New returns blame, off until :blame.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "blame" }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "blame", Help: "shows who last changed the cursor's line, and when, after it, or stops showing it", ID: 1}}
}

func (m *Module) Subscribed() []string { return []string{"view.moved", "buffer.changed"} }

func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	s.Done()
	return api.Result{}, nil
}

// Call runs :blame, which turns the note on or off.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, args ...int) (api.Result, error) {
	defer s.Done()
	if !m.mu.TryLock() {
		return api.Result{}, fmt.Errorf("blame %w", api.ErrBusy)
	}
	defer m.mu.Unlock()
	if m.on = !m.on; !m.on {
		return api.Result{Undecorate: m.hide()}, nil
	}
	m.buffer, m.cursor = s.About["buffer"], args[0]
	return m.ask(s), nil
}

// Event hears where the cursor went, and that the text changed under it.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	defer s.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	event, detail, _ := strings.Cut(name, " ")
	switch {
	case !m.on:
		return api.Result{}, nil
	case event == "view.moved":
		cursor, _, _ := strings.Cut(detail, " ")
		m.buffer = s.About["buffer"]
		m.cursor, _ = strconv.Atoi(cursor)
	case s.About["buffer"] != m.buffer:
		return api.Result{}, nil
	}
	return m.ask(s), nil
}

// Callback takes git's answer and shows it after the line it was about.
// The answer to a question since replaced is dropped.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	defer s.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.on || id != m.asked {
		return api.Result{}, nil
	}
	// Outside a repository git fails on every move, and blame shows nothing.
	note := parse(s.About["output"])
	if s.About["error"] != "" || note == "" || m.line >= s.Text.Lines() {
		return api.Result{}, nil
	}
	m.notes++
	m.note = m.notes
	at := s.Text.LineStart(m.line)
	return api.Result{Decorate: []api.Decoration{{ID: m.note, Start: at, End: at, Note: "   " + note}}}, nil
}

// Work runs git with the buffer's text on its stdin.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	cmd := exec.CommandContext(ctx, j.Argv[0], j.Argv[1:]...)
	cmd.Stdin = strings.NewReader(j.Input)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("blame: %w", err)
	}
	return string(out), nil
}

func (m *Module) Close(context.Context) error { return nil }

// ask hides the note and asks git about the cursor's line.
func (m *Module) ask(s api.Scope) api.Result {
	res := api.Result{Undecorate: m.hide()}
	path := s.About["path"]
	if s.About["buffer"] != m.buffer || path == "" {
		return res
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return res
	}
	m.jobs++
	m.asked, m.line = m.jobs, s.Text.LineOf(min(m.cursor, s.Text.Len()))
	n := strconv.Itoa(m.line + 1)
	// git reads the text, so lines changed since the last write line up.
	argv := []string{"git", "-C", filepath.Dir(abs), "blame", "--porcelain", "-L", n + "," + n, "--contents", "-", "--", filepath.Base(abs)}
	res.Jobs = []api.Job{{ID: m.asked, Argv: argv, Input: s.Text.Slice(0, s.Text.Len()) + "\n"}}
	return res
}

// hide returns the note to remove, if one shows.
func (m *Module) hide() []int {
	if m.note == 0 {
		return nil
	}
	id := m.note
	m.note = 0
	return []int{id}
}

// parse returns the note of git blame --porcelain's answer.
func parse(out string) string {
	var author, summary string
	var when time.Time
	for in := bufio.NewScanner(strings.NewReader(out)); in.Scan(); {
		key, value, _ := strings.Cut(in.Text(), " ")
		switch key {
		case "author":
			author = value
		case "author-time":
			if s, err := strconv.ParseInt(value, 10, 64); err == nil {
				when = time.Unix(s, 0)
			}
		case "summary":
			summary = value
		}
	}
	switch author {
	case "":
		return ""
	case "Not Committed Yet", "External file (--contents)":
		return "not committed yet"
	}
	return fmt.Sprintf("%s, %s · %s", author, ago(time.Since(when)), summary)
}

// ago says d in its largest whole unit, as "3 days ago".
func ago(d time.Duration) string {
	for _, u := range []struct {
		name string
		size time.Duration
	}{{"year", 365 * 24 * time.Hour}, {"month", 30 * 24 * time.Hour}, {"day", 24 * time.Hour}, {"hour", time.Hour}, {"minute", time.Minute}} {
		if n := int(d / u.size); n > 0 {
			if n == 1 {
				return "1 " + u.name + " ago"
			}
			return strconv.Itoa(n) + " " + u.name + "s ago"
		}
	}
	return "just now"
}
