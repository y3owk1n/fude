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
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// finder is the fuzzy file finder :files opens, or the live grep :grep
// opens: a box whose first line is the query, typed in insert mode, with
// the files or lines that match it below, the best first (B129, B134).
type finder struct {
	grep     bool     // it searches files' lines, not their names
	paths    []string // the working directory's files, nil until read
	query    string   // the query matches are for
	matches  []string // paths, or grep's path:line:column:text
	sel      int
	stale    bool                // paths came after matches were found
	busy     bool                // a match runs
	shown    string              // the match previewed
	preview  string              // the lines around it
	spans    []syntax.Span       // their highlights
	at       int                 // the row of the preview its line is on
	reading  bool                // a preview is read off the loop
	open     bool                // <Plug>(finder.open) ran, for finding to act on after the key
	queries  string              // where highlight queries are, for the preview
	filetype func(string) string // the filetype of a path, which config.toml does not change while fude runs
	queue    chan<- func()       // the editor loop's, for the preview
}

// openFinder opens f in a box with query typed.
func (e *Session) openFinder(f *finder, query string) {
	f.queries, f.queue, f.filetype = e.queries, e.queue, e.filetypeOf
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

// finderKeys are the finder's own keys, in insert mode: <CR> opens the
// selected file, and <C-n> and <C-p> move the selection. [keymap.finder]
// changes them (B142).
var finderKeys = map[string]string{
	"<CR>": "<Plug>(finder.open)", "<C-n>": "<Plug>(finder.next)", "<C-p>": "<Plug>(finder.prev)",
}

// finderAct runs act, one of the finder's actions, while the finder shows.
// open waits for the key to end, as closing the finder feeds the machine.
func (e *Session) finderAct(act string) {
	f := e.finder
	if f == nil {
		e.message = "no finder"
		return
	}
	switch act {
	case "open":
		f.open = true
	case "next":
		f.sel = max(min(f.sel+1, len(f.matches)-1), 0)
	case "prev":
		f.sel = max(f.sel-1, 0)
	}
}

// finding takes a key while the finder shows. Its keymap runs its actions,
// other keys edit the query, and leaving insert mode closes it.
func (e *Session) finding(k vim.Key) {
	f := e.finder
	e.vim.UseLocal(e.locals["finder"])
	e.settle(e.vim.Feed(k))
	switch {
	case f.open:
		e.closeFinder()
		if f.sel < len(f.matches) {
			path, line, col, _ := grepSpot(f.matches[f.sel])
			if e.edit(path); line > 0 && e.win.buf.is(path) {
				t := e.Text()
				start := t.LineStart(min(line-1, t.Lines()-1))
				e.setCursor(min(start+col-1, vim.LineEnd(t, start)))
			}
		}
	case e.vim.Mode() != vim.Insert:
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
	// A margin of two columns and a row keeps the box off the screen's edges.
	total, h := min(162, width-4), min(22, height-2)
	w, docW := total, 0
	if total >= 60 {
		w = total * 2 / 5
		docW = total - w
	}
	rows := max(h-4, 0) // below the query and its rule
	first := max(f.sel-rows+1, 0)
	var s strings.Builder
	var spans []syntax.Span
	// The query after a prompt, with the count of matches on the right.
	s.WriteString(finderPrompt + query)
	count := fmt.Sprintf("%d / %d", len(f.matches), len(f.paths))
	if f.grep {
		count = strconv.Itoa(len(f.matches))
	}
	if gap := w - 4 - utf8.RuneCountInString(finderPrompt+query) - len(count); gap > 1 {
		s.WriteString(strings.Repeat(" ", gap))
		spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(count), Name: "comment"})
		s.WriteString(count)
	}
	s.WriteByte('\n')
	spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len(), Name: "markup.rule"})
	for i := first; i < first+rows; i++ {
		s.WriteByte('\n')
		if i >= len(f.matches) {
			continue
		}
		p := f.matches[i]
		var marks []int // the bytes of p to mark as matching the query
		if path, line, _, text := grepSpot(p); f.grep {
			head := fmt.Sprintf("%s:%d: ", path, line)
			p = head + strings.TrimLeft(text, " \t")
			if r := []rune(p); len(r) > w-4 {
				p = string(r[:w-4])
			}
			marks = occurrences(p, f.query, min(len(head), len(p)))
		} else {
			marks = positions(lower(f.query), p)
			if r := []rune(p); len(r) > w-4 {
				// Cut the start of a long path, so its name stays in view.
				cut := len(p) - len(string(r[len(r)-w+5:]))
				p = "<" + p[cut:]
				marks = slices.DeleteFunc(marks, func(at int) bool { return at < cut })
				for k := range marks {
					marks[k] += 1 - cut
				}
			}
		}
		if i == f.sel {
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(p), Name: "menu.selected"})
		}
		for _, at := range marks {
			spans = append(spans, syntax.Span{Start: s.Len() + at, End: s.Len() + at + 1, Name: "match"})
		}
		s.WriteString(p)
	}
	doc, docSpans := "", []syntax.Span(nil)
	if docW > 0 && f.sel < len(f.matches) {
		if m := f.matches[f.sel]; m != f.shown && !f.reading {
			f.reading = true
			go f.read(m, h-4, docW-4)
		}
		// The previewed file's path heads the preview, over a rule.
		path, _, _, _ := grepSpot(f.shown)
		head := path + "\n\n"
		doc = head + f.preview
		docSpans = []syntax.Span{{Start: 0, End: len(path), Name: "comment"}, {Start: len(path) + 1, End: len(path) + 1, Name: "markup.rule"}}
		for _, sp := range f.spans {
			docSpans = append(docSpans, syntax.Span{Start: sp.Start + len(head), End: sp.End + len(head), Name: sp.Name})
		}
		if f.grep && f.at >= 0 {
			// The matching line is marked.
			start := len(head)
			for range f.at {
				start += strings.IndexByte(doc[start:], '\n') + 1
			}
			end := strings.IndexByte(doc[start:], '\n')
			if end < 0 {
				end = len(doc) - start
			}
			docSpans = append(slices.Clip(docSpans), syntax.Span{Start: start, End: start + end, Name: "menu.selected"})
		}
	}
	return Float{Text: s.String(), Spans: spans, Cursor: cursor + len(finderPrompt), Doc: doc, DocSpans: docSpans, X: (width - total) / 2, Y: (height - h) / 2, W: w, H: h, DocW: docW}
}

