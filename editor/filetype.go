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
	"rs": "rust", "h": "c", "cc": "cpp", "cxx": "cpp", "hpp": "cpp", "hh": "cpp", "htm": "html",
}

// nameFiletypes are the filetypes of files known by their whole name.
var nameFiletypes = map[string]string{
	".bashrc": "bash", ".bash_profile": "bash", ".bash_aliases": "bash", ".profile": "bash", "PKGBUILD": "bash",
	"Cargo.lock": "toml",
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
	return cmp.Or(extFiletypes[ext], ext)
}

// filetype returns b's filetype, or "" for a buffer with no file.
func (ed *Editor) filetype(b *buffer) string {
	if b.file.path == "" {
		return ""
	}
	return ed.filetypeOf(b.file.path)
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
