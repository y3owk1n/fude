package editor

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// hasGit reports whether git is installed. Without it, files show no git
// signs.
func hasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// The signs of git changes, as gitsigns shows them, by kind.
var (
	gitAdd          = Sign{"┃", "git.add"}
	gitChange       = Sign{"┃", "git.change"}
	gitDelete       = Sign{"▁", "git.delete"} // under the line before deleted ones
	gitTopDelete    = Sign{"▔", "git.delete"} // over the first line when the first lines went
	gitChangeDelete = Sign{"~", "git.change"} // a change that also deleted lines
)

// The signs of a listing entry's git status, by status code (B126).
var statusSigns = map[string]Sign{
	" M": {"✹", "git.change"}, "M ": {"•", "git.change"}, "MM": {"≠", "git.change"}, "R ": {"→", "git.change"},
	"A ": {"+", "git.add"}, "??": {"?", "git.delete"}, "D ": {"-", "git.delete"},
}

// gitStatus returns the git status sign of each name in dir, or false
// outside a repository. A directory takes the status of the first change
// below it, and a name whose status has no sign maps to the zero Sign.
func gitStatus(dir string) (map[string]Sign, bool) {
	// git names paths from the repository's root, so each starts with dir's
	// prefix.
	prefix, err := exec.Command("git", "-C", dir, "rev-parse", "--show-prefix").Output()
	if err != nil {
		return nil, false
	}
	out, err := exec.Command("git", "-C", dir, "--no-optional-locks", "status", "--porcelain", "-z", "--", ".").Output()
	if err != nil {
		return nil, false
	}
	signs, pre := map[string]Sign{}, strings.TrimSuffix(string(prefix), "\n")
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		if len(fields[i]) < 4 {
			continue
		}
		code, path := fields[i][:2], fields[i][3:]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // the old name
		}
		name := strings.TrimPrefix(path, pre)
		if top, _, below := strings.Cut(name, "/"); below {
			name = top + "/"
		}
		if _, seen := signs[name]; !seen {
			signs[name] = statusSigns[code]
		}
	}
	return signs, true
}

// gitBranch returns the branch checked out in dir's repository, or "" for
// none, outside one or on a detached head (B147).
func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitBase reads path as git's index has it, the text gitsigns compares
// with, or false when the file is not in a repository's index.
func gitBase(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	out, err := exec.Command("git", "-C", filepath.Dir(abs), "show", ":./"+filepath.Base(abs)).Output()
	if err != nil {
		return "", false
	}
	// A buffer holds no final line break, as it reads files (N5).
	return strings.TrimSuffix(string(out), "\n"), true
}

// loadGit reads b's base from git off the editor loop, then diffs it
// (B123), or a listing's git status (B126).
func (ed *Editor) loadGit(b *buffer) {
	if b.listed != "" && hasGit() {
		dir, had := b.listed, b.status != nil || b.branch != ""
		go func() {
			at := time.Now()
			signs, ok := gitStatus(dir)
			branch := gitBranch(dir)
			traced("file", "read the git status of "+dir, at, nil)
			if ok || had {
				ed.queue <- func() { b.status, b.branch = signs, branch }
			}
		}()
		return
	}
	if b.file.path == "" || b.title != "" || !hasGit() {
		return
	}
	path, had := b.file.path, b.base != nil || b.branch != ""
	go func() {
		at := time.Now()
		base, ok := gitBase(path)
		branch := gitBranch(filepath.Dir(path))
		traced("file", "read the git base of "+path, at, nil)
		if !ok && !had && branch == "" {
			// Nothing to show or to clear, so the loop need not hear of it.
			return
		}
		ed.queue <- func() {
			b.branch = branch
			if !ok {
				b.base, b.git = nil, nil
				return
			}
			b.base, b.gitStale = &base, true
		}
	}()
}

// gitEvery is how often at most a buffer's signs diff, so typing in a long
// file copies and diffs its text a few times a second, not once a key.
const gitEvery = 100 * time.Millisecond

