package syntax

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

// language calls the function a grammar's library exports, which returns
// its language.
static const void *language(void *f) { return ((const void *(*)(void))f)(); }
*/
import "C"

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unsafe"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// grammar is a grammar fude builds when a file needs it (D29): the repo it
// comes from, the commit fude pins, the directory of its sources in the
// repo, its highlight queries in the order they combine, and its language's
// comment syntax. A query named <filetype>:<path> is in that grammar's repo.
// Its library exports tree_sitter_<filetype>.
type grammar struct {
	repo, commit, src string
	highlights        []string
	comment           string
}

// registry are the grammars fude builds, by filetype.
var registry = map[string]grammar{
	"c":          {"https://github.com/tree-sitter/tree-sitter-c", "b780e47fc780ddc8da13afa35a3f4ed5c157823d", ".", []string{"queries/highlights.scm"}, "// %s"},
	"cpp":        {"https://github.com/tree-sitter/tree-sitter-cpp", "f41e1a044c8a84ea9fa8577fdd2eab92ec96de02", ".", []string{"c:queries/highlights.scm", "queries/highlights.scm"}, "// %s"},
	"rust":       {"https://github.com/tree-sitter/tree-sitter-rust", "77a3747266f4d621d0757825e6b11edcbf991ca5", ".", []string{"queries/highlights.scm"}, "// %s"},
	"bash":       {"https://github.com/tree-sitter/tree-sitter-bash", "a06c2e4415e9bc0346c6b86d401879ffb44058f7", ".", []string{"queries/highlights.scm"}, "# %s"},
	"css":        {"https://github.com/tree-sitter/tree-sitter-css", "dda5cfc5722c429eaba1c910ca32c2c0c5bb1a3f", ".", []string{"queries/highlights.scm"}, "/* %s */"},
	"html":       {"https://github.com/tree-sitter/tree-sitter-html", "5a5ca8551a179998360b4a4ca2c0f366a35acc03", ".", []string{"queries/highlights.scm"}, "<!-- %s -->"},
	"javascript": {"https://github.com/tree-sitter/tree-sitter-javascript", "44c892e0be055ac465d5eeddae6d3e194424e7de", ".", []string{"queries/highlights.scm", "queries/highlights-jsx.scm", "queries/highlights-params.scm"}, "// %s"},
	"json":       {"https://github.com/tree-sitter/tree-sitter-json", "ee35a6ebefcef0c5c416c0d1ccec7370cfca5a24", ".", []string{"queries/highlights.scm"}, ""},
	"lua":        {"https://github.com/tree-sitter-grammars/tree-sitter-lua", "10fe0054734eec83049514ea2e718b2a56acd0c9", ".", []string{"queries/highlights.scm"}, "-- %s"},
	"markdown":   {"https://github.com/tree-sitter-grammars/tree-sitter-markdown", "f969cd3ae3f9fbd4e43205431d0ae286014c05b5", "tree-sitter-markdown", []string{"tree-sitter-markdown/queries/highlights.scm"}, "<!-- %s -->"},
	"python":     {"https://github.com/tree-sitter/tree-sitter-python", "293fdc02038ee2bf0e2e206711b69c90ac0d413f", ".", []string{"queries/highlights.scm"}, "# %s"},
	"toml":       {"https://github.com/tree-sitter-grammars/tree-sitter-toml", "64b56832c2cffe41758f28e05c756a3a98d16f41", ".", []string{"queries/highlights.scm"}, "# %s"},
	"tsx":        {"https://github.com/tree-sitter/tree-sitter-typescript", "f975a621f4e7f532fe322e13c4f79495e0a7b2e7", "tsx", []string{"queries/highlights.scm", "javascript:queries/highlights-jsx.scm", "javascript:queries/highlights.scm"}, "// %s"},
	"typescript": {"https://github.com/tree-sitter/tree-sitter-typescript", "f975a621f4e7f532fe322e13c4f79495e0a7b2e7", "typescript", []string{"queries/highlights.scm", "javascript:queries/highlights.scm"}, "// %s"},
	"zig":        {"https://github.com/tree-sitter-grammars/tree-sitter-zig", "b670c8df85a1568f498aa5c8cae42f51a90473c0", ".", []string{"queries/highlights.scm"}, "// %s"},
	"yaml":       {"https://github.com/tree-sitter-grammars/tree-sitter-yaml", "7708026449bed86239b1cd5bce6e3c34dbca6415", ".", []string{"queries/highlights.scm"}, "# %s"},
}

// shipped holds the text objects fude writes for the grammars it builds
// (D28).
//
//go:embed queries
var shipped embed.FS

var (
	loadMu      sync.Mutex
	grammarsDir string                   // where built grammars are, which Use sets
	loaded      = map[string]*language{} // the built grammars loaded so far, by filetype
)

// Use makes dir where fude looks for built grammars and builds them.
// dir/<filetype>/ holds a grammar's library and its queries/ (B165).
func Use(dir string) {
	loadMu.Lock()
	defer loadMu.Unlock()
	grammarsDir = dir
}

// library is the file of filetype's grammar in dir: named for the commit
// fude pins, so a new pin needs a new build, or parser.so for a grammar
// fude does not know.
func library(dir, filetype string) string {
	name := "parser"
	if g, ok := registry[filetype]; ok {
		name = g.commit
	}
	return filepath.Join(dir, filetype, name+".so")
}

