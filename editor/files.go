package editor

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// finder is the fuzzy file finder :pick files opens, or the live grep
// :pick grep opens: a box whose first line is the query, typed in insert
// mode, with the files or lines that match it below, the best first (B129,
// B134).
type finder struct {
	files  bool   // it finds the working directory's files by name
	places bool   // its items are places, path:line:column:text, shown under their file
	enc    string // the encoding of places' columns, "" for bytes
	hint   string // what an empty query of a live search shows
	// lookup asks for the query's items on the loop and passes them to
	// found, for a search a language server answers.
	lookup   func(q string, found func([]string, error))
	all      bool     // it searches the files git ignores and hidden ones too
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
	image    string              // the image file previewed, when it is one (B305)
	open     bool                // <Plug>(finder.open) ran, for finding to act on after the key
	list     bool                // <Plug>(finder.list) ran
	queries  string              // where highlight queries are, for the preview
	filetype func(string) string // the filetype of a path, which config.toml does not change while fude runs
	project  string              // for a guest, the project outside which nothing previews (B245)
	queue    chan<- func()       // the editor loop's, for the preview
	// prompt and chose are a plugin's list or question (B185). chose runs
	// instead of opening a file. It gets the index in paths of the item
	// picked, or in the matches of a live list, or -1, the item, the query,
	// and false when cancelled.
	prompt string
	title  string // drawn in the box's border, such as :pick's source
	chose  func(picked int, item, query string, ok bool)
	// A module's live pick: the module and pick its items come from, and
	// found takes the items it answers a query with (B285).
	owner api.Module
	pick  uint32
	found func([]string, error)
}

// openFinder opens f in a box with query typed.
func (e *Session) openFinder(f *finder, query string) {
	f.queries, f.queue, f.filetype = e.queries, e.queue, e.filetypeOf
	if e.power != Host {
		f.project = e.project
	}
	b := &buffer{text: kernel.New(query, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}, title: "[find]"}
	e.finder, e.under, e.win = f, e.win, newWindow(b, 0, 0)
	e.later = append(e.later, "A")
}

// files opens the file finder.
func (e *Session) files() {
	f := &finder{files: true}
	e.openFinder(f, "")
	e.readPaths(f)
}

// readPaths reads the working directory's files for f off the loop. It
// drops a list read before f.all changed.
func (e *Session) readPaths(f *finder) {
	all := f.all
	go func() {
		at := time.Now()
		paths, err := listFiles(".", all)
		traced("file", fmt.Sprintf("listed %d files", len(paths)), at, err)
		e.queue <- func() {
			if err != nil {
				e.message = err.Error()
			}
			if f.all == all {
				f.paths, f.stale = paths, true
			}
		}
	}()
}

