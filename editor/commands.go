package editor

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// command is one of the editor's ex commands, what runs it and what its
// argument may become.
type command struct {
	vim.Command
	run func(e *Session, x call)
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
		{vim.Command{Name: "accept", Args: "[n]", Help: "applies proposal n, or the buffer's newest"}, func(e *Session, x call) {
			e.decide(x.arg, true)
		}, nil},
		{vim.Command{Name: "bdelete", Short: []string{"bd"}, Args: "[buffer]", Help: "closes the buffer. ! drops its changes"}, func(e *Session, x call) {
			b := e.win.buf
			if x.arg != "" {
				b = e.find(x.arg)
			}
			if b != nil {
				e.close(b, x.force)
			}
		}, completeBuffer},
		{vim.Command{Name: "bnext", Short: []string{"bn"}, Help: "shows the next buffer"}, func(e *Session, _ call) { e.stepBuffer(1) }, nil},
		{vim.Command{Name: "bprevious", Short: []string{"bp", "bN", "bNext"}, Help: "shows the buffer before"}, func(e *Session, _ call) { e.stepBuffer(-1) }, nil},
		{vim.Command{Name: "buffer", Short: []string{"b"}, Args: "{buffer}", Help: "shows the buffer with that number, or whose name holds it. # is the one shown before"}, func(e *Session, x call) {
			if b := e.find(x.arg); b != nil {
				e.show(b)
			}
		}, completeBuffer},
		{vim.Command{Name: "buffers", Short: []string{"ls"}, Help: "lists the buffers"}, func(e *Session, _ call) { e.list() }, nil},
		{vim.Command{Name: "cd", Args: "[dir]", Help: "changes the working directory, to home without dir"}, func(e *Session, x call) { e.cd(x.arg) }, completePath},
		{vim.Command{Name: "changes", Help: "lists where the buffer changed"}, func(e *Session, _ call) { e.changeList() }, nil},
		{vim.Command{Name: "close", Short: []string{"clo"}, Help: "closes the window"}, func(e *Session, _ call) {
			if err := e.closeWindow(e.win); err != nil {
				e.message = err.Error()
			}
		}, nil},
		{vim.Command{Name: "diff", Args: "{n} [m]", Help: "shows the lines that differ between states n and m, or n and now"}, func(e *Session, x call) { e.diff(x.arg) }, nil},
		{vim.Command{Name: "earlier", Args: "{n}", Help: "goes n states back. n may be a time such as 5m, or a count of writes such as 1f"}, func(e *Session, x call) { e.travel(x.arg, false) }, nil},
		{vim.Command{Name: "edit", Short: []string{"e"}, Args: "[file]", Help: "opens file, or reads the buffer's file again. ! drops changes"}, func(e *Session, x call) {
			if x.arg == "" {
				e.reread(x.force)
				return
			}
			e.edit(x.arg)
		}, completePath},
		{vim.Command{Name: "enew", Help: "shows a new empty buffer"}, func(e *Session, _ call) { e.empty() }, nil},
		{vim.Command{Name: "follow", Args: "{name}", Help: "keeps the window on what the session called name shows, until a key is typed"}, func(e *Session, x call) { e.startFollow(x.arg) }, completeFollow},
		{vim.Command{Name: "pick", Args: "[source] [text]", Help: "opens the finder on a source, or on the sources: files, grep [text], buffers, recent, diagnostics, symbols, help or keymaps"}, func(e *Session, x call) { e.openPick(x.arg) }, func(e *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, match(e.pickNames(), arg)
		}},
		{vim.Command{Name: "plug", Args: "name", Help: "runs <Plug>(name) as a key mapped to it does"}, func(e *Session, x call) { e.runPlug(x.arg) }, func(e *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, match(e.plugNames(), arg)
		}},
		{vim.Command{Name: "cnext", Short: []string{"cn"}, Help: "goes to the next place of the last list of places"}, func(e *Session, _ call) {
			e.stepPlace(func(at, _ int) int { return at + 1 })
		}, nil},
		{vim.Command{Name: "cprevious", Short: []string{"cp", "cN", "cNext"}, Help: "goes to the place before in the last list of places"}, func(e *Session, _ call) {
			e.stepPlace(func(at, _ int) int { return max(at, 0) - 1 })
		}, nil},
		{vim.Command{Name: "cfirst", Short: []string{"cfir", "crewind", "cr"}, Help: "goes to the first place of the last list of places"}, func(e *Session, _ call) {
			e.stepPlace(func(int, int) int { return 0 })
		}, nil},
		{vim.Command{Name: "clast", Short: []string{"cla"}, Help: "goes to the last place of the last list of places"}, func(e *Session, _ call) {
			e.stepPlace(func(_, n int) int { return n - 1 })
		}, nil},
		{vim.Command{Name: "cdo", Args: "{cmd}", Help: "runs cmd at each place of the last list of places"}, func(e *Session, x call) { e.cdo(x.arg, false) }, nil},
		{vim.Command{Name: "cfdo", Args: "{cmd}", Help: "runs cmd once in each file of the last list of places"}, func(e *Session, x call) { e.cdo(x.arg, true) }, nil},
		{vim.Command{Name: "cc", Args: "[n]", Help: "goes to place n of the last list of places, or the one last gone to"}, func(e *Session, x call) {
			n, err := strconv.Atoi(x.arg)
			if x.arg != "" && err != nil {
				e.message = "not a number: " + x.arg
				return
			}
			e.stepPlace(func(at, _ int) int {
				if x.arg == "" {
					return max(at, 0)
				}
				return n - 1
			})
		}, nil},
		{vim.Command{Name: "copen", Short: []string{"cope"}, Help: "shows the last list of places in a window"}, func(e *Session, _ call) { e.openList() }, nil},
		{vim.Command{Name: "cclose", Short: []string{"ccl"}, Help: "closes the window of the last list of places"}, func(e *Session, _ call) { e.closeList() }, nil},
		{vim.Command{Name: "resize", Short: []string{"res"}, Args: "[+-]{n}", Help: "makes the window n rows tall, or n taller or shorter, or as tall as it can be"}, func(e *Session, x call) {
			e.resizeTo(false, x.arg)
		}, nil},
		{vim.Command{Name: "vertical", Short: []string{"vert"}, Args: "resize [+-]{n}", Help: "makes the window n columns wide, or n wider or narrower, or as wide as it can be"}, func(e *Session, x call) {
			name, arg, _ := strings.Cut(x.arg, " ")
			if name != "resize" && name != "res" {
				e.message = "usage: :vertical resize [+-]{n}"
				return
			}
			e.resizeTo(true, strings.TrimSpace(arg))
		}, nil},
		{vim.Command{Name: "config", Args: "reload", Help: "reads config.toml again, without writing it"}, func(e *Session, x call) {
			if x.arg != "reload" {
				e.message = "config what? reload"
				return
			}
			e.reloadConfig()
		}, func(_ *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, match([]string{"reload"}, arg)
		}},
		{vim.Command{Name: "plugin", Args: "reload", Help: "reads config.toml's plugins again and loads them anew"}, func(e *Session, x call) {
			if x.arg != "reload" {
				e.message = "usage: :plugin reload"
				return
			}
			e.reloadPlugins()
		}, func(_ *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, match([]string{"reload"}, arg)
		}},
		{vim.Command{Name: "grammar", Args: "[install {filetype}|update]", Help: "lists the grammars and whether each is built, builds one again, or builds again those built at an older pin"}, func(e *Session, x call) { e.grammar(x.arg) }, func(_ *Session, _ string, start int, arg string, _ bool) (int, []string) {
			names := []string{"update"}
			for _, g := range syntax.Grammars() {
				names = append(names, "install "+g.Filetype)
			}
			return start, match(names, arg)
		}},
		{vim.Command{Name: "help", Short: []string{"h"}, Args: "[name]", Help: "opens help, at the entry for a command, key, option or setting"}, func(e *Session, x call) { e.help(x.arg) }, func(e *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, match(e.helpTags(), arg)
		}},
		{vim.Command{Name: "jumps", Short: []string{"ju"}, Help: "lists the window's jumps"}, func(e *Session, _ call) { e.jumps() }, nil},
		{vim.Command{Name: "kick", Args: "{name}", Help: "ends the session of the guest called name"}, func(e *Session, x call) { e.kick(x.arg) }, completeKick},
		{vim.Command{Name: "later", Args: "{n}", Help: "goes n states forward. n may be a time such as 5m, or a count of writes such as 1f"}, func(e *Session, x call) { e.travel(x.arg, true) }, nil},
		{vim.Command{Name: "lsp", Args: "[restart|stop|disable|enable]", Help: "opens the panel of language servers, or acts on the file's servers"}, func(e *Session, x call) {
			if x.arg == "" {
				e.lspPanel()
				return
			}
			names := e.serverNames(e.win.buf)
			if len(names) == 0 {
				e.lspCommand(x.arg, "")
				return
			}
			var said []string
			for _, name := range names {
				e.lspCommand(x.arg, name)
				said = append(said, e.message)
			}
			e.message = strings.Join(slices.Compact(said), "  ")
		}, func(_ *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, match(lspCommands, arg)
		}},
		{vim.Command{Name: "marks", Help: "lists the buffer's marks"}, func(e *Session, _ call) { e.marks() }, nil},
		{vim.Command{Name: "messages", Short: []string{"mes"}, Args: "[yank]", Help: "lists the messages shown, oldest first, or with yank puts the last on the clipboard"}, func(e *Session, x call) { e.messages(x.arg) }, func(_ *Session, _ string, start int, arg string, _ bool) (int, []string) {
			return start, match([]string{"yank"}, arg)
		}},
		{vim.Command{Name: "new", Help: "opens a window below with a new empty buffer, or above with split_below off"}, func(e *Session, _ call) {
			e.split(false)
			e.empty()
		}, nil},
		{vim.Command{Name: "only", Short: []string{"on"}, Help: "closes every other window"}, func(e *Session, _ call) { e.only() }, nil},
		{vim.Command{Name: "pwd", Help: "shows the working directory"}, func(e *Session, _ call) {
			if dir, err := os.Getwd(); err != nil {
				e.message = err.Error()
			} else {
				e.message = dir
			}
		}, nil},
		{vim.Command{Name: "quit", Short: []string{"q"}, Help: "closes the window, and quits with the last. ! drops changes"}, func(e *Session, x call) { e.quit(x.force) }, nil},
		{vim.Command{Name: "quitall", Short: []string{"qa", "qall", "quita"}, Help: "quits. ! drops every change"}, func(e *Session, x call) { e.quitAll(x.force) }, nil},
		{vim.Command{Name: "read", Short: []string{"r"}, Args: "{file} | !{cmd}", Help: "puts a file, or what cmd writes, below the line"}, func(e *Session, x call) { e.readIn(x.arg, x.last) }, completePath},
		{vim.Command{Name: "center", Help: "centers the windows on a wide screen, as wide as the colour column, or stops"}, func(e *Session, _ call) { e.center() }, nil},
		{vim.Command{Name: "restart", Help: "starts fude again on the same files, windows and unsaved changes, with config, plugins and servers loaded afresh"}, func(e *Session, _ call) { e.restart() }, nil},
		{vim.Command{Name: "recover", Help: "goes to the newest unsaved change from a session that did not close"}, func(e *Session, _ call) { e.recover() }, nil},
		{vim.Command{Name: "registers", Short: []string{"reg"}, Args: "[names]", Help: "lists the registers, or those named"}, func(e *Session, x call) { e.registers(x.arg) }, nil},
		{vim.Command{Name: "reject", Args: "[n]", Help: "drops proposal n, or the buffer's newest"}, func(e *Session, x call) {
			e.decide(x.arg, false)
		}, nil},
		{vim.Command{Name: "restore", Args: "[n]", Help: "makes the text of state n the newest. In a timeline, n is the state under the cursor"}, func(e *Session, x call) { e.restore(x.arg) }, nil},
		{vim.Command{Name: "set", Short: []string{"se"}, Args: "[name [value]]", Help: "sets an option to a value, as config.toml would, shows one, or lists them all"}, func(e *Session, x call) { e.set(x.arg) }, completeOption},
		{vim.Command{Name: "share", Help: "shows a new share string, which lets one more guest join"}, func(e *Session, _ call) { e.reshare() }, nil},
		{vim.Command{Name: "split", Short: []string{"sp"}, Args: "[file]", Help: "opens a window below, then opens file in it"}, splitAndEdit, completePath},
		{vim.Command{Name: "terminal", Short: []string{"ter", "term"}, Args: "[cmd]", Help: "runs cmd, or the shell, in a terminal window below. <C-\\><C-n> or <Esc><Esc> leaves terminal mode, and i goes back"}, func(e *Session, x call) { e.openTerminal(x.arg, nil, "") }, nil},
		{vim.Command{Name: "theme", Args: "[dark|light|{file}]", Help: "switches the theme of every client, or shows the one in use"}, func(e *Session, x call) {
			if x.arg == "" {
				e.message = e.theme
			} else if err := e.setTheme(x.arg); err != nil {
				e.message = err.Error()
			}
		}, completeTheme},
		{vim.Command{Name: "timeline", Help: "lists every state of the buffer's text"}, func(e *Session, _ call) { e.timeline() }, nil},
		{vim.Command{Name: "update", Short: []string{"up"}, Help: "writes the buffer if it is modified"}, func(e *Session, x call) {
			if e.win.buf.modified() {
				e.write(e.win.buf, x.force, nil)
			}
		}, nil},
		{vim.Command{Name: "vnew", Short: []string{"vne"}, Help: "opens a window to the right with a new empty buffer, or left with split_right off"}, func(e *Session, _ call) {
			e.split(true)
			e.empty()
		}, nil},
		{vim.Command{Name: "vsplit", Short: []string{"vs"}, Args: "[file]", Help: "opens a window to the right, then opens file in it"}, splitAndEdit, completePath},
		{vim.Command{Name: "wall", Short: []string{"wa"}, Help: "writes every modified buffer"}, func(e *Session, x call) { e.writeAll(e.bufs, x.force, nil) }, nil},
		{vim.Command{Name: "who", Help: "lists the guests"}, func(e *Session, _ call) { e.who() }, nil},
		{vim.Command{Name: "wq", Help: "writes the buffer and closes the window"}, func(e *Session, x call) {
			e.write(e.win.buf, x.force, func() { e.quit(x.force) })
		}, nil},
		{vim.Command{Name: "wqall", Short: []string{"wqa"}, Help: "writes every modified buffer and quits"}, writeAllAndQuit, nil},
		{vim.Command{Name: "write", Short: []string{"w"}, Args: "[file]", Help: "writes the buffer, or to file. ! overwrites a file changed on disk"}, func(e *Session, x call) {
			if x.arg != "" {
				e.writeTo(e.win.buf, x.arg, x.force)
				return
			}
			e.write(e.win.buf, x.force, nil)
		}, completePath},
		{vim.Command{Name: "xall", Short: []string{"xa"}, Help: "writes every modified buffer and quits"}, writeAllAndQuit, nil},
		{vim.Command{Name: "xit", Short: []string{"x", "exit"}, Help: "writes the buffer if it is modified and closes the window"}, func(e *Session, x call) {
			if e.win.buf.modified() {
				e.write(e.win.buf, x.force, func() { e.quit(x.force) })
			} else {
				e.quit(x.force)
			}
		}, nil},
	}
	plugs = []plug{
		{"listing.open", "opens the entry under the cursor", func(e *Session, _ int) { e.listingAct("open") }},
		{"listing.split", "opens the entry under the cursor in a window below", func(e *Session, _ int) { e.listingAct("split") }},
		{"listing.vsplit", "opens the entry under the cursor in a window to the right", func(e *Session, _ int) { e.listingAct("vsplit") }},
		{"listing.cwd", "makes the listing's directory the working directory", func(e *Session, _ int) { e.listingAct("cwd") }},
		{"listing.reload", "reads the listing's directory again", func(e *Session, _ int) { e.listingAct("reload") }},
		{"restart", "starts fude again on the same session", func(e *Session, _ int) { e.restart() }},
		{"jump", "jumps to a match of the characters typed next, by its label", func(e *Session, _ int) { e.jumping = &jump{} }},
		{"center", "centers the windows on a wide screen, or stops", func(e *Session, _ int) { e.center() }},
		{"listing.up", "lists the directory of the file or listing, n levels up", func(e *Session, count int) { e.up(count) }},
		{"timeline.earlier", "moves the text a state back", func(e *Session, _ int) { e.timelineAct("earlier") }},
		{"timeline.later", "moves the text a state forward", func(e *Session, _ int) { e.timelineAct("later") }},
		{"timeline.diff", "shows how the state under the cursor differs from now", func(e *Session, _ int) { e.timelineAct("diff") }},
		{"timeline.restore", "makes the state under the cursor the newest", func(e *Session, _ int) { e.timelineAct("restore") }},
		{"finder.open", "opens the selected match", func(e *Session, _ int) { e.finderAct("open") }},
		{"finder.list", "lists every match as places, the last list", func(e *Session, _ int) { e.finderAct("list") }},
		{"finder.next", "selects the next match", func(e *Session, _ int) { e.finderAct("next") }},
		{"finder.prev", "selects the match before", func(e *Session, _ int) { e.finderAct("prev") }},
		{"finder.all", "turns searching the files git ignores and hidden ones on or off", func(e *Session, _ int) { e.finderAct("all") }},
		{"float.close", "closes the float the cursor is in", func(e *Session, _ int) {
			if e.under != nil {
				e.unfocus()
			}
		}},
		{"list.open", "goes to the place under the cursor", func(e *Session, _ int) { e.openSpot() }},
		{"help.follow", "goes to the help entry of the name under the cursor", func(e *Session, _ int) { e.followHelp() }},
		{"lsp.restart", "restarts the language server under the cursor", func(e *Session, _ int) { e.lspCommand("restart", e.lspCardName()) }},
		{"lsp.stop", "stops the language server under the cursor", func(e *Session, _ int) { e.lspCommand("stop", e.lspCardName()) }},
		{"lsp.disable", "keeps the language server under the cursor off", func(e *Session, _ int) { e.lspCommand("disable", e.lspCardName()) }},
		{"lsp.enable", "lets the language server under the cursor start again", func(e *Session, _ int) { e.lspCommand("enable", e.lspCardName()) }},
		{"lsp.next", "moves n servers down", func(e *Session, count int) { e.lspMove(max(count, 1)) }},
		{"lsp.prev", "moves n servers up", func(e *Session, count int) { e.lspMove(-max(count, 1)) }},
		{"list.next", "goes to the next place of the last list", func(e *Session, n int) { e.stepPlace(func(at, _ int) int { return at + max(n, 1) }) }},
		{"list.prev", "goes to the place before in the last list", func(e *Session, n int) { e.stepPlace(func(at, _ int) int { return max(at, 0) - max(n, 1) }) }},
		{"list.first", "goes to the first place of the last list", func(e *Session, _ int) { e.stepPlace(func(int, int) int { return 0 }) }},
		{"list.last", "goes to the last place of the last list", func(e *Session, _ int) { e.stepPlace(func(_, n int) int { return n - 1 }) }},
		{"window.left", "goes to the window, or tmux pane, to the left", func(e *Session, _ int) { e.navigate('h') }},
		{"window.down", "goes to the window, or tmux pane, below", func(e *Session, _ int) { e.navigate('j') }},
		{"window.up", "goes to the window, or tmux pane, above", func(e *Session, _ int) { e.navigate('k') }},
		{"window.right", "goes to the window, or tmux pane, to the right", func(e *Session, _ int) { e.navigate('l') }},
		{"terminal.close", "closes the terminal or task window once its program has ended", func(e *Session, _ int) { e.closeTerminal() }},
		{"start.open", "runs the start screen's line under the cursor", func(e *Session, _ int) { e.startOpen() }},
		{"start.config", "opens config.toml", func(e *Session, _ int) { e.startConfig() }},
		{"start.session", "restores the last session of the working directory", func(e *Session, _ int) { e.restoreProject() }},
	}
	for n := 1; n <= 9; n++ {
		plugs = append(plugs, plug{"start." + strconv.Itoa(n), "opens the start screen's recent file " + strconv.Itoa(n), func(e *Session, _ int) { e.startRecent(n) }})
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

// splitAndEdit is :split and :vsplit.
func splitAndEdit(e *Session, x call) {
	e.split(x.name[0] == 'v')
	if x.arg != "" {
		e.edit(x.arg)
	}
}

// writeAllAndQuit is :wqall and :xall.
func writeAllAndQuit(e *Session, x call) {
	e.writeAll(e.bufs, x.force, func() { e.quitAll(x.force) })
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
		names := slices.Concat(slices.Collect(maps.Keys(opts.switches())), slices.Collect(maps.Keys(opts.numbers())), slices.Collect(maps.Keys(settings)))
		slices.Sort(names)
		return start, match(names, name)
	}
	// A switch takes true or false, theme a theme, and completion_sources
	// its sources.
	at := start + len(name) + 1
	_, isSwitch := opts.switches()[name]
	switch {
	case isSwitch || settings[name].IsValid() && settings[name].Kind() == reflect.Bool:
		return at, match([]string{"false", "true"}, value)
	case name == "theme":
		return completeTheme(e, line, at, value, wait)
	case name == "completion_sources":
		done := value[:strings.LastIndexByte(value, ',')+1]
		return at + len(done), match(defaults.Sources, value[len(done):])
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
	return start, match(names, arg)
}

// completeBuffer offers the paths of the buffers that have files.
func completeBuffer(e *Session, _ string, start int, arg string, _ bool) (int, []string) {
	var names []string
	for _, b := range e.bufs {
		if b.file.path != "" {
			names = append(names, b.file.path)
		}
	}
	return start, match(names, arg)
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
			if r.Kind == api.Key || r.Kind == api.Plug {
				names = append(names, p.Name()+"."+r.Name)
			}
		}
	}
	return names
}

// runPlug runs <Plug>(name) once the command line closes, as :plug does
// (B271).
func (e *Session) runPlug(name string) {
	if !slices.Contains(e.plugNames(), name) {
		e.message = "no plug is called " + name
		return
	}
	e.later = append(e.later, vim.Keys("<Plug>("+name+")")...)
}

// defaultMaps are the normal mode keys fude maps to its commands.
var defaultMaps = map[string]string{
	"-": "<Plug>(listing.up)", "ZR": "<Plug>(restart)",
	"]q": "<Plug>(list.next)", "[q": "<Plug>(list.prev)", "[Q": "<Plug>(list.first)", "]Q": "<Plug>(list.last)",
	"s": "<Plug>(jump)",
}

// hostOperator is an operator fude adds, and what it does.
type hostOperator struct {
	keys, help string
	run        func(e *Session, t vim.Text, start, end int, lines bool) []vim.Edit
}

// hostOperators are the operators fude adds.
var hostOperators = []hostOperator{}
