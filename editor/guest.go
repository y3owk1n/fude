package editor

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Power is how much a session may do, fixed when it is made (B243).
type Power int

// powers name each Power, as a call's About has it (B292).
var powers = map[Power]string{Host: "host", Guest: "guest", ReadOnly: "read-only"}

const (
	Host     Power = iota // all a session may do
	Guest                 // limited as B244 to B246 say
	ReadOnly              // a guest that changes no text either (B247)
)

// mayEdit returns an error when e may not change text (B247).
func (e *Session) mayEdit() error {
	if e.power == ReadOnly {
		return errors.New("cannot make changes, the session is read-only")
	}
	return nil
}

// refuses says whether a guest may not run the ex command name with arg,
// because it runs a program or changes what every session shares (B244).
func refuses(name, arg string) bool {
	switch name {
	case "terminal", "restart", "cd", "theme", "share", "kick", "config":
		return true
	case "read":
		return strings.HasPrefix(arg, "!")
	case "grammar", "plugin":
		return arg != ""
	}
	return false
}

// refusedPlugs are the <Plug> commands a guest may not run, as refuses
// says of ex commands (B244).
var refusedPlugs = map[string]bool{
	"restart": true,
}

// inside says whether path, once symlinks resolve, is under project and
// not in a .git, whose config can name programs git runs. A path that does
// not exist yet resolves through its nearest existing directory (B245).
func inside(project, path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			rel, ok := under(project, filepath.Join(real, rest))
			return ok && !slices.Contains(strings.Split(rel, string(filepath.Separator)), ".git")
		}
		up := filepath.Dir(abs)
		if up == abs {
			return false
		}
		rest, abs = filepath.Join(filepath.Base(abs), rest), up
	}
}

// outside returns an error when e is a guest and path is outside the
// project (B245).
func (e *Session) outside(path string) error {
	if e.power == Host || path == "" || inside(e.project, path) {
		return nil
	}
	return fmt.Errorf("%s is outside the project", path)
}

// barred returns why e may not show b. A guest shows no terminal, whose
// keys would go to a program on the host (B244), and no file outside the
// project (B245).
func (e *Session) barred(b *buffer) error {
	if e.power != Host && b.term != nil {
		return errors.New("a guest may not use a terminal")
	}
	return e.outside(cmp.Or(b.file.path, b.listed))
}

// mayShow returns b if e may show it. Otherwise it returns the first
// buffer e may show, or a new empty one (B245).
func (e *Session) mayShow(b *buffer) *buffer {
	ok := func(b *buffer) bool { return e.barred(b) == nil }
	if ok(b) {
		return b
	}
	if i := slices.IndexFunc(e.bufs, ok); i >= 0 {
		return e.bufs[i]
	}
	if _, err := e.add(""); err != nil {
		e.message = err.Error()
	}
	return e.bufs[len(e.bufs)-1]
}

// Own is what a guest's session takes from config.toml on the guest's
// machine. Its client keeps the theme and draws in it (B254).
type Own struct {
	Leader  string
	Keymap  keymap
	Options options
	Theme   string       `json:"-"`
	Base16  *[16]int32   `json:"-"`
	Colors  *wire.Colors `json:"-"`
}

// OwnConfig reads what of config.toml on this machine a guest takes. After
// an error it returns what it read and the defaults for the rest.
func OwnConfig() (Own, error) {
	cfg, err := loadConfig()
	own := Own{Leader: cfg.Leader, Keymap: cfg.Keymap, Options: cfg.options, Theme: cfg.Theme}
	if cfg.Colors != (wire.Colors{}) {
		own.Colors = &cfg.Colors
	}
	if cfg.Theme != "dark" && cfg.Theme != "light" {
		colours, berr := readBase16(cfg.Theme)
		if berr != nil {
			own.Theme = "dark"
			return own, cmp.Or(err, berr)
		}
		own.Base16 = &colours
	}
	return own, err
}

// use gives e the keys and window options of a guest's own config, and
// moves the plugins' keys to its leader (B254).
func (e *Session) use(own Own) {
	e.useKeys(own.Keymap, own.Leader)
	if own.Leader != "" {
		e.moveModuleKeys(e.leader, own.Leader)
		e.leader = own.Leader
	}
	e.opts = own.Options
	for _, w := range e.root.windows() {
		w.opts = own.Options
	}
}

// moveModuleKeys maps the modules' keys that hold <Leader> with the leader
// to, in place of the leader from. An attached key moves only while it is mapped
// (B254, B292).
func (e *Session) moveModuleKeys(from, to string) {
	for _, p := range e.modules {
		for _, r := range p.Registered() {
			if r.Kind != api.Key || r.Attached && !e.lspKeys {
				continue
			}
			for _, mode := range keyModes(r) {
				e.vim.Unmap(mode, withLeader(r.Name, from))
				e.vim.Map(mode, withLeader(r.Name, to), "<Plug>("+p.Name()+"."+r.Name+")")
			}
		}
	}
}

// moveLeader makes to the leader of every session whose leader is from.
// The host's keys from config.toml, fude's own and the modules' then map
// with to in place of from. A guest with a leader of its own keeps it (B39, B254).
func (ed *Editor) moveLeader(from, to string) {
	for _, s := range ed.sessions {
		if s.leader != from {
			continue
		}
		if s.power == Host {
			move := func(m mapper, mode vim.Mode, maps map[string]string) {
				moved := map[string]string{}
				for keys, then := range maps {
					if strings.Contains(keys+then, "<Leader>") {
						m.Unmap(mode, withLeader(keys, from))
						moved[keys] = then
					}
				}
				mapAll(m, mode, moved, to)
			}
			move(s.vim, vim.Normal, defaultMaps)
			km := ed.cfg.Keymap
			for mode, maps := range map[vim.Mode]map[string]string{vim.Normal: km.Normal, vim.Visual: km.Visual, vim.Insert: km.Insert} {
				move(s.vim, mode, maps)
			}
			for kind, maps := range map[string]map[string]string{"listing": km.Listing, "list": km.List, "timeline": km.Timeline, "float": km.Float, "lsp": km.Lsp, "help": km.Help, "scratch": km.Scratch, "start": km.Start, "terminal": km.Terminal, "debug": km.Debug} {
				move(s.locals[kind], vim.Normal, maps)
			}
			move(s.locals["finder"], vim.Insert, km.Finder)
		}
		s.moveModuleKeys(from, to)
		s.leader = to
	}
}
