package editor

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// pickSources are the sources :pick finds in, by name.
var pickSources = []string{"files", "grep", "buffers", "recent", "diagnostics", "symbols", "tasks", "help", "keymaps", "commands"}

// openPick opens the finder on the items of source, or on the sources
// without one. grep takes the text after it as its query (B213).
func (e *Session) openPick(arg string) {
	source, rest, _ := strings.Cut(arg, " ")
	// A guest's finder and grep work only while the working directory is
	// in the project (B245).
	if source == "files" || source == "grep" {
		if err := e.outside("."); err != nil {
			e.message = "the working directory is outside the project"
			return
		}
	}
	switch source {
	case "":
		e.pickFrom("pick", pickSources, func(i int) { e.openPick(pickSources[i]) })
	case "help":
		tags := e.helpTags()
		e.pickFrom("help", tags, func(i int) { e.help(tags[i]) })
	case "keymaps":
		e.pickKeys()
	case "commands":
		e.pickCommands()
	case "files":
		e.files()
	case "grep":
		e.grepFiles(strings.TrimSpace(rest))
	case "buffers":
		var paths []string
		for _, b := range e.bufs {
			if b.file.path != "" && b != e.win.buf && b.kind() == "" {
				paths = append(paths, b.file.path)
			}
		}
		e.openFinder(&finder{paths: paths, stale: true, title: "buffers"}, "")
	case "recent":
		f, current := &finder{title: "recent files"}, e.win.buf.file.path
		e.openFinder(f, "")
		go func() {
			here, elsewhere, err := recentFiles(current)
			e.queue <- func() {
				if err != nil {
					e.message = err.Error()
				}
				f.paths, f.stale = append(here, elsewhere...), true
			}
		}()
	case "diagnostics":
		e.openFinder(&finder{paths: e.diagnosticPlaces(), stale: true, places: true, title: "diagnostics"}, "")
	case "symbols":
		e.pickSymbols()
	case "tasks":
		e.pickTasks()
	default:
		e.message = fmt.Sprintf("no source named %q, only %s", source, strings.Join(pickSources, ", "))
	}
}

// recentFiles returns the recent files that still exist, but current,
// newest first: those under the working directory, relative to it, and the
// others (B213).
func recentFiles(current string) (here, elsewhere []string, err error) {
	paths, err := readRecent()
	if err != nil {
		return nil, nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("recent files: %w", err)
	}
	cur, _ := filepath.Abs(current)
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil || p == cur {
			continue
		}
		if rel, ok := under(wd, p); ok {
			here = append(here, rel)
		} else {
			elsewhere = append(elsewhere, p)
		}
	}
	return here, elsewhere, nil
}

// pickCommands opens the finder on every ex command and :plug of each
// <Plug>, each by what it does, as :pick commands does. The one chosen runs, or waits on the command line
// for the argument it needs (B266).
func (e *Session) pickCommands() {
	type command struct{ name, args, help string }
	var all []command
	for _, c := range slices.Concat(vim.Commands(), helpCommands()) {
		all = append(all, command{c.Name, c.Args, c.Help})
	}
	for _, name := range slices.Sorted(maps.Keys(e.commands)) {
		all = append(all, command{name, "", cmp.Or(e.pluginHelp[":"+name], "a plugin's command")})
	}
	for _, name := range e.plugNames() {
		all = append(all, command{"plug " + name, "", e.mappingText("<Plug>(" + name + ")")})
	}
	nameW := 0
	for _, c := range all {
		nameW = max(nameW, len(c.name))
	}
	items := make([]string, len(all))
	for i, c := range all {
		items[i] = fmt.Sprintf(":%-*s  %s", nameW, c.name, c.help)
	}
	e.pickFrom("commands", items, func(i int) {
		c := all[i]
		// A command that needs an argument, whose arguments are not all in
		// brackets, waits on the command line for it.
		line := ":" + c.name + "<CR>"
		if c.args != "" && !strings.HasPrefix(c.args, "[") {
			line = ":" + c.name + " "
		}
		e.later = append(e.later, vim.Keys(line)...)
	})
}

