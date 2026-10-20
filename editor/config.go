package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/tomlerr"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// taskConfig is a task config.toml names (B228).
type taskConfig struct {
	Command string `toml:"command" help:"the shell command the task runs. {file}, {dir}, {line} and {word} in it stand for the current buffer's file, its folder, the cursor's line and the word under it"`
}

// config is what config.toml holds (D27). Each key's help tag is what
// help says of it.
type config struct {
	IndentWidth    uint     `toml:"indent_width" help:"the columns of an indent level, 0 for the tab width"`
	TabWidth       uint     `toml:"tab_width" help:"the columns a tab shows as"`
	KeyTimeout     uint     `toml:"key_timeout" help:"the milliseconds a mapping waits for its next key"`
	LargeFile      uint     `toml:"large_file" help:"the megabytes over which a file opens with syntax and language servers off"`
	Center         bool     `toml:"center" help:"the windows start centered on a wide screen, as :center toggles"`
	TerminalHeight uint     `toml:"terminal_height" help:"the percent of the screen's rows a terminal or task window takes when it opens"`
	SplitBelow     bool     `toml:"split_below" help:"a split opens below the window, else above it"`
	SplitRight     bool     `toml:"split_right" help:"a vertical split opens right of the window, else left of it"`
	Autocomplete   bool     `toml:"autocomplete" help:"completions show while typing, not only on <C-x><C-o>"`
	RemoteImages   bool     `toml:"remote_images" help:"images at http and https addresses load from the network"`
	Sources        []string `toml:"completion_sources" help:"where completions come from, in the menu's order: lsp, snippet, path and buffer"`
	AutoInstall    bool     `toml:"auto_install" help:"builds a missing grammar the first time a file needs it"`
	Socket         string   `toml:"socket" help:"where clients attach, instead of a path for each process"`
	FormatOnSave   bool     `toml:"format_on_save" help:"a write formats the buffer first"`
	FormatTimeout  uint     `toml:"format_timeout" help:"the milliseconds formatting may take"`
	Leader         string   `toml:"leader" help:"what <Leader> in a mapping stands for"` // N18
	Theme          string   `toml:"theme" help:"the colours: dark, light or a base16 scheme file"`
	options
	Colors   wire.Colors `toml:"colors" help:"colours by role, each as #rrggbb, that replace the theme's"`
	Keymap   keymap      `toml:"keymap"`
	Filetype map[string]struct {
		Files        []string `toml:"files" help:"the names of the files of the type, as patterns such as *.toml.tmpl"`
		IndentWidth  uint     `toml:"indent_width" help:"the columns of an indent level in files of the type"`
		TabWidth     uint     `toml:"tab_width" help:"the columns a tab shows as in files of the type"`
		LSP          []string `toml:"lsp" help:"the language servers of files of the type, by the names of their [lsp.{name}] tables"`
		Format       []string `toml:"format" help:"the formatters of files of the type, by the names of their [formatter.{name}] tables, run in turn"`
		FormatFirst  bool     `toml:"format_first" help:"only the first formatter that is installed runs"`
		FormatOnSave *bool    `toml:"format_on_save" help:"a write formats files of the type first"`
		ExpandTab    *bool    `toml:"expand_tab" help:"files of the type indent with spaces only"`
		Wrap         *bool    `toml:"wrap" help:"long lines in files of the type go on the rows below"`
		ColorColumn  *int     `toml:"color_column" help:"the column shaded in files of the type, 0 for none"`
		Spell        *bool    `toml:"spell" help:"spell checks files of the type, as spell.enabled"`
		Debug        []string `toml:"debug" help:"the debug adapters of files of the type, by the names of their [debug.{name}] tables"`
	} `toml:"filetype" key:"type"`
	Debug map[string]struct {
		Command []string `toml:"command" help:"the debug adapter's command, with {port} where it listens if it does not talk on stdio"`
	} `toml:"debug" key:"name"`
	LSP map[string]struct {
		Command     []string       `toml:"command" help:"the server's command, its name when unset"`
		Root        []string       `toml:"root" help:"the files that mark a project's root, most wanted first, looked for from the file's folder up"`
		Settings    map[string]any `toml:"settings" help:"the server's settings, which it reads by section"`
		InitOptions map[string]any `toml:"init_options" help:"what the server gets as its initialization options"`
		Files       []string       `toml:"files" help:"the files the server is for, as patterns of names or, with a slash, of the ends of paths, such as .github/workflows/*"`
		Workspace   bool           `toml:"workspace_required" help:"the server starts only for a file under one of its root files"`
	} `toml:"lsp" key:"name"`
	Formatter map[string]struct {
		Command   []string `toml:"command" help:"the formatter's command, its name when unset, which reads the text on stdin and writes it on stdout. {path} in it is the file's path"`
		Root      []string `toml:"root" help:"the files that mark the folder it runs in, most wanted first, looked for from the file's folder up"`
		Workspace bool     `toml:"workspace_required" help:"the formatter runs only for a file under one of its root files"`
	} `toml:"formatter" key:"name"`
	Task   map[string]taskConfig     `toml:"task" key:"name"`
	Open   map[string]string         `toml:"open" key:"module" help:"where the module's windows open: above, below, left, right, here, float or cursor"`
	Module map[string]map[string]any `toml:"module" key:"name" help:"the starting values of a module's options by their names, as enabled = false in [module.autopairs] for autopairs.enabled"`
	Plugin []struct {
		Path         string         `toml:"path" help:"the plugin's file, from the config directory"`
		Capabilities []string       `toml:"capabilities" help:"what the plugin may do"`
		Programs     []string       `toml:"programs" help:"the programs process may run"`
		Hosts        []string       `toml:"hosts" help:"the hosts net may reach"`
		Options      map[string]any `toml:"options" help:"where the plugin's options start, by name"`
		Disabled     bool           `toml:"disabled" help:"keeps the plugin from loading, with its table kept"`
	} `toml:"plugin"`
}