// lookup returns filetype's language, linked in or loaded from its built
// grammar, or false when it has neither.
func lookup(filetype string) (*language, bool, error) {
	if l, ok := linked[filetype]; ok {
		return l, true, nil
	}
	loadMu.Lock()
	defer loadMu.Unlock()
	if l, ok := loaded[filetype]; ok {
		return l, true, nil
	}
	if grammarsDir == "" || filetype == "" {
		return nil, false, nil
	}
	path := library(grammarsDir, filetype)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	l, err := load(path, filetype)
	if err != nil {
		return nil, false, err
	}
	loaded[filetype] = l
	return l, true, nil
}

// load opens the grammar library at path, which fude never closes, and
// reads the queries beside it.
func load(path, filetype string) (*language, error) {
	cpath, csym := C.CString(path), C.CString("tree_sitter_"+filetype)
	defer C.free(unsafe.Pointer(cpath))
	defer C.free(unsafe.Pointer(csym))
	h := C.dlopen(cpath, C.RTLD_NOW|C.RTLD_LOCAL)
	if h == nil {
		return nil, fmt.Errorf("load the %s grammar: %s", filetype, C.GoString(C.dlerror()))
	}
	f := C.dlsym(h, csym)
	if f == nil {
		return nil, fmt.Errorf("load the %s grammar: %s has no tree_sitter_%s", filetype, path, filetype)
	}
	l := &language{lang: ts.NewLanguage(unsafe.Pointer(C.language(f))), comment: registry[filetype].comment}
	read := func(name string) (string, error) {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(path), "queries", name+".scm"))
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("load the %s grammar: %w", filetype, err)
		}
		return string(b), nil
	}
	var err error
	if l.highlights, err = read("highlights"); err != nil {
		return nil, err
	}
	// fude's own text objects come first, as the grammar's repo has none.
	if b, err := shipped.ReadFile("queries/" + filetype + "/textobjects.scm"); err == nil {
		l.objects = string(b)
	} else if l.objects, err = read("textobjects"); err != nil {
		return nil, err
	}
	return l, nil
}

// Buildable reports whether fude knows how to build filetype's grammar.
func Buildable(filetype string) bool {
	_, ok := registry[filetype]
	return ok
}

// Build fetches filetype's grammar at the commit fude pins and builds it
// into the directory Use named, with git and the tree-sitter CLI. It takes
// seconds, so it belongs off the editor loop (B165).
func Build(filetype string) error {
	g, ok := registry[filetype]
	if !ok {
		return fmt.Errorf("fude cannot build a grammar for %s", filetype)
	}
	loadMu.Lock()
	grammars := grammarsDir
	loadMu.Unlock()
	if grammars == "" {
		return fmt.Errorf("build the %s grammar: there is no data directory to build it in", filetype)
	}
	for _, tool := range []string{"git", "tree-sitter"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("build the %s grammar: %s is not installed", filetype, tool)
		}
	}
	work, err := os.MkdirTemp("", "fude-grammar-")
	if err != nil {
		return fmt.Errorf("build the %s grammar: %w", filetype, err)
	}
	defer os.RemoveAll(work)
	run := func(dir, name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build the %s grammar: %s %s: %w: %s", filetype, name, args[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// fetch checks out ft's grammar at its pin, once, and returns where.
	repos := map[string]string{}
	fetch := func(ft string) (string, error) {
		if dir, ok := repos[ft]; ok {
			return dir, nil
		}
		dir := filepath.Join(work, ft)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", fmt.Errorf("build the %s grammar: %w", filetype, err)
		}
		for _, step := range [][]string{{"init", "-q"}, {"fetch", "-q", "--depth", "1", registry[ft].repo, registry[ft].commit}, {"checkout", "-q", "FETCH_HEAD"}} {
			if err := run(dir, "git", step...); err != nil {
				return "", err
			}
		}
		repos[ft] = dir
		return dir, nil
	}
	src, err := fetch(filetype)
	if err != nil {
		return err
	}
	var highlights []string
	for _, q := range g.highlights {
		ft, path, ok := strings.Cut(q, ":")
		if !ok {
			ft, path = filetype, q
		}
		dir, err := fetch(ft)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			return fmt.Errorf("build the %s grammar: %w", filetype, err)
		}
		highlights = append(highlights, string(b))
	}
	out := filepath.Join(grammars, filetype)
	if err := os.MkdirAll(filepath.Join(out, "queries"), 0o755); err != nil {
		return fmt.Errorf("build the %s grammar: %w", filetype, err)
	}
	if err := os.WriteFile(filepath.Join(out, "queries", "highlights.scm"), []byte(strings.Join(highlights, "\n")), 0o644); err != nil {
		return fmt.Errorf("build the %s grammar: %w", filetype, err)
	}
	// Build writes the library last, through a rename, so a library there
	// means the rest of the build is there too.
	lib := library(grammars, filetype)
	if err := run(src, "tree-sitter", "build", "-o", lib+".new", filepath.Join(src, g.src)); err != nil {
		return err
	}
	if err := os.Rename(lib+".new", lib); err != nil {
		return fmt.Errorf("build the %s grammar: %w", filetype, err)
	}
	return nil
}

// Grammar is a grammar fude has or can build, and its state.
type Grammar struct {
	Filetype, State string
}

// Grammars returns the grammars fude links in or builds, by filetype, and
// whether each is built.
func Grammars() []Grammar {
	loadMu.Lock()
	dir := grammarsDir
	loadMu.Unlock()
	var out []Grammar
	for _, ft := range slices.Sorted(maps.Keys(linked)) {
		out = append(out, Grammar{ft, "linked in"})
	}
	for _, ft := range slices.Sorted(maps.Keys(registry)) {
		state := "not built"
		if _, err := os.Stat(library(dir, ft)); err == nil {
			state = "built"
		}
		out = append(out, Grammar{ft, state})
	}
	return out
}
