// Package format formats a buffer through its filetype's formatters, or
// else its language server. A write with format_on_save formats the whole
// text first, gq and = format the lines a motion covers, and :format
// formats the text or a range of it (B65, B136, B171, B234).
package format

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/api/diff"
	"github.com/y3owk1n/fude/internal/home"
	"github.com/y3owk1n/fude/internal/root"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// What format registers, by the ids its calls run them by.
const (
	gq uint32 = iota + 1
	command
)

// Module is format.
type Module struct {
	mu   sync.Mutex
	jobs uint32
	asks map[uint32]*ask // by job or request id
}

// ask is one formatting that waits for its formatters or the server.
type ask struct {
	text     string // the text the formatting is for
	from, to int    // the lines to format, as a byte range
	runs     []run
	missing  []string      // the formatters not installed, for the message
	timeout  time.Duration // how long the formatters may take
	ranged   bool          // the server was asked for the range, before the whole text
	options  string        // the server's formatting options, as JSON
	explicit bool          // :format or gq asked, so nothing to format says so
}

// run is a formatter's command and the folder it runs in.
type run struct {
	name    string
	command []string
	dir     string
}

// New returns format.
func New() *Module { return &Module{asks: map[uint32]*ask{}} }

func (m *Module) Name() string { return "format" }

func (m *Module) Subscribed() []string { return []string{"buffer.saving"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Operator, Name: "gq", Help: "formats the lines a motion or object covers", ID: gq},
		{Kind: api.Operator, Name: "=", Help: "formats the lines a motion or object covers, as gq does", ID: gq},
		{Kind: api.Command, Name: "format", Help: "formats the buffer, or the lines of a range, as a write with format_on_save does, without writing it", ID: command},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call formats the lines gq covers, or those of :format's range, or the
// whole text.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	if !m.mu.TryLock() {
		return api.Result{}, fmt.Errorf("format %w", api.ErrBusy)
	}
	defer m.mu.Unlock()
	t := s.Text
	from, to := 0, t.Len()
	switch {
	case id == gq:
		from, to = lineStart(t, args[0]), lineEnd(t, args[1])
	case s.About["ranged"] == "true":
		first, _ := strconv.Atoi(s.About["first"])
		last, _ := strconv.Atoi(s.About["last"])
		from, to = first, lineEnd(t, last)
	}
	return m.start(s, from, to, true), nil
}

// Event formats the whole text before a write, when format_on_save is on
// for the filetype, or else at the top of config.toml (B136).
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	on, ok := s.Config("filetype." + s.About["filetype"] + ".format_on_save")
	if !ok {
		on, _ = s.Config("format_on_save")
	}
	if on != "true" {
		return api.Result{}, nil
	}
	return m.start(s, 0, s.Text.Len(), false), nil
}

// Callback takes what the formatters wrote, or the server's edits, and
// applies the changes inside the lines, as one change (B65).
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.asks[id]
	delete(m.asks, id)
	t := s.Text
	switch {
	case a == nil:
		return api.Result{}, nil
	case t.Slice(0, t.Len()) != a.text:
		return api.Result{Error: "the text changed while it was formatted"}, nil
	case a.ranged && strings.HasPrefix(s.About["error"], "no language server offers"):
		// The server formats whole documents only, so it is asked for that
		// and only its edits inside the lines apply.
		a.ranged = false
		return m.request(s, a, "textDocument/formatting"), nil
	case a.explicit && strings.HasPrefix(s.About["error"], "no language server offers"):
		// Nothing else formats the file, so its lines are filled as prose.
		return api.Result{Edits: wrap(s, a.from, a.to)}, nil
	case s.About["error"] != "":
		return api.Result{Error: s.About["error"]}, nil
	}
	var formatted string
	if len(a.runs) > 0 {
		// A buffer holds no last line break, and a formatter writes one. A
		// buffer's lines end in \n, where one on Windows may write \r\n.
		formatted = strings.TrimSuffix(strings.ReplaceAll(s.About["output"], "\r\n", "\n"), "\n")
	} else {
		var err error
		if formatted, err = applied(t, s.About["output"], s.About["encoding"], a.from, a.to); err != nil {
			return api.Result{Error: err.Error()}, nil
		}
	}
	res := api.Result{Message: a.note()}
	for _, ed := range diff.Edits(a.text, formatted) {
		if ed.Start >= a.from && ed.End <= a.to {
			res.Edits = append(res.Edits, ed)
		}
	}
	return res, nil
}

