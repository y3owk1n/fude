package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/y3owk1n/fude/vim"
)

// config is what config.toml holds (D27).
type config struct {
	IndentWidth uint `toml:"indent_width"` // 0 means the tab width
	KeyTimeout  uint `toml:"key_timeout"`  // milliseconds
	LargeFile   uint `toml:"large_file"`   // megabytes
	// Ask the language server for completions while typing, not only on
	// <C-x><C-o>.
	Autocomplete bool   `toml:"autocomplete"`
	Socket       string `toml:"socket"` // where clients attach, instead of a path per process
	FormatOnSave bool   `toml:"format_on_save"`
	Leader       string `toml:"leader"` // what <Leader> in a mapping stands for (N18)
	Theme        string `toml:"theme"`  // the client's colours, dark or light
	options
	Keymap struct {
		Normal map[string]string `toml:"normal"`
		Visual map[string]string `toml:"visual"`
		Insert map[string]string `toml:"insert"`
		// A kind of buffer's normal mode keys, over [keymap.normal].
		Listing  map[string]string `toml:"listing"`
		List     map[string]string `toml:"list"`
		Timeline map[string]string `toml:"timeline"`
		Finder   map[string]string `toml:"finder"` // in insert mode
		Float    map[string]string `toml:"float"`  // a float the cursor is in
		Lsp      map[string]string `toml:"lsp"`    // the panel of language servers
	} `toml:"keymap"`
	Filetype map[string]struct {
		IndentWidth uint       `toml:"indent_width"`
		LSP         []string   `toml:"lsp"`    // the language server's command
		Format      [][]string `toml:"format"` // formatters' commands, run in turn
	} `toml:"filetype"`
	Plugin []struct {
		Path         string   `toml:"path"` // relative to the config directory
		Capabilities []string `toml:"capabilities"`
		Programs     []string `toml:"programs"` // what process may run
		Hosts        []string `toml:"hosts"`    // what net may reach
	} `toml:"plugin"`
}

var defaults = config{IndentWidth: 8, KeyTimeout: 1000, LargeFile: 50, Leader: "<Space>", Theme: "dark", options: options{Wrap: true, IndentGuides: true}}

// options are the settings each window has, which :set changes for the
// current window and the windows opened after it (B39).
type options struct {
	Number         bool `toml:"number"`          // show line numbers
	RelativeNumber bool `toml:"relative_number"` // number lines from the cursor's
	Wrap           bool `toml:"wrap"`            // long lines go on the rows below, else the view scrolls sideways
	ScrollOff      int  `toml:"scroll_off"`      // lines kept in view above and below the cursor's
	CursorLine     bool `toml:"cursor_line"`     // shade the cursor's line
	List           bool `toml:"list"`            // show tabs, trailing spaces and no-break spaces
	IgnoreCase     bool `toml:"ignore_case"`     // searches ignore case
	SmartCase      bool `toml:"smart_case"`      // a pattern with an uppercase letter matches case
	ExpandTab      bool `toml:"expand_tab"`      // indent with spaces only
	Clipboard      bool `toml:"clipboard"`       // yanks, deletes and puts use the system clipboard
	SignColumn     bool `toml:"sign_column"`     // keep the sign column when there are no signs
	IndentGuides   bool `toml:"indent_guides"`   // a faint line at each indent level
	Icons          bool `toml:"icons"`           // nerd font icons in listings and signs
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

// withLeader puts leader in place of <Leader> in keys, as Neovim does when a
// mapping is made (B144).
func withLeader(keys, leader string) string {
	return leaderKey.ReplaceAllLiteralString(keys, leader)
}
