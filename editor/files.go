package editor

import (
	"bytes"
	"cmp"
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

// finder is the fuzzy file finder :files opens: a box whose first line is
// the query, typed in insert mode, with the files that match it below, the
// best first (B129).
type finder struct {
	paths   []string // the working directory's files, nil until read
	query   string   // the query matches are for
	matches []string
	sel     int
	stale   bool   // paths came after matches were found
	busy    bool   // a match runs
	shown   string // the path previewed
	preview string // its first lines
}

// files opens the finder and reads the working directory's files off the
// loop.
func (e *Session) files() {
	f := &finder{}
	b := &buffer{text: kernel.New("", nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}, title: "[files]"}
	e.finder, e.under, e.win = f, e.win, newWindow(b, 0, 0)
	e.later = append(e.later, "i")
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
			e.edit(f.matches[f.sel])
		}
		return
	case "<C-n>", "<Down>":
		f.sel = max(min(f.sel+1, len(f.matches)-1), 0)
		return
	case "<C-p>", "<Up>":
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
	if f.busy || f.paths == nil || q == f.query && !f.stale {
		return
	}
	from := f.paths
	if !f.stale && strings.HasPrefix(q, f.query) {
		from = f.matches
	}
	f.busy, f.stale = true, false
	go func() {
		matches := match(from, q)
		e.queue <- func() { f.query, f.matches, f.sel, f.busy = q, matches, 0, false }
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
		if r := []rune(p); len(r) > w-2 {
			// Cut the start of a long path, so its name stays in view.
			p = "<" + string(r[len(r)-w+3:])
		}
		if i == f.sel {
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(p), Name: "menu.selected"})
		}
		s.WriteString(p)
	}
	doc := ""
	if docW > 0 && f.sel < len(f.matches) {
		if p := f.matches[f.sel]; p != f.shown {
			f.shown, f.preview = p, preview(p, h-2, docW-2)
		}
		doc = f.preview
	}
	return Float{Text: s.String(), Spans: spans, Cursor: cursor, Doc: doc, X: (width - total) / 2, Y: (height - h) / 2, W: w, H: h, DocW: docW}
}

// preview returns at most rows lines from the start of the file at path,
// cut to cols columns with tabs as 4 spaces, or why it shows none.
func preview(path string, rows, cols int) string {
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer f.Close()
	buf := make([]byte, 16<<10)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err.Error()
	}
	if bytes.IndexByte(buf[:n], 0) >= 0 {
		return "binary file"
	}
	lines := strings.Split(strings.TrimSuffix(string(buf[:n]), "\n"), "\n")
	lines = lines[:min(len(lines), rows)]
	for i, l := range lines {
		r := []rune(strings.ReplaceAll(l, "\t", "    "))
		lines[i] = string(r[:min(len(r), cols)])
	}
	return strings.Join(lines, "\n")
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