// finderPrompt goes before the finder's query.
const finderPrompt = "› "

// read reads and highlights the preview of match m off the loop, as the
// highlight takes over a millisecond (B130).
func (f *finder) read(m string, rows, cols int) {
	path, line, _, _ := grepSpot(m)
	text, at := preview(path, max(line-1, 0), rows, cols)
	var spans []syntax.Span
	if at >= 0 {
		var err error
		if spans, err = syntax.Highlight(f.filetype(path), f.queries, text); err != nil {
			logLine(err.Error())
		}
	}
	f.queue <- func() {
		f.shown, f.preview, f.spans, f.at, f.reading = m, text, spans, at, false
	}
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
	bonuses(s, bonus)
	for i := range len(q) {
		row(q, i, s, bonus, pd, pm, d, m)
		d, pd = pd, d
		m, pm = pm, m
	}
	return pm[n-1], true
}

// positions returns where in s each letter of q, in lower case, matches
// for fuzzy's best score, or nil when s lacks them, so a list can mark them
// (B152).
func positions(q, s string) []int {
	if _, ok := fuzzy(q, s, new([]float64)); !ok || q == "" {
		return nil
	}
	n := len(s)
	bonus := make([]float64, n)
	bonuses(s, bonus)
	d, m := make([][]float64, len(q)), make([][]float64, len(q))
	for i := range len(q) {
		d[i], m[i] = make([]float64, n), make([]float64, n)
		var pd, pm []float64
		if i > 0 {
			pd, pm = d[i-1], m[i-1]
		}
		row(q, i, s, bonus, pd, pm, d[i], m[i])
	}
	// Back from the end, each letter takes the last place that leads to the
	// best score, or the place a run of letters needs.
	out := make([]int, len(q))
	run := false
	j := n - 1
	for i := len(q) - 1; i >= 0; i-- {
		for ; j >= 0; j-- {
			if !math.IsInf(d[i][j], -1) && (run || d[i][j] == m[i][j]) {
				run = i > 0 && j > 0 && m[i][j] == d[i-1][j-1]+matchRun
				out[i] = j
				j--
				break
			}
		}
	}
	return out
}

// occurrences returns the bytes of each place in s from start that query
// is at, ignoring case for a query without capitals, as grep searched.
func occurrences(s, query string, start int) []int {
	hay := s[start:]
	if query == lower(query) {
		hay = lower(hay)
	}
	var out []int
	for at := 0; query != ""; at += len(query) {
		i := strings.Index(hay[at:], query)
		if i < 0 {
			break
		}
		at += i
		for k := range len(query) {
			out = append(out, start+at+k)
		}
	}
	return out
}

// bonuses fills bonus with what a match at each byte of s scores for the
// byte before it: after a separator, or a capital after a lower case letter.
func bonuses(s string, bonus []float64) {
	clear(bonus)
	prev := byte('/')
	for j := range len(s) {
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
}

// row fills d and m for q's letter i from pd and pm, the row of q[i-1]: d
// is the best score with q[i] matched at s[j], and m the best with
// q[:i+1] matched by s[:j+1].
func row(q string, i int, s string, bonus, pd, pm, d, m []float64) {
	best := math.Inf(-1)
	gap := gapInner
	if i == len(q)-1 {
		gap = gapTrailing
	}
	for j := range len(s) {
		switch {
		case q[i] != lowerByte(s[j]):
			d[j] = math.Inf(-1)
		case i == 0:
			d[j] = float64(j)*gapLeading + bonus[j]
		case j > 0:
			d[j] = max(pm[j-1]+bonus[j], pd[j-1]+matchRun)
		default:
			d[j] = math.Inf(-1)
		}
		best = max(d[j], best+gap)
		m[j] = best
	}
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
