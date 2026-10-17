// Package git shows how a file differs from git's index, as signs, hunks to
// jump to, reset and stage, a preview and :gitdiff (B123, B138, B221,
// B222).
package git

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/api/diff"
)

// What git registers, by the ids its calls run them by.
const (
	next uint32 = iota + 1
	prev
	first
	last
	preview
	reset
	stage
	gitdiff
)

// A sign of a git change: what the column shows and its style.
type sign struct{ text, style string }

var (
	added        = sign{"┃", "git.add"}
	changed      = sign{"┃", "git.change"}
	deleted      = sign{"▁", "git.delete"} // under the line before deleted ones
	topDeleted   = sign{"▔", "git.delete"} // over the first line when the first lines went
	changeDelete = sign{"~", "git.change"} // a change that also deleted lines
)

// Module is git. It keeps each buffer's text as git's index has it, and
// what it shows on the buffer.
type Module struct {
	mu    sync.Mutex
	base  map[string]*string // by buffer, the index's text, nil outside a repository
	stale map[string]bool    // by buffer, the index may have changed since base was read
	signs map[string][]int   // by buffer, the ids of the signs showing
	ids   int
	jobs  uint32
	about map[uint32]job // by id, what each job that runs does
}

// job is what one job does, for the buffer buf: it reads the base from
// the index, or stages the text.
type job struct {
	kind, buf string
}

// New returns git.
func New() *Module {
	return &Module{base: map[string]*string{}, stale: map[string]bool{}, signs: map[string][]int{}, about: map[uint32]job{}}
}

func (m *Module) Name() string { return "git" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "view.moved", "editor.focused"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Key, Name: "]h", Help: "goes to the next hunk", ID: next},
		{Kind: api.Key, Name: "[h", Help: "goes to the hunk before", ID: prev},
		{Kind: api.Key, Name: "[H", Help: "goes to the first hunk", ID: first},
		{Kind: api.Key, Name: "]H", Help: "goes to the last hunk", ID: last},
		{Kind: api.Plug, Name: "preview", Help: "shows the hunk under the cursor as git's index has it", ID: preview},
		{Kind: api.Operator, Name: "gH", Help: "puts the hunks a motion or object covers back as git's index has them", ID: reset, Host: true},
		{Kind: api.Operator, Name: "gh", Help: "stages the hunks a motion or object covers in git's index", ID: stage, Host: true},
		{Kind: api.Command, Name: "gitdiff", Help: "shows the lines that differ from git's index", ID: gitdiff},
	}
}

// Init reads the base of the buffer open at the start.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.read(s), nil
}

// Call runs a key, the preview, gH, gh or :gitdiff.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	if !m.mu.TryLock() {
		return api.Result{}, fmt.Errorf("git %w", api.ErrBusy)
	}
	defer m.mu.Unlock()
	base := m.base[s.About["buffer"]]
	if base == nil {
		return api.Result{Message: "not in a git repository"}, nil
	}
	t := s.Text
	hs := hunks(*base, t.Slice(0, t.Len()))
	switch id {
	case next, prev, first, last:
		return jump(t, hs, id, args[0], max(args[1], 1)), nil
	case preview:
		return previewHunk(t, hs, args[0]), nil
	case reset:
		return resetHunks(t, hs, args[0], args[1]), nil
	case stage:
		return m.stageHunks(s, hs, args[0], args[1]), nil
	}
	return gitDiff(s, *base), nil
}

// Event reads a buffer's base as it opens, or once the index may have
// changed, and signs its changes as the text changes.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	switch event, _, _ := strings.Cut(name, " "); event {
	case "buffer.opened":
		return m.read(s), nil
	case "editor.focused":
		// git's index may have changed while fude was away.
		for b := range m.base {
			m.stale[b] = true
		}
		return m.read(s), nil
	case "view.moved":
		if m.stale[buf] {
			return m.read(s), nil
		}
		return api.Result{}, nil
	}
	return m.sign(s), nil
}

