package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/y3owk1n/fude/vim"
)

// config is what config.toml holds (D27). Each key's help tag is what
// help says of it.
type config struct {
	IndentWidth     uint     `toml:"indent_width" help:"the columns of an indent level, 0 for the tab width"`
	KeyTimeout      uint     `toml:"key_timeout" help:"the milliseconds a mapping waits for its next key"`
	KeyHints        uint     `toml:"key_hints" help:"the milliseconds a command being typed waits before the keys that may follow it show, 0 for never"`
	LargeFile       uint     `toml:"large_file" help:"the megabytes over which a file opens with syntax and language servers off"`
	Center          bool     `toml:"center" help:"the windows start centered on a wide screen, as :center toggles"`
	Autocomplete    bool     `toml:"autocomplete" help:"completions show while typing, not only on <C-x><C-o>"`
	HighlightColors bool     `toml:"highlight_colors" help:"text that names a colour, such as #ff8800 or rgb(255, 136, 0), shows on that colour"`
	InlayHints      bool     `toml:"inlay_hints" help:"language servers' inlay hints, such as parameter names and types, show in the text"`
	StickyContext   bool     `toml:"sticky_context" help:"the first lines of the functions and classes the top of a window is in stay pinned over it"`
	Autopairs       bool     `toml:"autopairs" help:"an opening bracket or quote typed comes with its closing one"`
	Sources         []string `toml:"completion_sources" help:"where completions come from, in the menu's order: lsp, snippet, path and buffer"`
	AutoInstall     bool     `toml:"auto_install" help:"builds a missing grammar the first time a file needs it"`
	Socket          string   `toml:"socket" help:"where clients attach, instead of a path for each process"`
	FormatOnSave    bool     `toml:"format_on_save" help:"a write formats the buffer first"`
	FormatTimeout   uint     `toml:"format_timeout" help:"the milliseconds formatting before a write may take"`
	Leader          string   `toml:"leader" help:"what <Leader> in a mapping stands for"` // N18
	Theme           string   `toml:"theme" help:"the colours: dark, light or a base16 scheme file"`
	options
	Keymap struct {
		Normal   map[string]string `toml:"normal" help:"normal mode mappings"`
		Visual   map[string]string `toml:"visual" help:"visual mode mappings"`
		Insert   map[string]string `toml:"insert" help:"insert mode mappings"`
		Listing  map[string]string `toml:"listing" help:"a listing's keys, over the normal mode ones"`
		List     map[string]string `toml:"list" help:"a list of places' keys, over the normal mode ones"`
		Timeline map[string]string `toml:"timeline" help:"a timeline's keys, over the normal mode ones"`
		Finder   map[string]string `toml:"finder" help:"the finder's keys, in insert mode"`
		Float    map[string]string `toml:"float" help:"a float's keys while the cursor is in it"`
		Lsp      map[string]string `toml:"lsp" help:"the language server panel's keys"`
		Help     map[string]string `toml:"help" help:"help's keys"`
		Start    map[string]string `toml:"start" help:"the start screen's keys"`
		Scratch  map[string]string `toml:"scratch" help:"the keys of a window fude writes to show something, such as :messages or a diff"`
		Groups   map[string]string `toml:"groups" help:"names of the keys mappings start with, which key hints show"`
	} `toml:"keymap"`
	Filetype map[string]struct {
		Files       []string `toml:"files" help:"the names of the files of the type, as patterns such as *.toml.tmpl"`
		IndentWidth uint     `toml:"indent_width" help:"the columns of an indent level in files of the type"`
		LSP         []string `toml:"lsp" help:"the language servers of files of the type, by the names of their [lsp.{name}] tables"`
		Format      []string `toml:"format" help:"the formatters of files of the type, by the names of their [formatter.{name}] tables, run in turn"`
		FormatFirst bool     `toml:"format_first" help:"only the first formatter that is installed runs"`
	} `toml:"filetype" key:"type"`
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
	Plugin []struct {
		Path         string         `toml:"path" help:"the plugin's file, from the config directory"`
		Capabilities []string       `toml:"capabilities" help:"what the plugin may do"`
		Programs     []string       `toml:"programs" help:"the programs process may run"`
		Hosts        []string       `toml:"hosts" help:"the hosts net may reach"`
		Settings     map[string]any `toml:"settings" help:"what the plugin reads with its setting function"`
	} `toml:"plugin"`
}

