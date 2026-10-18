package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
)

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

// B270
func TestB270Clean(t *testing.T) {
	config(t, "number = true\n")
	remove, err := editor.Clean()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(remove)
	e := start(t)
	if e.Views()[0].Number || e.Message() != "" {
		t.Errorf("--clean started with number %v and %q, want the defaults", e.Views()[0].Number, e.Message())
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
	// A filetype's options hold while a window shows its file, and the
	// window gets its own back after. A :set in that window stays.
	config(t, "[filetype.yaml]\nexpand_tab = true\nwrap = false\n")
	a = filepath.Join(dir, "a.yaml")
	if err := os.WriteFile(a, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e = start(t, a, b)
	feed(e, ">>:set number true<CR>")
	if v := e.Views()[0]; text(e) != "        x" || v.Wrap {
		t.Errorf(">> in a.yaml gave %q with wrap %v, want spaces without wrap", text(e), v.Wrap)
	}
	feed(e, ":bn<CR>>>")
	if v := e.Views()[0]; text(e) != "\tx" || !v.Wrap || !v.Number {
		t.Errorf(">> in b.txt gave %q with wrap %v and number %v, want a tab with both", text(e), v.Wrap, v.Number)
	}
}

// B237
func TestB237TabWidth(t *testing.T) {
	config(t, "tab_width = 4\nindent_width = 2\n")
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := start(t, path)
	// Two indents of 2 columns make one tab of 4.
	if feed(e, ">>"); text(e) != "  x" {
		t.Errorf(">> gave %q, want two spaces", text(e))
	}
	if feed(e, ">>"); text(e) != "\tx" {
		t.Errorf(">>>> gave %q, want a tab", text(e))
	}
	// k keeps the column, which the tab's width decides.
	change(t, path, "\tx\nabcdefgh\n")
	e = start(t, path)
	if feed(e, "j4lk"); e.Cursor() != 1 {
		t.Errorf("k from column 4 went to %d, want the x after the tab", e.Cursor())
	}
}
