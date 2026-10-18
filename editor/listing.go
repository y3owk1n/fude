package editor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
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
	b.head, b.saved, b.noted = b.text.Head(), b.text.Head(), nil
	b.mark()
	e.emit("listing.read", "", b)
}

// notes returns the note of each line of listing b from top to last,
// reading each name's note once.
func (b *buffer) notes(t kernel.Snapshot, top, last int) map[int]string {
	if b.listed == "" {
		return nil
	}
	if b.noted == nil {
		b.noted = map[string]string{}
	}
	out := map[int]string{}
	for line := t.LineOf(top); line <= t.LineOf(last); line++ {
		start := t.LineStart(line)
		name := t.Slice(start, vim.LineEnd(t, start))
		n, ok := b.noted[name]
		if !ok {
			n = note(filepath.Join(b.listed, name))
			b.noted[name] = n
		}
		if n != "" {
			out[line] = n
		}
	}
	return out
}

// icons returns the icon of each line of listing b from top to last.
func (b *buffer) icons(t kernel.Snapshot, top, last int) map[int]wire.Sign {
	if b.listed == "" {
		return nil
	}
	out := map[int]wire.Sign{}
	for line := t.LineOf(top); line <= t.LineOf(last); line++ {
		start := t.LineStart(line)
		if name := t.Slice(start, vim.LineEnd(t, start)); name != "" {
			out[line] = iconOf(name)
		}
	}
	return out
}

// note is what a listing shows after the entry at path: where a symlink
// points, * for an executable, or a mark for a pipe, socket or device
// (B127).
func note(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	m := info.Mode()
	switch {
	case m&fs.ModeSymlink != 0:
		to, err := os.Readlink(path)
		if err != nil {
			to = "?"
		}
		return " -> " + to
	case m&fs.ModeNamedPipe != 0:
		return "|"
	case m&fs.ModeSocket != 0:
		return "="
	case m&fs.ModeCharDevice != 0:
		return "%"
	case m&fs.ModeDevice != 0:
		return "#"
	case m.IsRegular() && m&0o111 != 0:
		return "*"
	}
	return ""
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
			b.file.path = filepath.Join(to, rel)
		}
	}
}

// writer is what a module registered to write a kind of buffer (B327).
type writer struct {
	p  api.Module
	id uint32
}

// writeKind has the module that writes b's kind write it, asking first
// when it asks to, then reads a listing again and runs then. A guest's
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
