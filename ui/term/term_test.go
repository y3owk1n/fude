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
	return &client{s: mock(t), e: e}
}

// mock returns a screen on a mock terminal of 120 by 40.
func mock(t *testing.T) tcell.Screen {
	t.Helper()
	s, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40}))
	if err == nil {
		err = s.Init()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Fini)
	return s
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

// B197
func TestB197FrameAfterFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	text := "package a\n\nfunc f(x int) {\n\tif x > 0 {  \n\n\t\treturn\n\t}\n" +
		"\t// " + strings.Repeat("a long line that wraps ", 12) + "\n}\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "number = true\nrelative_number = true\ncursor_line = true\n", path)
	// step feeds keys, draws, and compares each cell with a first frame's.
	step := func(keys string) {
		t.Helper()
		for _, k := range vim.Keys(keys) {
			c.e.Feed(k)
		}
		c.draw()
		fresh := &client{s: mock(t), e: c.e}
		fresh.draw()
		for y := range 40 {
			for x := range 120 {
				s, style, _ := c.s.Get(x, y)
				if fs, fstyle, _ := fresh.s.Get(x, y); s != fs || style != fstyle {
					t.Fatalf("after %s the cell at %d,%d is %q, and %q in a first frame", keys, x, y, s, fs)
				}
			}
		}
	}
	// keyword reports whether row y shows a keyword.
	keyword := func(y int) bool {
		for x := range 120 {
			if _, style, _ := c.s.Get(x, y); style.GetForeground() == themes["dark"].keyword {
				return true
			}
		}
		return false
	}
	// parsed runs queued work, such as a new parse, with a step after each,
	// until done reports true.
	parsed := func(done func() bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for step(""); !done(); step("") {
			select {
			case f := <-c.e.Queue():
				f()
			case <-deadline:
				t.Fatal("the parse never showed")
			}
		}
	}
	parsed(func() bool { return keyword(0) })
	for _, keys := range []string{
		"jj", "f{", "%", "*", "j", "*", "ggjj", "Vjj", "y", "<C-v>jjl", "y",
		"jjjihello<Esc>", "dd", "u", "Gk", "J", "u", "ggjA" + strings.Repeat(" more", 30) + "<Esc>", "u",
		"/return<CR>", "<<", "k", ":vsplit<CR>", "/that<CR>", "b", "rq", "<C-w>w", ":set list<CR>",
		":set nowrap<CR>", "/wraps<CR>", "j", "k", "$", "0", "x", "gg", ":w<CR>", "j", ":set nonumber<CR>",
	} {
		step(keys)
	}
	// The parse after a comment around the text recolours lines away from
	// the cursor.
	step("ggO/*<Esc>")
	step("Go*/<Esc>")
	parsed(func() bool { return !keyword(1) })
}

// B199
func TestB199Center(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "center = true\n", path)
	// On the 120 columns of the mock terminal, 100 columns of text start at
	// 10. With a colour column of 50 they start at 35, and stay there as
	// line numbers of 4 columns come and a message shows. Two windows side
	// by side take 109 columns from 5, and three take the whole width.
	for _, step := range []struct {
		keys        string
		text, right int
	}{
		{"", 10, 110},
		{":set color_column=50<CR>", 35, 85},
		{":set number<CR>", 35, 85},
		{":set number?<CR>", 35, 85},
		{":vsplit<CR>", 9, 114},
		{":vsplit<CR>", 4, 120},
		{":center<CR>", 4, 120},
	} {
		for _, k := range vim.Keys(step.keys) {
			c.e.Feed(k)
		}
		c.draw()
		left := step.text - c.e.Views()[0].Gutter
		for y := range 40 {
			for x := range 120 {
				if s, _, _ := c.s.Get(x, y); (x < left || x >= step.right) && s != " " {
					t.Fatalf("after %q the margin at %d,%d shows %q", step.keys, x, y, s)
				}
			}
		}
		if s, _, _ := c.s.Get(step.text, 0); s != "t" {
			t.Errorf("after %q the text starts with %q at column %d", step.keys, s, step.text)
		}
		// Centered, the colour column at 50 is the edge, and shows no shade.
		if _, style, _ := c.s.Get(step.text+49, 0); step.right < 120 && style.GetBackground() == themes["dark"].raised {
			t.Errorf("after %q the centered window shades its colour column", step.keys)
		}
	}
	for _, k := range vim.Keys(":set color_column=200<CR>:center<CR>") {
		c.e.Feed(k)
	}
	if !strings.Contains(c.e.Message(), "too narrow") {
		t.Errorf("centering 200 columns on 120 said %q", c.e.Message())
	}
	// :restart keeps the windows centered.
	for _, k := range vim.Keys(":only<CR>:set color_column=60<CR>ZR") {
		c.e.Feed(k)
	}
	t.Setenv(editor.RestartEnv, c.e.Restart())
	c = startWith(t, "")
	c.draw()
	if s, _, _ := c.s.Get(30, 0); s != "t" {
		t.Errorf("after :restart the text starts with %q at column 30", s)
	}
}