// keymap is config.toml's [keymap] tables by name: normal, visual and
// insert mode's mappings, a kind of buffer's, fude's or a module's such as
// pins, and groups, the names key hints show (B142, B172).
type keymap map[string]map[string]string

// keymapTables are the [keymap] tables help explains, in order.
var keymapTables = []struct{ name, help string }{
	{"normal", "normal mode mappings"},
	{"visual", "visual mode mappings"},
	{"insert", "insert mode mappings"},
	{"explorer", "an explorer's keys, over the normal mode ones"},
	{"list", "a list of places' keys, over the normal mode ones"},
	{"timeline", "a timeline's keys, over the normal mode ones"},
	{"finder", "the finder's keys, in insert mode"},
	{"float", "a float's keys while the cursor is in it"},
	{"lsp", "the language server panel's keys"},
	{"debug", "the debug panel's keys"},
	{"help", "help's keys"},
	{"start", "the start screen's keys"},
	{"terminal", "a terminal window's keys in normal mode"},
	{"scratch", "the keys of a window fude writes to show something, such as :messages or a diff"},
	{"<kind>", "the keys of a kind of buffer a module adds, such as pins, over the normal mode ones"},
	{"groups", "names of the keys mappings start with, which key hints show"},
}

var defaults = config{TabWidth: 8, KeyTimeout: 1000, FormatTimeout: 1000, LargeFile: 2, TerminalHeight: 33, SplitBelow: true, SplitRight: true, RemoteImages: true, Sources: []string{"lsp", "snippet", "path", "buffer"}, AutoInstall: true, Leader: "<Space>", Theme: "dark", options: options{Wrap: true, IncSearch: true}}

// options are the settings each window has, which :set changes for the
// current window and the windows opened after it (B39). :set takes each by
// its toml name, a bool as a switch and an int as a number.
type options struct {
	Number         bool `toml:"number" help:"shows line numbers"`
	RelativeNumber bool `toml:"relative_number" help:"numbers lines from the cursor's"`
	Wrap           bool `toml:"wrap" help:"long lines go on the rows below, else the window scrolls sideways"`
	ScrollOff      int  `toml:"scroll_off" help:"the lines kept in view above and below the cursor's"`
	CursorLine     bool `toml:"cursor_line" help:"shades the cursor's line"`
	List           bool `toml:"list" help:"shows tabs, trailing spaces and no-break spaces"`
	IgnoreCase     bool `toml:"ignore_case" help:"searches ignore case"`
	SmartCase      bool `toml:"smart_case" help:"a pattern with an uppercase letter matches case"`
	ExpandTab      bool `toml:"expand_tab" help:"indents with spaces only"`
	Clipboard      bool `toml:"clipboard" help:"yanks, deletes and puts use the system clipboard"`
	SignColumn     bool `toml:"sign_column" help:"keeps the sign column when there are no signs"`
	ColorColumn    int  `toml:"color_column" help:"shades this column of the text, and is how wide :center makes it, 0 for none"`
	Icons          bool `toml:"icons" help:"shows nerd font icons in explorers and signs"`
	IncSearch      bool `toml:"inc_search" help:"while a search is typed, the window shows the match it goes to, lit apart from the rest"`
	// Modules are the values :set gave modules' window options here, by
	// name. A change replaces the map, so a copy of options keeps its own.
	Modules map[string]string `toml:"-"`
}

