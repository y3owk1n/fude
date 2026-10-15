package editor

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// finder is the fuzzy file finder :files opens, or the live grep :grep
// opens: a box whose first line is the query, typed in insert mode, with
// the files or lines that match it below, the best first (B129, B134).
type finder struct {
	grep    bool     // it searches files' lines, not their names
	paths   []string // the working directory's files, nil until read
	query   string   // the query matches are for
	matches []string // paths, or grep's path:line:column:text
	sel     int
	stale   bool   // paths came after matches were found
	busy    bool   // a match runs
	shown   string // the match previewed
	preview string // the lines around it
	at      int    // the row of the preview its line is on
}

// openFinder opens f in a box with query typed.
func (e *Session) openFinder(f *finder, query string) {
	b := &buffer{text: kernel.New(query, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}, title: "[find]"}
	e.finder, e.under, e.win = f, e.win, newWindow(b, 0, 0)
	e.later = append(e.later, "A")
}

// files opens the file finder and reads the working directory's files off
// the loop.
func (e *Session) files() {
	f := &finder{}
	e.openFinder(f, "")
	go func() {
		paths, err := listFiles(".")
		e.queue <- func() {
			if err != nil {
				e.message = err.Error()
			}
			f.paths, f.stale = paths, true
		}
	}()
}

// listFiles returns the files below dir: in a repository, those git tracks
// and the untracked ones it does not ignore, and elsewhere every file but
// those in unreadable directories.
func listFiles(dir string) ([]string, error) {
	if hasGit() {
		// Outside a repository git fails, and the walk lists the files.
		out, err := exec.Command("git", "-C", dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
		if err == nil {
			return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 }), nil
		}
	}
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil && path == dir:
			return err
		case err != nil || d.IsDir() && d.Name() == ".git":
			return fs.SkipDir
		case !d.IsDir():
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

// finding takes a key while the finder shows: <CR> opens the selected file,
// <C-n> and <C-p> move the selection, and other keys edit the query.
// Leaving insert mode closes it.
func (e *Session) finding(k vim.Key) {
	f := e.finder
	switch k {
	case "<CR>":
		e.closeFinder()
		if f.sel < len(f.matches) {
			path, line, col, _ := grepSpot(f.matches[f.sel])
			if e.edit(path); line > 0 && e.win.buf.is(path) {
				t := e.Text()
				start := t.LineStart(min(line-1, t.Lines()-1))
				e.setCursor(min(start+col-1, vim.LineEnd(t, start)))
			}
		}
		return
	case "<C-n>":
		f.sel = max(min(f.sel+1, len(f.matches)-1), 0)
		return
	case "<C-p>":
		f.sel = max(f.sel-1, 0)
		return
	}
	e.settle(e.vim.Feed(k))
	if e.vim.Mode() != vim.Insert {
		e.closeFinder()
	}
}

// closeFinder leaves insert mode in the query and closes the finder.
func (e *Session) closeFinder() {
	if e.vim.Mode() == vim.Insert {
		e.settle(e.vim.Feed("<Esc>"))
	}
	e.finder = nil
	e.unfocus()
}

// seek matches the query off the loop when it changed since the last
// match. One match runs at a time, and a query that grows searches only the
// last matches.
func (e *Session) seek(f *finder) {
	t := e.Text()
	q := t.Slice(0, t.Len())
	if f.busy || !f.grep && f.paths == nil || q == f.query && !f.stale {
		return
	}
	from := f.paths
	if !f.stale && strings.HasPrefix(q, f.query) {
		from = f.matches
	}
	f.busy, f.stale = true, false
	go func() {
		var matches []string
		var err error
		if f.grep {
			matches, err = grep(q)
		} else {
			matches = match(from, q)
		}
		e.queue <- func() {
			if err != nil {
				e.message = err.Error()
			}
			f.query, f.matches, f.sel, f.busy = q, matches, 0, false
		}
	}()
}

// float shows the finder in the middle of the screen, at most 20 rows tall,
// with the cursor in the query. On a screen of 60 columns or more, the
// selected file's first lines show in a box right of the list (B130).
func (f *finder) float(query string, cursor, width, height int) Float {
	total, h := min(162, width), min(22, height)
	w, docW := total, 0
	if total >= 60 {
		w = total * 2 / 5
		docW = total - w
	}
	rows := max(h-3, 0) // below the query
	first := max(f.sel-rows+1, 0)
	var s strings.Builder
	var spans []syntax.Span
	s.WriteString(query)
	for i := first; i < first+rows; i++ {
		s.WriteByte('\n')
		if i >= len(f.matches) {
			continue
		}
		p := f.matches[i]
		if path, line, _, text := grepSpot(p); f.grep {
			p = fmt.Sprintf("%s:%d: %s", path, line, strings.TrimLeft(text, " \t"))
			if r := []rune(p); len(r) > w-2 {
				p = string(r[:w-2])
			}
		} else if r := []rune(p); len(r) > w-2 {
			// Cut the start of a long path, so its name stays in view.
			p = "<" + string(r[len(r)-w+3:])
		}
		if i == f.sel {
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(p), Name: "menu.selected"})
		}
		s.WriteString(p)
	}
	doc, docSpans := "", []syntax.Span(nil)
	if docW > 0 && f.sel < len(f.matches) {
		if m := f.matches[f.sel]; m != f.shown {
			path, line, _, _ := grepSpot(m)
			f.shown = m
			f.preview, f.at = preview(path, max(line-1, 0), h-2, docW-2)
		}
		doc = f.preview
		if f.grep && f.at >= 0 {
			// The matching line is marked.
			start := 0
			for range f.at {
				start += strings.IndexByte(doc[start:], '\n') + 1
			}
			end := strings.IndexByte(doc[start:], '\n')
			if end < 0 {
				end = len(doc) - start
			}
			docSpans = []syntax.Span{{Start: start, End: start + end, Name: "menu.selected"}}
		}
	}
	return Float{Text: s.String(), Spans: spans, Cursor: cursor, Doc: doc, DocSpans: docSpans, X: (width - total) / 2, Y: (height - h) / 2, W: w, H: h, DocW: docW}
}