// B200
func TestB200ColorColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("text\n\nmore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "color_column = 3\ncursor_line = true\n", path)
	c.draw()
	ground := func(x, y int) color.Color {
		_, style, _ := c.s.Get(x, y)
		return style.GetBackground()
	}
	for y := 1; y < 3; y++ {
		if ground(2, y) != themes["dark"].raised || ground(1, y) != themes["dark"].bg {
			t.Errorf("on line %d the colour column is %v and the column before it %v", y+1, ground(2, y), ground(1, y))
		}
	}
	if ground(2, 0) != themes["dark"].cursorLine {
		t.Errorf("on the cursor's line the colour column is %v, want the line's shade", ground(2, 0))
	}
}

// B64, for a menu longer than its box
func TestB64MenuScrolls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	var words []string
	for i := range 30 {
		words = append(words, fmt.Sprintf("w%02d", i))
	}
	if err := os.WriteFile(path, []byte(strings.Join(words, " ")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	for _, k := range vim.Keys("o w<C-x><C-o>" + strings.Repeat("<C-n>", 24)) {
		c.e.Feed(k)
	}
	c.draw()
	buf := c.e.Text()
	chosen := strings.TrimSpace(buf.Slice(buf.LineStart(1), buf.Len()))
	for y := range 40 {
		var row strings.Builder
		for x := range 120 {
			s, _, _ := c.s.Get(x, y)
			row.WriteString(s)
		}
		if strings.Contains(row.String(), chosen+"  [buffer]") {
			return
		}
	}
	t.Errorf("the 25th item, %s, is not in the menu on screen", chosen)
}

// B208
func TestB208Colours(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a #ff0000 rgb(0, 255, 0) hsl(240, 100%, 50%) #12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The cursor's line keeps the colours.
	c := startWith(t, "cursor_line = true\n", path)
	c.draw()
	for _, cell := range []struct {
		x       int
		bg, ink color.Color
	}{{2, color.NewHexColor(0xff0000), color.White}, {10, color.NewHexColor(0x00ff00), color.Black}, {26, color.NewHexColor(0x0000ff), color.White}} {
		if _, style, _ := c.s.Get(cell.x, 0); style.GetBackground() != cell.bg || style.GetForeground() != cell.ink {
			t.Errorf("the colour at column %d is drawn on %v in %v, want %v in %v", cell.x, style.GetBackground(), style.GetForeground(), cell.bg, cell.ink)
		}
	}
	if _, style, _ := c.s.Get(46, 0); style.GetBackground() != themes["dark"].cursorLine {
		t.Errorf("#12, no colour, is drawn on %v", style.GetBackground())
	}
	c = startWith(t, "highlight_colors = false\n", path)
	c.draw()
	if _, style, _ := c.s.Get(2, 0); style.GetBackground() != themes["dark"].bg {
		t.Errorf("with highlight_colors off, #ff0000 is drawn on %v", style.GetBackground())
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
