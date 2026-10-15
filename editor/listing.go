package editor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
		e.loadGit(b)
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
// cursor on what it came from. count goes up that many
// levels, and 1 lists the working directory instead.
func (e *Session) up(count int) {
	from := e.win.buf.listed
	if from == "" {
		from = e.win.buf.file.path
	}
	if count == 1 || from == "" {
		cwd, err := os.Getwd()
		if err != nil {
			e.message = err.Error()
			return
		}
		e.openListing(cwd, "")
		return
	}
	from, err := filepath.Abs(from)
	if err != nil {
		e.message = err.Error()
		return
	}
	at, isListing := "", e.win.buf.listed != ""
	for range max(count, 1) {
		at = filepath.Base(from)
		if isListing {
			at += "/"
		}
		from, isListing = filepath.Dir(from), true
	}
	e.openListing(from, at)
}

// listingKeys are a listing's own keys: <CR> opens the
// entry under the cursor, <C-s> and <C-h> open it in a split beside or
// below, _ lists the working directory and <C-l> reads the listing again.
// [keymap.listing] changes them (B142).
var listingKeys = map[string]string{
	"<CR>": "<Plug>(listing.open)", "<C-s>": "<Plug>(listing.vsplit)", "<C-h>": "<Plug>(listing.split)",
	"_": "<Plug>(listing.cwd)", "<C-l>": "<Plug>(listing.reload)",
}

// listingAct runs act, a listing's action, on the entry under the cursor.
func (e *Session) listingAct(act string) {
	b := e.win.buf
	if b.listed == "" {
		e.message = "not in a listing"
		return
	}
	t, c := b.text.Snapshot(), e.Cursor()
	name := t.Slice(vim.LineStart(t, c), vim.LineEnd(t, c))
	switch act {
	case "open":
		e.openEntry(b.listed, name)
	case "split", "vsplit":
		e.split(act == "vsplit")
		e.openEntry(b.listed, name)
	case "cwd":
		e.up(1)
	case "reload":
		e.reload(b)
	}
}

