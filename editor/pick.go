package editor

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/vim"
)

// openPick opens the finder on the items of source, or on the sources
// without one. grep takes the text after it as its query (B213).
func (e *Session) openPick(arg string) error {
	source, rest, _ := strings.Cut(arg, " ")
	// A guest's finder and grep work only while the working directory is
	// in the project (B245).
	if source == "files" || source == "grep" {
		if err := e.outside("."); err != nil {
			return errors.New("the working directory is outside the project")
		}
	}
	switch source {
	case "":
		names := e.pickNames()
		e.pickFrom("pick", names, func(i int) { e.report(e.openPick(names[i])) })
	case "resume":
		return e.resume()
	default:
		if src := e.sources[source]; src != nil {
			src(e, strings.TrimSpace(rest))
			return nil
		}
		return fmt.Errorf("no source named %q, only %s", source, strings.Join(e.pickNames(), ", "))
	}
	return nil
}

// resume opens the finder that closed last again, with the query typed in
// it, its matches and the one selected (B374).
func (e *Session) resume() error {
	f := e.lastFinder
	if f == nil {
		return errors.New("no finder to resume")
	}
	if e.finder == nil {
		e.openFinder(f, f.typed)
	}
	return nil
}

// pickNames returns the sources of :pick, the modules' (B287).
func (e *Session) pickNames() []string {
	return append(slices.Sorted(maps.Keys(e.sources)), "resume")
}

// commandList is every ex command, the modules' too, and :plug of each
// <Plug>, each with its arguments and help (B324).
func (e *Session) commandList() []api.ExCommand {
	var all []api.ExCommand
	for _, c := range slices.Concat(vim.Commands(), helpCommands()) {
		all = append(all, api.ExCommand{Name: c.Name, Args: c.Args, Help: c.Help})
	}
	for _, name := range slices.Sorted(maps.Keys(e.commands)) {
		if _, ok := e.shortOf[name]; ok {
			continue
		}
		all = append(all, api.ExCommand{Name: name, Args: e.pluginArgs[name], Help: cmp.Or(e.pluginHelp[":"+name], "a plugin's command")})
	}
	for _, name := range e.plugNames() {
		all = append(all, api.ExCommand{Name: "plug " + name, Help: e.mappingText("<Plug>(" + name + ")")})
	}
	return all
}

// keyList is every key: the mappings of normal, visual and insert mode,
// each by what it does, then the machine's keys, fude's and the modules',
// each with its help and kind (B324).
func (e *Session) keyList() []api.Binding {
	var all []api.Binding
	for _, mode := range []vim.Mode{vim.Normal, vim.Visual, vim.Insert} {
		e.vim.Each(mode, func(from, to string) {
			all = append(all, api.Binding{Keys: from, Help: e.mappingText(to), Kind: modes[mode] + " mapping"})
		})
	}
	for _, b := range slices.Concat(vim.Bindings(), hostBindings(), e.pluginBindings) {
		all = append(all, api.Binding{Keys: b.Keys, Help: b.Help, Kind: b.Kind})
	}
	return all
}

// pickFrom opens the finder on items with no preview, and runs picked with
// the index of the one picked (B215).
func (e *Session) pickFrom(title string, items []string, picked func(int)) {
	e.openFinder(&finder{paths: items, stale: true, title: title, chose: func(i int, _, _ string, ok bool) {
		if ok && i >= 0 {
			picked(i)
		}
	}}, "")
}
