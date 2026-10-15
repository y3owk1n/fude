package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
)

// config writes src as config.toml in a new config directory, with no
// grammar fetched.
func config(t *testing.T, src string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "fude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fude", "config.toml"), []byte("auto_install = false\n"+src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func start(t *testing.T, args ...string) *editor.Session {
	t.Helper()
	e, err := editor.Start(args)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// B92
func TestB92NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	e := start(t)
	if e.Message() != "" {
		t.Errorf("started with %q, want no message", e.Message())
	}
	feed(e, ":q<CR>")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the config directory has %d entries, want none", len(entries))
	}
}

// B93
func TestB93ConfigErrorShowsItsLine(t *testing.T) {
	// The file as written, without the line config adds, as its lines count.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "fude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fude", "config.toml"), []byte("indent_width = 2\nkey_timeout = \"soon\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := start(t)
	if !strings.Contains(e.Message(), "config.toml line 2: ") {
		t.Errorf("started with %q, want the error on line 2", e.Message())
	}
	if d, _ := e.Wait(); d != time.Second {
		t.Errorf("key timeout is %v, want the default 1s", d)
	}
}

// B94
func TestB94FiletypeSettings(t *testing.T) {
	config(t, "indent_width = 2\n[filetype.go]\nindent_width = 4\n")
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := start(t, a, b)
	feed(e, ">>")
	if text(e) != "    x" {
		t.Errorf(">> in a.go gave %q, want four spaces", text(e))
	}
	feed(e, ":bn<CR>>>")
	if text(e) != "  x" {
		t.Errorf(">> in b.txt gave %q, want two spaces", text(e))
	}
}
