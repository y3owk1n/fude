package editor

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/vim"
)

// pickSources are the sources :pick finds in, by name.
var pickSources = []string{"help", "keymaps", "commands"}

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
		names := e.pickNames()
		e.pickFrom("pick", names, func(i int) { e.openPick(names[i]) })
	case "help":
		tags := e.helpTags()
		e.pickFrom("help", tags, func(i int) { e.help(tags[i]) })
	case "keymaps":
		e.pickKeys()
	case "commands":
		e.pickCommands()
	default:
		if src := e.sources[source]; src != nil {
			src(e, strings.TrimSpace(rest))
			return
		}
		e.message = fmt.Sprintf("no source named %q, only %s", source, strings.Join(e.pickNames(), ", "))
	}
}

// pickNames returns the sources of :pick, fude's then the modules' (B287).
func (e *Session) pickNames() []string {
	return append(slices.Clone(pickSources), slices.Sorted(maps.Keys(e.sources))...)
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
	e.openFinder(&finder{paths: items, stale: true, title: title, chose: func(i int, _, _ string, ok bool) {
		if ok && i >= 0 {
			picked(i)
		}
	}}, "")
}
