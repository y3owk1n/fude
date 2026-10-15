package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// dirListing returns dir's entries, one name a line in the order the
// directory gives them, directories with a trailing slash (B124).
func dirListing(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", dir, err)
	}
	names := make([]string, len(entries))
	for i, en := range entries {
		names[i] = en.Name()
		if en.IsDir() {
			names[i] += "/"
		}
	}
	return strings.Join(names, "\n"), nil
}

// dirBuffer returns a listing of dir as a buffer numbered id.
func dirBuffer(id int, dir string) (*buffer, error) {
	text, err := dirListing(dir)
	if err != nil {
		return nil, err
	}
	b := &buffer{id: id, text: kernel.New(text, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}}
	b.dir, b.title = dir, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator)
	return b, nil
}

// openDir shows the listing of dir in the current window, with the cursor
// on the entry named at when there is one. A directory already listed shows
// its buffer as it was left.
func (e *Session) openDir(dir, at string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		e.message = err.Error()
		return
	}
	var b *buffer
	for _, o := range e.bufs {
		if o.dir == abs {
			b = o
		}
	}
	if b == nil {
		if b, err = dirBuffer(e.next, abs); err != nil {
			e.message = err.Error()
			return
		}
		e.next++
		e.bufs = append(e.bufs, b)
	}
	e.show(b)
	t := b.text.Snapshot()
	for line := range t.Lines() {
		if start := t.LineStart(line); at != "" && t.Slice(start, vim.LineEnd(t, start)) == at {
			e.setCursor(start)
		}
	}
}

// up lists the directory holding the current file or listing, with the
// cursor on what it came from, as Neovim's - does. count goes up that many
// levels, and 1 lists the working directory instead.
func (e *Session) up(count int) {
	from := e.win.buf.dir
	if from == "" {
		from = e.win.buf.file.path
	}
	if count == 1 || from == "" {
		cwd, err := os.Getwd()
		if err != nil {
			e.message = err.Error()
			return
		}
		e.openDir(cwd, "")
		return
	}
	from, err := filepath.Abs(from)
	if err != nil {
		e.message = err.Error()
		return
	}
	at, isDir := "", e.win.buf.dir != ""
	for range max(count, 1) {
		at = filepath.Base(from)
		if isDir {
			at += "/"
		}
		from, isDir = filepath.Dir(from), true
	}
	e.openDir(from, at)
}

// dirKey handles a key in a listing, as oil.nvim's are: <CR> opens the
// entry under the cursor, <C-s> and <C-h> open it in a split beside or
// above, _ lists the working directory and <C-l> reads the listing again.
func (e *Session) dirKey(k vim.Key) bool {
	b := e.win.buf
	t, c := b.text.Snapshot(), e.Cursor()
	name := t.Slice(vim.LineStart(t, c), vim.LineEnd(t, c))
	switch k {
	case "<CR>":
		e.openEntry(b.dir, name)
	case "<C-s>", "<C-h>":
		e.split(k == "<C-s>")
		e.openEntry(b.dir, name)
	case "_":
		e.up(1)
	case "<C-l>":
		e.reload(b)
	default:
		return false
	}
	return true
}

// openEntry opens the entry name of dir: a listing for a directory, the
// file otherwise.
func (e *Session) openEntry(dir, name string) {
	switch {
	case name == "":
	case strings.HasSuffix(name, "/"):
		e.openDir(filepath.Join(dir, name), "")
	default:
		e.edit(filepath.Join(dir, name))
	}
}

// reload reads b's directory again and puts its entries in place of the
// listing.
func (e *Session) reload(b *buffer) {
	text, err := dirListing(b.dir)
	if err != nil {
		e.message = err.Error()
		return
	}
	t := b.text.Snapshot()
	if old := t.Slice(0, t.Len()); old != text {
		if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: "reload", Time: e.now(), Edits: []kernel.Edit{replace(old, text)}}); err != nil {
			e.message = err.Error()
			return
		}
	}
	b.head, b.saved = b.text.Head(), b.text.Head()
}