// loadConfig reads config.toml. A missing file is no error. After an error
// it returns what it read before the error and the defaults for the rest.
func loadConfig() (config, error) {
	c := defaults
	dir, err := keep.Dir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return c, fmt.Errorf("find config: %w", err)
	}
	path := filepath.Join(dir, "fude", "config.toml")
	at := time.Now()
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	trace.Since("file", fmt.Sprintf("read %q, %d bytes", path, len(src)), at, err)
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	d := toml.NewDecoder(bytes.NewReader(src))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, tomlerr.Where(path, err)
	}
	if err := aliasFiletypes(&c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if err := badPattern(c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	for _, s := range c.Sources {
		if !slices.Contains(defaults.Sources, s) {
			return c, fmt.Errorf("%s: completion_sources has no source %q", path, s)
		}
	}
	for module, where := range c.Open {
		if !slices.Contains(placements, where) {
			return c, fmt.Errorf("%s: open.%s has no placement %q", path, module, where)
		}
	}
	return c, nil
}

// Clean makes fude read no config, as --clean asks, until the caller runs
// remove (B270).
func Clean() (remove func(), err error) {
	dir, err := os.MkdirTemp("", "fude-clean-")
	if err != nil {
		return nil, fmt.Errorf("clean: %w", err)
	}
	keep.Clean = dir
	return func() {
		keep.Clean = ""
		os.RemoveAll(dir)
	}, nil
}

// ConfigDir returns fude's config directory, the empty one with --clean
// (B270).
func ConfigDir() (string, error) {
	dir, err := keep.Dir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", fmt.Errorf("find the config directory: %w", err)
	}
	return filepath.Join(dir, "fude"), nil
}

// mapper is what keymaps go into: a machine, or a kind of buffer's keymaps.
type mapper interface {
	Map(mode vim.Mode, from, to string)
	Unmap(mode vim.Mode, from string)
}

// mapAll applies a config keymap table in mode, with <Leader> in it as
// leader. Keys mapped to "" lose their mapping, a default one too, and
// <Nop> makes keys do nothing (B142).
func mapAll(m mapper, mode vim.Mode, maps map[string]string, leader string) {
	for from, to := range maps {
		from, to = withLeader(from, leader), withLeader(to, leader)
		if to == "" {
			m.Unmap(mode, from)
		} else {
			m.Map(mode, from, to)
		}
	}
}

// leaderKey is <Leader> in Vim notation, in any case.
var leaderKey = regexp.MustCompile(`(?i)<leader>`)

// withLeader puts leader in place of <Leader> in keys, when a mapping is
// made (B144).
func withLeader(keys, leader string) string {
	return leaderKey.ReplaceAllLiteralString(keys, leader)
}

// kindKeys are fude's own keys of each kind of buffer, which
// [keymap.<kind>] changes (B142).
func kindKeys() map[string]map[string]string {
	return map[string]map[string]string{"explorer": nil, "list": nil, "timeline": nil, "finder": finderKeys, "float": floatKeys, "lsp": nil, "help": nil, "scratch": scratchKeys, "start": nil, "terminal": terminalKeys, "debug": nil}
}

// table returns the keymap that the [keymap] table name changes, a mode's
// or a kind of buffer's, and the mode its keys map in. The finder's keys
// map in insert mode (B142).
func (e *Session) table(name string) (mapper, vim.Mode) {
	switch name {
	case "normal":
		return e.vim, vim.Normal
	case "visual":
		return e.vim, vim.Visual
	case "insert":
		return e.vim, vim.Insert
	}
	if e.locals[name] == nil {
		e.locals[name] = &vim.Keymaps{}
	}
	if name == "finder" {
		return e.locals[name], vim.Insert
	}
	return e.locals[name], vim.Normal
}