// preview returns rows lines of the file at path from a third of them
// above line, cut to cols columns with tabs as 4 spaces, and the row line
// is on. A file with a zero byte in its first 16KB, or one that fails to
// read, shows why there is no preview.
func preview(path string, line, rows, cols int) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return err.Error(), -1
	}
	defer f.Close()
	r := bufio.NewReader(f)
	if head, _ := r.Peek(16 << 10); bytes.IndexByte(head, 0) >= 0 {
		return "binary file", -1
	}
	first := max(line-rows/3, 0)
	var lines []string
	for i := 0; len(lines) < rows; i++ {
		l, err := r.ReadString('\n')
		if i >= first && (l != "" || err == nil) {
			c := []rune(strings.ReplaceAll(strings.TrimSuffix(l, "\n"), "\t", "    "))
			lines = append(lines, string(c[:min(len(c), cols)]))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err.Error(), -1
		}
	}
	return strings.Join(lines, "\n"), line - first
}

// match returns the paths query matches, the best first, and all of them
// for no query.
func match(paths []string, query string) []string {
	if query == "" {
		return paths
	}
	type scored struct {
		path  string
		score float64
	}
	var found []scored
	q, scratch := lower(query), []float64(nil)
	for _, p := range paths {
		if s, ok := fuzzy(q, p, &scratch); ok {
			found = append(found, scored{p, s})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.path
	}
	return out
}

// The scores of fzy's algorithm, which ranks as matchfuzzy() does.
const (
	gapLeading   = -0.005
	gapTrailing  = -0.005
	gapInner     = -0.01
	matchRun     = 1.0
	matchSlash   = 0.9
	matchWord    = 0.8
	matchCapital = 0.7
	matchDot     = 0.6
)

// fuzzy scores how well q, in lower case, matches s, ignoring case, or
// reports false when s lacks q's letters in order. A letter scores more
// after a separator or as a capital, and runs of letters score more than
// scattered ones. scratch is memory to reuse between calls.
func fuzzy(q, s string, scratch *[]float64) (float64, bool) {
	i := 0
	for j := 0; j < len(s) && i < len(q); j++ {
		if lowerByte(s[j]) == q[i] {
			i++
		}
	}
	if i < len(q) {
		return 0, false
	}
	if len(q) == len(s) {
		return math.MaxFloat64, true
	}
	n := len(s)
	if cap(*scratch) < 5*n {
		*scratch = make([]float64, 5*n)
	}
	buf := (*scratch)[:5*n]
	bonus, d, m, pd, pm := buf[:n], buf[n:2*n], buf[2*n:3*n], buf[3*n:4*n], buf[4*n:]
	clear(bonus)
	prev := byte('/')
	for j := range n {
		c := s[j]
		switch {
		case prev == '/':
			bonus[j] = matchSlash
		case prev == '-' || prev == '_' || prev == ' ':
			bonus[j] = matchWord
		case prev == '.':
			bonus[j] = matchDot
		case prev >= 'a' && prev <= 'z' && c >= 'A' && c <= 'Z':
			bonus[j] = matchCapital
		}
		prev = c
	}
	// d is the best score with q[i] matched at s[j], and m the best with
	// q[:i+1] matched by s[:j+1]. pd and pm are the row of q[i-1].
	for i := range len(q) {
		best := math.Inf(-1)
		gap := gapInner
		if i == len(q)-1 {
			gap = gapTrailing
		}
		for j := range n {
			if q[i] != lowerByte(s[j]) {
				d[j] = math.Inf(-1)
			} else {
				switch {
				case i == 0:
					d[j] = float64(j)*gapLeading + bonus[j]
				case j > 0:
					d[j] = max(pm[j-1]+bonus[j], pd[j-1]+matchRun)
				default:
					d[j] = math.Inf(-1)
				}
			}
			best = max(d[j], best+gap)
			m[j] = best
		}
		d, pd = pd, d
		m, pm = pm, m
	}
	return pm[n-1], true
}

// lower returns s with ASCII letters in lower case, byte for byte.
func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		b[i] = lowerByte(c)
	}
	return string(b)
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
