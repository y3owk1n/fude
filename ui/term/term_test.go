package term

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	// No test fetches a grammar.
	if err := os.WriteFile(filepath.Join(dir, "fude", "config.toml"), []byte("auto_install = false\n"+config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
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
	return &client{s: s, e: e}
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

// B147
func TestB147StatusLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl := start(t, path)
	row := func() string {
		cl.draw()
		var b strings.Builder
		w, h := cl.s.Size()
		for x := 0; x < w; x++ {
			s, _, _ := cl.s.Get(x, h-1)
			b.WriteString(s)
		}
		return b.String()
	}
	if got := row(); !strings.HasPrefix(got, " NORMAL ") || !strings.Contains(got, "notes.txt") || !strings.Contains(got, "1:1") {
		t.Fatalf("the status line is %q", got)
	}
	feed := func(keys string) {
		for _, k := range vim.Keys(keys) {
			cl.e.Feed(k)
		}
	}
	if feed("ix"); !strings.HasPrefix(row(), " INSERT ") {
		t.Fatalf("in insert mode the status line is %q", row())
	}
	if feed("<Esc>"); !strings.Contains(row(), "notes.txt ●") {
		t.Fatalf("after a change the status line is %q", row())
	}
}

// B148, B149
func TestB148CommandLine(t *testing.T) {
	cl := start(t)
	screen := func() string {
		cl.draw()
		var b strings.Builder
		w, h := cl.s.Size()
		for y := range h {
			for x := range w {
				s, _, _ := cl.s.Get(x, y)
				b.WriteString(s)
			}
			b.WriteByte('\n')
		}
		return b.String()
	}
	feed := func(keys string) {
		for _, k := range vim.Keys(keys) {
			cl.e.Feed(k)
		}
	}
	// The line floats in a box in the upper middle, with its menu below.
	feed(":ti<Tab>")
	rows := strings.Split(screen(), "\n")
	w, h := cl.s.Size()
	at := slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, ": timeline") })
	below := slices.IndexFunc(rows[at+1:], func(r string) bool { return strings.Contains(r, "timeline") })
	if at < 1 || at > h/2 || !strings.Contains(rows[at-1], "╭") || below < 0 {
		t.Fatalf("the command line is on row %d of:\n%s", at, strings.Join(rows, "\n"))
	}
	// A message is a toast in the top right corner.
	feed("<Esc>:set number?<CR>")
	if rows = strings.Split(screen(), "\n"); !strings.HasSuffix(strings.TrimRight(rows[1], " │"), "nonumber") || len(rows[1]) < w {
		t.Fatalf("the toast is not in the top right:\n%s", strings.Join(rows[:3], "\n"))
	}
	// A long message wraps onto more lines instead of being cut.
	long := strings.Repeat("word ", 30) + "end"
	feed(":" + long + "<CR>")
	if s := screen(); !strings.Contains(s, "word end") {
		t.Fatalf("the long message lost its end:\n%s", strings.Join(strings.Split(s, "\n")[:6], "\n"))
	}
}

// B150
func TestB150Panels(t *testing.T) {
	cl := start(t)
	for _, k := range vim.Keys(":ti<Tab>") {
		cl.e.Feed(k)
	}
	cl.draw()
	w, h := cl.s.Size()
	for y := range h {
		for x := range w {
			if s, style, _ := cl.s.Get(x, y); s == "╭" {
				// The border and the inside are on one ground, so no seam shows
				// between them.
				if _, in, _ := cl.s.Get(x+1, y+1); style.GetBackground() != in.GetBackground() {
					t.Fatalf("the border's ground is %v and the inside's %v", style.GetBackground(), in.GetBackground())
				}
				// The selected completion has the bar for its border.
				if bar, _, _ := cl.s.Get(x, y+3); bar != "▌" {
					t.Fatalf("the selected row starts with %q", bar)
				}
				return
			}
		}
	}
	t.Fatal("no panel on the screen")
}

