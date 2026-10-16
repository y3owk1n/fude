package editor

import (
	"cmp"
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// helpEntry is one entry of help: the names :h finds it by, its head and
// what it says.
type helpEntry struct {
	tags       []string
	head, text string
}

// helpSection is a titled part of help, with a note under its title.
type helpSection struct {
	title, note string
	entries     []helpEntry
}

// guideFiles are the guide's pages, each a markdown file whose name orders
// it and names its tag: 01-start.md is guide-start.
//
//go:embed guide/*.md
var guideFiles embed.FS

// guidePage is a page of the guide: its tag, its title and its markdown.
type guidePage struct {
	tag, title, text string
}

// guide returns the guide's pages in order (B162).
func guide() []guidePage {
	// The files are in the binary, so neither call fails.
	names, _ := fs.Glob(guideFiles, "guide/*.md")
	var out []guidePage
	for _, name := range names {
		data, _ := guideFiles.ReadFile(name)
		text := strings.TrimSpace(string(data))
		_, base, _ := strings.Cut(strings.TrimSuffix(path.Base(name), ".md"), "-")
		title, _, _ := strings.Cut(strings.TrimPrefix(text, "# "), "\n")
		out = append(out, guidePage{"guide-" + base, title, text})
	}
	return out
}

// settingsFile holds the longer explanations of the options and settings,
// each under a heading of the name :h finds it by.
//
//go:embed settings.md
var settingsFile string

// details returns the longer explanations of the options and settings, by
// the name :h finds each by (B162).
func details() map[string]string {
	out := map[string]string{}
	parts := strings.Split(settingsFile, "\n## ")
	for _, p := range parts[1:] {
		name, text, _ := strings.Cut(p, "\n")
		out[name] = strings.TrimSpace(text)
	}
	return out
}

// helpKeys are help's own keys, which [keymap.help] changes (B162).
var helpKeys = map[string]string{"<CR>": "<Plug>(help.follow)", "<C-]>": "<Plug>(help.follow)", "q": ":close<CR>"}

// keyKinds are the kinds of vim.Bindings in the order help shows them, with
// their titles and notes.
var keyKinds = []struct{ kind, title, note string }{
	{"motion", "MOTIONS", "Moves in normal and visual mode, and targets after an operator. n is the count, or 1 without one."},
	{"operator", "OPERATORS", "Wait for a motion or object, and act on the text it covers."},
	{"object", "TEXT OBJECTS", "Select text after an operator or in visual mode. The syntax ones need the file's grammar."},
	{"normal", "NORMAL MODE", ""},
	{"visual", "VISUAL MODE", "Keys while text is selected, beside the motions and objects."},
	{"insert", "INSERT MODE", "Keys while typing. They work in replace mode too."},
	{"command line", "COMMAND LINE", "Keys while a : command or a search is typed."},
}

// helpSections returns help: fude's commands, keys, mappings, options and
// settings, from the tables that run them, and the session's mappings.
func (e *Session) helpSections() []helpSection {
	var out []helpSection
	cmds := slices.Concat(vim.Commands(), helpCommands())
	slices.SortFunc(cmds, func(a, b vim.Command) int { return strings.Compare(a.Name, b.Name) })
	commands := helpSection{title: "COMMANDS", note: "Type them after :. A range of lines may come before the name, and ! after it forces the command."}
	for _, c := range cmds {
		// A name that is not a word takes its argument straight after.
		sep := " "
		if !unicode.IsLetter(rune(c.Name[len(c.Name)-1])) {
			sep = ""
		}
		head := strings.TrimSpace(":" + c.Name + sep + c.Args)
		tags := []string{":" + c.Name}
		for _, s := range c.Short {
			head += "  :" + s
			tags = append(tags, ":"+s)
		}
		commands.entries = append(commands.entries, helpEntry{tags, head, c.Help})
	}
	for _, name := range slices.Sorted(maps.Keys(e.commands)) {
		commands.entries = append(commands.entries, helpEntry{[]string{":" + name}, ":" + name, cmp.Or(e.pluginHelp[":"+name], "a plugin's command")})
	}
	out = append(out, commands)

	bindings := slices.Concat(vim.Bindings(), hostBindings(), e.pluginBindings)
	for _, k := range keyKinds {
		s := helpSection{title: k.title, note: k.note}
		for _, b := range bindings {
			if b.Kind != k.kind {
				continue
			}
			// Keys that do the same share an entry.
			if n := len(s.entries); n > 0 && s.entries[n-1].text == b.Help {
				s.entries[n-1].head += "  " + b.Keys
				s.entries[n-1].tags = append(s.entries[n-1].tags, keyTags(b.Keys)...)
				continue
			}
			s.entries = append(s.entries, helpEntry{keyTags(b.Keys), b.Keys, b.Help})
		}
		out = append(out, s)
	}

	out = append(out, e.mappings()...)

	plugsSection := helpSection{title: "COMMANDS FOR KEYS", note: `Map a key to one in config.toml, such as "<Leader>d" = "<Plug>(lsp.definition)" in [keymap.normal].`}
	for _, p := range plugs {
		plugsSection.entries = append(plugsSection.entries, helpEntry{[]string{"<Plug>(" + p.name + ")", p.name}, "<Plug>(" + p.name + ")", p.help})
	}
	out = append(out, plugsSection)

	options := helpSection{title: "OPTIONS", note: ":set changes them in the window. name turns one on, noname off, name! flips it, name=n sets a number and name? shows it. config.toml sets their starting values."}
	for _, f := range helpFields(reflect.ValueOf(defaults.options)) {
		head := f.name
		switch v := f.value.Interface().(type) {
		case bool:
			head += map[bool]string{true: "  on", false: "  off"}[v]
		case int:
			head += fmt.Sprintf("  %d", v)
		}
		options.entries = append(options.entries, helpEntry{[]string{f.name}, head, f.help})
	}
	out = append(out, options)

	settings := helpSection{title: "SETTINGS", note: "The keys of config.toml, beside the options above."}
	cfg := reflect.ValueOf(defaults)
	for _, f := range reflect.VisibleFields(cfg.Type()) {
		name, v := f.Tag.Get("toml"), cfg.FieldByIndex(f.Index)
		switch {
		case f.Anonymous || len(f.Index) > 1:
			// The options, which help shows above.
		case v.Kind() == reflect.Struct:
			for _, k := range helpFields(v) {
				settings.entries = append(settings.entries, helpEntry{[]string{name + "." + k.name}, "[" + name + "." + k.name + "]", k.help})
			}
		case (v.Kind() == reflect.Map || v.Kind() == reflect.Slice) && v.Type().Elem().Kind() == reflect.Struct:
			// [filetype.go], [lsp.gopls] and [[plugin]] tables, whose keys are
			// the fields of their elements.
			table := "[" + name + ".{" + f.Tag.Get("key") + "}]"
			if v.Kind() == reflect.Slice {
				table = "[[" + name + "]]"
			}
			for _, k := range helpFields(reflect.New(v.Type().Elem()).Elem()) {
				settings.entries = append(settings.entries, helpEntry{[]string{name + "." + k.name}, table + " " + k.name, k.help})
			}
		default:
			value := fmt.Sprint(v.Interface())
			switch v.Kind() {
			case reflect.String:
				value = fmt.Sprintf("%q", value)
			case reflect.Slice:
				// A list of strings, as completion_sources is.
				value = strings.ReplaceAll(fmt.Sprintf("%q", v.Interface()), `" "`, `", "`)
			}
			settings.entries = append(settings.entries, helpEntry{[]string{name}, name + " = " + value, f.Tag.Get("help")})
		}
	}
	return append(out, settings)
}

// keyTags are the names :h finds keys by: the keys, and for keys such as
// f{char} also f.
func keyTags(keys string) []string {
	tags := []string{keys}
	if i := strings.IndexByte(keys, '{'); i > 0 {
		tags = append(tags, keys[:i])
	}
	return tags
}

// hostBindings are the operators, objects and window commands fude adds to
// the machine's.
func hostBindings() []vim.Binding {
	var out []vim.Binding
	for _, op := range hostOperators {
		out = append(out, vim.Binding{Kind: "operator", Keys: op.keys + "{motion}", Help: op.help})
	}
	for _, name := range slices.Sorted(maps.Keys(syntax.Objects)) {
		out = append(out, vim.Binding{Kind: "object", Keys: name, Help: syntax.Objects[name].Help})
	}
	for _, w := range windowKeys {
		for _, k := range w.keys {
			out = append(out, vim.Binding{Kind: "normal", Keys: "<C-w>" + k, Help: w.help})
		}
	}
	return out
}

// mappingText says what a mapping that runs to does: its <Plug>'s help, a
// plugin's own, or the keys it types.
func (ed *Editor) mappingText(to string) string {
	name, ok := strings.CutPrefix(to, "<Plug>(")
	if !ok {
		return "types " + to
	}
	if help := ed.pluginHelp[to]; help != "" {
		return help
	}
	name = strings.TrimSuffix(name, ")")
	if i := slices.IndexFunc(plugs, func(p plug) bool { return p.name == name }); i >= 0 {
		return plugs[i].help
	}
	return "runs " + to
}

// mappings returns help's sections of the session's mappings: the
// machine's in each mode and each kind of buffer's own, fude's and those
// config.toml sets.
func (e *Session) mappings() []helpSection {
	mine := map[string]map[string]string{
		"normal": e.cfg.Keymap.Normal, "visual": e.cfg.Keymap.Visual, "insert": e.cfg.Keymap.Insert,
		"listing": e.cfg.Keymap.Listing, "list": e.cfg.Keymap.List, "timeline": e.cfg.Keymap.Timeline,
		"finder": e.cfg.Keymap.Finder, "float": e.cfg.Keymap.Float, "lsp": e.cfg.Keymap.Lsp, "help": e.cfg.Keymap.Help,
	}
	leader := vim.Notation(e.cfg.Leader)
	section := func(title, kind string, each func(func(from, to string))) helpSection {
		s := helpSection{title: title}
		each(func(from, to string) {
			text := e.mappingText(to)
			for k := range mine[kind] {
				if slices.Equal(vim.Keys(withLeader(k, e.cfg.Leader)), vim.Keys(from)) {
					text += ", as config.toml maps it"
				}
			}
			tags := []string{from}
			if rest, ok := strings.CutPrefix(from, leader); ok && leader != "" {
				tags = append(tags, "<Leader>"+rest)
			}
			s.entries = append(s.entries, helpEntry{tags, from, text})
		})
		return s
	}
	out := []helpSection{
		section("MAPPINGS IN NORMAL MODE", "normal", func(f func(string, string)) { e.vim.Each(vim.Normal, f) }),
		section("MAPPINGS IN VISUAL MODE", "visual", func(f func(string, string)) { e.vim.Each(vim.Visual, f) }),
		section("MAPPINGS IN INSERT MODE", "insert", func(f func(string, string)) { e.vim.Each(vim.Insert, f) }),
	}
	for _, k := range []struct{ kind, title string }{
		{"listing", "KEYS IN A LISTING"},
		{"list", "KEYS IN A LIST OF PLACES"},
		{"timeline", "KEYS IN A TIMELINE"},
		{"finder", "KEYS IN THE FINDER"},
		{"float", "KEYS IN A FLOAT"},
		{"lsp", "KEYS IN THE LANGUAGE SERVER PANEL"},
		{"help", "KEYS IN HELP"},
		{"scratch", "KEYS IN A WINDOW FUDE WROTE"},
	} {
		mode := vim.Normal
		if k.kind == "finder" {
			mode = vim.Insert
		}
		out = append(out, section(k.title, k.kind, func(f func(string, string)) { e.locals[k.kind].Each(mode, f) }))
	}
	out[0].note = "<Leader> is " + e.cfg.Leader + ". The keys language servers add show while the file has one."
	return out
}

// helpField is a field of a struct help shows: its toml name, its value and
// its help tag.
type helpField struct {
	name, help string
	value      reflect.Value
}

// helpFields returns the fields of v that have a help tag, in order.
func helpFields(v reflect.Value) []helpField {
	var out []helpField
	for _, f := range reflect.VisibleFields(v.Type()) {
		if help := f.Tag.Get("help"); help != "" {
			out = append(out, helpField{f.Tag.Get("toml"), help, v.FieldByIndex(f.Index)})
		}
	}
	return out
}

// helpText returns help as markdown: an index, the guide's pages, then the
// reference built from the tables. It also returns the line of each name
// :h finds a page, section or entry by, the first one's when several share
// it.
func (e *Session) helpText() (string, map[string]int) {
	var b strings.Builder
	tags := map[string]int{}
	line := 0
	write := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
		line += 1 + strings.Count(s, "\n")
	}
	tag := func(t string) {
		if _, ok := tags[t]; !ok {
			tags[t] = line
		}
	}
	pages := guide()
	var sections []helpSection
	for _, s := range e.helpSections() {
		if len(s.entries) > 0 {
			sections = append(sections, s)
		}
	}
	tag("index")
	write("# fude help")
	write("")
	write("`:h {name}` goes to a page of the guide, a section of the reference, or a")
	write("command, key, option or setting. `<CR>` on a name goes to it, and `q`")
	write("closes this window.")
	write("")
	write("## Guide")
	write("")
	for _, p := range pages {
		write("- " + p.title + "  `" + p.tag + "`")
	}
	write("")
	write("## Reference")
	write("")
	for _, s := range sections {
		write("- " + sentence(s.title) + "  `" + slug(s.title) + "`")
	}
	for _, p := range pages {
		write("")
		tag(p.tag)
		write(p.text)
	}
	more := details()
	for _, s := range sections {
		write("")
		tag(slug(s.title))
		write("## " + sentence(s.title))
		if s.note != "" {
			write("")
			write(s.note)
		}
		for _, en := range s.entries {
			write("")
			for _, t := range en.tags {
				tag(t)
			}
			write(code(en.head))
			write("  " + en.text)
			if d := more[en.tags[0]]; d != "" {
				write("")
				write("  " + strings.ReplaceAll(d, "\n", "\n  "))
			}
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), tags
}

// sentence returns an upper case title in sentence case.
func sentence(title string) string {
	return title[:1] + strings.ToLower(title[1:])
}

// slug is the tag of a reference section, such as normal-mode.
func slug(title string) string {
	return strings.ReplaceAll(strings.ToLower(title), " ", "-")
}

// code returns s as markdown's inline code, fenced past any backtick in it.
func code(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// helpLine returns the line of the entry topic names: the entry tagged
// topic, else :topic, else <Plug>(topic), else topic's keys written as
// mappings list them.
func helpLine(tags map[string]int, topic, leader string) (int, bool) {
	for _, t := range []string{topic, ":" + topic, "<Plug>(" + topic + ")", vim.Notation(withLeader(topic, leader))} {
		if line, ok := tags[t]; ok {
			return line, true
		}
	}
	return 0, false
}

// help shows help in its window, opening one when none shows it, with the
// cursor on the entry topic names, or at the top for none (B162).
func (e *Session) help(topic string) {
	text, tags := e.helpText()
	line := 0
	if topic != "" {
		var ok bool
		if line, ok = helpLine(tags, topic, e.cfg.Leader); !ok {
			e.message = "no help for " + topic
			return
		}
	}
	// Help is markdown, coloured as such once its grammar is built.
	if !e.panel("[help]", "markdown", text) {
		return
	}
	// The entry goes at the top of the window, its text below it.
	at := e.win.buf.text.Snapshot().LineStart(line)
	e.setCursor(at)
	e.win.buf.text.MoveMark(e.win.top, at)
}

// followHelp goes to the entry of the name under the cursor in help.
func (e *Session) followHelp() {
	t, c := e.Text(), e.Cursor()
	ls, le := t.LineStart(t.LineOf(c)), vim.LineEnd(t, c)
	line := t.Slice(ls, le)
	at := c - ls
	start := strings.LastIndexAny(line[:at], " \t") + 1
	end := len(line)
	if i := strings.IndexAny(line[at:], " \t"); i >= 0 {
		end = at + i
	}
	word := strings.Trim(line[start:end], ".,`()")
	if word == "" {
		return
	}
	e.help(word)
}

// helpTags returns every name :h takes, for its completion: the names of
// the entries, and a command's without its colon.
func (e *Session) helpTags() []string {
	_, tags := e.helpText()
	names := map[string]bool{}
	for t := range tags {
		names[t], names[strings.TrimPrefix(t, ":")] = true, true
	}
	return slices.Sorted(maps.Keys(names))
}
