package editor

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// cmenu is the completions of the word being typed in a command line, which
// <Tab> and <S-Tab> step through, each replacing the word (B131).
type cmenu struct {
	start int    // where the word begins in the line, after the prompt
	typed string // the word as typed, the menu's last stop
	items []string
	sel   int // the item in the line, or len(items) for typed
}

// commandKey takes a key while a command line is typed: <Tab> and <C-n>
// put the next completion in the line, <S-Tab> and <C-p> the one before,
// <C-y> keeps it and <C-e> puts back what was typed. It reports whether it
// took the key, and other keys close the menu.
func (e *Session) commandKey(k vim.Key) bool {
	line, ok := strings.CutPrefix(e.vim.Line(), ":")
	m := e.cmenu
	if e.vim.Mode() != vim.Cmdline || !ok {
		e.cmenu, e.dirs = nil, nil
		return false
	}
	switch k {
	case "\t", "<C-n>", "<S-Tab>", "<C-p>":
		if m == nil {
			if m = e.menuOf(line, true); m == nil {
				return true
			}
			e.cmenu = m
		}
		step := 1
		if k == "<S-Tab>" || k == "<C-p>" {
			step = len(m.items)
		}
		m.sel = (m.sel + step) % (len(m.items) + 1)
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
		e.cmenu = nil
		return m != nil
	case "<C-y>":
		e.cmenu = nil
		return m != nil
	}
	e.cmenu = nil
	return false
}

// autocommand shows the completions of a command line as it is typed, with
// autocomplete on.
func (e *Session) autocommand() {
	line, ok := strings.CutPrefix(e.vim.Line(), ":")
	if !e.cfg.Autocomplete || e.vim.Mode() != vim.Cmdline || !ok || line == "" {
		return
	}
	e.cmenu = e.menuOf(line, false)
}

// menuOf returns the completions of line's last word as a menu with none
// of them in the line, or nil when there are none. Without wait, a path
// completes off the loop, and its menu shows later.
func (e *Session) menuOf(line string, wait bool) *cmenu {
	start, items := e.completions(line, wait)
	return newMenu(line, start, items)
}

// newMenu returns items, the completions of line from start, as a menu with
// none of them in the line, or nil when there are none.
func newMenu(line string, start int, items []string) *cmenu {
	if len(items) == 0 {
		return nil
	}
	return &cmenu{start: start, typed: line[start:], items: items, sel: len(items)}
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
		names := slices.Collect(maps.Keys(e.commands))
		for _, c := range slices.Concat(vim.Commands(), helpCommands()) {
			names = append(names, c.Name)
		}
		slices.Sort(names)
		return start, match(names, line[start:])
	}
	arg = strings.TrimLeft(arg, " ")
	start := len(line) - len(arg)
	if c, ok := commandsByName[name]; ok && c.complete != nil {
		return c.complete(e, line, start, arg, wait)
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
	out := match(names, part)
	for i, n := range out {
		out[i] = dir + n
	}
	return out
}

// entries returns the names in dir, directories with a trailing slash, read
// once for each command line.
func (e *Session) entries(dir string) []string {
	names, ok := e.dirs[dir]
	if !ok {
		names = readEntries(dir)
		e.keep(dir, names)
	}
	return names
}

// keep notes dir's entries for the rest of the command line.
func (e *Session) keep(dir string, names []string) {
	if e.dirs == nil {
		e.dirs = map[string][]string{}
	}
	e.dirs[dir] = names
}

// completeLater completes the path arg at start of line off the loop, as
// reading a big directory and matching its entries take milliseconds, and
// shows the menu if the line is still line then.
func (e *Session) completeLater(line string, start int, arg string) {
	dir, _ := filepath.Split(arg)
	names, ok := e.dirs[dir]
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
			e.cmenu = newMenu(line, start, items)
		}
	}()
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
func (m *cmenu) float() Float {
	rows := min(len(m.items), 10)
	first := max(min(m.sel, len(m.items)-1)-rows+1, 0)
	var s strings.Builder
	var spans []syntax.Span
	for i := first; i < first+rows; i++ {
		if i > first {
			s.WriteByte('\n')
		}
		it := m.items[i]
		if i == m.sel {
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(it), Name: "menu.selected"})
		}
		// A path marks the letters of its last part that the typed part
		// matches (B152).
		dir := strings.LastIndexByte(m.typed, '/') + 1
		for _, at := range positions(lower(m.typed[dir:]), it[dir:]) {
			spans = append(spans, syntax.Span{Start: s.Len() + dir + at, End: s.Len() + dir + at + 1, Name: "match"})
		}
		s.WriteString(it)
	}
	return Float{Text: s.String(), Spans: spans, Cursor: -1}
}
