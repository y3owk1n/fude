package editor

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/y3owk1n/fude/internal/fuzzy"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// finder is the fuzzy file finder :pick files opens, or the live grep
// :pick grep opens: a box whose first line is the query, typed in insert
// mode, with the files or lines that match it below, the best first (B129,
// B134).
type finder struct {
	places  bool   // its items are places, path:line:column:text, shown under their file
	asFiles bool   // its items are file paths, shown and opened as the finder's files are
	enc     string // the encoding of places' columns, "" for bytes
	hint    string // what an empty query of a live search shows
	// lookup asks for the query's items on the loop and passes them to
	// found, for a search a language server answers.
	lookup   func(q string, found func([]string, error))
	all      bool          // it searches the files git ignores and hidden ones too
	paths    []string      // the working directory's files, nil until read
	query    string        // the query matches are for
	matches  *fuzzy.Ranked // paths, or grep's path:line:column:text
	sel      int
	first    int                 // the list's first row shown
	rows     []int               // the match each row shown is, -1 for a file's name over its matches
	stale    bool                // paths came after matches were found
	busy     bool                // a match runs
	seeking  string              // the query it runs for
	stop     *atomic.Bool        // stops it, nil for a lookup
	shown    string              // the match previewed
	preview  string              // the lines around it
	spans    []wire.Span         // their highlights
	at       int                 // the row of the preview its line is on
	reading  bool                // a preview is read off the loop
	image    string              // the image file previewed, when it is one (B305)
	seen     []string            // the image files it previewed, to release as it closes (B306)
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
	typed  string // the query as it stood when the finder closed
	chose  func(picked int, item, query string, ok bool)
	// A module's live pick: the module and pick its items come from, and
	// found takes the items it answers a query with (B285).
	owner api.Module
	pick  uint32
	found func([]string, error)
	tell  func(api.Context) // what the owner's pick hears, such as all (B321)
}

