package terminal_test

import (
	"os"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/terminal"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B369
func TestB369PickTerminals(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	e := editortest.Start(t)
	e.Use(terminal.New())
	e.SetSize(80, 24)
	// A shell out of sight is in the list, and picking it shows it again.
	if editortest.Feed(e, ":terminal<CR><C-\\><C-n>:terminal<CR>"); len(e.Windows()) != 1 {
		t.Fatalf(":terminal twice left %d windows", len(e.Windows()))
	}
	editortest.Feed(e, ":pick terminals<CR>")
	editortest.Settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "shell 1") })
	editortest.Feed(e, "<CR>")
	editortest.Settle(t, e, func() bool { return len(e.Windows()) == 2 && e.Terminal() })
}

// B366
func TestB366Open(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	editortest.Config(t, "[open]\nterminal = \"right\"\n")
	e := editortest.Start(t)
	e.Use(terminal.New())
	e.SetSize(80, 24)
	if editortest.Feed(e, ":terminal<CR>"); len(e.Windows()) != 2 || e.Windows()[1].X == 0 || !e.Terminal() {
		t.Fatalf("with terminal = \"right\" :terminal made %d windows, the last at column %d", len(e.Windows()), e.Windows()[len(e.Windows())-1].X)
	}
	// A value that is not a placement is a config error.
	editortest.Config(t, "[open]\nterminal = \"up\"\n")
	if e := editortest.Start(t); !strings.Contains(e.Message(), `open.terminal has no placement "up"`) {
		t.Errorf("open.terminal = \"up\" said %q", e.Message())
	}
}

// B205, for completing :terminal
func TestB205CompleteShells(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	e := editortest.Start(t)
	e.Use(terminal.New())
	e.SetSize(80, 24)
	// A shell completes as its number, then shell, and that names it, as
	// :b's completion names a buffer.
	if editortest.Feed(e, ":terminal 2<CR><C-\\><C-n>:terminal <Tab>"); e.Line() != ":terminal 2 shell" {
		t.Fatalf(":terminal <Tab> made the line %q, want :terminal 2 shell", e.Line())
	}
	if editortest.Feed(e, "<CR>"); len(e.Windows()) != 1 {
		t.Errorf(":terminal 2 shell in shell 2 left %d windows, want it hidden", len(e.Windows()))
	}
}