// Callback takes a buffer's base once read, or what staging said.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.about[id]
	delete(m.about, id)
	if !ok || j.buf != s.About["buffer"] {
		return api.Result{}, nil
	}
	if j.kind == "stage" {
		if err := s.About["error"]; err != "" {
			return api.Result{Message: err}, nil
		}
		return m.read(s), nil
	}
	if s.About["error"] != "" {
		m.base[j.buf] = nil
		return m.hide(j.buf), nil
	}
	// A buffer holds no final line break, as it reads files (N5).
	base := strings.TrimSuffix(s.About["output"], "\n")
	m.base[j.buf] = &base
	return m.sign(s), nil
}

// Work runs git: reading a file as the index has it, or staging text as
// the file.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	path := j.Argv[1]
	if j.Argv[0] == "stage" {
		return "", stageText(ctx, path, j.Input)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("git: %w", err)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", filepath.Dir(abs), "show", ":./"+filepath.Base(abs)).Output()
	if err != nil {
		return "", fmt.Errorf("git: %s is not in a repository's index", path)
	}
	return string(out), nil
}

func (m *Module) Close(context.Context) error { return nil }

// read asks git for the base of the buffer s is about, when it is a file.
func (m *Module) read(s api.Scope) api.Result {
	buf, path := s.About["buffer"], s.About["path"]
	delete(m.stale, buf)
	if path == "" {
		return api.Result{}
	}
	m.jobs++
	m.about[m.jobs] = job{"base", buf}
	return api.Result{Jobs: []api.Job{{ID: m.jobs, Argv: []string{"base", path}}}}
}

// hide takes the signs off buffer buf.
func (m *Module) hide(buf string) api.Result {
	res := api.Result{Undecorate: m.signs[buf]}
	delete(m.signs, buf)
	return res
}

// sign puts the sign of each line that differs from the base on the buffer
// s is about, in place of the signs it had (B123).
func (m *Module) sign(s api.Scope) api.Result {
	buf, t := s.About["buffer"], s.Text
	res := m.hide(buf)
	base := m.base[buf]
	if base == nil {
		return res
	}
	for _, h := range hunks(*base, t.Slice(0, t.Len())) {
		add, del := h.count, len(h.old)
		signs := map[int]sign{}
		for j := range add {
			signs[h.line+j] = added
			if j < del {
				signs[h.line+j] = changed
			}
		}
		switch {
		case add == 0 && h.line == 0:
			signs[0] = topDeleted
		case add == 0:
			signs[h.line-1] = deleted
		case del > add:
			signs[h.line+add-1] = changeDelete
		}
		for _, line := range slices.Sorted(maps.Keys(signs)) {
			if line >= t.Lines() {
				continue
			}
			m.ids++
			at := t.LineStart(line)
			res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: at, End: at, Style: signs[line].style, Sign: signs[line].text, Under: true})
			m.signs[buf] = append(m.signs[buf], m.ids)
		}
	}
	return res
}

// hunk is one run of lines that differ from git's index: count lines of
// the text from line, which replaced old.
type hunk struct {
	line, count int
	old         []string
}

// span returns the first and last line a hunk's sign is on. A deletion's
// is the line before it, or the first line.
func (h hunk) span() (int, int) {
	if h.count == 0 {
		return max(h.line-1, 0), max(h.line-1, 0)
	}
	return h.line, h.line + h.count - 1
}

