package editor

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/y3owk1n/fude/internal/fuzzy"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// cmdline is what completes a command line: the menu showing, the entries
// of the directories it completed in, and the module asked to complete,
// until it answers (B290).
type cmdline struct {
	menu  *cmenu
	dirs  map[string][]string
	asked *moduleMenu
}

// cmenu is the completions of the word being typed in a command line, which
// <Tab> and <S-Tab> step through, each replacing the word (B131).
type cmenu struct {
	start int    // where the word begins in the line, after the prompt
	typed string // the word as typed, the menu's last stop
	items []string
	notes []string // dim beside each item: a command's short names
	sel   int      // the item in the line, or len(items) for typed
	// lit is the item that shows lit, which <CR> takes while the typed word
	// is in the line, or -1 for none.
	lit int
}

// commandKey takes a key while a command line is typed: <Tab> puts the lit
// completion in the line, or else the next, <C-n> the next, <S-Tab> and
// <C-p> the one before, <C-y> keeps it and <C-e> puts back what was typed. It reports whether it
// took the key, and other keys close the menu.
func (c *cmdline) key(e *Session, k vim.Key) bool {
	line, ok := strings.CutPrefix(e.vim.Line(), ":")
	m := c.menu
	if e.vim.Mode() != vim.Cmdline || !ok {
		c.menu, c.dirs = nil, nil
		return false
	}
	switch k {
	case "\t", "<C-n>", "<S-Tab>", "<C-p>":
		fresh := m == nil
		if fresh {
			if m = e.menuOf(line, true); m == nil {
				return true
			}
			c.menu = m
		}
		n, back := len(m.items), k == "<S-Tab>" || k == "<C-p>"
		lit := !fresh && m.sel == n && m.lit >= 0
		switch {
		case lit && k == "\t":
			// <Tab> takes the lit item of a menu already shown.
			m.sel = m.lit
		case lit && back:
			m.sel = (m.lit + n - 1) % n
		case lit:
			m.sel = (m.lit + 1) % n
		case back:
			// The typed word comes after the last item.
			m.sel = (m.sel + n) % (n + 1)
		default:
			m.sel = (m.sel + 1) % (n + 1)
		}
		word := m.typed
		if m.sel < len(m.items) {
			word = m.items[m.sel]
		}
		e.vim.SetLine(line[:m.start] + word)
		return true
	case "<C-e>":
		if m != nil {
			e.vim.SetLine(line[:m.start] + m.typed)
		}
		c.menu = nil
		return m != nil
	case "<C-y>":
		c.menu = nil
		return m != nil
	case "<CR>":
		if m != nil && m.lit >= 0 && m.sel == len(m.items) {
			e.vim.SetLine(line[:m.start] + m.items[m.lit])
		}
	}
	c.menu = nil
	return false
}

// autocommand shows the completions of a command line as it is typed, with
// autocomplete on.
func (e *Session) autocommand() {
	line, ok := strings.CutPrefix(e.vim.Line(), ":")
	if !e.cfg.Autocomplete || e.vim.Mode() != vim.Cmdline || !ok || line == "" {
		return
	}
	e.cmdline.menu = e.menuOf(line, false)
}

// menuOf returns the completions of line's last word as a menu with none
// of them in the line, or nil when there are none. Without wait, a path
// completes off the loop, and its menu shows later.
func (e *Session) menuOf(line string, wait bool) *cmenu {
	start, items := e.completions(line, wait)
	m := newMenu(line, start, items)
	e.light(m, line)
	return m
}

// light chooses the item of m that shows lit, which <CR> takes: the one
// typed, a command by any of its names, or else the first. A command's
// short names show beside it (B131).
func (e *Session) light(m *cmenu, line string) {
	if m == nil {
		return
	}
	if !strings.Contains(line, " ") {
		m.lit = 0
		if full, ok := e.fullName(m.typed); ok {
			m.lit = slices.Index(m.items, full)
		}
		m.notes = make([]string, len(m.items))
		for i, it := range m.items {
			if c, ok := commandsByName[it]; ok {
				m.notes[i] = strings.Join(c.Short, " ")
			} else if shorts := e.shortsOf(it); len(shorts) > 0 {
				m.notes[i] = strings.Join(shorts, " ")
			} else if j := slices.IndexFunc(vim.Commands(), func(c vim.Command) bool { return c.Name == it }); j >= 0 {
				m.notes[i] = strings.Join(vim.Commands()[j].Short, " ")
			}
		}
		return
	}
	m.lit = max(slices.Index(m.items, m.typed), 0)
}

