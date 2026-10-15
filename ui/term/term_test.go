package term

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

// start opens args in an editor drawn on a mock terminal, with its config
// and timeline logs in temp directories.
func start(t *testing.T, args ...string) *client {
	t.Helper()
	return startWith(t, "", args...)
}

// startWith is start with config as config.toml.
func startWith(t *testing.T, config string, args ...string) *client {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fude", "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	e, err := editor.Start(args)
	if err != nil {
		t.Fatal(err)
	}
	s, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40}))
	if err == nil {
		err = s.Init()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Fini)
	return newClient(s, e)
}

// typed is a terminal the user typed in, as bytes, and whose output goes
// nowhere.
type typed struct{ in chan []byte }

func (t typed) Start() error                { return nil }
func (t typed) Stop() error                 { return nil }
func (t typed) Drain() error                { return nil }
func (t typed) NotifyResize(chan<- bool)    {}
func (t typed) Close() error                { return nil }
func (t typed) Write(p []byte) (int, error) { return len(p), nil }
func (t typed) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: 80, Height: 24}, nil
}

func (t typed) Read(p []byte) (int, error) {
	b, ok := <-t.in
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}

// B141
func TestB141CtrlH(t *testing.T) {
	in := make(chan []byte, 1)
	s, err := screen(typed{in})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	for _, c := range []struct {
		in   string
		want vim.Key
	}{{"\x08", "<C-h>"}, {"\x7f", "<BS>"}} {
		in <- []byte(c.in)
		for {
			select {
			case ev := <-s.EventQ():
				k, ok := ev.(*tcell.EventKey)
				if !ok {
					continue
				}
				if got := keys(k); len(got) != 1 || got[0] != c.want {
					t.Fatalf("%q read as %v, want %v", c.in, got, c.want)
				}
			case <-time.After(time.Second):
				t.Fatalf("%q read as nothing", c.in)
			}
			break
		}
	}
}

// B145
func TestB145Theme(t *testing.T) {
	for _, c := range []struct{ config, theme string }{{"", "dark"}, {`theme = "light"`, "light"}} {
		cl := startWith(t, c.config)
		cl.draw()
		if _, style, _ := cl.s.Get(0, 0); style.GetBackground() != themes[c.theme].bg {
			t.Errorf("with %q the ground is %v, want %s's", c.config, style.GetBackground(), c.theme)
		}
	}
	// Text between highlights keeps the theme's colours too.
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package a\n\nvar x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl := start(t, path)
	deadline := time.After(5 * time.Second)
	for {
		cl.draw()
		if _, style, _ := cl.s.Get(0, 2); style.GetForeground() == themes["dark"].keyword {
			break
		}
		select {
		case f := <-cl.e.Queue():
			f()
		case <-deadline:
			t.Fatal("var was never highlighted")
		}
	}
	if _, style, _ := cl.s.Get(3, 2); style.GetBackground() != themes["dark"].bg {
		t.Fatalf("the space after var has ground %v", style.GetBackground())
	}
}

// B146
func TestB146Base16(t *testing.T) {
	dir := t.TempDir()
	stylix := filepath.Join(dir, "palette.json")
	tinted := filepath.Join(dir, "scheme.yaml")
	var j, y strings.Builder
	for i := range 16 {
		fmt.Fprintf(&j, "  \"base0%X\": \"%06x\",\n", i, 0x101010*(i%15+1))
		fmt.Fprintf(&y, "  base0%X: \"#%06x\"\n", i, 0x101010*(i%15+1))
	}
	for path, src := range map[string]string{stylix: "{\n" + j.String() + "  \"scheme\": \"x\"\n}\n", tinted: "system: \"base16\"\npalette:\n" + y.String()} {
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		cl := startWith(t, fmt.Sprintf("theme = %q", path))
		cl.draw()
		if _, style, _ := cl.s.Get(0, 0); style.GetBackground() != color.NewHexColor(0x101010) {
			t.Errorf("with %s the ground is %v, want base00", filepath.Base(path), style.GetBackground())
		}
	}
	// A file that is not a scheme says so, and fude draws in dark.
	cl := startWith(t, fmt.Sprintf("theme = %q", filepath.Join(dir, "none")))
	if cl.draw(); !strings.Contains(cl.e.Message(), "theme") {
		t.Errorf("a missing scheme said %q", cl.e.Message())
	}
}