// hunks returns where now differs from base, line by line. Only the lines
// between those both start and end with are split and diffed, so a small
// change to a long text is cheap.
func hunks(base, now string) []hunk {
	if base == now {
		return nil
	}
	// pre is the start of the first line that differs, and the shared end
	// starts with the newline after the last one.
	pre := common(base, now, false)
	pre = strings.LastIndexByte(base[:pre], '\n') + 1
	end := common(base[pre:], now[pre:], true)
	for end > 0 && base[len(base)-end] != '\n' {
		end--
	}
	a, b := base[pre:len(base)-end], now[pre:len(now)-end]
	ops := diff.Lines(strings.Split(a, "\n"), strings.Split(b, "\n"))
	var out []hunk
	line := strings.Count(base[:pre], "\n") // the line of now the next op is at
	for i := 0; i < len(ops); {
		if ops[i].Kind == ' ' {
			line, i = line+1, i+1
			continue
		}
		h := hunk{line: line}
		for ; i < len(ops) && ops[i].Kind != ' '; i++ {
			if ops[i].Kind == '-' {
				h.old = append(h.old, ops[i].Line)
			} else {
				h.count++
			}
		}
		line += h.count
		out = append(out, h)
	}
	return out
}

// common returns how many bytes a and b start with that are the same, or
// end with when back is set. It compares 4KB blocks first, then bytes.
func common(a, b string, back bool) int {
	n := min(len(a), len(b))
	if back {
		a, b = a[len(a)-n:], b[len(b)-n:]
	}
	const block = 4 << 10
	i := 0
	if back {
		for i+block <= n && a[n-i-block:n-i] == b[n-i-block:n-i] {
			i += block
		}
		for i < n && a[n-1-i] == b[n-1-i] {
			i++
		}
		return i
	}
	for i+block <= n && a[i:i+block] == b[i:i+block] {
		i += block
	}
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// jump moves the cursor to the start of a hunk: the count'th after its
// line for ]h, before it for [h, or the first or last for [H and ]H
// (B138).
func jump(t api.Text, hs []hunk, to uint32, cursor, count int) api.Result {
	if len(hs) == 0 {
		return api.Result{Message: "no hunks"}
	}
	cur := t.LineOf(cursor)
	var starts []int
	for _, h := range hs {
		start, _ := h.span()
		starts = append(starts, start)
	}
	i, name := -1, ""
	switch to {
	case first:
		i, name = 0, "first"
	case last:
		i, name = len(starts)-1, "last"
	case next:
		name = "next"
		if j := slices.IndexFunc(starts, func(l int) bool { return l > cur }); j >= 0 {
			i = min(j+count-1, len(starts)-1)
		}
	case prev:
		name = "previous"
		for j, l := range starts {
			if l < cur {
				i = j
			}
		}
		if i >= 0 {
			i = max(i-count+1, 0)
		}
	}
	if i < 0 {
		return api.Result{Message: "no " + name + " hunk"}
	}
	return api.Result{Jump: &api.Jump{Pos: t.LineStart(starts[i])}}
}

// previewHunk shows the hunk on the cursor's line in a float, its old lines
// after - and its new ones after + (B138).
func previewHunk(t api.Text, hs []hunk, cursor int) api.Result {
	cur := t.LineOf(cursor)
	for _, h := range hs {
		if lo, hi := h.span(); cur < lo || cur > hi {
			continue
		}
		var lines []string
		for _, l := range h.old {
			lines = append(lines, "-"+l)
		}
		for i := h.line; i < h.line+h.count; i++ {
			lines = append(lines, "+"+line(t, i))
		}
		md := "```diff\n" + strings.Join(lines, "\n") + "\n```"
		return api.Result{Float: &md}
	}
	return api.Result{Message: "no hunk on this line"}
}

// resetHunks puts the hunks meeting the lines from start to end back as
// git's index has them, as gH does (B138).
func resetHunks(t api.Text, hs []hunk, start, end int) api.Result {
	now := t.Slice(0, t.Len())
	lines := strings.Split(now, "\n")
	first, last := t.LineOf(start), t.LineOf(end)
	for _, h := range slices.Backward(hs) {
		if lo, hi := h.span(); hi >= first && lo <= last {
			lines = slices.Concat(lines[:h.line], h.old, lines[h.line+h.count:])
		}
	}
	return api.Result{Edits: diff.Edits(now, strings.Join(lines, "\n"))}
}

// stageHunks writes the hunks meeting the lines from start to end to git's
// index, as gh does, and leaves the others as the index has them. git runs
// off the loop, and the signs follow once it is done (B221).
func (m *Module) stageHunks(s api.Scope, hs []hunk, start, end int) api.Result {
	t := s.Text
	lines := strings.Split(t.Slice(0, t.Len()), "\n")
	first, last, staged := t.LineOf(start), t.LineOf(end), 0
	for _, h := range slices.Backward(hs) {
		if lo, hi := h.span(); hi >= first && lo <= last {
			staged++
			continue
		}
		lines = slices.Concat(lines[:h.line], h.old, lines[h.line+h.count:])
	}
	if staged == 0 {
		return api.Result{Message: "no hunk here"}
	}
	m.jobs++
	m.about[m.jobs] = job{"stage", s.About["buffer"]}
	return api.Result{Jobs: []api.Job{{ID: m.jobs, Argv: []string{"stage", s.About["path"]}, Input: strings.Join(lines, "\n")}}}
}

// stageText puts text in git's index as the file at path, keeping its
// mode, with the line endings the file on disk has, which git cleans as it
// cleans the file.
func stageText(ctx context.Context, path, text string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("stage %s: %w", path, err)
	}
	if disk, err := os.ReadFile(abs); err == nil {
		if strings.HasSuffix(string(disk), "\n") {
			text += "\n"
		}
		if strings.Contains(string(disk), "\r\n") {
			text = strings.ReplaceAll(text, "\n", "\r\n")
		}
	}
	dir, name := filepath.Dir(abs), filepath.Base(abs)
	mode := "100644"
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "-s", "--", name).Output(); err == nil {
		if f := strings.Fields(string(out)); len(f) > 0 {
			mode = f[0]
		}
	}
	hash := exec.CommandContext(ctx, "git", "-C", dir, "hash-object", "-w", "--stdin", "--path", name)
	hash.Stdin = strings.NewReader(text)
	out, err := hash.Output()
	if err != nil {
		return fmt.Errorf("stage %s: %w", path, err)
	}
	info := mode + "," + strings.TrimSpace(string(out)) + "," + name
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "update-index", "--cacheinfo", info).CombinedOutput(); err != nil {
		return fmt.Errorf("stage %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitDiff shows the lines of the buffer that differ from git's index, with
// 3 lines around each, in a window of its own (B222).
func gitDiff(s api.Scope, base string) api.Result {
	t := s.Text
	ops := diff.Lines(strings.Split(base, "\n"), strings.Split(t.Slice(0, t.Len()), "\n"))
	if !slices.ContainsFunc(ops, func(o diff.Op) bool { return o.Kind != ' ' }) {
		return api.Result{Message: "no changes from git's index"}
	}
	text := diff.Unified(ops, 3)
	panel := &api.Panel{Title: "[git diff " + filepath.Base(s.About["path"]) + "]", Filetype: "diff", Text: text}
	at := 0
	for _, l := range strings.Split(text, "\n") {
		style := ""
		switch {
		case strings.HasPrefix(l, "@@"):
			style = "comment"
		case strings.HasPrefix(l, "-"):
			style = "diff.minus"
		case strings.HasPrefix(l, "+"):
			style = "diff.plus"
		}
		if style != "" {
			panel.Spans = append(panel.Spans, api.Span{Start: at, End: at + len(l), Name: style})
		}
		at += len(l) + 1
	}
	return api.Result{Show: panel}
}

// line returns line n of t.
func line(t api.Text, n int) string {
	start, end := t.LineStart(n), t.Len()
	if n+1 < t.Lines() {
		end = t.LineStart(n+1) - 1
	}
	return t.Slice(start, end)
}