// B151
func TestB151Tint(t *testing.T) {
	cl := start(t)
	cl.draw()
	_, before, _ := cl.s.Get(0, 0)
	// The key hints leave the windows lit.
	cl.e.Feed("g")
	cl.e.ShowHints()
	cl.draw()
	if _, under, _ := cl.s.Get(0, 0); under.GetBackground() != before.GetBackground() {
		t.Fatalf("under the key hints the ground is %v", under.GetBackground())
	}
	cl.e.Feed("<Esc>")
	for _, k := range vim.Keys(":ab") {
		cl.e.Feed(k)
	}
	cl.draw()
	// The windows under the command line are on a darker ground.
	_, under, _ := cl.s.Get(0, 0)
	if under.GetBackground() == before.GetBackground() {
		t.Fatalf("under the command line the ground stays %v", under.GetBackground())
	}
	// The box is on the same ground, with its text at full strength.
	w, h := cl.s.Size()
	for y := range h {
		for x := range w {
			if s, style, _ := cl.s.Get(x, y); s == "a" {
				if style.GetBackground() != under.GetBackground() || style.GetForeground() != th.text {
					t.Fatalf("the typed text is %v on %v", style.GetForeground(), style.GetBackground())
				}
				return
			}
		}
	}
	t.Fatal("the typed text is not on the screen")
}

// B153
func TestB153IndentGuides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("a\n\tb\n\n\tc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl := start(t, path)
	cl.draw()
	cell := func(x, y int) string { s, _, _ := cl.s.Get(x, y); return s }
	// An indented line and the blank line before the next one show a guide.
	if cell(0, 1) != "│" || cell(0, 2) != "│" || cell(0, 0) != "a" {
		t.Fatalf("the first column reads %q %q %q", cell(0, 0), cell(0, 1), cell(0, 2))
	}
	for _, k := range vim.Keys(":set noindent_guides<CR>") {
		cl.e.Feed(k)
	}
	if cl.draw(); cell(0, 1) != " " {
		t.Fatalf("with noindent_guides the first column has %q", cell(0, 1))
	}
}

// B158
func TestB158SwitchTheme(t *testing.T) {
	cl := start(t)
	ground := func() color.Color {
		cl.draw()
		_, style, _ := cl.s.Get(0, 0)
		return style.GetBackground()
	}
	feed := func(keys string) {
		for _, k := range vim.Keys(keys) {
			cl.e.Feed(k)
		}
	}
	if feed(":theme light<CR>"); ground() != themes["light"].bg {
		t.Fatalf(":theme light drew the ground %v", ground())
	}
	if feed(":theme<CR>"); cl.e.Message() != "light" {
		t.Errorf(":theme said %q", cl.e.Message())
	}
	// A scheme that does not load says why and keeps the theme.
	feed(":theme " + filepath.Join(t.TempDir(), "none") + "<CR>")
	if !strings.Contains(cl.e.Message(), "theme") || ground() != themes["light"].bg {
		t.Errorf("a missing scheme said %q and drew the ground %v", cl.e.Message(), ground())
	}
	// <Tab> offers the scheme config.toml names, to switch back to it.
	path := filepath.Join(t.TempDir(), "palette.json")
	var j strings.Builder
	for i := range 16 {
		fmt.Fprintf(&j, "\"base0%X\": \"%06x\",\n", i, 0x101010*(i%15+1))
	}
	if err := os.WriteFile(path, []byte(j.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cl = startWith(t, fmt.Sprintf("theme = %q", path))
	if feed(":theme light<CR>:theme pal<Tab><CR>"); ground() != color.NewHexColor(0x101010) {
		t.Errorf(":theme pal<Tab> drew the ground %v, want the scheme's", ground())
	}
}

// B159
func TestB159LspPanelScrolls(t *testing.T) {
	// The config sets more cards than the screen has rows.
	var config strings.Builder
	for i := range 20 {
		fmt.Fprintf(&config, "[filetype.t%02d]\nlsp = [\"server%02d\"]\n", i, i)
	}
	cl := startWith(t, config.String())
	shows := func(name string) bool {
		cl.draw()
		w, h := cl.s.Size()
		var screen strings.Builder
		for y := range h {
			for x := range w {
				s, _, _ := cl.s.Get(x, y)
				screen.WriteString(s)
			}
		}
		return strings.Contains(screen.String(), name)
	}
	for _, k := range vim.Keys(":lsp<CR>") {
		cl.e.Feed(k)
	}
	if !shows("server00") {
		t.Fatal("the panel does not show its first card")
	}
	for _, k := range vim.Keys(strings.Repeat("j", 19)) {
		cl.e.Feed(k)
	}
	if !shows("server19") {
		t.Fatal("the panel does not scroll to its last card")
	}
}