// fullName returns the name of the command name names, by its name or a
// short one, or false when it names none.
func (e *Session) fullName(name string) (string, bool) {
	if c, ok := commandsByName[name]; ok {
		return c.Name, true
	}
	if full, ok := e.shortOf[name]; ok {
		return full, true
	}
	if _, ok := e.commands[name]; ok {
		return name, true
	}
	i := slices.IndexFunc(vim.Commands(), func(c vim.Command) bool { return c.Name == name || slices.Contains(c.Short, name) })
	if i < 0 {
		return "", false
	}
	return vim.Commands()[i].Name, true
}

// newMenu returns items, the completions of line from start, as a menu with
// none of them in the line, or nil when there are none.
func newMenu(line string, start int, items []string) *cmenu {
	if len(items) == 0 {
		return nil
	}
	return &cmenu{start: start, typed: line[start:], items: items, sel: len(items), lit: -1}
}

// completions returns where the word being typed at the end of line begins,
// and what it may become, the best first: a command's name, or what its
// command completes its argument with.
func (e *Session) completions(line string, wait bool) (int, []string) {
	name, arg, spaced := strings.Cut(line, " ")
	if !spaced {
		// A name after a range such as '<,'>.
		start := len(strings.TrimRightFunc(line, unicode.IsLetter))
		if strings.ContainsFunc(line[:start], unicode.IsLetter) {
			return 0, nil
		}
		// A short name matches as its command, which comes once.
		names, of := slices.Collect(maps.Keys(e.commands)), maps.Clone(e.shortOf)
		for _, c := range slices.Concat(vim.Commands(), helpCommands()) {
			names = append(names, c.Name)
			for _, s := range c.Short {
				names, of[s] = append(names, s), c.Name
			}
		}
		slices.Sort(names)
		var items []string
		for _, n := range fuzzy.Match(names, line[start:]) {
			if full := cmp.Or(of[n], n); !slices.Contains(items, full) {
				items = append(items, full)
			}
		}
		return start, items
	}
	arg = strings.TrimLeft(arg, " ")
	start := len(line) - len(arg)
	if c, ok := commandsByName[name]; ok && c.complete != nil {
		// A path completes from what it expands to, which replaces it.
		if takesPath(c) {
			if full, err := e.expand(arg); err == nil {
				arg = full
			}
		}
		return c.complete(e, line, start, arg, wait)
	}
	if complete, ok := e.completers[name]; ok {
		// A module may answer in the call itself, or later.
		a := &moduleMenu{line: line, start: start, sync: true}
		e.cmdline.asked = a
		complete(e, arg)
		a.sync = false
		return start, a.items
	}
	return 0, nil
}

// pathMatches returns the names of all, a directory's entries, that the
// last part of arg matches, after arg's directory. Hidden entries come only
// for a part that starts with a dot.
func pathMatches(arg string, all []string) []string {
	dir, part := filepath.Split(arg)
	var names []string
	for _, n := range all {
		if !strings.HasPrefix(n, ".") || strings.HasPrefix(part, ".") {
			names = append(names, n)
		}
	}
	out := fuzzy.Match(names, part)
	for i, n := range out {
		out[i] = dir + n
	}
	return out
}

// entries returns the names in dir, directories with a trailing slash, read
// once for each command line.
func (e *Session) entries(dir string) []string {
	names, ok := e.cmdline.dirs[dir]
	if !ok {
		names = readEntries(dir)
		e.keep(dir, names)
	}
	return names
}

// keep notes dir's entries for the rest of the command line.
func (e *Session) keep(dir string, names []string) {
	if e.cmdline.dirs == nil {
		e.cmdline.dirs = map[string][]string{}
	}
	e.cmdline.dirs[dir] = names
}

// completeLater completes the path arg at start of line off the loop, as
// reading a big directory and matching its entries take milliseconds, and
// shows the menu if the line is still line then.
func (e *Session) completeLater(line string, start int, arg string) {
	dir, _ := filepath.Split(arg)
	names, ok := e.cmdline.dirs[dir]
	go func() {
		if !ok {
			names = readEntries(dir)
		}
		items := pathMatches(arg, names)
		e.queue <- func() {
			if now, _ := strings.CutPrefix(e.vim.Line(), ":"); e.vim.Mode() != vim.Cmdline || now != line {
				return
			}
			e.keep(dir, names)
			e.cmdline.menu = newMenu(line, start, items)
			e.light(e.cmdline.menu, line)
		}
	}()
}

