package editor

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// extFiletypes are the filetypes of extensions that differ from their
// language's name (B164).
var extFiletypes = map[string]string{
	"yml": "yaml", "md": "markdown", "sh": "bash", "py": "python",
	"js": "javascript", "mjs": "javascript", "cjs": "javascript", "jsx": "javascript",
	"ts": "typescript", "mts": "typescript", "cts": "typescript",
	"rs": "rust", "m": "objc", "h": "c", "cc": "cpp", "cxx": "cpp", "hpp": "cpp", "hh": "cpp", "htm": "html",
	"just": "just", "dockerfile": "dockerfile", "containerfile": "dockerfile", "patch": "diff", "mk": "make",
	"rb": "ruby", "rake": "ruby", "gemspec": "ruby", "ru": "ruby", "kt": "kotlin", "kts": "kotlin", "phtml": "php",
	"ex": "elixir", "exs": "elixir", "hs": "haskell", "sc": "scala", "sbt": "scala",
	"gql": "graphql", "graphqls": "graphql",
	"svg": "xml", "plist": "xml", "xsd": "xml", "xsl": "xml", "xslt": "xml", "rss": "xml", "atom": "xml",
}

// nameFiletypes are the filetypes of files known by their whole name.
var nameFiletypes = map[string]string{
	".bashrc": "bash", ".bash_profile": "bash", ".bash_aliases": "bash", ".profile": "bash", "PKGBUILD": "bash",
	"Cargo.lock":    "toml",
	"tsconfig.json": "jsonc", "jsconfig.json": "jsonc", ".eslintrc": "jsonc", ".babelrc": "jsonc", "bun.lock": "jsonc",
	"justfile": "just", "Justfile": "just", ".justfile": "just",
	"Dockerfile": "dockerfile", "dockerfile": "dockerfile", "Containerfile": "dockerfile",
	"go.mod": "gomod", "go.sum": "gosum", "go.work.sum": "gosum", "go.work": "gowork",
	"COMMIT_EDITMSG": "gitcommit", "MERGE_MSG": "gitcommit", "TAG_EDITMSG": "gitcommit", "git-rebase-todo": "git_rebase",
	".gitignore": "gitignore", ".ignore": "gitignore", ".rgignore": "gitignore", ".fdignore": "gitignore",
	"Makefile": "make", "makefile": "make", "GNUmakefile": "make",
	"Gemfile": "ruby", "Rakefile": "ruby", "Guardfile": "ruby", "Podfile": "ruby", "Vagrantfile": "ruby", "Brewfile": "ruby",
	"mix.lock": "elixir",
}

// filetypeOf returns the filetype of the file at path: the one whose files
// patterns match its name, else the one fude knows for its name or its
// extension, else its extension (B164).
func (ed *Editor) filetypeOf(path string) string {
	name := filepath.Base(path)
	for _, ft := range slices.Sorted(maps.Keys(ed.cfg.Filetype)) {
		for _, p := range ed.cfg.Filetype[ft].Files {
			// loadConfig turned away bad patterns.
			if ok, _ := filepath.Match(p, name); ok {
				return ft
			}
		}
	}
	if ft, ok := nameFiletypes[name]; ok {
		return ft
	}
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	// An extension in capitals has the filetype of its lower case.
	return cmp.Or(extFiletypes[ext], extFiletypes[strings.ToLower(ext)], ext)
}

// filetype returns b's filetype. A buffer with no file has the one it was
// given, or "".
func (ed *Editor) filetype(b *buffer) string {
	if b.file.path == "" {
		return b.ft
	}
	return ed.filetypeOf(b.file.path)
}

// aliasFiletypes renames each [filetype.<ext>] table of c to the filetype
// its extension has, such as [filetype.yml] to [filetype.yaml] (B164).
func aliasFiletypes(c *config) error {
	for _, ext := range slices.Sorted(maps.Keys(c.Filetype)) {
		ft, ok := extFiletypes[ext]
		if !ok || ft == ext {
			continue
		}
		if _, both := c.Filetype[ft]; both {
			return fmt.Errorf("[filetype.%s] and [filetype.%s] are the same filetype", ext, ft)
		}
		c.Filetype[ft] = c.Filetype[ext]
		delete(c.Filetype, ext)
	}
	return nil
}

// badPattern returns an error naming the first files pattern in c that is
// not a pattern, or nil.
func badPattern(c config) error {
	for _, ft := range slices.Sorted(maps.Keys(c.Filetype)) {
		for _, p := range c.Filetype[ft].Files {
			if _, err := filepath.Match(p, ""); err != nil {
				return fmt.Errorf("[filetype.%s] files: %q: %w", ft, p, err)
			}
		}
	}
	return nil
}
