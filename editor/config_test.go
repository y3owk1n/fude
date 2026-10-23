package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	explorermod "github.com/y3owk1n/fude/modules/explorer"
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
	if e.Windows()[0].Number || e.Message() != "" {
		t.Errorf("--clean started with number %v and %q, want the defaults", e.Windows()[0].Number, e.Message())
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
	e := withCommands(start(t, a, b))
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
	e = withCommands(start(t, a, b))
	feed(e, ">>:set number true<CR>")
	if v := e.Windows()[0]; text(e) != "        x" || v.Wrap {
		t.Errorf(">> in a.yaml gave %q with wrap %v, want spaces without wrap", text(e), v.Wrap)
	}
	feed(e, ":bn<CR>>>")
	if v := e.Windows()[0]; text(e) != "\tx" || !v.Wrap || !v.Number {
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
	// From a tab, j keeps its last column, where the cursor shows.
	if feed(e, "0j"); e.Cursor() != 6 {
		t.Errorf("j from a tab went to %d, want the d under its end", e.Cursor())
	}
}

// B39, for the settings of config.toml
func TestB39SetSettings(t *testing.T) {
	config(t, "[keymap.normal]\n\"<Leader>x\" = \":set number true<CR>\"\n")
	file := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "config.toml")
	before := disk(t, file)
	e := start(t)
	e.SetSize(80, 20)
	// A setting shows, and takes a value as config.toml would.
	if feed(e, ":set key_timeout<CR>"); e.Message() != "key_timeout 1000" {
		t.Errorf(":set key_timeout said %q", e.Message())
	}
	feed(e, ":set completion_sources buffer,lsp<CR>")
	if feed(e, ":set completion_sources<CR>"); e.Message() != "completion_sources buffer,lsp" {
		t.Errorf("after setting it, :set completion_sources said %q", e.Message())
	}
	if feed(e, ":set completion_sources words<CR>"); !strings.Contains(e.Message(), `not "words"`) {
		t.Errorf("an unknown source said %q", e.Message())
	}
	// tab_width changes the windows at once, and theme acts as :theme does.
	if feed(e, ":set tab_width 4<CR>"); e.Windows()[0].Tab != 4 {
		t.Errorf("after :set tab_width 4 the window's tab is %d", e.Windows()[0].Tab)
	}
	if feed(e, ":set theme light<CR>"); func() string { name := e.Theme().Name; return name }() != "light" {
		t.Errorf(":set theme light left the theme %q", e.Message())
	}
	// A new leader moves the keys that hold it, and the old one maps none.
	if feed(e, ":set leader ,<CR>,x"); !e.Windows()[0].Number {
		t.Fatal("after :set leader , the key ,x did not run")
	}
	if feed(e, ":set number false<CR><Space>x"); e.Windows()[0].Number {
		t.Error("after :set leader , the key <Space>x still ran")
	}
	// :set never writes config.toml.
	if disk(t, file) != before {
		t.Errorf(":set changed config.toml to %q", disk(t, file))
	}
}

// B39, for :config reload
func TestConfigReload(t *testing.T) {
	config(t, "key_timeout = 900\n[keymap.normal]\n\"-\" = \":set number true<CR>\"\n")
	file := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "config.toml")
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\n")
	e := start(t, filepath.Join(dir, "a.txt"))
	e.Use(explorermod.New())
	e.SetSize(80, 20)
	feed(e, ":set wrap false<CR>")
	// In the new file, - loses its mapping and z8 gets one. A setting and an
	// option change too.
	change(t, file, "auto_install = false\nkey_timeout = 500\ncursor_line = true\n[keymap.normal]\n\"z8\" = \":set number true<CR>\"\n")
	if feed(e, ":config reload<CR>"); e.Message() != "reloaded config.toml" {
		t.Fatalf(":config reload said %q", e.Message())
	}
	if feed(e, ":set key_timeout<CR>"); e.Message() != "key_timeout 500" {
		t.Errorf("after the reload :set key_timeout said %q", e.Message())
	}
	// An option the file changed applies to the window, and one :set
	// changed and the file did not stays.
	if v := e.Windows()[0]; !v.CursorLine || v.Wrap {
		t.Errorf("after the reload cursor_line is %v and wrap %v", v.CursorLine, v.Wrap)
	}
	// A key the file no longer maps does what it does by default, and a new
	// one maps.
	if feed(e, "z8"); !e.Windows()[0].Number {
		t.Error("after the reload z8 did not run its mapping")
	}
	if feed(e, ":set number false<CR>-"); e.Windows()[0].Number || !strings.HasSuffix(e.Windows()[0].Name, string(filepath.Separator)) {
		t.Errorf("after the reload - showed %q with number %v, want the folder's explorer", e.Windows()[0].Name, e.Windows()[0].Number)
	}
	// A file with an error changes nothing and says where.
	change(t, file, "key_timeout = \"soon\"\n")
	if feed(e, ":config reload<CR>"); !strings.Contains(e.Message(), "line 1") {
		t.Errorf("a bad config.toml said %q", e.Message())
	}
	if feed(e, ":set key_timeout<CR>"); e.Message() != "key_timeout 500" {
		t.Errorf("after a bad reload :set key_timeout said %q", e.Message())
	}
}