var defaults = config{IndentWidth: 8, KeyTimeout: 1000, KeyHints: 300, FormatTimeout: 1000, LargeFile: 2, HighlightColors: true, InlayHints: true, StickyContext: true, Autopairs: true, Sources: []string{"lsp", "snippet", "path", "buffer"}, AutoInstall: true, Leader: "<Space>", Theme: "dark", options: options{Wrap: true, IndentGuides: true}}

// options are the settings each window has, which :set changes for the
// current window and the windows opened after it (B39). :set takes each by
// its toml name, a bool as a switch and an int as a number.
type options struct {
	Number         bool `toml:"number" help:"shows line numbers"`
	RelativeNumber bool `toml:"relative_number" help:"numbers lines from the cursor's"`
	Wrap           bool `toml:"wrap" help:"long lines go on the rows below, else the view scrolls sideways"`
	ScrollOff      int  `toml:"scroll_off" help:"the lines kept in view above and below the cursor's"`
	CursorLine     bool `toml:"cursor_line" help:"shades the cursor's line"`
	List           bool `toml:"list" help:"shows tabs, trailing spaces and no-break spaces"`
	IgnoreCase     bool `toml:"ignore_case" help:"searches ignore case"`
	SmartCase      bool `toml:"smart_case" help:"a pattern with an uppercase letter matches case"`
	ExpandTab      bool `toml:"expand_tab" help:"indents with spaces only"`
	Clipboard      bool `toml:"clipboard" help:"yanks, deletes and puts use the system clipboard"`
	SignColumn     bool `toml:"sign_column" help:"keeps the sign column when there are no signs"`
	IndentGuides   bool `toml:"indent_guides" help:"draws a faint line at each indent level"`
	ColorColumn    int  `toml:"color_column" help:"shades this column of the text, and is how wide :center makes it, 0 for none"`
	Icons          bool `toml:"icons" help:"shows nerd font icons in listings and signs"`
}

// loadConfig reads config.toml. A missing file is no error. After an error
// it returns what it read before the error and the defaults for the rest.
func loadConfig() (config, error) {
	c := defaults
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return c, fmt.Errorf("find config: %w", err)
	}
	path := filepath.Join(dir, "fude", "config.toml")
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	d := toml.NewDecoder(bytes.NewReader(src))
	d.DisallowUnknownFields()
	err = d.Decode(&c)
	var strict *toml.StrictMissingError
	var de *toml.DecodeError
	var msg string
	switch {
	case err == nil:
		if err := badPattern(c); err != nil {
			return c, fmt.Errorf("%s: %w", path, err)
		}
		for _, s := range c.Sources {
			if !slices.Contains(defaults.Sources, s) {
				return c, fmt.Errorf("%s: completion_sources has no source %q", path, s)
			}
		}
		return c, nil
	case errors.As(err, &strict):
		de = &strict.Errors[0]
		msg = "unknown key " + strings.Join(de.Key(), ".")
	case errors.As(err, &de) && len(de.Key()) > 0:
		msg = "bad value for " + strings.Join(de.Key(), ".")
	case errors.As(err, &de):
		msg = strings.TrimPrefix(de.Error(), "toml: ")
	default:
		return c, fmt.Errorf("%s: %w", path, err)
	}
	line, _ := de.Position()
	return c, fmt.Errorf("%s line %d: %s", path, line, msg)
}

// xdgDir returns the directory the XDG variable env names, or else fallback
// in the home directory.
func xdgDir(env, fallback string) (string, error) {
	if dir := os.Getenv(env); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
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
