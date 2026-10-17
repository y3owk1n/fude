package editor

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Power is how much a session may do, fixed when it is made (B243).
type Power int

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
	case "!", "terminal", "task", "restart", "cd", "theme", "share", "kick":
		return true
	case "read":
		return strings.HasPrefix(arg, "!")
	case "pick":
		source, _, _ := strings.Cut(arg, " ")
		return source == "tasks"
	case "grammar", "lsp", "plugin":
		return arg != ""
	}
	return false
}

// refusedPlugs are the <Plug> commands a guest may not run, as refuses
// says of ex commands (B244).
var refusedPlugs = map[string]bool{
	"restart": true, "listing.cwd": true,
	"lsp.restart": true, "lsp.stop": true, "lsp.disable": true, "lsp.enable": true,
}

// refusedOperators are the operators a guest may not run, because they
// stage or reset hunks in git's index (B244).
var refusedOperators = map[string]bool{"gh": true, "gH": true}

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

// mayShow returns b if e may show it. Otherwise it returns the first
// buffer e may show, or a new empty one (B245).
func (e *Session) mayShow(b *buffer) *buffer {
	ok := func(b *buffer) bool { return e.outside(cmp.Or(b.file.path, b.listed)) == nil }
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
