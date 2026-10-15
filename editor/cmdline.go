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

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// cmenu is the completions of the word being typed in a command line, which
// <Tab> and <S-Tab> step through, each replacing the word (B131).
type cmenu struct {
	start int    // where the word begins in the line, after the prompt
	col   int    // the screen column it begins at, after the prompt
	typed string // the word as typed, the menu's last stop
	items []string
	sel   int // the item in the line, or len(items) for typed
}

// commandNames are the editor's commands, one full name each.
var commandNames = []string{
	"accept", "bdelete", "bnext", "bprevious", "buffer", "buffers", "close", "diff", "earlier", "edit", "exit",
	"files", "later", "only", "quit", "quitall", "recover", "registers", "reject", "rename", "restore", "set",
	"split", "timeline", "vsplit", "wall", "wq", "wqall", "write", "xall", "xit",
}

// commandKey takes a key while a command line is typed: <Tab> and <C-n>
// put the next completion in the line, <S-Tab> and <C-p> the one before,
// <C-y> keeps it and <C-e> puts back what was typed. It reports whether it
// took the key, and other keys close the menu.
func (e *Session) commandKey(k vim.Key) bool {
	line, ok := strings.CutPrefix(e.vim.Line(), ":")
	m := e.cmenu
	if e.vim.Mode() != vim.Cmdline || !ok {
		e.cmenu = nil
		return false
	}
	switch k {
	case "\t", "<C-n>", "<S-Tab>", "<C-p>":
		if m == nil {
			if m = e.menuOf(line); m == nil {
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
	e.cmenu = e.menuOf(line)
}

// menuOf returns the completions of line's last word as a menu with none
// of them in the line, or nil when there are none.
func (e *Session) menuOf(line string) *cmenu {
	start, items := e.completions(line)
	if len(items) == 0 {
		return nil
	}
	return &cmenu{start: start, col: utf8.RuneCountInString(line[:start]), typed: line[start:], items: items, sel: len(items)}
}

// completions returns where the word being typed at the end of line begins,
// and what it may become, the best first: a command's name, a path for
// :edit, :split and :vsplit, an option for :set or a buffer for :buffer
// and :bdelete.
func (e *Session) completions(line string) (int, []string) {
	name, arg, spaced := strings.Cut(line, " ")
	if !spaced {
		// A name after a range such as '<,'>.
		start := len(strings.TrimRightFunc(line, unicode.IsLetter))
		if strings.ContainsFunc(line[:start], unicode.IsLetter) {
			return 0, nil
		}
		names := slices.Concat(commandNames, vim.ExNames(), slices.Collect(maps.Keys(e.commands)))
		slices.Sort(names)
		return start, match(names, line[start:])
	}
	arg = strings.TrimLeft(arg, " ")
	start := len(line) - len(arg)
	switch name {
	case "e", "edit", "sp", "split", "vs", "vsplit":
		return start, pathCompletions(arg)
	case "se", "set":
		i := strings.LastIndexByte(arg, ' ') + 1
		opts := e.win.opts
		names := slices.Concat(slices.Collect(maps.Keys(opts.switches())), slices.Collect(maps.Keys(opts.numbers())))
		slices.Sort(names)
		return start + i, match(names, arg[i:])
	case "b", "buffer", "bd", "bdelete":
		var names []string
		for _, b := range e.bufs {
			if b.file.path != "" {
				names = append(names, b.file.path)
			}
		}
		return start, match(names, arg)
	}
	return 0, nil
}

// pathCompletions returns the entries of the directory arg names that its
// last part matches, directories with a trailing slash. Hidden entries
// come only for a part that starts with a dot, and an unreadable directory
// has none.
func pathCompletions(arg string) []string {
	dir, part := filepath.Split(arg)
	entries, err := os.ReadDir(cmp.Or(dir, "."))
	if err != nil {
		return nil
	}
	var names []string
	for _, en := range entries {
		n := en.Name()
		if strings.HasPrefix(n, ".") && !strings.HasPrefix(part, ".") {
			continue
		}
		if en.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	out := match(names, part)
	for i, n := range out {
		out[i] = dir + n
	}
	return out
}

// float shows the menu above the command line, under the word it
// completes, at most 10 rows tall.
func (m *cmenu) float(width, height int) Float {
	rows := min(len(m.items), 10, max(height-2, 0))
	first := max(min(m.sel, len(m.items)-1)-rows+1, 0)
	w := 0
	for _, it := range m.items {
		w = max(w, utf8.RuneCountInString(it))
	}
	var s strings.Builder
	var spans []syntax.Span
	for i := first; i < first+rows; i++ {
		if i > first {
			s.WriteByte('\n')
		}
		if i == m.sel {
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(m.items[i]), Name: "menu.selected"})
		}
		s.WriteString(m.items[i])
	}
	// The box's border goes left of the word, after the prompt.
	x := max(min(m.col, width-w-2), 0)
	return Float{Text: s.String(), Spans: spans, Cursor: -1, X: x, Y: height - rows - 2, W: w + 2, H: rows + 2}
}