// Work passes the text through the formatters in turn, from each one's
// stdin to its stdout, in its folder, and stops them after the timeout.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	m.mu.Lock()
	a := m.asks[j.ID]
	m.mu.Unlock()
	if a == nil {
		return "", errors.New("format: the job is unknown")
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	text, path := j.Input, j.Argv[1]
	for _, r := range a.runs {
		args := make([]string, len(r.command)-1)
		for i, arg := range r.command[1:] {
			args[i] = strings.ReplaceAll(arg, "{path}", path)
		}
		cmd := exec.CommandContext(ctx, r.command[0], args...)
		cmd.Dir, cmd.Stdin = r.dir, strings.NewReader(text)
		out, err := cmd.Output()
		switch exit, ok := errors.AsType[*exec.ExitError](err); {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return "", fmt.Errorf("formatting took over %s", a.timeout)
		case ok && len(exit.Stderr) > 0:
			return "", fmt.Errorf("%s: %s", r.name, strings.TrimSpace(string(exit.Stderr)))
		case err != nil:
			return "", fmt.Errorf("%s: %w", r.name, err)
		}
		text = string(out)
	}
	return text, nil
}

func (m *Module) Close(context.Context) error { return nil }

// wrap fills the paragraphs of the lines from from to to as prose, out to
// text_width, or 79 columns without it, and returns the edits that make
// them so. Blank lines and list items start paragraphs. Each line keeps its
// paragraph's leader, its indent and a comment's marker, and the lines
// after a list item's first go under its text (B451).
func wrap(s api.Scope, from, to int) []api.Edit {
	v, _ := s.Option("text_width")
	width, _ := strconv.Atoi(v)
	if width <= 0 {
		width = 79
	}
	marker := ""
	if c, ok := s.Comment(from); ok {
		marker = strings.TrimSpace(strings.SplitN(c, "%s", 2)[0])
	}
	text := s.Text.Slice(from, to)
	var out []string
	var words []string
	lead, next := "", ""
	fill := func() {
		if len(words) == 0 {
			return
		}
		line := lead + words[0]
		for _, w := range words[1:] {
			if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) > width {
				out = append(out, line)
				line = next + w
				continue
			}
			line += " " + w
		}
		out, words = append(out, line), nil
	}
	for _, l := range strings.Split(text, "\n") {
		ll, nl, item := leaderOf(l, marker)
		body := strings.TrimSpace(l[len(ll):])
		if body == "" {
			fill()
			out = append(out, l)
			continue
		}
		if len(words) == 0 || item || ll != next {
			fill()
			lead, next = ll, nl
		}
		words = append(words, strings.Fields(body)...)
	}
	fill()
	wrapped := strings.Join(out, "\n")
	if wrapped == text {
		return nil
	}
	return []api.Edit{{Start: from, End: to, Text: wrapped}}
}

// listItem is the marker that starts a list item.
var listItem = regexp.MustCompile(`^([-*+]|[0-9]+[.)])[ \t]+`)

// leaderOf returns what line starts with that the line after it in a
// paragraph repeats, what that line starts with, and whether line starts a
// list item.
func leaderOf(line, marker string) (string, string, bool) {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	rest := line[len(indent):]
	if marker != "" && strings.HasPrefix(rest, marker) {
		after := rest[len(marker):]
		l := indent + marker + after[:len(after)-len(strings.TrimLeft(after, " \t"))]
		return l, l, false
	}
	if item := listItem.FindString(rest); item != "" {
		return indent + item, indent + strings.Repeat(" ", utf8.RuneCountInString(item)), true
	}
	return indent, indent, false
}

// start formats [from, to) of the text through the filetype's formatters,
// or else the server. explicit says :format or gq asked, so what is wrong
// is said at once.
func (m *Module) start(s api.Scope, from, to int, explicit bool) api.Result {
	t := s.Text
	a := &ask{text: t.Slice(0, t.Len()), from: from, to: to, explicit: explicit, timeout: time.Second}
	if ms, ok := s.Option("format_timeout"); ok {
		if n, err := strconv.Atoi(ms); err == nil {
			a.timeout = time.Duration(n) * time.Millisecond
		}
	}
	var err error
	if a.runs, a.missing, err = formatters(s); err != nil {
		return api.Result{Error: err.Error()}
	}
	m.jobs++
	m.asks[m.jobs] = a
	if len(a.runs) > 0 {
		// A buffer holds no last line break, and a formatter wants one.
		return api.Result{Jobs: []api.Job{{ID: m.jobs, Argv: []string{"format", s.About["path"]}, Input: a.text + "\n"}}}
	}
	a.ranged = true
	a.options = fmt.Sprintf(`{"options":{"tabSize":%s,"insertSpaces":%s}}`, s.About["indent"], expandTab(s))
	return m.request(s, a, "textDocument/rangeFormatting")
}