// listFiles returns the files below dir. In a repository they are those git
// tracks and the untracked ones it does not ignore. Elsewhere, or with all,
// they are every file outside .git and unreadable directories.
func listFiles(dir string, all bool) ([]string, error) {
	if !all && hasGit() {
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
// selected file, <C-n> and <C-p> move the selection, <C-q> lists every
// match and <C-g> turns searching ignored files on or off. [keymap.finder]
// changes them (B142).
var finderKeys = map[string]string{
	"<CR>": "<Plug>(finder.open)", "<C-n>": "<Plug>(finder.next)", "<C-p>": "<Plug>(finder.prev)",
	"<C-q>": "<Plug>(finder.list)", "<C-g>": "<Plug>(finder.all)",
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
	case "list":
		f.list = true
	case "next":
		f.sel = max(min(f.sel+1, len(f.matches)-1), 0)
	case "prev":
		f.sel = max(f.sel-1, 0)
	case "all":
		if !f.files && f.owner == nil {
			// Only the files and live finders have files to add.
			return
		}
		f.all, f.stale = !f.all, true
		if f.files {
			f.paths = nil
			e.readPaths(f)
		}
	}
}

// key takes a key while the finder shows. Its keymap runs its actions,
// other keys edit the query, and leaving insert mode closes it.
func (f *finder) key(e *Session, k vim.Key) bool {
	if f == nil {
		return false
	}
	defer e.reveal()
	e.vim.UseLocal(e.locals["finder"])
	// A query matches what is typed, so a bracket in it comes alone (B202).
	e.vim.AutoPairs(false)
	e.settle(e.vim.Feed(k))
	e.vim.AutoPairs(e.cfg.Autopairs)
	t := e.Text()
	query := t.Slice(0, t.Len())
	if (f.open || f.list) && query != f.query && f.lookup == nil && f.paths != nil {
		// Keys typed faster than matching runs pick from what they typed.
		f.query, f.matches, f.sel = query, match(f.paths, query), 0
	}
	switch {
	case f.list && (f.chose == nil || f.places):
		e.closeFinder()
		e.listMatches(f)
	case f.open:
		e.closeFinder()
		if f.chose != nil {
			picked, item := -1, ""
			if f.sel < len(f.matches) {
				item = f.matches[f.sel]
				if picked = slices.Index(f.paths, item); picked < 0 && f.lookup != nil {
					picked = f.sel
				}
			}
			f.chose(picked, item, query, true)
		}
		// A place opens as the finder's own do, after the module hears of it.
		if (f.chose == nil || f.places) && f.sel < len(f.matches) {
			path, line, col, _ := grepSpot(f.matches[f.sel])
			if e.edit(path); line > 0 && e.win.buf.is(path) {
				t := e.Text()
				start := t.LineStart(min(line-1, t.Lines()-1))
				at := start + col - 1
				if f.enc != "" {
					at = lsp.Offset(t, lsp.Position{Line: line - 1, Character: col - 1}, f.enc)
				}
				e.setCursor(min(at, vim.LineEnd(t, start)))
			}
		}
	case e.vim.Mode() != vim.Insert:
		e.closeFinder()
		if f.chose != nil {
			f.chose(-1, "", query, false)
		}
	}
	return true
}

// listMatches shows the finder's matches in a list of places, which
// becomes the last list (B195).
func (e *Session) listMatches(f *finder) {
	var lines []string
	var spots []spot
	for _, m := range f.matches {
		path, line, col, text := grepSpot(m)
		sp := spot{path: path, enc: cmp.Or(f.enc, "utf-8")}
		if line > 0 {
			sp.pos = lsp.Position{Line: line - 1, Character: col - 1}
			m = fmt.Sprintf("%s:%d:%d: %s", path, line, col, strings.TrimSpace(text))
		}
		lines, spots = append(lines, m), append(spots, sp)
	}
	what := "files"
	if f.places {
		what = cmp.Or(f.title, "matches")
	}
	e.places(what, lines, spots)
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
	if f.busy || f.lookup == nil && f.paths == nil || q == f.query && !f.stale {
		return
	}
	if f.lookup != nil {
		f.busy, f.stale = true, false
		f.lookup(q, func(matches []string, err error) {
			if err != nil {
				e.message = err.Error()
			}
			f.query, f.matches, f.sel, f.busy = q, matches, 0, false
		})
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
func (f *finder) float(query string, cursor, width, height int) wire.Float {
	// A margin of two columns and a row keeps the box off the screen's edges.
	total, h := min(162, width-4), min(22, height-2)
	w, docW := total, 0
	if total >= 60 && (f.chose == nil || f.places) {
		w = total / 2
		docW = total - w
	}
	rows := max(h-4, 0) // below the query and its rule
	var s strings.Builder
	var spans []syntax.Span
	// The query after a prompt, with the count of matches on the right.
	prompt := cmp.Or(f.prompt, finderPrompt)
	s.WriteString(prompt)
	s.WriteString(query)
	count := fmt.Sprintf("%d / %d", len(f.matches), len(f.paths))
	if f.lookup != nil {
		count = strconv.Itoa(len(f.matches))
	}
	if f.all {
		count = "with ignored  " + count
	}
	if gap := w - 4 - utf8.RuneCountInString(prompt+query) - len(count); gap > 1 {
		s.WriteString(strings.Repeat(" ", gap))
		spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(count), Name: "comment"})
		s.WriteString(count)
	}
	s.WriteByte('\n')
	spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len(), Name: "markup.rule"})
	// Grep's matches show under a row for their file, so each is just its
	// line number and text. A row is a match, or its file's name when header.
	type listRow struct {
		match  int
		header bool
	}
	var list []listRow
	selRow, lineW := 0, 1
	for i, m := range f.matches {
		if path, line, _, _ := grepSpot(m); f.places {
			if prev, _, _, _ := grepSpot(f.matches[max(i-1, 0)]); i == 0 || prev != path {
				list = append(list, listRow{i, true})
			}
			lineW = max(lineW, len(strconv.Itoa(line)))
		}
		if i == f.sel {
			selRow = len(list)
		}
		list = append(list, listRow{i, false})
	}
	first := min(max(selRow-rows+1, 0), len(list))
	for _, r := range list[first:min(first+rows, len(list))] {
		s.WriteByte('\n')
		start := s.Len()
		m := f.matches[r.match]
		switch path, line, _, text := grepSpot(m); {
		case r.header:
			writePath(&s, &spans, path, nil, w-4, "finder.file")
		case f.places:
			num := fmt.Sprintf("%*d  ", lineW, line)
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + lineW, Name: "finder.dir"})
			s.WriteString(num)
			text = strings.TrimLeft(text, " \t")
			if r := []rune(text); len(r) > w-4-len(num) {
				text = string(r[:max(w-4-len(num), 0)])
			}
			marks := occurrences(text, f.query, 0)
			if f.places {
				marks = positions(lower(f.query), text)
			}
			for _, at := range marks {
				spans = append(spans, syntax.Span{Start: s.Len() + at, End: s.Len() + at + 1, Name: "match"})
			}
			s.WriteString(text)
		case f.chose != nil:
			// An item of a list is text, not a path.
			if r := []rune(m); len(r) > w-4 {
				m = string(r[:max(w-4, 0)])
			}
			for _, at := range positions(lower(f.query), m) {
				spans = append(spans, syntax.Span{Start: s.Len() + at, End: s.Len() + at + 1, Name: "match"})
			}
			s.WriteString(m)
		default:
			writePath(&s, &spans, m, positions(lower(f.query), m), w-4, "")
		}
		if r.match == f.sel && !r.header {
			spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: "menu.selected"})
		}
	}
	// An empty list says why, once the matches are for the query typed.
	if len(list) == 0 && f.query == query && !f.stale && !f.busy && (f.lookup != nil || f.paths != nil) {
		list = append(list, listRow{})
		empty := "no matches"
		if f.hint != "" && query == "" {
			empty = f.hint
		}
		s.WriteByte('\n')
		spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(empty), Name: "comment"})
		s.WriteString(empty)
	}
	if docW > 0 {
		// The list is as tall as the preview beside it.
		s.WriteString(strings.Repeat("\n", max(rows-len(list), 0)))
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
		if f.places && f.at >= 0 {
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
	return wire.Float{Text: s.String(), Spans: spans, Title: f.title, Cursor: cursor + len(prompt), Doc: doc, DocSpans: docSpans, X: (width - total) / 2, Y: (height - h) / 2, W: w, H: h, DocW: docW}
}

// writePath writes path in at most width columns: its name, in style when
// given, then its directory dimmed and cut in the middle to fit. marks are
// the bytes of path that match the query, marked where they show (B129, B152).
func writePath(s *strings.Builder, spans *[]syntax.Span, path string, marks []int, width int, style string) {
	dir, name := filepath.Split(path)
	nameAt, at := len(dir), s.Len()
	dir = strings.TrimSuffix(dir, "/")
	if r := []rune(name); len(r) > width {
		name = string(r[:max(width, 0)])
	}
	if style != "" {
		*spans = append(*spans, syntax.Span{Start: at, End: at + len(name), Name: style})
	}
	s.WriteString(name)
	shown, place := "", func(int) int { return -1 }
	if room := width - utf8.RuneCountInString(name) - 2; dir != "" && room >= 4 {
		shown, place = shorten(dir, room)
		s.WriteString("  ")
		*spans = append(*spans, syntax.Span{Start: s.Len(), End: s.Len() + len(shown), Name: "finder.dir"})
	}
	dirAt := s.Len()
	s.WriteString(shown)
	for _, m := range marks {
		k := -1
		switch {
		case m >= nameAt && m-nameAt < len(name):
			k = at + m - nameAt
		case m < len(dir) && place(m) >= 0:
			k = dirAt + place(m)
		}
		if k >= 0 {
			*spans = append(*spans, syntax.Span{Start: k, End: k + 1, Name: "match"})
		}
	}
}

// shorten returns dir in at most room columns: whole, or its first
// directory, an ellipsis and as many of its last ones as fit, or else the
// end of the last one. place returns where a byte of dir is in it, or -1.
func shorten(dir string, room int) (shown string, place func(int) int) {
	if utf8.RuneCountInString(dir) <= room {
		return dir, func(i int) int { return i }
	}
	head, _, _ := strings.Cut(dir, "/")
	for tail := dir; strings.Contains(tail, "/"); {
		tail = tail[strings.IndexByte(tail, '/')+1:]
		if shown := head + "/…/" + tail; utf8.RuneCountInString(shown) <= room {
			cut := len(dir) - len(tail)
			return shown, func(i int) int {
				switch {
				case i < len(head):
					return i
				case i >= cut:
					return len(head) + len("/…/") + i - cut
				}
				return -1
			}
		}
	}
	r := []rune(dir)
	tail := string(r[len(r)-room+1:])
	cut := len(dir) - len(tail)
	return "…" + tail, func(i int) int {
		if i >= cut {
			return len("…") + i - cut
		}
		return -1
	}
}

// finderPrompt goes before the finder's query.
const finderPrompt = "› "

// read reads and highlights the preview of match m off the loop, as the
// highlight takes over a millisecond (B130).
func (f *finder) read(m string, rows, cols int) {
	path, line, _, _ := grepSpot(m)
	text, at, image := path+" is outside the project", -1, ""
	if f.project == "" || inside(f.project, path) {
		if cfg, format, err := imageFile(path); err == nil {
			text, image = fmt.Sprintf("%s, %d by %d pixels", strings.ToUpper(format), cfg.Width, cfg.Height), path
		} else {
			text, at = preview(path, max(line-1, 0), rows, cols)
		}
	}
	var spans []syntax.Span
	if at >= 0 {
		var err error
		if spans, err = syntax.Highlight(f.filetype(path), f.queries, text); err != nil {
			logLine(err.Error())
		}
	}
	f.queue <- func() {
		f.shown, f.preview, f.spans, f.at, f.reading, f.image = m, text, spans, at, false, image
	}
}

// imageFile returns the size and format of the image file at path, or an
// error when it is not a PNG, JPEG or GIF.
func imageFile(path string) (image.Config, string, error) {
	r, err := os.Open(path)
	if err != nil {
		return image.Config{}, "", fmt.Errorf("image: %w", err)
	}
	defer r.Close()
	return image.DecodeConfig(r)
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
	r := bufio.NewReaderSize(f, 16<<10)
	if head, _ := r.Peek(16 << 10); bytes.IndexByte(head, 0) >= 0 {
		return "binary file", -1
	}
	first := max(line-rows/3, 0)
	var lines []string
	for i := 0; len(lines) < rows; i++ {
		b, err := r.ReadSlice('\n')
		l := string(b)
		// A long line shows its start, and the rest is never read in.
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
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

// The scores of fzy's algorithm, which ranks the finder's matches (B129).
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
