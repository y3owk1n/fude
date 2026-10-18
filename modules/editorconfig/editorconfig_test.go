package editorconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/editorconfig"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// indented adds editorconfig to e and checks that >> indents its one line to
// want. The module's options land through the loop, so it tries until they
// do, undoing each try.
func indented(t *testing.T, e *editor.Session, want, name string) {
	t.Helper()
	e.Use(editorconfig.New())
	got := ""
	editortest.Settle(t, e, func() bool {
		editortest.Feed(e, ">>")
		got = editortest.Text(e)
		editortest.Feed(e, "u")
		return got == want
	})
	if got != want {
		t.Errorf(">> in %s gave %q, want %q", name, got, want)
	}
}

// B238
func TestB238Editorconfig(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	files := map[string]string{
		// Above a file with root = true, so it is not read.
		".editorconfig": "[*]\nindent_size = 7\n",
		"proj/.editorconfig": "# the project\nroot = true\n[*]\nindent_style = space\n[*.txt]\nindent_size = 4\n" +
			"[*.{go,mk}]\nindent_style = tab\ntab_width = 2\n",
		"proj/sub/.editorconfig": "[*.txt]\nindent_size = 2\n",
		"proj/a.txt":             "x\n",
		"proj/a.go":              "x\n",
		"proj/sub/b.txt":         "x\n",
	}
	for name, text := range files {
		path := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// .editorconfig wins over config.toml, a later section over an earlier
	// one, and a nearer file over one above it.
	editortest.Config(t, "[filetype.txt]\nindent_width = 3\n")
	for name, want := range map[string]string{"a.txt": "    x", "a.go": "\tx", "sub/b.txt": "  x"} {
		e := editortest.Start(t, filepath.Join(proj, name))
		indented(t, e, want, name)
	}
	// editorconfig = false leaves config.toml's.
	editortest.Config(t, "editorconfig = false\n[filetype.txt]\nindent_width = 3\nexpand_tab = true\n")
	e := editortest.Start(t, filepath.Join(proj, "a.txt"))
	indented(t, e, "   x", "a.txt with editorconfig = false")
}