// openEntry opens the entry name of dir: a listing for a directory, the
// file otherwise.
func (e *Session) openEntry(dir, name string) {
	switch {
	case name == "":
	case strings.HasSuffix(name, "/"):
		e.openListing(filepath.Join(dir, name), "")
	default:
		e.edit(filepath.Join(dir, name))
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
	e.loadGit(b)
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
func (b *buffer) icons(t kernel.Snapshot, top, last int) map[int]Sign {
	if b.listed == "" {
		return nil
	}
	out := map[int]Sign{}
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

// changes are what saving a listing does to its directory.
type changes struct {
	renames [][2]string // old and new names
	deletes []string
	creates []string // a trailing slash makes a directory
}

// plan reads the edits to b's listing as changes (B125). Each entry's line
// is the one its marks are on. An entry whose marks met is gone, unless
// they are still on one line no other entry has. A line no entry has is a
// new entry. A name both gone and new is a line that moved, which changes
// nothing.
func (b *buffer) plan() (changes, error) {
	t := b.text.Snapshot()
	owner := map[int]string{} // line to the entry name on it
	var met []entry
	for _, en := range b.entries {
		s, e := b.text.Mark(en.start), b.text.Mark(en.end)
		if s >= e {
			met = append(met, en)
			continue
		}
		line := t.LineOf(s)
		if _, ok := owner[line]; ok {
			return changes{}, fmt.Errorf("%s and another entry are on one line", en.name)
		}
		owner[line] = en.name
	}
	var gone []string
	for _, en := range met {
		// cc leaves the marks met on one line, and the name there replaces
		// the entry's. Lines inserted where dd left them spread them apart,
		// and those lines are new.
		line := t.LineOf(b.text.Mark(en.end))
		if t.LineOf(b.text.Mark(en.start)) == line && owner[line] == "" {
			owner[line] = en.name
		} else {
			gone = append(gone, en.name)
		}
	}
	var c changes
	var made []string
	for line := range t.Lines() {
		start := t.LineStart(line)
		now := strings.TrimSpace(t.Slice(start, vim.LineEnd(t, start)))
		was, ok := owner[line]
		switch {
		case !ok && now != "":
			made = append(made, now)
		case ok && now == "":
			gone = append(gone, was)
		case ok && now != was:
			c.renames = append(c.renames, [2]string{was, now})
		}
	}
	for _, name := range gone {
		if i := slices.Index(made, name); i >= 0 {
			made = slices.Delete(made, i, i+1)
		} else {
			c.deletes = append(c.deletes, name)
		}
	}
	c.creates = made
	return c, nil
}

// ask is the question that lists the changes. It counts the files in each
// directory to delete.
func (c changes) ask(dir string) string {
	var parts []string
	for _, r := range c.renames {
		parts = append(parts, fmt.Sprintf("rename %s to %s", r[0], r[1]))
	}
	for _, name := range c.deletes {
		part := "delete " + name
		if strings.HasSuffix(name, "/") {
			if n := files(filepath.Join(dir, name)); n > 0 {
				part += fmt.Sprintf(" and its %d files", n)
			}
		}
		parts = append(parts, part)
	}
	for _, name := range c.creates {
		parts = append(parts, "create "+name)
	}
	return strings.Join(parts, ", ") + "? (y/n)"
}

// files counts the files below dir.
func files(dir string) int {
	n := 0
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// apply makes the changes in dir. Renames go through names of their own
// first, so two entries can swap names.
func (c changes) apply(dir string) error {
	join := func(name string) string { return filepath.Join(dir, strings.TrimSuffix(name, "/")) }
	// Nothing changes unless every new name is free, or one a rename frees.
	freed := map[string]bool{}
	names := slices.Clone(c.creates)
	for _, r := range c.renames {
		freed[r[0]] = true
		names = append(names, r[1])
	}
	for _, name := range names {
		if _, err := os.Lstat(join(name)); err == nil && !freed[name] {
			return fmt.Errorf("%s exists", name)
		}
	}
	for i, r := range c.renames {
		if err := os.Rename(join(r[0]), join(r[0])+fmt.Sprintf(".fude-rename-%d", i)); err != nil {
			return fmt.Errorf("rename %s: %w", r[0], err)
		}
	}
	for i, r := range c.renames {
		to := join(r[1])
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return fmt.Errorf("rename %s: %w", r[0], err)
		}
		if err := os.Rename(join(r[0])+fmt.Sprintf(".fude-rename-%d", i), to); err != nil {
			return fmt.Errorf("rename %s to %s: %w", r[0], r[1], err)
		}
	}
	for _, name := range c.deletes {
		if err := os.RemoveAll(join(name)); err != nil {
			return fmt.Errorf("delete %s: %w", name, err)
		}
	}
	for _, name := range c.creates {
		path := join(name)
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
	}
	return nil
}

// save asks, then makes the edits to listing b in its directory. It moves
// open buffers of renamed files, reads the listing again and runs then
// (B125).
func (e *Session) save(b *buffer, then func()) {
	c, err := b.plan()
	if err != nil {
		e.message = err.Error()
		return
	}
	if len(c.renames)+len(c.deletes)+len(c.creates) == 0 {
		e.reload(b)
		if then != nil {
			then()
		}
		return
	}
	e.message = c.ask(b.listed)
	e.ask = func(yes bool) {
		if !yes {
			e.message = ""
			return
		}
		err := c.apply(b.listed)
		for _, r := range c.renames {
			e.moved(filepath.Join(b.listed, r[0]), filepath.Join(b.listed, r[1]))
		}
		e.reload(b)
		if err != nil {
			e.message = err.Error()
			return
		}
		if then != nil {
			then()
		}
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
		if rel, err := filepath.Rel(from, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			b.file.path = filepath.Join(to, rel)
		}
	}
}