// completeOff finds the completions of line from start off the loop, and
// shows them when the line is the same once they come.
func (e *Session) completeOff(line string, start int, find func() []string) {
	go func() {
		items := find()
		e.queue <- func() {
			if now, _ := strings.CutPrefix(e.vim.Line(), ":"); e.vim.Mode() == vim.Cmdline && now == line {
				e.cmdline.menu = newMenu(line, start, items)
				e.light(e.cmdline.menu, line)
			}
		}
	}()
}

// moduleMenu is the completion of a command line a module was asked for
// (B290).
type moduleMenu struct {
	line  string
	start int
	items []string // what came in the call that asked
	sync  bool     // the call that asked is running
}

// answered shows the completions a module gave for a, when the command
// line is still a's, ranked against the word typed.
func (e *Session) answered(a *moduleMenu, items []string) {
	e.cmdline.asked = nil
	if a.sync {
		a.items = fuzzy.Match(items, a.line[a.start:])
		return
	}
	if now, _ := strings.CutPrefix(e.vim.Line(), ":"); e.vim.Mode() != vim.Cmdline || now != a.line {
		return
	}
	e.cmdline.menu = newMenu(a.line, a.start, fuzzy.Match(items, a.line[a.start:]))
	e.light(e.cmdline.menu, a.line)
}

// readEntries returns the names in dir, directories with a trailing slash,
// and none for a directory it cannot read.
func readEntries(dir string) []string {
	list, _ := os.ReadDir(cmp.Or(dir, "."))
	names := make([]string, 0, len(list))
	for _, en := range list {
		n := en.Name()
		if en.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	return names
}

// float shows the menu, at most 10 rows tall, for the client to put under
// the command line (B148).
func (m *cmenu) float() wire.Float {
	rows := min(len(m.items), 10)
	// The rows start at the top until an item is selected, then follow it.
	first := 0
	if m.sel < len(m.items) {
		first = max(m.sel-rows+1, 0)
	}
	var s strings.Builder
	var spans []syntax.Span
	width := 0
	for _, it := range m.items[first : first+rows] {
		width = max(width, utf8.RuneCountInString(it))
	}
	for i := first; i < first+rows; i++ {
		if i > first {
			s.WriteByte('\n')
		}
		it := m.items[i]
		if i == m.sel || i == m.lit && m.sel == len(m.items) {
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(it), Name: "menu.selected"})
		}
		// A path marks the letters of its last part that the typed part
		// matches (B152).
		dir := strings.LastIndexByte(m.typed, '/') + 1
		for _, at := range fuzzy.Positions(fuzzy.Lower(m.typed[dir:]), it[dir:]) {
			spans = append(spans, syntax.Span{Start: s.Len() + dir + at, End: s.Len() + dir + at + 1, Name: "match"})
		}
		s.WriteString(it)
		if i < len(m.notes) && m.notes[i] != "" {
			s.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(it)+2))
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(m.notes[i]), Name: "comment"})
			s.WriteString(m.notes[i])
		}
	}
	return wire.Float{Text: s.String(), Spans: spans, Cursor: -1}
}

// incSearch remembers top and at, the view and the cursor before a search
// first matched, since reveal shows the match while the search is typed.
// It puts the view back when the pattern stops matching or the search is
// cancelled. A search that runs keeps the view, which shows where it went
// (B21).
func (e *Session) incSearch(top, at int) {
	w := e.win
	if _, _, ok := e.vim.Incremental(); ok && w.opts.IncSearch {
		if e.searching == nil {
			e.searching = &[2]int{top, at}
		}
		return
	}
	if e.searching == nil {
		return
	}
	typing := e.vim.Mode() == vim.Cmdline
	if typing || e.Cursor() == e.searching[1] {
		w.buf.text.MoveMark(w.top, e.searching[0])
	}
	if !typing {
		e.searching = nil
		e.reveal()
	}
}

// shortsOf returns the short names of the modules' command name (B329).
func (e *Session) shortsOf(name string) []string {
	var out []string
	for short, full := range e.shortOf {
		if full == name {
			out = append(out, short)
		}
	}
	slices.Sort(out)
	return out
}
