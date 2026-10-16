package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// startKeys are the start screen's keys, which [keymap.start] changes. A
// digit opens that recent file, and <CR> runs the key its line starts with
// (B225).
var startKeys = map[string]string{
	"f": ":pick files<CR>", "g": ":pick grep<CR>", "r": ":pick recent<CR>", "e": ":enew<CR>",
	"c": "<Plug>(start.config)", "q": ":qa<CR>", "<CR>": "<Plug>(start.open)",
	"1": "<Plug>(start.1)", "2": "<Plug>(start.2)", "3": "<Plug>(start.3)", "4": "<Plug>(start.4)", "5": "<Plug>(start.5)",
	"6": "<Plug>(start.6)", "7": "<Plug>(start.7)", "8": "<Plug>(start.8)", "9": "<Plug>(start.9)",
}

// startActions are the start screen's first lines, each a key and what it
// does.
var startActions = [][2]string{
	{"f", "find a file"},
	{"g", "search the files' text"},
	{"r", "recent files"},
	{"e", "new buffer"},
	{"c", "open config.toml"},
	{"q", "quit"},
}

// startOwner names the start screen's decorations.
type startOwner struct{}

// Welcome shows the start screen in place of the empty buffer, for fude
// started with no file, and lists the newest recent files on it once they
// are read (B225).
func (e *Session) Welcome() {
	b, _, err := openBuffer(e.next, "")
	if err != nil {
		e.message = err.Error()
		return
	}
	e.next++
	b.title, b.file.readonly = "[start]", true
	e.bufs = append(e.bufs, b)
	e.win.show(b)
	// The screen shows no numbers, guides or wrapped lines, and the window
	// gets its options back when it shows another buffer.
	opts := e.win.opts
	e.win.saved, e.win.opts = &opts, options{}
	e.fillStart(b, nil)
	go func() {
		paths, err := recentFiles("")
		e.queue <- func() {
			if err != nil {
				e.message = err.Error()
			}
			e.fillStart(b, paths[:min(len(paths), 9)])
		}
	}()
}

// fillStart writes the start screen into b, with recent, and styles its
// keys and title.
func (e *Session) fillStart(b *buffer, recent []string) {
	var s strings.Builder
	type span struct {
		start, end int
		style      string
	}
	var spans []span
	mark := func(text, style string) {
		spans = append(spans, span{s.Len(), s.Len() + len(text), style})
		s.WriteString(text)
	}
	s.WriteString("\n   ")
	mark("fude 筆", "markup.heading")
	s.WriteString("\n\n")
	for _, a := range startActions {
		s.WriteString("   ")
		mark(a[0], "keyword")
		s.WriteString("  " + a[1] + "\n")
	}
	if len(recent) > 0 {
		s.WriteString("\n   ")
		mark("recent files", "markup.heading")
		s.WriteString("\n\n")
		for i, p := range recent {
			s.WriteString("   ")
			mark(strconv.Itoa(i+1), "keyword")
			if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
				p = "~" + p[len(home):]
			}
			dir, name := filepath.Split(p)
			s.WriteString("  " + name)
			if dir != "" {
				s.WriteString("  ")
				mark(strings.TrimSuffix(dir, "/"), "comment")
			}
			s.WriteString("\n")
		}
	}
	t := b.text.Snapshot()
	text := strings.TrimSuffix(s.String(), "\n")
	if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Edits: []kernel.Edit{{Start: 0, End: t.Len(), Text: text}}}); err != nil {
		e.message = err.Error()
		return
	}
	b.saved = b.text.Head()
	for _, d := range e.decos[startOwner{}] {
		d.remove()
	}
	var ds []decoration
	for _, sp := range spans {
		ds = append(ds, decoration{buf: b, start: b.text.SetMark(sp.start, kernel.Left), end: b.text.SetMark(sp.end, kernel.Right), style: sp.style})
	}
	e.decos[startOwner{}] = ds
	b.recent = recent
	if e.win.buf == b {
		e.setCursor(strings.Index(text, "   "+startActions[0][0]+"  ") + 3)
	}
}

// startOpen runs the start screen's line under the cursor: the key it
// starts with.
func (e *Session) startOpen() {
	t := e.Text()
	ls := t.LineStart(t.LineOf(e.Cursor()))
	key, _, _ := strings.Cut(strings.TrimSpace(t.Slice(ls, vim.LineEnd(t, ls))), " ")
	if _, ok := startKeys[key]; ok {
		e.later = append(e.later, vim.Keys(key)...)
	}
}

// startConfig opens config.toml.
func (e *Session) startConfig() {
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		e.message = fmt.Sprintf("config.toml: %v", err)
		return
	}
	e.edit(filepath.Join(dir, "fude", "config.toml"))
}

// startRecent opens the start screen's n'th recent file.
func (e *Session) startRecent(n int) {
	if r := e.win.buf.recent; n <= len(r) {
		e.edit(r[n-1])
		return
	}
	e.message = fmt.Sprintf("no recent file %d", n)
}
