package start_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/files"
	"github.com/y3owk1n/fude/modules/recent"
	"github.com/y3owk1n/fude/modules/start"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B225
func TestB225StartScreen(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Chdir(dir)
	e := editortest.Open(t, "a.txt")
	e.Use(recent.New())
	e.Use(start.New())
	e.Use(files.New())
	e.SetSize(80, 24)
	abs, err := filepath.Abs("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	editortest.Settle(t, e, func() bool {
		b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fude", "recent"))
		return strings.Contains(string(b), abs)
	})
	// The start screen lists the actions, then the recent files, without
	// line numbers.
	editortest.Feed(e, ":set number true<CR>:enew<CR>")
	e.Welcome()
	editortest.Settle(t, e, func() bool {
		got := editortest.Text(e)
		return strings.Contains(got, "f  find a file") && strings.Contains(got, "q  quit") && strings.Contains(got, "1  a.txt")
	})
	if e.Windows()[0].Number {
		t.Error("the start screen shows line numbers")
	}
	// A digit opens its recent file, and the window shows numbers again.
	if editortest.Feed(e, "1"); e.Windows()[0].Name != "a.txt" || !e.Windows()[0].Number {
		t.Errorf("1 opened %q", e.Windows()[0].Name)
	}
	// <CR> runs the line's key, as f opens the finder.
	editortest.Feed(e, ":enew<CR>")
	e.Welcome()
	editortest.Settle(t, e, func() bool { return strings.Contains(editortest.Text(e), "f  find a file") })
	if editortest.Feed(e, "<CR>"); func() bool { _, ok := e.Menu(); return !ok }() {
		t.Error("<CR> on f did not open the finder")
	}
	// c opens config.toml, so :w writes it on a machine with no config
	// directory yet.
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	editortest.Feed(e, "<Esc>:enew<CR>")
	e.Welcome()
	editortest.Settle(t, e, func() bool { return strings.Contains(editortest.Text(e), "c  open config.toml") })
	editortest.Feed(e, "ci# mine<Esc>:w<CR>")
	if b, err := os.ReadFile(filepath.Join(config, "fude", "config.toml")); err != nil || string(b) != "# mine\n" {
		t.Errorf("c then :w wrote %q, %v, and said %q", b, err, e.Message())
	}
}
