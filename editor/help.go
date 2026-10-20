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

	"github.com/y3owk1n/fude/api"
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

// referenceFiles hold the longer explanations of help's entries, a file for
// each section of the reference titled as it is, and each explanation under
// a heading of the name :h finds its entry by.
//
//go:embed reference/*.md
var referenceFiles embed.FS

// details returns the longer explanations of help's entries, by the slug of
// their section and the name :h finds each by. A section's own opening text
// is under "" (B162).
func details() map[string]map[string]string {
	out := map[string]map[string]string{}
	// The files are in the binary, so neither call fails.
	names, _ := fs.Glob(referenceFiles, "reference/*.md")
	for _, name := range names {
		data, _ := referenceFiles.ReadFile(name)
		parts := strings.Split(string(data), "\n## ")
		title, intro, _ := strings.Cut(strings.TrimPrefix(parts[0], "# "), "\n")
		section := map[string]string{"": strings.TrimSpace(intro)}
		for _, p := range parts[1:] {
			name, text, _ := strings.Cut(p, "\n")
			section[name] = strings.TrimSpace(text)
		}
		out[slug(title)] = section
	}
	return out
}

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
	shorts := slices.Sorted(maps.Keys(e.shortOf))
	for _, name := range slices.Sorted(maps.Keys(e.commands)) {
		// A short name shows on its command's line.
		if _, ok := e.shortOf[name]; ok {
			continue
		}
		head := strings.TrimSpace(":" + name + " " + e.pluginArgs[name])
		tags := []string{":" + name}
		for _, s := range shorts {
			if e.shortOf[s] == name {
				head += "  :" + s
				tags = append(tags, ":"+s)
			}
		}
		commands.entries = append(commands.entries, helpEntry{tags, head, cmp.Or(e.pluginHelp[":"+name], "a plugin's command")})
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
	for _, m := range e.modules {
		for _, r := range m.Registered() {
			if (r.Kind == api.Key || r.Kind == api.Plug) && r.Chars == 0 {
				name := m.Name() + "." + r.Name
				plugsSection.entries = append(plugsSection.entries, helpEntry{[]string{"<Plug>(" + name + ")", name}, "<Plug>(" + name + ")", r.Help})
			}
		}
	}
	out = append(out, plugsSection)

	options := helpSection{title: "OPTIONS", note: ":set name value changes one in the window. A switch takes true or false. :set name shows its value. config.toml sets their starting values."}
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
	options.entries = append(options.entries, e.declaredEntries(true)...)
	out = append(out, options)

	settings := helpSection{title: "SETTINGS", note: "The keys of config.toml, beside the options above."}
	cfg := reflect.ValueOf(defaults)
	for _, f := range reflect.VisibleFields(cfg.Type()) {
		name, v := f.Tag.Get("toml"), cfg.FieldByIndex(f.Index)
		switch {
		case f.Anonymous || len(f.Index) > 1:
			// The options, which help shows above.
		case name == "keymap":
			for _, k := range keymapTables {
				settings.entries = append(settings.entries, helpEntry{[]string{name + "." + k.name}, "[" + name + "." + k.name + "]", k.help})
			}
		case v.Kind() == reflect.Struct && f.Tag.Get("help") != "":
			// A table such as [colors], whose explanation lists its keys.
			settings.entries = append(settings.entries, helpEntry{[]string{name}, "[" + name + "]", f.Tag.Get("help")})
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
		case v.Kind() == reflect.Map:
			// [open], a value for any key, such as a module's name.
			settings.entries = append(settings.entries, helpEntry{[]string{name}, "[" + name + "] {" + f.Tag.Get("key") + "}", f.Tag.Get("help")})
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
	settings.entries = append(settings.entries, e.declaredEntries(false)...)
	return append(out, settings)
}

// declaredEntries returns help's entries of the options modules registered,
// each window's or the others, with the values they start at (B377).
func (e *Session) declaredEntries(window bool) []helpEntry {
	var out []helpEntry
	for _, name := range slices.Sorted(maps.Keys(e.declared)) {
		d := e.declared[name]
		if d.reg.Window != window {
			continue
		}
		value := d.reg.Default
		if value != "true" && value != "false" && !number(value) {
			value = fmt.Sprintf("%q", value)
		}
		head := name + " = " + value
		if window {
			// As fude's options show theirs.
			head = name + "  " + cmp.Or(map[string]string{"true": "on", "false": "off"}[value], value)
		}
		out = append(out, helpEntry{[]string{name}, head, d.reg.Help})
	}
	return out
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
	mine := e.keymap
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
	titles := map[string]string{
		"explorer": "KEYS IN AN EXPLORER",
		"list":     "KEYS IN A LIST OF PLACES",
		"timeline": "KEYS IN A TIMELINE",
		"finder":   "KEYS IN THE FINDER",
		"float":    "KEYS IN A FLOAT",
		"lsp":      "KEYS IN THE LANGUAGE SERVER PANEL",
		"debug":    "KEYS IN THE DEBUG PANEL",
		"help":     "KEYS IN HELP",
		"scratch":  "KEYS IN A WINDOW FUDE WROTE",
		"start":    "KEYS ON THE START SCREEN",
		"terminal": "KEYS IN A TERMINAL IN NORMAL MODE",
	}
	// fude's own kinds come in keymapTables' order, then the modules'.
	var kinds []string
	for _, t := range keymapTables {
		if _, ok := titles[t.name]; ok {
			kinds = append(kinds, t.name)
		}
	}
	for _, kind := range slices.Sorted(maps.Keys(e.locals)) {
		if _, ok := titles[kind]; !ok {
			kinds = append(kinds, kind)
			titles[kind] = "KEYS IN " + strings.ToUpper(kind)
		}
	}
	for _, kind := range kinds {
		_, mode := e.table(kind)
		out = append(out, section(titles[kind], kind, func(f func(string, string)) { e.locals[kind].Each(mode, f) }))
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

// moduleDocs returns the longer explanations modules give their entries in
// Registration.Doc, by the slug of the entry's section and the name :h
// finds it by (B375).
func (e *Session) moduleDocs() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, p := range e.modules {
		for _, r := range p.Registered() {
			if r.Doc == "" {
				continue
			}
			var section, tag string
			switch {
			case r.Kind == api.Command:
				section, tag = "commands", ":"+r.Name
			case r.Kind == api.Plug, r.Kind == api.Key && r.Chars == 0:
				section, tag = "commands-for-keys", "<Plug>("+p.Name()+"."+r.Name+")"
			case r.Kind == api.Operator:
				section, tag = "operators", bindingKeys(r)
			case r.Kind == api.Object:
				section, tag = "text-objects", r.Name
			case r.Kind == api.Key:
				section, tag = modes[keyedMode(r)]+"-mode", bindingKeys(r)
			case r.Kind == api.Insert, r.Kind == api.Typed:
				section, tag = "insert-mode", r.Name
			case r.Kind == api.Option && r.Window:
				section, tag = "options", p.Name()+"."+r.Name
			case r.Kind == api.Option:
				section, tag = "settings", p.Name()+"."+r.Name
			default:
				continue
			}
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			out[section][tag] = strings.TrimSpace(r.Doc)
		}
	}
	return out
}

// pages returns the guide's pages: fude's, then those modules add (B376).
func (e *Session) pages() []guidePage {
	out := guide()
	for _, p := range e.modules {
		for _, r := range p.Registered() {
			if r.Kind == api.Page {
				text := strings.TrimSpace(r.Doc)
				title, _, _ := strings.Cut(strings.TrimPrefix(text, "# "), "\n")
				out = append(out, guidePage{"guide-" + r.Name, title, text})
			}
		}
	}
	return out
}

// HelpGaps returns what help lacks with fude's defaults and modules, for a
// distribution's tests. missing is each entry with no longer explanation,
// in editor/reference or its module's Doc, and each guide page with no #
// title. unused is each explanation in editor/reference that names no entry
// of its section, and each file there with no section. It is empty only
// with every module fude adds (B162, B375).
func HelpGaps(modules ...api.Module) (missing, unused []string, err error) {
	e, err := open(defaults, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("help: %w", err)
	}
	for _, m := range modules {
		e.Use(m)
	}
	more, docs := details(), e.moduleDocs()
	sections := map[string]bool{}
	for _, s := range e.helpSections() {
		own, ok := more[slug(s.title)]
		if !ok {
			// The mappings and a kind's keys come from keymaps alone.
			continue
		}
		sections[slug(s.title)] = true
		named := map[string]bool{"": true}
		for _, en := range s.entries {
			if named[en.tags[0]] = true; own[en.tags[0]] == "" && docs[slug(s.title)][en.tags[0]] == "" {
				missing = append(missing, fmt.Sprintf("%s: %s has no explanation", slug(s.title), en.tags[0]))
			}
		}
		for name := range own {
			if !named[name] {
				unused = append(unused, fmt.Sprintf("%s: %s explains no entry", slug(s.title), name))
			}
		}
	}
	for name := range more {
		if !sections[name] {
			unused = append(unused, fmt.Sprintf("%s.md is no section of the reference", name))
		}
	}
	for _, pg := range e.pages() {
		if !strings.HasPrefix(pg.text, "# ") {
			missing = append(missing, pg.tag+" has no # title")
		}
	}
	slices.Sort(missing)
	slices.Sort(unused)
	return missing, unused, nil
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
	pages := e.pages()
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
	write("closes this window. An example shows the text before, the keys typed and")
	write("the text after, with `|` at the cursor and `\\n` for a line break.")
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
	more, docs := details(), e.moduleDocs()
	for _, s := range sections {
		write("")
		tag(slug(s.title))
		write("## " + sentence(s.title))
		own := more[slug(s.title)]
		if note := cmp.Or(own[""], s.note); note != "" {
			write("")
			write(note)
		}
		for _, en := range s.entries {
			write("")
			for _, t := range en.tags {
				tag(t)
			}
			write(code(en.head))
			write("  " + en.text)
			if d := cmp.Or(own[en.tags[0]], docs[slug(s.title)][en.tags[0]]); d != "" {
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

// helpAt returns help's text and the line of the entry topic names, or of
// the start for "" (B162, B334).
func (e *Session) helpAt(topic string) (string, int, bool) {
	text, tags := e.helpText()
	if topic == "" {
		return text, 0, true
	}
	line, ok := helpLine(tags, topic, e.cfg.Leader)
	return text, line, ok
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
