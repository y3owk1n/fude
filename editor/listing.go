package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// readListing returns dir's entries, one name a line in the order the
// directory gives them, directories with a trailing slash (B124).
func readListing(dir string) (string, error) {
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

// listingBuffer returns a listing of dir as a buffer numbered id.
func listingBuffer(id int, dir string) (*buffer, error) {
	text, err := readListing(dir)
	if err != nil {
		return nil, err
	}
	b := &buffer{id: id, text: kernel.New(text, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}}
	b.listed, b.title = dir, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator)
	b.mark()
	return b, nil
}

// openListing shows the listing of dir in the current window, with the
// cursor on the entry named at when there is one. A directory already
// listed shows its buffer as it was left.
func (e *Session) openListing(dir, at string) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		err = e.outside(abs)
	}
	if err != nil {
		e.message = err.Error()
		return
	}
	var b *buffer
	for _, o := range e.bufs {
		if o.listed == abs {
			b = o
		}
	}
	if b == nil {
		if b, err = listingBuffer(e.next, abs); err != nil {
			e.message = err.Error()
			return
		}
		e.next++
		e.bufs = append(e.bufs, b)
		e.emit("listing.read", "", b)
	}
	e.show(b)
	t := b.text.Snapshot()
	for line := range t.Lines() {
		if start := t.LineStart(line); at != "" && t.Slice(start, vim.LineEnd(t, start)) == at {
			e.setCursor(start)
		}
	}
}

// reload reads b's directory again and puts its entries in place of the
// listing.
func (e *Session) reload(b *buffer) {
	text, err := readListing(b.listed)
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
	b.mark()
	e.emit("listing.read", "", b)
}

// entry is one name a listing showed when it was read. Text inserted at
// its start mark goes before it, and text inserted at its end mark goes
// after it, so lines added around the entry stay outside it.
type entry struct {
	name       string
	start, end kernel.MarkID
}

// mark places the marks of b's entries, one a line, dropping old ones.
func (b *buffer) mark() {
	for _, en := range b.entries {
		b.text.DeleteMark(en.start)
		b.text.DeleteMark(en.end)
	}
	b.entries = nil
	t := b.text.Snapshot()
	for line := range t.Lines() {
		start := t.LineStart(line)
		end := vim.LineEnd(t, start)
		if start == end {
			continue
		}
		b.entries = append(b.entries, entry{t.Slice(start, end), b.text.SetMark(start, kernel.Right), b.text.SetMark(end, kernel.Left)})
	}
}

// moved points the buffers of files at or below from to where they are
// now, below to.
func (e *Session) moved(from, to string) {
	for _, b := range e.bufs {
		abs, err := filepath.Abs(b.file.path)
		if b.file.path == "" || err != nil {
			continue
		}
		if rel, ok := under(from, abs); ok {
			old := b.file.path
			b.file.path = filepath.Join(to, rel)
			e.emit("buffer.renamed", old, b)
		}
	}
}

// writer is what a module registered to write a kind of buffer (B327).
type writer struct {
	p  api.Module
	id uint32
}

// writeKind has the module that writes b's kind write it, asking first
// when it asks to, then reads a listing again, or marks another kind
// written, and runs then. A guest's
// write changes nothing outside the project (B245, B327).
func (e *Session) writeKind(b *buffer, then func()) {
	w, ok := e.writers[b.kind()]
	if !ok {
		e.message = "nothing writes a " + b.kind()
		return
	}
	call := func(confirmed bool) (api.Result, bool) {
		scope := e.scope(w.p, b, b.text.Snapshot())
		scope.Entries = func() []api.Entry { return b.entryList() }
		if confirmed {
			scope.About["confirmed"] = "true"
		}
		_, res, ok := e.run(w.p, w.id, scope, e.Cursor())
		return res, ok
	}
	done := func(res api.Result) {
		if b.listed != "" {
			e.reload(b)
		} else {
			b.saved = b.text.Head()
		}
		if res.Message != "" {
			e.message = res.Message
			return
		}
		if then != nil {
			then()
		}
	}
	res, ok := call(false)
	switch {
	case !ok:
		return
	case res.Message != "":
		// The edits stay, for the user to fix.
		e.message = res.Message
		return
	case res.Confirm == nil:
		done(res)
		return
	}
	for _, path := range res.Confirm.Paths {
		if err := e.outside(path); err != nil {
			e.message = err.Error()
			return
		}
	}
	e.message = res.Confirm.Question
	e.ask = func(yes bool) {
		if !yes {
			e.message = ""
			return
		}
		if res, ok := call(true); ok {
			done(res)
		}
	}
}

// entryList is where b's entries are now (B327).
func (b *buffer) entryList() []api.Entry {
	out := make([]api.Entry, len(b.entries))
	for i, en := range b.entries {
		out[i] = api.Entry{Name: en.name, Start: b.text.Mark(en.start), End: b.text.Mark(en.end)}
	}
	return out
}
