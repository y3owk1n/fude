package syntax

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

// language calls the function a grammar's library exports, which returns
// its language.
static const void *language(void *f) { return ((const void *(*)(void))f)(); }

// ts_set_allocator is in the tree-sitter library the binding links in.
void ts_set_allocator(void *(*)(size_t), void *(*)(size_t, size_t), void *(*)(void *, size_t), void (*)(void *));
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

// The binding has tree-sitter allocate through a call into Go, which then
// calls C's malloc. Those calls cost a fifth of a parse. This has tree-sitter
// call C's malloc and free itself. Both use the one C heap, so free releases
// memory allocated before the switch.
func init() { C.ts_set_allocator(nil, nil, nil, nil) }

// grammar is a grammar fude builds when a file needs it (D29): the repo it
// comes from, the commit fude pins, the directory of its sources in the
// repo, its highlight and injection queries in the order they combine, and
// its language's comment syntax. A query named <filetype>:<path> is in that grammar's repo.
// Its library exports tree_sitter_<filetype>.
type grammar struct {
	repo, commit, src      string
	highlights, injections []string
	comment                string
}

// registry are the grammars fude builds, by filetype.
var registry = map[string]grammar{
	"go":              {"https://github.com/tree-sitter/tree-sitter-go", "1547678a9da59885853f5f5cc8a99cc203fa2e2c", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"c":               {"https://github.com/tree-sitter/tree-sitter-c", "b780e47fc780ddc8da13afa35a3f4ed5c157823d", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"cpp":             {"https://github.com/tree-sitter/tree-sitter-cpp", "f41e1a044c8a84ea9fa8577fdd2eab92ec96de02", ".", []string{"c:queries/highlights.scm", "queries/highlights.scm"}, []string{"queries/injections.scm"}, "// %s"},
	"rust":            {"https://github.com/tree-sitter/tree-sitter-rust", "77a3747266f4d621d0757825e6b11edcbf991ca5", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "// %s"},
	"bash":            {"https://github.com/tree-sitter/tree-sitter-bash", "a06c2e4415e9bc0346c6b86d401879ffb44058f7", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"css":             {"https://github.com/tree-sitter/tree-sitter-css", "dda5cfc5722c429eaba1c910ca32c2c0c5bb1a3f", ".", []string{"queries/highlights.scm"}, nil, "/* %s */"},
	"html":            {"https://github.com/tree-sitter/tree-sitter-html", "5a5ca8551a179998360b4a4ca2c0f366a35acc03", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "<!-- %s -->"},
	"javascript":      {"https://github.com/tree-sitter/tree-sitter-javascript", "44c892e0be055ac465d5eeddae6d3e194424e7de", ".", []string{"queries/highlights.scm", "queries/highlights-jsx.scm", "queries/highlights-params.scm"}, []string{"queries/injections.scm"}, "// %s"},
	"json":            {"https://github.com/tree-sitter/tree-sitter-json", "ee35a6ebefcef0c5c416c0d1ccec7370cfca5a24", ".", []string{"queries/highlights.scm"}, nil, ""},
	"lua":             {"https://github.com/tree-sitter-grammars/tree-sitter-lua", "10fe0054734eec83049514ea2e718b2a56acd0c9", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "-- %s"},
	"markdown_inline": {"https://github.com/tree-sitter-grammars/tree-sitter-markdown", "f969cd3ae3f9fbd4e43205431d0ae286014c05b5", "tree-sitter-markdown-inline", []string{"tree-sitter-markdown-inline/queries/highlights.scm"}, []string{"tree-sitter-markdown-inline/queries/injections.scm"}, ""},
	"markdown":        {"https://github.com/tree-sitter-grammars/tree-sitter-markdown", "f969cd3ae3f9fbd4e43205431d0ae286014c05b5", "tree-sitter-markdown", []string{"tree-sitter-markdown/queries/highlights.scm"}, []string{"tree-sitter-markdown/queries/injections.scm"}, "<!-- %s -->"},
	"python":          {"https://github.com/tree-sitter/tree-sitter-python", "293fdc02038ee2bf0e2e206711b69c90ac0d413f", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"toml":            {"https://github.com/tree-sitter-grammars/tree-sitter-toml", "64b56832c2cffe41758f28e05c756a3a98d16f41", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"tsx":             {"https://github.com/tree-sitter/tree-sitter-typescript", "f975a621f4e7f532fe322e13c4f79495e0a7b2e7", "tsx", []string{"queries/highlights.scm", "javascript:queries/highlights-jsx.scm", "javascript:queries/highlights.scm"}, []string{"javascript:queries/injections.scm"}, "// %s"},
	"typescript":      {"https://github.com/tree-sitter/tree-sitter-typescript", "f975a621f4e7f532fe322e13c4f79495e0a7b2e7", "typescript", []string{"queries/highlights.scm", "javascript:queries/highlights.scm"}, []string{"javascript:queries/injections.scm"}, "// %s"},
	"zig":             {"https://github.com/tree-sitter-grammars/tree-sitter-zig", "b670c8df85a1568f498aa5c8cae42f51a90473c0", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "// %s"},
	"yaml":            {"https://github.com/tree-sitter-grammars/tree-sitter-yaml", "7708026449bed86239b1cd5bce6e3c34dbca6415", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"fish":            {"https://github.com/ram02z/tree-sitter-fish", "b7f1d682941e0c62dfcd6bf9ef481638351bab16", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"jsonc":           {"https://gitlab.com/WhyNotHugo/tree-sitter-jsonc.git", "f7890ba894d885d1b2b721cfae759b07d53c8886", ".", []string{"json:queries/highlights.scm"}, nil, "// %s"},
	"just":            {"https://github.com/casey/tree-sitter-just", "5685543a6e64f66335e25518c9ae8ffa1dae3d01", ".", []string{"queries/just/highlights.scm"}, []string{"queries/just/injections.scm"}, "# %s"},
	"dockerfile":      {"https://github.com/camdencheek/tree-sitter-dockerfile", "971acdd908568b4531b0ba28a445bf0bb720aba5", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"nix":             {"https://github.com/nix-community/tree-sitter-nix", "32c3ce98afdafc2ec9aedf0280a678750f76254c", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "# %s"},
	"gomod":           {"https://github.com/camdencheek/tree-sitter-go-mod", "2e886870578eeba1927a2dc4bd2e2b3f598c5f9a", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"gosum":           {"https://github.com/tree-sitter-grammars/tree-sitter-go-sum", "27816eb6b7315746ae9fcf711e4e1396dc1cf237", ".", []string{"queries/highlights.scm"}, nil, ""},
	"gowork":          {"https://github.com/omertuc/tree-sitter-go-work", "949a8a470559543857a62102c84700d291fc984c", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"sql":             {"https://github.com/derekstride/tree-sitter-sql", "39fdb006403747241244326e8af3b3e96b85381c", ".", []string{"queries/highlights.scm"}, nil, "-- %s"},
	"gitcommit":       {"https://github.com/gbprod/tree-sitter-gitcommit", "55a265cf763ec15d6e2d96bb206e3f5e30d82bae", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "# %s"},
	"git_rebase":      {"https://github.com/the-mikedavis/tree-sitter-git-rebase", "32686d6b72980b36f876ae2d07719c9c3ed154e2", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"diff":            {"https://github.com/tree-sitter-grammars/tree-sitter-diff", "ada384ac7bfc1307f32de474620120add29998fb", ".", []string{"queries/highlights.scm"}, nil, ""},
	"gitignore":       {"https://github.com/shunsambongi/tree-sitter-gitignore", "f4685bf11ac466dd278449bcfe5fd014e94aa504", ".", nil, nil, "# %s"},
	"make":            {"https://github.com/tree-sitter-grammars/tree-sitter-make", "70613f3d812cbabbd7f38d104d60a409c4008b43", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "# %s"},
	"ruby":            {"https://github.com/tree-sitter/tree-sitter-ruby", "ad907a69da0c8a4f7a943a7fe012712208da6dee", ".", []string{"queries/highlights.scm"}, nil, "# %s"},
	"java":            {"https://github.com/tree-sitter/tree-sitter-java", "e10607b45ff745f5f876bfa3e94fbcc6b44bdc11", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"kotlin":          {"https://github.com/fwcd/tree-sitter-kotlin", "1852ea17b7f60fb3f9d84e0b1555d56b46b39fb1", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"swift":           {"https://github.com/alex-pinkus/tree-sitter-swift", "82bb3a533e0801fd2bbaa11dc49676e10bf41948", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "// %s"},
	"php":             {"https://github.com/tree-sitter/tree-sitter-php", "58d5643086ae60e93a3746cca648363ee97c8761", "php", []string{"queries/highlights.scm"}, []string{"queries/injections.scm", "queries/injections-text.scm"}, "// %s"},
	"elixir":          {"https://github.com/elixir-lang/tree-sitter-elixir", "4b0c7118760af58a2e7081bbc8396e136f820b37", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "# %s"},
	"haskell":         {"https://github.com/tree-sitter-grammars/tree-sitter-haskell", "98aedbd2d6947a168ba3ba3755d70b0cb6b78395", ".", []string{"queries/highlights.scm"}, []string{"queries/injections.scm"}, "-- %s"},
	"scala":           {"https://github.com/tree-sitter/tree-sitter-scala", "db390f312a54b04b13790e1767bfac32665c17ac", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"dart":            {"https://github.com/UserNobody14/tree-sitter-dart", "3f0c7a8309d89927e7cc6bef39569c815fddb2e5", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
	"scss":            {"https://github.com/tree-sitter-grammars/tree-sitter-scss", "5da5ba71a558b007b352c505bfcd9095ae337022", ".", []string{"css:queries/highlights.scm", "queries/highlights.scm"}, nil, "// %s"},
	"vue":             {"https://github.com/tree-sitter-grammars/tree-sitter-vue", "ce8011a414fdf8091f4e4071752efc376f4afb08", ".", []string{"html:queries/highlights.scm"}, []string{"html:queries/injections.scm", "queries/vue/injections.scm"}, "<!-- %s -->"},
	"svelte":          {"https://github.com/tree-sitter-grammars/tree-sitter-svelte", "ae5199db47757f785e43a14b332118a5474de1a2", ".", []string{"html:queries/highlights.scm", "queries/highlights.scm"}, []string{"html:queries/injections.scm"}, "<!-- %s -->"},
	"astro":           {"https://github.com/virchau13/tree-sitter-astro", "213f6e6973d9b456c6e50e86f19f66877e7ef0ee", ".", []string{"queries/highlights.scm"}, []string{"html:queries/injections.scm", "queries/injections.scm"}, "<!-- %s -->"},
	"graphql":         {"https://github.com/bkegley/tree-sitter-graphql", "5e66e961eee421786bdda8495ed1db045e06b5fe", ".", []string{"queries/graphql/highlights.scm"}, nil, "# %s"},
	"xml":             {"https://github.com/tree-sitter-grammars/tree-sitter-xml", "5000ae8f22d11fbe93939b05c1e37cf21117162d", "xml", []string{"queries/xml/highlights.scm"}, nil, "<!-- %s -->"},
	"prisma":          {"https://github.com/victorhqc/tree-sitter-prisma", "3556b2c1f20ec9ac91e92d32c43d9d2a0ca3cc49", ".", []string{"queries/highlights.scm"}, nil, "// %s"},
}

// shipped holds the text objects fude writes for the grammars it builds
// (D28).
//
//go:embed queries
var shipped embed.FS

var (
	loadMu      sync.Mutex
	grammarsDir string                   // where built grammars are, which Use sets
	resolver    func(name string) string // the filetype an injection's language name stands for
	loaded      = map[string]*language{} // the built grammars loaded so far, by filetype
)

// Use makes dir where fude looks for built grammars and builds them, and
// resolve what turns a language an injection names, such as py, into a
// filetype. dir/<filetype>/ holds a grammar's library and its queries/
// (B165, B166).
func Use(dir string, resolve func(name string) string) {
	loadMu.Lock()
	defer loadMu.Unlock()
	grammarsDir, resolver = dir, resolve
}

// resolve returns the filetype the language name stands for. Workers call
// it, so it holds the lock to read the resolver.
func resolve(name string) string {
	loadMu.Lock()
	f := resolver
	loadMu.Unlock()
	if f == nil || name == "" {
		return name
	}
	return f(name)
}

// buildVersion is the version of what a build writes. It goes up when a
// build starts writing something new, so grammars built before build again
// on first use. Version 2 began copying injections.
const buildVersion = 2

// Library is the file of filetype's grammar in dir: named for the commit
// fude pins and the build version, so a new pin or version needs a new
// build, or parser.so for a grammar fude does not know.
func Library(dir, filetype string) string {
	name := "parser"
	if g, ok := registry[filetype]; ok {
		name = fmt.Sprintf("%s-%d", g.commit, buildVersion)
	}
	return filepath.Join(dir, filetype, name+".so")
}

// lookup returns filetype's language, loaded from its built grammar, or
// false when it has none.
func lookup(filetype string) (*language, bool, error) {
	loadMu.Lock()
	defer loadMu.Unlock()
	if l, ok := loaded[filetype]; ok {
		return l, true, nil
	}
	if grammarsDir == "" || filetype == "" {
		return nil, false, nil
	}
	path := Library(grammarsDir, filetype)
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
	// fude's own highlights stand in for a grammar's repo that has none.
	if b, err := shipped.ReadFile("queries/" + filetype + "/highlights.scm"); err == nil && l.highlights == "" {
		l.highlights = string(b)
	}
	if l.injections, err = read("injections"); err != nil {
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
	out := filepath.Join(grammars, filetype)
	if err := os.MkdirAll(filepath.Join(out, "queries"), 0o755); err != nil {
		return fmt.Errorf("build the %s grammar: %w", filetype, err)
	}
	for name, files := range map[string][]string{"highlights": g.highlights, "injections": g.injections} {
		var parts []string
		for _, q := range files {
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
			parts = append(parts, string(b))
		}
		if err := os.WriteFile(filepath.Join(out, "queries", name+".scm"), []byte(strings.Join(parts, "\n")), 0o644); err != nil {
			return fmt.Errorf("build the %s grammar: %w", filetype, err)
		}
	}
	// Build writes the library last, through a rename, so a library there
	// means the rest of the build is there too.
	lib := Library(grammars, filetype)
	if err := run(src, "tree-sitter", "build", "-o", lib+".new", filepath.Join(src, g.src)); err != nil {
		return err
	}
	if err := os.Rename(lib+".new", lib); err != nil {
		return fmt.Errorf("build the %s grammar: %w", filetype, err)
	}
	// Libraries of older pins go, as nothing loads them now.
	old, _ := filepath.Glob(filepath.Join(out, "*-*.so"))
	for _, p := range old {
		if p != lib {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("build the %s grammar: %w", filetype, err)
			}
		}
	}
	return nil
}

// Grammar is a grammar fude has or can build, and its state.
type Grammar struct {
	Filetype, State string
}

// Grammars returns the grammars fude builds, by filetype, and whether each
// is built.
func Grammars() []Grammar {
	loadMu.Lock()
	dir := grammarsDir
	loadMu.Unlock()
	var out []Grammar
	for _, ft := range slices.Sorted(maps.Keys(registry)) {
		state := "not built"
		if _, err := os.Stat(Library(dir, ft)); err == nil {
			state = "built"
		} else if old, _ := filepath.Glob(filepath.Join(dir, ft, "*-*.so")); len(old) > 0 {
			// It was built at an older pin, or by an older fude.
			state = "outdated"
		}
		out = append(out, Grammar{ft, state})
	}
	return out
}
