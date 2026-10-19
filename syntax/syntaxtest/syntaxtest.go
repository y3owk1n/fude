// Package syntaxtest gives tests Go's grammar without fetching it, built
// from tree-sitter-go's module the way fude builds it from the repo (D29).
package syntaxtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	// The module's sources are what the grammar builds from, so they stay in
	// go.mod and the module cache.
	_ "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/y3owk1n/fude/syntax"
)

// InstallGo puts Go's grammar where fude started with data as its
// XDG_DATA_HOME loads it. It builds the grammar with the tree-sitter CLI
// once, into the user's cache, and links that build into data after.
func InstallGo(data string) error {
	built, err := grammars()
	if err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	if _, err := os.Stat(syntax.Library(built, "go")); err != nil {
		if err := build(built); err != nil {
			return err
		}
	}
	dir := filepath.Join(data, "fude", "grammars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	if err := os.Symlink(filepath.Join(built, "go"), filepath.Join(dir, "go")); err != nil && !os.IsExist(err) {
		return fmt.Errorf("go grammar: %w", err)
	}
	return nil
}

// Install puts the grammars of filetypes where fude started with data as
// its XDG_DATA_HOME loads them, as InstallGo does Go's. syntax.Build builds
// each from its pinned repo once, into the user's cache, so the first run
// needs git, tree-sitter and the network.
func Install(data string, filetypes ...string) error {
	built, err := grammars()
	if err != nil {
		return fmt.Errorf("grammars: %w", err)
	}
	dir := filepath.Join(data, "fude", "grammars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("grammars: %w", err)
	}
	for _, ft := range filetypes {
		if _, err := os.Stat(syntax.Library(built, ft)); err != nil {
			syntax.Use(built, nil)
			if err := syntax.Build(ft); err != nil {
				return err
			}
		}
		if err := os.Symlink(filepath.Join(built, ft), filepath.Join(dir, ft)); err != nil && !os.IsExist(err) {
			return fmt.Errorf("%s grammar: %w", ft, err)
		}
	}
	return nil
}

// grammarsEnv holds the grammars directory. Processes a test starts inherit
// it, and it keeps its value when a test moves XDG_CACHE_HOME.
const grammarsEnv = "FUDE_TEST_GRAMMARS"

// grammars returns the directory tests build grammars in, under the user's
// cache as the first call found it. On Linux the cache follows
// XDG_CACHE_HOME, and tests move that.
func grammars() (string, error) {
	if dir := os.Getenv(grammarsEnv); dir != "" {
		return dir, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "fude-test", "grammars")
	return dir, os.Setenv(grammarsEnv, dir)
}

// build builds Go's grammar and its highlights into dir, from a copy of the
// module, as the module cache is read-only.
func build(dir string) error {
	mod, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/tree-sitter/tree-sitter-go").Output()
	if err != nil {
		return fmt.Errorf("go grammar: go list: %w", err)
	}
	src := strings.TrimSpace(string(mod))
	work, err := os.MkdirTemp("", "fude-go-grammar-")
	if err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	defer os.RemoveAll(work)
	if err := os.CopyFS(work, os.DirFS(src)); err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	queries := filepath.Join(dir, "go", "queries")
	if err := os.MkdirAll(queries, 0o755); err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	b, err := os.ReadFile(filepath.Join(src, "queries", "highlights.scm"))
	if err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	if err := os.WriteFile(filepath.Join(queries, "highlights.scm"), b, 0o644); err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	// Test packages may build it at the same time, so each writes a name of
	// its own.
	lib := syntax.Library(dir, "go")
	tmp := fmt.Sprintf("%s.%d.new", lib, os.Getpid())
	if err := syntax.Compile(work, tmp); err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	if err := os.Rename(tmp, lib); err != nil {
		return fmt.Errorf("go grammar: %w", err)
	}
	return nil
}