// pickKeys opens the finder on every key: the mappings of normal, visual
// and insert mode, each by what it does, then the machine's keys and
// fude's, each with its help and kind. Picking a key normal mode runs, such
// as a mapping, a motion or an operator, runs it as if typed, up to the
// argument it waits for. Picking any other says what it does and where
// (B215).
func (e *Session) pickKeys() {
	type key struct{ keys, help, kind string }
	var all []key
	for _, mode := range []struct {
		m    vim.Mode
		name string
	}{{vim.Normal, "normal"}, {vim.Visual, "visual"}, {vim.Insert, "insert"}} {
		e.vim.Each(mode.m, func(from, to string) {
			all = append(all, key{from, e.mappingText(to), mode.name + " mapping"})
		})
	}
	for _, b := range slices.Concat(vim.Bindings(), hostBindings()) {
		all = append(all, key{b.Keys, b.Help, b.Kind})
	}
	keysW, kindW := 0, 0
	for _, k := range all {
		keysW, kindW = max(keysW, utf8.RuneCountInString(k.keys)), max(kindW, len(k.kind))
	}
	items := make([]string, len(all))
	for i, k := range all {
		items[i] = fmt.Sprintf("%-*s  %-*s  %s", keysW, k.keys, kindW, k.kind, k.help)
	}
	e.pickFrom("keymaps", items, func(i int) {
		k := all[i]
		typed, _, _ := strings.Cut(k.keys, "{")
		switch k.kind {
		case "normal mapping", "normal", "motion", "operator":
			if typed != "" {
				// The keys run as if typed once the finder closes.
				e.later = append(e.later, vim.Keys(typed)...)
				return
			}
		}
		e.message = k.keys + " " + k.help + ", in " + strings.TrimSuffix(k.kind, " mapping")
	})
}

// pickFrom opens the finder on items with no preview, and runs picked with
// the index of the one picked (B215).
func (e *Session) pickFrom(title string, items []string, picked func(int)) {
	e.openFinder(&finder{paths: items, stale: true, title: title, chose: func(i int, _ string, ok bool) {
		if ok && i >= 0 {
			picked(i)
		}
	}}, "")
}

// diagnosticPlaces returns the diagnostics of every buffer as places, the
// current buffer's first, each buffer's worst first and then in the order
// of its text (B196).
func (e *Session) diagnosticPlaces() []string {
	bufs := slices.Concat([]*buffer{e.win.buf}, slices.DeleteFunc(slices.Clone(e.bufs), func(b *buffer) bool { return b == e.win.buf }))
	var out []string
	for _, b := range bufs {
		t := b.text.Snapshot()
		ds := slices.Clone(e.diagnostics(b))
		worst := func(d decoration) int {
			return slices.IndexFunc(diagnosticSigns, func(s wire.Sign) bool { return s.Name == d.style })
		}
		slices.SortStableFunc(ds, func(x, y decoration) int {
			return cmp.Or(cmp.Compare(worst(x), worst(y)), cmp.Compare(b.text.Mark(x.start), b.text.Mark(y.start)))
		})
		for _, d := range ds {
			p := lsp.Point(t, b.text.Mark(d.start), "utf-8")
			note, _, _ := strings.Cut(d.note, "\n")
			out = append(out, fmt.Sprintf("%s:%d:%d:%s  %s", b.file.path, p.Line+1, p.Character+1, strings.TrimPrefix(d.style, "diagnostic."), note))
		}
	}
	return out
}

// pickSymbols opens the finder on workspace symbols. The first of the
// current buffer's language servers that offers them finds them as the
// query is typed (B214).
func (e *Session) pickSymbols() {
	s := first(e.win.buf.servers, "workspace/symbol")
	if s == nil {
		e.message = "no language server offers workspace symbols"
		return
	}
	f := &finder{places: true, enc: s.client.Encoding(), title: "symbols", hint: "type to search the symbols"}
	f.lookup = func(q string, found func([]string, error)) {
		if q == "" {
			found(nil, nil)
			return
		}
		s.client.Request("workspace/symbol", func(string) any { return map[string]any{"query": q} }, func(res json.RawMessage) {
			found(symbolPlaces(res))
		})
	}
	e.openFinder(f, "")
}

// symbolPlaces returns the symbols of a workspace/symbol answer as places,
// each its name and kind, grouped by file, at most grepLimit of them.
func symbolPlaces(res json.RawMessage) ([]string, error) {
	var found []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			URI   string    `json:"uri"`
			Range lsp.Range `json:"range"`
		} `json:"location"`
	}
	if err := json.Unmarshal(res, &found); err != nil {
		return nil, fmt.Errorf("workspace symbols: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("workspace symbols: %w", err)
	}
	var out []string
	for _, sym := range found[:min(len(found), grepLimit)] {
		u, err := url.Parse(sym.Location.URI)
		if err != nil || u.Path == "" {
			continue
		}
		path := u.Path
		if rel, ok := under(wd, path); ok {
			path = rel
		}
		kind := ""
		if sym.Kind > 0 && sym.Kind < len(kinds) {
			kind = "  " + kinds[sym.Kind]
		}
		p := sym.Location.Range.Start
		out = append(out, fmt.Sprintf("%s:%d:%d:%s%s", path, p.Line+1, p.Character+1, sym.Name, kind))
	}
	// Places show under their file, so a file's come together.
	slices.SortStableFunc(out, func(a, b string) int {
		pa, _, _, _ := grepSpot(a)
		pb, _, _, _ := grepSpot(b)
		return strings.Compare(pa, pb)
	})
	return out, nil
}
