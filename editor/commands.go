package editor

import (
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/internal/fuzzy"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// command is one of the editor's ex commands, what runs it and what its
// argument may become.
type command struct {
	vim.Command
	run func(e *Session, x call) error
	// complete returns where the word being typed in arg begins and what it
	// may become, or is nil for an argument with no completion.
	complete func(e *Session, line string, start int, arg string, wait bool) (int, []string)
}

// call is what an editor command gets from its command line. force says
// whether ! followed the name, and first and last are the starts of the
// range's first and last lines.
type call struct {
	name, arg   string
	force       bool
	first, last int
	ranged      bool
}

// commands are the editor's ex commands, plugs the commands keys run, and
// commandsByName finds a command by any of its names. init fills them
// because :help lists them.
var (
	commands       []command
	plugs          []plug
	commandsByName map[string]*command
)

func init() {
	commands = []command{
		{vim.Command{Name: "accept", Args: "[n]", Help: "applies proposal n, or the buffer's newest"}, func(e *Session, x call) error {
			return e.decide(x.arg, true)
		}, nil},
		{vim.Command{Name: "bufdo", Short: []string{"bufd"}, Args: "{cmd}", Help: "runs cmd in each buffer being edited, showing each in turn"}, func(e *Session, x call) error { return e.bufdo(x.arg) }, nil},
		{vim.Command{Name: "cquit", Short: []string{"cq"}, Args: "[n]", Help: "quits without writing, and fude exits with n, or 1"}, func(e *Session, x call) error { return e.cquit(x.arg) }, nil},
		{vim.Command{Name: "cd", Args: "[dir]", Help: "changes the working directory, to home without dir"}, func(e *Session, x call) error { return e.cd(x.arg) }, completePath},
		{vim.Command{Name: "close", Short: []string{"clo"}, Help: "closes the window"}, func(e *Session, _ call) error { return e.closeWindow(e.win) }, nil},
		{vim.Command{Name: "edit", Short: []string{"e"}, Args: "[file]", Help: "opens file, or reads the buffer's file again. ! drops changes"}, func(e *Session, x call) error {
			if x.arg == "" {
				return e.reread(x.force)
			}
			return e.edit(x.arg)
		}, completePath},
		{vim.Command{Name: "file", Short: []string{"f"}, Args: "[file]", Help: "names the buffer's file, then says its name, lines and the cursor's place, as <C-g> does"}, func(e *Session, x call) error {
			if x.arg != "" {
				if err := e.rename(e.win.buf, x.arg); err != nil {
					return err
				}
			}
			e.message = e.fileInfo(0)
			return nil
		}, completePath},
		{vim.Command{Name: "pick", Args: "[source] [text]", Help: "opens the finder on a source, or on the sources: files, grep [text], buffers, recent, diagnostics, symbols, help or keymaps. resume opens the last one again"}, func(e *Session, x call) error { return e.openPick(x.arg) }, func(e *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, fuzzy.Match(e.pickNames(), arg)
		}},
		{vim.Command{Name: "config", Args: "reload", Help: "reads config.toml again, without writing it"}, func(e *Session, x call) error {
			if x.arg != "reload" {
				return errors.New("config what? reload")
			}
			return e.reloadConfig()
		}, func(_ *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, fuzzy.Match([]string{"reload"}, arg)
		}},
		{vim.Command{Name: "plugin", Args: "[install|update|enable|disable|reload] [name]", Help: "lists the plugins, installs or updates those of repos, enables or disables one, or reads config.toml's plugins again and loads them anew"}, func(e *Session, x call) error {
			return e.pluginCommand(x.arg)
		}, func(e *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, fuzzy.Match(e.pluginWords(), arg)
		}},
		{vim.Command{Name: "quit", Short: []string{"q"}, Help: "closes the window, and quits with the last. ! drops changes"}, func(e *Session, x call) error { return e.quit(x.force) }, nil},
		{vim.Command{Name: "quitall", Short: []string{"qa", "qall", "quita"}, Help: "quits. ! drops every change"}, func(e *Session, x call) error { return e.quitAll(x.force) }, nil},
		{vim.Command{Name: "restart", Help: "starts fude again on the same files, windows and unsaved changes, with config, plugins and servers loaded afresh"}, func(e *Session, _ call) error { return e.restart() }, nil},
		{vim.Command{Name: "reject", Args: "[n]", Help: "drops proposal n, or the buffer's newest"}, func(e *Session, x call) error {
			return e.decide(x.arg, false)
		}, nil},
		{vim.Command{Name: "saveas", Short: []string{"sav"}, Args: "{file}", Help: "names the buffer's file and writes it there. ! writes over a file that exists"}, func(e *Session, x call) error { return e.saveAs(x.arg, x.force) }, completePath},
		{vim.Command{Name: "set", Short: []string{"se"}, Args: "[name [value]]", Help: "sets an option to a value, as config.toml would, shows one, or lists them all"}, func(e *Session, x call) error { return e.set(x.arg) }, completeOption},
		{vim.Command{Name: "share", Help: "shows a new share string, which lets one more guest join"}, func(e *Session, _ call) error { return e.reshare() }, nil},
		{vim.Command{Name: "tabclose", Short: []string{"tabc"}, Help: "closes the tab page and its windows"}, func(e *Session, _ call) error { return e.closePage(e.tabpage) }, nil},
		{vim.Command{Name: "tabedit", Short: []string{"tabe"}, Args: "[file]", Help: "opens file, or a new empty buffer, in a new tab page"}, func(e *Session, x call) error { return e.tabNew(x.arg) }, completePath},
		{vim.Command{Name: "tabmove", Short: []string{"tabm"}, Args: "[n]", Help: "moves the tab page after page n, first with 0, by n with +n or -n, or to the end"}, func(e *Session, x call) error { return e.movePage(x.arg) }, nil},
		{vim.Command{Name: "tabnew", Args: "[file]", Help: "opens file, or a new empty buffer, in a new tab page"}, func(e *Session, x call) error { return e.tabNew(x.arg) }, completePath},
		{vim.Command{Name: "tabnext", Short: []string{"tabn"}, Args: "[n]", Help: "goes to the next tab page, page n, or n pages on with +n"}, func(e *Session, x call) error { return e.nextPage(x.arg) }, nil},
		{vim.Command{Name: "tabonly", Short: []string{"tabo"}, Help: "closes every other tab page"}, func(e *Session, _ call) error { return e.onlyPage() }, nil},
		{vim.Command{Name: "tabprevious", Short: []string{"tabp", "tabNext", "tabN"}, Args: "[n]", Help: "goes to the tab page before, or n pages back"}, func(e *Session, x call) error { return e.previousPage(x.arg) }, nil},
		{vim.Command{Name: "windo", Args: "{cmd}", Help: "runs cmd in each window, moving to each in turn"}, func(e *Session, x call) error { return e.windo(x.arg) }, nil},
		{vim.Command{Name: "wall", Short: []string{"wa"}, Help: "writes every modified buffer"}, func(e *Session, x call) error { return e.writeAll(e.bufs, x.force, nil) }, nil},
		{vim.Command{Name: "wq", Help: "writes the buffer and closes the window"}, func(e *Session, x call) error {
			return e.write(e.win.buf, x.force, func() { e.report(e.quit(x.force)) })
		}, nil},
		{vim.Command{Name: "wqall", Short: []string{"wqa"}, Help: "writes every modified buffer and quits"}, writeAllAndQuit, nil},
		{vim.Command{Name: "write", Short: []string{"w"}, Args: "[file]", Help: "writes the buffer, or to file. ! overwrites a file changed on disk"}, func(e *Session, x call) error {
			if x.arg != "" {
				return e.writeTo(e.win.buf, x.arg, x.force)
			}
			return e.write(e.win.buf, x.force, nil)
		}, completePath},
		{vim.Command{Name: "xall", Short: []string{"xa"}, Help: "writes every modified buffer and quits"}, writeAllAndQuit, nil},
		{vim.Command{Name: "xit", Short: []string{"x", "exit"}, Help: "writes the buffer if it is modified and closes the window"}, func(e *Session, x call) error {
			if e.win.buf.modified() {
				return e.write(e.win.buf, x.force, func() { e.report(e.quit(x.force)) })
			}
			return e.quit(x.force)
		}, nil},
	}
	plugs = []plug{
		{"restart", "starts fude again on the same session", func(e *Session, _ int) { e.report(e.restart()) }},
		{"file", "says the buffer's name, lines and the cursor's place, with a count its full path", func(e *Session, n int) { e.message = e.fileInfo(n) }},
		{"suspend", "suspends fude to the shell that started it, until the shell resumes it", func(e *Session, _ int) { e.suspending = true }},
		{"finder.open", "opens the selected match", func(e *Session, _ int) { e.finderAct("open") }},
		{"finder.list", "lists every match as places, the last list", func(e *Session, _ int) { e.finderAct("list") }},
		{"finder.next", "selects the next match", func(e *Session, _ int) { e.finderAct("next") }},
		{"finder.prev", "selects the match before", func(e *Session, _ int) { e.finderAct("prev") }},
		{"finder.all", "turns searching the files git ignores and hidden ones on or off", func(e *Session, _ int) { e.finderAct("all") }},
		{"finder.resume", "opens the finder that closed last again, as it was", func(e *Session, _ int) { e.report(e.resume()) }},
		{"float.close", "closes the float the cursor is in", func(e *Session, _ int) {
			if e.win.anchor != "" {
				e.closeFloat(e.win)
			}
		}},
		{"terminal.close", "closes the terminal or task window once its program has ended", func(e *Session, _ int) { e.closeTerminal() }},
	}
	commandsByName = byName(commands)
}

// byName indexes cs by each of their names.
func byName(cs []command) map[string]*command {
	out := map[string]*command{}
	for i := range cs {
		for _, name := range append([]string{cs[i].Name}, cs[i].Short...) {
			out[name] = &cs[i]
		}
	}
	return out
}

// writeAllAndQuit is :wqall and :xall.
func writeAllAndQuit(e *Session, x call) error {
	return e.writeAll(e.bufs, x.force, func() { e.report(e.quitAll(x.force)) })
}

// bufdo shows each buffer being edited in turn and runs cmd in it,
// stopping at the first error (B425).
func (e *Session) bufdo(cmd string) error {
	if cmd == "" {
		return errors.New("run what? :bufdo cmd")
	}
	for _, b := range slices.Clone(e.bufs) {
		if b.kind() != "" || !b.numbered() || e.barred(b) != nil || !slices.Contains(e.bufs, b) {
			continue
		}
		e.show(b)
		if err := e.vim.Ex(cmd); err != nil {
			return err
		}
	}
	return nil
}

// windo moves to each window of the layout in turn and runs cmd there,
// stopping at the first error (B425).
func (e *Session) windo(cmd string) error {
	if cmd == "" {
		return errors.New("run what? :windo cmd")
	}
	for _, w := range e.root.windows() {
		if !slices.Contains(e.root.windows(), w) {
			continue
		}
		e.win = w
		if err := e.vim.Ex(cmd); err != nil {
			return err
		}
	}
	return nil
}

// completePath completes a path, off the loop when it must read a
// directory.
func completePath(e *Session, line string, start int, arg string, wait bool) (int, []string) {
	if !wait {
		e.completeLater(line, start, arg)
		return 0, nil
	}
	dir, _ := filepath.Split(arg)
	return start, pathMatches(arg, e.entries(dir))
}

// completeOption completes the last option of :set.
func completeOption(e *Session, line string, start int, arg string, wait bool) (int, []string) {
	opts, settings := e.win.opts, e.cfg.settings()
	name, value, given := strings.Cut(arg, " ")
	if !given {
		names := slices.Concat(slices.Collect(maps.Keys(opts.switches())), slices.Collect(maps.Keys(opts.numbers())), slices.Collect(maps.Keys(settings)), slices.Collect(maps.Keys(e.declared)), []string{"filetype"})
		slices.Sort(names)
		return start, fuzzy.Match(names, name)
	}
	// A switch takes true or false, theme a theme, filetype a filetype with
	// a grammar or a table, completion_sources its sources, and a setting of
	// words those words.
	at := start + len(name) + 1
	_, isSwitch := opts.switches()[name]
	if def := e.declared[name].reg.Default; def == "true" || def == "false" {
		isSwitch = true
	}
	switch {
	case isSwitch || settings[name].IsValid() && settings[name].Kind() == reflect.Bool:
		return at, fuzzy.Match([]string{"false", "true"}, value)
	case name == "theme":
		return completeTheme(e, line, at, value, wait)
	case name == "filetype":
		fts := slices.Collect(maps.Keys(e.cfg.Filetype))
		for _, g := range syntax.Grammars() {
			fts = append(fts, g.Filetype)
		}
		slices.Sort(fts)
		return at, fuzzy.Match(slices.Compact(fts), value)
	case name == "completion_sources":
		done := value[:strings.LastIndexByte(value, ',')+1]
		return at + len(done), fuzzy.Match(e.knownSources(), value[len(done):])
	case settingWords[name] != nil:
		return at, fuzzy.Match(settingWords[name], value)
	}
	return 0, nil
}

// completeTheme offers the themes, and the scheme config.toml names so
// :theme can switch back to it.
func completeTheme(e *Session, _ string, start int, arg string, _ bool) (int, []string) {
	names := []string{"dark", "light"}
	if !slices.Contains(names, e.cfg.Theme) {
		names = append(names, e.cfg.Theme)
	}
	return start, fuzzy.Match(names, arg)
}

// helpCommands returns the editor's commands as help shows them.
func helpCommands() []vim.Command {
	out := make([]vim.Command, len(commands))
	for i, c := range commands {
		out[i] = c.Command
	}
	return out
}

// plug is a command keys run as <Plug>(name), and what it does.
type plug struct {
	name, help string
	run        func(e *Session, count int)
}

// plugNames returns the name of each <Plug>, fude's and those of plugins'
// keys.
func (e *Session) plugNames() []string {
	var names []string
	for _, p := range plugs {
		names = append(names, p.name)
	}
	for _, p := range e.modules {
		for _, r := range p.Registered() {
			// A key with Chars is the vim machine's own, with no plug.
			if (r.Kind == api.Key || r.Kind == api.Plug) && r.Chars == 0 {
				names = append(names, p.Name()+"."+r.Name)
			}
		}
	}
	return names
}

// defaultMaps are the normal mode keys fude maps to its commands.
var defaultMaps = map[string]string{
	"ZR":    "<Plug>(restart)",
	"<C-g>": "<Plug>(file)",
	"<C-z>": "<Plug>(suspend)",
	"s":     "<Plug>(jump.start)",
}

// hostOperator is an operator fude adds, and what it does.
type hostOperator struct {
	keys, help string
	run        func(e *Session, t vim.Text, start, end int, lines bool) []vim.Edit
}

// hostOperators are the operators fude adds.
var hostOperators = []hostOperator{}