// configures reports whether the session's [keymap] table maps keys,
// which then wins over a module's key (B327).
func (e *Session) configures(table, keys string) bool {
	want := vim.Keys(withLeader(keys, e.leader))
	for from := range e.keymap[table] {
		if slices.Equal(vim.Keys(withLeader(from, e.leader)), want) {
			return true
		}
	}
	return false
}

// reloadConfig reads config.toml again, and never writes it. A setting the
// file changed applies as :set would. An option it changed applies to every
// window, and an option it did not change keeps what :set gave it. A key it
// no longer maps does what fude does by default. Its tables replace the old
// ones, and its plugins load again. A file with an error changes nothing
// (B39).
func (e *Session) reloadConfig() error {
	cfg, err := loadConfig()
	if err == nil {
		err = e.moduleConfig(cfg)
	}
	if err != nil {
		return err
	}
	old := e.cfg
	e.redeclare(cfg)
	settings := cfg.settings()
	for name, v := range old.settings() {
		if now := settingText(settings[name]); now != settingText(v) {
			e.report(e.setSetting(name, now, true))
		}
	}
	// The options the file changed apply to every window.
	was, now := old.options, cfg.options
	changed := map[string]bool{}
	for name, p := range now.switches() {
		changed[name] = *p != *was.switches()[name]
	}
	for name, p := range now.numbers() {
		changed[name] = *p != *was.numbers()[name]
	}
	for _, s := range e.sessions {
		if s.power != Host {
			continue
		}
		windows := s.root.windows()
		for _, w := range windows {
			apply(&w.opts, now, changed)
			if w.own != nil {
				apply(w.own, now, changed)
			}
			w.fitted = nil
		}
		apply(&s.opts, now, changed)
		s.rekey(old.Keymap, cfg.Keymap)
	}
	e.cfg.options, e.cfg.Keymap = cfg.options, cfg.Keymap
	e.cfg.Filetype, e.cfg.LSP, e.cfg.Formatter, e.cfg.Debug, e.cfg.Task, e.cfg.Module = cfg.Filetype, cfg.LSP, cfg.Formatter, cfg.Debug, cfg.Task, cfg.Module
	if e.started {
		e.report(e.reloadPlugins())
	}
	e.layout()
	e.emit("config.reloaded", "", e.win.buf)
	e.message = "reloaded config.toml"
	return nil
}

// apply copies to o each option of from that changed names.
func apply(o *options, from options, changed map[string]bool) {
	for name, p := range from.switches() {
		if changed[name] {
			*o.switches()[name] = *p
		}
	}
	for name, p := range from.numbers() {
		if changed[name] {
			*o.numbers()[name] = *p
		}
	}
}

// rekey replaces the keys of keymap was with those of now. A key that was
// maps and now does not gets fude's or a module's own mapping back, or
// none.
func (e *Session) rekey(was, now keymap) {
	own := e.ownKeys()
	e.keymap = now
	names := map[string]bool{}
	for name := range was {
		names[name] = true
	}
	for name := range now {
		names[name] = true
	}
	for name := range names {
		if name == "groups" {
			continue
		}
		m, mode := e.table(name)
		for keys := range was[name] {
			if _, ok := now[name][keys]; ok {
				continue
			}
			if to, ok := own[name][keys]; ok {
				m.Map(mode, withLeader(keys, e.leader), to)
			} else {
				m.Unmap(mode, withLeader(keys, e.leader))
			}
		}
		mapAll(m, mode, now[name], e.leader)
	}
}

// ownKeys are fude's and the modules' keys of each [keymap] table, those
// that map in the session now (B142, B327).
func (e *Session) ownKeys() keymap {
	own := keymap{}
	add := func(table string, keys map[string]string) {
		if own[table] == nil {
			own[table] = map[string]string{}
		}
		maps.Copy(own[table], keys)
	}
	for kind, keys := range kindKeys() {
		add(kind, keys)
	}
	add("normal", defaultMaps)
	add("visual", map[string]string{"s": "<Plug>(jump.start)"})
	for _, k := range e.moduleKeys() {
		if !k.attached || e.lspKeys {
			add(k.table, map[string]string{k.keys: k.plug})
		}
	}
	return own
}