// diffGit diffs b's text with its base off the editor loop, when the text
// changed since the last diff. One diff runs at a time, and a change made
// while it runs, or within gitEvery after, starts another then.
func (ed *Editor) diffGit(b *buffer) {
	if b.base == nil || !b.gitStale || b.gitBusy {
		return
	}
	b.gitStale, b.gitBusy = false, true
	base, t := *b.base, b.text.Snapshot()
	go func() {
		signs := gitSigns(base, t.Slice(0, t.Len()))
		ed.queue <- func() { b.git = signs }
		time.Sleep(gitEvery)
		// The frame after this diffs what changed meanwhile.
		ed.queue <- func() { b.gitBusy = false }
	}()
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
	ops := lineDiff(strings.Split(a, "\n"), strings.Split(b, "\n"))
	var out []hunk
	line := strings.Count(base[:pre], "\n") // the line of now the next op is at
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			line, i = line+1, i+1
			continue
		}
		h := hunk{line: line}
		for ; i < len(ops) && ops[i].kind != ' '; i++ {
			if ops[i].kind == '-' {
				h.old = append(h.old, ops[i].line)
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

// gitSigns returns the sign of each line of now that differs from base.
func gitSigns(base, now string) map[int]Sign {
	out := map[int]Sign{}
	for _, h := range hunks(base, now) {
		add, del := h.count, len(h.old)
		for j := range add {
			out[h.line+j] = gitAdd
			if j < del {
				out[h.line+j] = gitChange
			}
		}
		switch {
		case add == 0 && h.line == 0:
			out[0] = gitTopDelete
		case add == 0:
			out[h.line-1] = gitDelete
		case del > add:
			out[h.line+add-1] = gitChangeDelete
		}
	}
	return out
}

// hunks returns b's hunks against git's index, or false outside a
// repository, after telling the user so.
func (e *Session) hunks(b *buffer) ([]hunk, bool) {
	if b.base == nil {
		e.message = "not in a git repository"
		return nil, false
	}
	t := b.text.Snapshot()
	return hunks(*b.base, t.Slice(0, t.Len())), true
}

// jumpHunk moves the cursor to the start of a hunk: the count'th after its
// line for ]h, before it for [h, or the first or last for [H and ]H
// (B138).
func (e *Session) jumpHunk(to string, count int) {
	hs, ok := e.hunks(e.win.buf)
	if !ok {
		return
	}
	if len(hs) == 0 {
		e.message = "no hunks"
		return
	}
	t := e.Text()
	cur := t.LineOf(e.Cursor())
	var starts []int
	for _, h := range hs {
		start, _ := h.span()
		starts = append(starts, start)
	}
	i := -1
	switch to {
	case "first":
		i = 0
	case "last":
		i = len(starts) - 1
	case "next":
		if j := slices.IndexFunc(starts, func(l int) bool { return l > cur }); j >= 0 {
			i = min(j+count-1, len(starts)-1)
		}
	case "prev":
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
		e.message = "no " + to + " hunk"
		return
	}
	e.setCursor(t.LineStart(starts[i]))
}

// resetHunks returns the edits that put the hunks meeting the lines from
// start to end back as git's index has them, as gH does (B138).
func (e *Session) resetHunks(t vim.Text, start, end int, _ bool) []vim.Edit {
	hs, ok := e.hunks(e.win.buf)
	if !ok {
		return nil
	}
	now := t.Slice(0, t.Len())
	lines := strings.Split(now, "\n")
	first, last := t.LineOf(start), t.LineOf(end)
	for _, h := range slices.Backward(hs) {
		if lo, hi := h.span(); hi >= first && lo <= last {
			lines = slices.Concat(lines[:h.line], h.old, lines[h.line+h.count:])
		}
	}
	var edits []vim.Edit
	for _, ed := range lineEdits(now, strings.Join(lines, "\n")) {
		edits = append(edits, vim.Edit(ed))
	}
	return edits
}

// previewHunk shows the hunk on the cursor's line in a float, its old lines
// after - and its new ones after + (B138).
func (e *Session) previewHunk() {
	b := e.win.buf
	hs, ok := e.hunks(b)
	if !ok {
		return
	}
	t := b.text.Snapshot()
	cur := t.LineOf(e.Cursor())
	for _, h := range hs {
		if lo, hi := h.span(); cur < lo || cur > hi {
			continue
		}
		var s strings.Builder
		var spans []syntax.Span
		line := func(prefix, text, style string) {
			if s.Len() > 0 {
				s.WriteByte('\n')
			}
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + 1 + len(text), Name: style})
			s.WriteString(prefix + text)
		}
		for _, l := range h.old {
			line("-", l, "diff.minus")
		}
		for i := h.line; i < h.line+h.count; i++ {
			start := t.LineStart(i)
			line("+", t.Slice(start, vim.LineEnd(t, start)), "diff.plus")
		}
		e.float, e.floatSpans = s.String(), spans
		return
	}
	e.message = "no hunk on this line"
}
