// Package listing is what a listing of a directory does, as oil.nvim
// does: - lists the current file's directory, a listing's keys open its
// entries, and editing it and :w renames, deletes and creates them. :pwd
// shows the working directory (B124, B125, B155).
package listing

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// What listing registers, by the ids Call runs them by.
const (
	up uint32 = iota + 1
	open
	split
	vsplit
	cwd
	reload
	write
	pwd
)

// Module is listing. It keeps the note of each name each listing showed.
type Module struct {
	mu    sync.Mutex
	notes map[string]map[string]string // by buffer, by name
	ids   int
}

// New returns listing.
func New() *Module { return &Module{notes: map[string]map[string]string{}} }

func (m *Module) Name() string { return "listing" }

func (m *Module) Subscribed() []string {
	return []string{"listing.read", "buffer.changed", "buffer.closed", "option.set", "config.reloaded"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Plug, Name: "up", Keys: "-", Help: "lists the directory of the file or listing, n levels up", ID: up},
		{Kind: api.Plug, Name: "open", Keys: "<CR>", In: "listing", Help: "opens the entry under the cursor", ID: open},
		{Kind: api.Plug, Name: "split", Keys: "<C-h>", In: "listing", Help: "opens the entry under the cursor in a window below", ID: split},
		{Kind: api.Plug, Name: "vsplit", Keys: "<C-s>", In: "listing", Help: "opens the entry under the cursor in a window to the right", ID: vsplit},
		{Kind: api.Plug, Name: "cwd", Keys: "_", In: "listing", Help: "lists the working directory", ID: cwd, Host: true},
		{Kind: api.Plug, Name: "reload", Keys: "<C-l>", In: "listing", Help: "reads the listing's directory again", ID: reload},
		{Kind: api.Write, Name: "listing", Help: "renames, deletes and creates what the listing's edits say", ID: write},
		{Kind: api.Command, Name: "pwd", Help: "shows the working directory", ID: pwd},
	}
}

// Init decorates a listing that opened before the module loaded, as the
// one fude dir opens does.
func (m *Module) Init(ctx context.Context, s api.Scope) (api.Result, error) {
	return m.Event(ctx, "listing.read", s)
}

// Call lists a directory, opens the entry under the cursor, writes a
// listing's edits to its directory, or shows the working directory.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	dir := s.About["listing"]
	switch id {
	case pwd:
		return api.Result{Message: s.About["cwd"]}, nil
	case up:
		return listUp(s, args[1]), nil
	case cwd:
		wd, err := os.Getwd()
		if err != nil {
			return api.Result{Message: err.Error()}, nil
		}
		return api.Result{Jump: list(s, wd)}, nil
	case reload:
		return api.Result{Ex: []string{"e!"}}, nil
	case write:
		return save(s, dir), nil
	}
	if dir == "" {
		return api.Result{Message: "not in a listing"}, nil
	}
	t := s.Text
	line := t.LineOf(args[0])
	name := t.Slice(t.LineStart(line), lineEnd(t, line))
	if name == "" {
		return api.Result{}, nil
	}
	jump := &api.Jump{Path: filepath.Join(dir, name)}
	if strings.HasSuffix(name, "/") {
		jump = list(s, jump.Path)
	}
	jump.Split = map[uint32]string{split: "below", vsplit: "right"}[id]
	return api.Result{Jump: jump}, nil
}

// list lists dir with the cursor on the entry that leads to the current
// file or listing, when dir holds it, or else on the first entry.
func list(s api.Scope, dir string) *api.Jump {
	jump := &api.Jump{Path: dir}
	from := cmp.Or(s.About["listing"], s.About["path"])
	if from == "" {
		return jump
	}
	from, err := filepath.Abs(from)
	if err != nil {
		return jump
	}
	rel, err := filepath.Rel(dir, from)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return jump
	}
	at, _, deeper := strings.Cut(rel, string(filepath.Separator))
	if deeper || s.About["listing"] != "" {
		at += "/"
	}
	// The directory lists its entries in the order it reads them.
	if entries, err := os.ReadDir(dir); err == nil {
		if i := slices.IndexFunc(entries, func(en fs.DirEntry) bool { return name(en) == at }); i >= 0 {
			jump.Line = i + 1
		}
	}
	return jump
}