// openFinder opens f in a box with query typed.
func (e *Session) openFinder(f *finder, query string) {
	// A finder opened again keeps these, which a preview may still read
	// off the loop (B374).
	if f.queue == nil {
		f.queries, f.queue, f.filetype = e.queries, e.queue, e.filetypeOf
		if e.power != Host {
			f.project = e.project
		}
	}
	b := &buffer{text: kernel.New(query, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}, title: "[find]"}
	w := newWindow(b, 0, 0)
	w.from, w.opts.Clipboard = e.win, e.opts.Clipboard
	e.finder, e.win = f, w
	e.later = append(e.later, "A")
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
		f.sel = max(min(f.sel+1, f.matches.Len()-1), 0)
	case "prev":
		f.sel = max(f.sel-1, 0)
	case "all":
		// Only a module's pick has files to add.
		if f.owner == nil {
			return
		}
		f.all, f.stale = !f.all, true
		if f.lookup == nil {
			f.tell(api.Context{"all": strconv.FormatBool(f.all)})
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
	e.vim.Prompt(true)
	e.settle(e.vim.Feed(k))
	e.vim.Prompt(false)
	t := e.Text()
	query := t.Slice(0, t.Len())
	if (f.open || f.list) && query != f.query && f.lookup == nil && f.paths != nil {
		// Keys typed faster than matching runs pick from what they typed.
		f.matches, _ = fuzzy.All(f.paths).Narrow(query, nil)
		f.query, f.sel = query, 0
	}
	switch {
	case f.list && (f.chose == nil || f.places || f.asFiles):
		e.closeFinder()
		e.listMatches(f)
	case f.open:
		e.closeFinder()
		if f.chose != nil {
			picked, item := -1, ""
			if f.sel < f.matches.Len() {
				item = f.matches.At(f.sel)
				if picked = slices.Index(f.paths, item); picked < 0 && f.lookup != nil {
					picked = f.sel
				}
			}
			f.chose(picked, item, query, true)
		}
		// A place or file opens as the finder's own do, after the module hears
		// of it.
		if (f.chose == nil || f.places || f.asFiles) && f.sel < f.matches.Len() {
			path, line, col, _ := grepSpot(f.matches.At(f.sel))
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

// listMatches makes the finder's matches the last list of places, shown in
// a window of its own (B195).
func (e *Session) listMatches(f *finder) {
	what := "files"
	if f.places {
		what = cmp.Or(f.title, "matches")
	}
	if f.matches.Len() == 0 {
		e.message = "no " + what
		return
	}
	// A match shows its text without the indent around it.
	items := make([]string, f.matches.Len())
	for i := range items {
		m := f.matches.At(i)
		items[i] = m
		if path, line, col, text := grepSpot(m); line > 0 {
			items[i] = fmt.Sprintf("%s:%d:%d:%s", path, line, col, strings.TrimSpace(text))
		}
	}
	e.setPlaces(&api.Places{Title: what, Items: items, Show: true, Encoding: f.enc})
}

// closeFinder leaves insert mode in the query and closes the finder.
func (e *Session) closeFinder() {
	if e.vim.Mode() == vim.Insert {
		e.settle(e.vim.Feed("<Esc>"))
	}
	var seen []string
	// A list resumes with what was typed in it, and a question does not.
	if f := e.finder; f != nil {
		if f.stop != nil {
			f.stop.Store(true)
		}
		seen = f.seen
		if f.prompt == "" {
			f.typed, f.open, f.list, f.seen, f.shown = e.Text().Slice(0, e.Text().Len()), false, false, nil, ""
			e.lastFinder = f
		}
	}
	e.finder = nil
	e.release(seen)
	if e.win.from != nil {
		e.win = e.win.from
	}
}

// seek matches the query off the loop when it changed since the last
// match. One match runs at a time, and a query that grows searches only the
// last matches.
func (e *Session) seek(f *finder) {
	t := e.Text()
	q := t.Slice(0, t.Len())
	if f.busy {
		// A match for a query since typed over stops early.
		if f.stop != nil && q != f.seeking {
			f.stop.Store(true)
		}
		return
	}
	if f.lookup == nil && f.paths == nil || q == f.query && !f.stale {
		return
	}
	if f.lookup != nil {
		f.busy, f.stale = true, false
		f.lookup(q, func(matches []string, err error) {
			if err != nil {
				e.message = err.Error()
			}
			f.query, f.matches, f.sel, f.busy = q, fuzzy.All(matches), 0, false
		})
		return
	}
	from := fuzzy.All(f.paths)
	if !f.stale && strings.HasPrefix(q, f.query) {
		from = f.matches
	}
	stop := new(atomic.Bool)
	f.busy, f.stale, f.seeking, f.stop = true, false, q, stop
	go func() {
		matches, ok := from.Narrow(q, stop)
		e.queue <- func() {
			f.busy = false
			if ok {
				f.query, f.matches, f.sel = q, matches, 0
			}
		}
	}()
}

// box shows the finder in the middle of the screen, at most 20 rows tall,
// with the cursor in the query. On a screen of 60 columns or more, the
// selected file's first lines show in a box right of the list (B130).
func (f *finder) box(query string, cursor, width, height int) wire.Menu {
	// A margin of two columns and a row keeps the box off the screen's edges.
	total, h := min(162, width-4), min(22, height-2)
	w, docW := total, 0
	if total >= 60 && (f.chose == nil || f.places || f.asFiles) {
		w = total / 2
		docW = total - w
	}
	rows := max(h-4, 0) // below the query and its rule
	var s strings.Builder
	var spans []wire.Span
	// The query after a prompt, with the count of matches on the right.
	prompt := cmp.Or(f.prompt, finderPrompt)
	s.WriteString(prompt)
	s.WriteString(query)
	count := fmt.Sprintf("%d / %d", f.matches.Len(), len(f.paths))
	if f.lookup != nil {
		count = strconv.Itoa(f.matches.Len())
	}
	if f.all {
		count = "with ignored  " + count
	}
	if gap := w - 4 - utf8.RuneCountInString(prompt+query) - len(count); gap > 1 {
		s.WriteString(strings.Repeat(" ", gap))
		spans = append(spans, wire.Span{Start: s.Len(), End: s.Len() + len(count), Name: "comment"})
		s.WriteString(count)
	}
	s.WriteByte('\n')
	spans = append(spans, wire.Span{Start: s.Len(), End: s.Len(), Name: "markup.rule"})
	// Grep's matches show under a row for their file, so each is just its
	// line number and text. A row is a match, or its file's name when header.
	type listRow struct {
		match  int
		header bool
	}
	var list []listRow
	selRow, lineW := 0, 1
	for i := range f.matches.Len() {
		// The list stops once it holds the selection, the rows shown and
		// those a frontend gets.
		if i > f.sel && len(list) >= max(listMost, f.first+rows) {
			break
		}
		if path, line, _, _ := grepSpot(f.matches.At(i)); f.places {
			if prev, _, _, _ := grepSpot(f.matches.At(max(i-1, 0))); i == 0 || prev != path {
				list = append(list, listRow{i, true})
			}
			lineW = max(lineW, len(strconv.Itoa(line)))
		}
		if i == f.sel {
			selRow = len(list)
		}
		list = append(list, listRow{i, false})
	}
	// The rows move only when the selection would leave them, and the
	// selection's file name stays above it.
	top := selRow
	if top > 0 && list[top-1].header {
		top--
	}
	f.first = max(min(f.first, top), selRow-rows+1)
	f.first = max(min(f.first, len(list)-rows), 0)
	// A frontend that lays the rows out gets all of them, up to listMost,
	// and scrolls them itself (B560).
	f.rows = f.rows[:0]
	var items []wire.Item
	selected := -1
	for _, r := range list[:min(len(list), listMost)] {
		m := f.matches.At(r.match)
		var item wire.Item
		switch path, line, _, text := grepSpot(m); {
		case r.header:
			item = wire.Item{Label: path, Header: true, Kind: "file", Path: path}
		case f.places:
			text = strings.TrimLeft(text, " \t")
			item = wire.Item{Label: text, Line: line, Path: path, Marks: fuzzy.Positions(fuzzy.Lower(f.query), text)}
		case f.chose != nil && !f.asFiles:
			item = wire.Item{Label: m, Marks: fuzzy.Positions(fuzzy.Lower(f.query), m)}
		default:
			item = fileItem(m, fuzzy.Positions(fuzzy.Lower(f.query), m))
		}
		if r.header {
			f.rows = append(f.rows, -1)
		} else {
			f.rows = append(f.rows, r.match)
		}
		if r.match == f.sel && !r.header {
			selected = len(items)
		}
		items = append(items, item)
	}
	for _, r := range list[f.first:min(f.first+rows, len(list))] {
		s.WriteByte('\n')
		start := s.Len()
		m := f.matches.At(r.match)
		switch path, line, _, text := grepSpot(m); {
		case r.header:
			writePath(&s, &spans, path, nil, w-4, "finder.file")
		case f.places:
			num := fmt.Sprintf("%*d  ", lineW, line)
			spans = append(spans, wire.Span{Start: s.Len(), End: s.Len() + lineW, Name: "finder.dir"})
			s.WriteString(num)
			text = strings.TrimLeft(text, " \t")
			if r := []rune(text); len(r) > w-4-len(num) {
				text = string(r[:max(w-4-len(num), 0)])
			}
			marks := fuzzy.Occurrences(text, f.query, 0)
			if f.places {
				marks = fuzzy.Positions(fuzzy.Lower(f.query), text)
			}
			for _, at := range marks {
				spans = append(spans, wire.Span{Start: s.Len() + at, End: s.Len() + at + 1, Name: "match"})
			}
			s.WriteString(text)
		case f.chose != nil && !f.asFiles:
			// An item of a list is text, not a path.
			if r := []rune(m); len(r) > w-4 {
				m = string(r[:max(w-4, 0)])
			}
			for _, at := range fuzzy.Positions(fuzzy.Lower(f.query), m) {
				spans = append(spans, wire.Span{Start: s.Len() + at, End: s.Len() + at + 1, Name: "match"})
			}
			s.WriteString(m)
		default:
			writePath(&s, &spans, m, fuzzy.Positions(fuzzy.Lower(f.query), m), w-4, "")
		}
		if r.match == f.sel && !r.header {
			spans = append(spans, wire.Span{Start: start, End: s.Len(), Name: "menu.selected"})
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
		spans = append(spans, wire.Span{Start: s.Len(), End: s.Len() + len(empty), Name: "comment"})
		s.WriteString(empty)
		items = append(items, wire.Item{Label: empty, Disabled: true})
	}
	if docW > 0 {
		// The list is as tall as the preview beside it.
		s.WriteString(strings.Repeat("\n", max(rows-len(list), 0)))
	}
	doc, docSpans := "", []wire.Span(nil)
	if docW > 0 && f.sel < f.matches.Len() {
		if m := f.matches.At(f.sel); m != f.shown && !f.reading {
			f.reading = true
			go f.read(m, h-4, docW-4)
		}
		// The previewed file's path heads the preview, over a rule.
		path, _, _, _ := grepSpot(f.shown)
		head := path + "\n\n"
		doc = head + f.preview
		docSpans = []wire.Span{{Start: 0, End: len(path), Name: "comment"}, {Start: len(path) + 1, End: len(path) + 1, Name: "markup.rule"}}
		for _, sp := range f.spans {
			docSpans = append(docSpans, wire.Span{Start: sp.Start + len(head), End: sp.End + len(head), Name: sp.Name})
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
			docSpans = append(slices.Clip(docSpans), wire.Span{Start: start, End: start + end, Name: "menu.selected"})
		}
	}
	return wire.Menu{
		Anchor: "middle", Text: s.String(), Spans: spans, Title: f.title, Cursor: cursor + len(prompt), Doc: doc, DocSpans: docSpans,
		X: (width - total) / 2, Y: (height - h) / 2, W: w, H: h, DocW: docW,
		Items: items, Selected: selected, Prompt: prompt, Query: query, Count: count,
	}
}

// fileItem is a path the finder lists, as a frontend lays its row out: its
// name, with the letters marks says the query matches, after its folder
// (B560).
func fileItem(path string, marks []int) wire.Item {
	dir, name := filepath.Split(strings.TrimSuffix(path, "/"))
	it := wire.Item{Label: name, Detail: strings.TrimSuffix(dir, string(filepath.Separator)), Kind: "file", Path: path}
	if strings.HasSuffix(path, "/") {
		it.Kind = "folder"
	}
	for _, at := range marks {
		if at >= len(dir) && at < len(dir)+len(name) {
			it.Marks = append(it.Marks, at-len(dir))
		}
	}
	return it
}

// writePath writes path in at most width columns: its name, in style when
// given, then its directory dimmed and cut in the middle to fit. marks are
// the bytes of path that match the query, marked where they show (B129, B152).
func writePath(s *strings.Builder, spans *[]wire.Span, path string, marks []int, width int, style string) {
	dir, name := filepath.Split(path)
	nameAt, at := len(dir), s.Len()
	dir = strings.TrimSuffix(dir, string(filepath.Separator))
	if r := []rune(name); len(r) > width {
		name = string(r[:max(width, 0)])
	}
	if style != "" {
		*spans = append(*spans, wire.Span{Start: at, End: at + len(name), Name: style})
	}
	s.WriteString(name)
	shown, place := "", func(int) int { return -1 }
	if room := width - utf8.RuneCountInString(name) - 2; dir != "" && room >= 4 {
		shown, place = shorten(dir, room)
		s.WriteString("  ")
		*spans = append(*spans, wire.Span{Start: s.Len(), End: s.Len() + len(shown), Name: "finder.dir"})
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
			*spans = append(*spans, wire.Span{Start: k, End: k + 1, Name: "match"})
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
	sep := string(filepath.Separator)
	head, _, _ := strings.Cut(dir, sep)
	for tail := dir; strings.Contains(tail, sep); {
		tail = tail[strings.Index(tail, sep)+1:]
		if shown := head + sep + "…" + sep + tail; utf8.RuneCountInString(shown) <= room {
			cut := len(dir) - len(tail)
			return shown, func(i int) int {
				switch {
				case i < len(head):
					return i
				case i >= cut:
					return len(head) + len(sep+"…"+sep) + i - cut
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

// previewMore is how many lines a preview reads past those its box shows,
// which a window frontend scrolls to (B130).
const previewMore = 300

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
			text, at = preview(path, max(line-1, 0), rows, previewMore, cols)
		}
	}
	var spans []wire.Span
	if at >= 0 {
		hl, err := syntax.Highlight(f.filetype(path), f.queries, text)
		spans = shownSpans(spans[:0], hl)
		if err != nil {
			logLine(err.Error())
		}
	}
	f.queue <- func() {
		f.shown, f.preview, f.spans, f.at, f.reading, f.image = m, text, spans, at, false, image
		if image != "" {
			f.seen = append(f.seen, image)
		}
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
// above line, and up to more lines after them for a window frontend to
// scroll to, cut to cols columns with tabs as 4 spaces, and the row line
// is on. A file with a zero byte in its first 16KB, or one that fails to
// read, shows why there is no preview.
func preview(path string, line, rows, more, cols int) (string, int) {
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
	for i := 0; len(lines) < rows+more; i++ {
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