// request asks the server to format, by method, what a asks.
func (m *Module) request(s api.Scope, a *ask, method string) api.Result {
	m.jobs++
	m.asks[m.jobs] = a
	rq := api.Request{ID: m.jobs, Method: method, Pos: -1, End: -1, Params: a.options}
	if a.ranged {
		rq.Pos, rq.End = a.from, a.to
	}
	return api.Result{Requests: []api.Request{rq}}
}

// note says which formatters are not installed, or nothing.
func (a *ask) note() string {
	if len(a.missing) == 0 {
		return ""
	}
	return strings.Join(a.missing, ", ") + " not installed"
}

// expandTab returns whether the buffer indents with spaces, as JSON.
func expandTab(s api.Scope) string {
	on, _ := s.Option("expand_tab")
	return strconv.FormatBool(on == "true")
}

// formatters returns the formatters of the filetype that apply to the
// file, in order. One applies when its program is installed and any root
// it requires is found. With format_first, only the first that applies is
// returned. missing names those not installed (B136, B171).
func formatters(s api.Scope) (runs []run, missing []string, err error) {
	ft := "filetype." + s.About["filetype"] + "."
	var names []string
	if v, ok := s.Config(ft + "format"); ok {
		json.Unmarshal([]byte(v), &names)
	}
	first, _ := s.Config(ft + "format_first")
	abs, err := filepath.Abs(s.About["path"])
	if err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		table := "formatter." + name + "."
		command, err := commandOf(name, list(s, table+"command"))
		if err != nil {
			return nil, nil, fmt.Errorf("format with %s: %w", name, err)
		}
		if _, err := exec.LookPath(command[0]); err != nil {
			if first != "true" {
				missing = append(missing, name)
			}
			continue
		}
		dir, found := root.Of(abs, list(s, table+"root"))
		if workspace, _ := s.Config(table + "workspace_required"); !found && workspace == "true" {
			continue
		}
		runs = append(runs, run{name, command, dir})
		if first == "true" {
			break
		}
	}
	return runs, missing, nil
}

// list returns the list of strings at a config.toml path, or none.
func list(s api.Scope, path string) []string {
	var out []string
	if v, ok := s.Config(path); ok {
		json.Unmarshal([]byte(v), &out)
	}
	return out
}

// commandOf returns the command config.toml gives name, or the name alone,
// with a leading ~/ in each word made the home directory.
func commandOf(name string, command []string) ([]string, error) {
	if len(command) == 0 {
		return []string{name}, nil
	}
	out := make([]string, len(command))
	for i, word := range command {
		path, err := home.Expand(word)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", word, err)
		}
		out[i] = path
	}
	return out, nil
}

// textEdit is an edit as the server sends it.
type textEdit struct {
	Range   lsp.Range `json:"range"`
	NewText string    `json:"newText"`
}

// applied returns t with the server's edits, as JSON in enc, made. Only
// those inside [from, to) apply, and overlapping ones are refused.
func applied(t api.Text, answer, enc string, from, to int) (string, error) {
	var edits []textEdit
	json.Unmarshal([]byte(answer), &edits)
	type edit struct {
		start, end int
		text       string
	}
	var kept []edit
	for _, a := range edits {
		start, end := lsp.Offset(t, a.Range.Start, enc), lsp.Offset(t, a.Range.End, enc)
		// A buffer holds no last line break, so one a server adds at the end
		// is left out.
		if end == t.Len() {
			if a.NewText = strings.TrimSuffix(a.NewText, "\n"); start == end && a.NewText == "" {
				continue
			}
		}
		if start >= from && end <= to {
			kept = append(kept, edit{start, end, a.NewText})
		}
	}
	slices.SortStableFunc(kept, func(x, y edit) int { return x.start - y.start })
	old := t.Slice(0, t.Len())
	var out strings.Builder
	at := 0
	for _, e := range kept {
		if e.start < at {
			return "", errors.New("the server's edits overlap")
		}
		out.WriteString(old[at:e.start])
		out.WriteString(e.text)
		at = e.end
	}
	out.WriteString(old[at:])
	return out.String(), nil
}

// lineStart returns where the line holding pos starts.
func lineStart(t api.Text, pos int) int { return t.LineStart(t.LineOf(pos)) }

// lineEnd returns where the line holding pos ends, before its line break.
func lineEnd(t api.Text, pos int) int {
	if n := t.LineOf(pos); n+1 < t.Lines() {
		return t.LineStart(n+1) - 1
	}
	return t.Len()
}