// listUp lists the directory holding the current file or listing, with the
// cursor on what it came from. count goes up that many levels, and 1 lists
// the working directory instead.
func listUp(s api.Scope, count int) api.Result {
	from := cmp.Or(s.About["listing"], s.About["path"])
	if count == 1 || from == "" {
		wd, err := os.Getwd()
		if err != nil {
			return api.Result{Message: err.Error()}
		}
		return api.Result{Jump: list(s, wd)}
	}
	from, err := filepath.Abs(from)
	if err != nil {
		return api.Result{Message: err.Error()}
	}
	for range max(count, 1) {
		from = filepath.Dir(from)
	}
	return api.Result{Jump: list(s, from)}
}

// name is how a listing shows en, a directory with a trailing slash.
func name(en fs.DirEntry) string {
	if en.IsDir() {
		return en.Name() + "/"
	}
	return en.Name()
}

// save asks, then makes the edits to the listing of dir in it. The open
// buffers of renamed files follow them (B125).
func save(s api.Scope, dir string) api.Result {
	c, err := plan(s.Text, s.Entries())
	if err != nil {
		return api.Result{Message: err.Error()}
	}
	if len(c.renames)+len(c.deletes)+len(c.creates) == 0 {
		return api.Result{}
	}
	if s.About["confirmed"] != "true" {
		var paths []string
		for _, name := range slices.Concat(c.deletes, c.creates) {
			paths = append(paths, filepath.Join(dir, name))
		}
		for _, r := range c.renames {
			paths = append(paths, filepath.Join(dir, r[0]), filepath.Join(dir, r[1]))
		}
		return api.Result{Confirm: &api.Confirm{Question: c.ask(dir), Paths: paths}}
	}
	var res api.Result
	err = c.apply(dir)
	for _, r := range c.renames {
		res.Moved = append(res.Moved, [2]string{filepath.Join(dir, r[0]), filepath.Join(dir, r[1])})
	}
	if err != nil {
		res.Message = err.Error()
	}
	return res
}

// changes are what saving a listing does to its directory.
type changes struct {
	renames [][2]string // old and new names
	deletes []string
	creates []string // a trailing slash makes a directory
}

// plan reads the edits to a listing as changes. Each entry's line is the
// one its marks are on. An entry whose marks met is gone, unless they are
// still on one line no other entry has. A line no entry has is a new entry.
// A name both gone and new is a line that moved, which changes nothing.
func plan(t api.Text, entries []api.Entry) (changes, error) {
	owner := map[int]string{} // line to the entry name on it
	var met []api.Entry
	for _, en := range entries {
		if en.Start >= en.End {
			met = append(met, en)
			continue
		}
		line := t.LineOf(en.Start)
		if _, ok := owner[line]; ok {
			return changes{}, fmt.Errorf("%s and another entry are on one line", en.Name)
		}
		owner[line] = en.Name
	}
	var gone []string
	for _, en := range met {
		// cc leaves the marks met on one line, and the name there replaces
		// the entry's. Lines inserted where dd left them spread them apart,
		// and those lines are new.
		line := t.LineOf(en.End)
		if t.LineOf(en.Start) == line && owner[line] == "" {
			owner[line] = en.Name
		} else {
			gone = append(gone, en.Name)
		}
	}
	var c changes
	var made []string
	for line := range t.Lines() {
		now := strings.TrimSpace(t.Slice(t.LineStart(line), lineEnd(t, line)))
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

// lineEnd is where line ends, before its newline.
func lineEnd(t api.Text, line int) int {
	if line+1 < t.Lines() {
		return t.LineStart(line+1) - 1
	}
	return t.Len()
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
				part += " and its " + strconv.Itoa(n) + " files"
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
	// Renames go through a name of their own, so two can swap names. One
	// that fails puts the names not yet moved back.
	temp := func(i int) string { return join(c.renames[i][0]) + fmt.Sprintf(".fude-rename-%d", i) }
	back := func(from, to int) error {
		var errs []error
		for j := from; j < to; j++ {
			errs = append(errs, os.Rename(temp(j), join(c.renames[j][0])))
		}
		return errors.Join(errs...)
	}
	for i, r := range c.renames {
		if err := os.Rename(join(r[0]), temp(i)); err != nil {
			return errors.Join(fmt.Errorf("rename %s: %w", r[0], err), back(0, i))
		}
	}
	for i, r := range c.renames {
		to := join(r[1])
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return errors.Join(fmt.Errorf("rename %s: %w", r[0], err), back(i, len(c.renames)))
		}
		if err := os.Rename(temp(i), to); err != nil {
			return errors.Join(fmt.Errorf("rename %s to %s: %w", r[0], r[1], err), back(i, len(c.renames)))
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

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
