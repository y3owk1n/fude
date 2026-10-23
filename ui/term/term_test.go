package term

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/y3owk1n/fude/api"
	fudeclient "github.com/y3owk1n/fude/client"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/internal/clipboard"
	"github.com/y3owk1n/fude/internal/relay"
	bufferlinemod "github.com/y3owk1n/fude/modules/bufferline"
	buffersmod "github.com/y3owk1n/fude/modules/buffers"
	colourmod "github.com/y3owk1n/fude/modules/colours"
	completionmod "github.com/y3owk1n/fude/modules/completion"
	filesmod "github.com/y3owk1n/fude/modules/files"
	guidesmod "github.com/y3owk1n/fude/modules/guides"
	hintsmod "github.com/y3owk1n/fude/modules/hints"
	lspmod "github.com/y3owk1n/fude/modules/lsp"
	sessionsmod "github.com/y3owk1n/fude/modules/sessions"
	stickymod "github.com/y3owk1n/fude/modules/sticky"
	terminalmod "github.com/y3owk1n/fude/modules/terminal"
	thememod "github.com/y3owk1n/fude/modules/theme"
	timelinemod "github.com/y3owk1n/fude/modules/timeline"
	windowmod "github.com/y3owk1n/fude/modules/window"
	"github.com/y3owk1n/fude/syntax/syntaxtest"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// An in-process session is one the terminal client draws (D39).
var _ fudeclient.Session = (*editor.Session)(nil)

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
	data := t.TempDir()
	if err := syntaxtest.InstallGo(data); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	e, err := editor.Start(args)
	if err != nil {
		t.Fatal(err)
	}
	// Quitting when the test ends stops what the editor started before its
	// temp directories go.
	t.Cleanup(func() {
		if e.Done() {
			return
		}
		for _, k := range vim.Keys("<Esc><Esc>:qa!<CR>") {
			e.Feed(k)
		}
	})
	return &client{s: mock(t), e: e}
}

// mock returns a screen on a mock terminal of 120 by 40.
func mock(t *testing.T) tcell.Screen {
	t.Helper()
	return screenOn(t, vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40}))
}

// screenOn returns a screen on the mock terminal mt.
func screenOn(t *testing.T, mt vt.MockTerm) tcell.Screen {
	t.Helper()
	s, err := tcell.NewTerminfoScreenFromTty(mt)
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
	s, err := screen(typed{in}, nil)
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
			case <-time.After(editortest.Patience):
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
	c := startWith(t, "number = true\nrelative_number = true\ncursor_line = true\n[filetype.txt]\ntab_width = 4\n", path)
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
		deadline := time.After(editortest.Patience)
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
		"/return<CR>", "<<", "k", ":vsplit<CR>", "/that<CR>", "b", "rq", "<C-w>w", ":set list true<CR>",
		":set wrap false<CR>", "/wraps<CR>", "j", "k", "$", "0", "x", "gg", ":w<CR>", "j", ":set number false<CR>",
	} {
		step(keys)
	}
	// The parse after a comment around the text recolours lines away from
	// the cursor.
	step("ggO/*<Esc>")
	step("Go*/<Esc>")
	parsed(func() bool { return !keyword(1) })
	// Renaming a file in an explorer beside it changes its tab width, and its
	// lines draw again.
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "b.txt"), []byte("x\ty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, keys := range []string{":w<CR>", ":only<CR>", ":e " + filepath.Join(filepath.Dir(path), "b.txt") + "<CR>", ":vsplit<CR>", "-", "Cb.text<Esc>", ":w<CR>", "y"} {
		step(keys)
	}
}

// B199
func TestB199Center(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "center = true\n", path)
	c.e.(*editor.Session).Use(windowmod.New())
	c.e.(*editor.Session).Use(thememod.New())
	// On the 120 columns of the mock terminal, 100 columns of text start at
	// 10. With a colour column of 50 they start at 35, and stay there as
	// line numbers of 4 columns come and a message shows. Two windows side
	// by side take 109 columns from 5, and three take the whole width.
	for _, step := range []struct {
		keys        string
		text, right int
	}{
		{"", 10, 110},
		{":set color_column 50<CR>", 35, 85},
		{":set number true<CR>", 35, 85},
		{":set number<CR>", 35, 85},
		{":vsplit<CR>", 9, 114},
		{":vsplit<CR>", 4, 120},
		{":center<CR>", 4, 120},
	} {
		for _, k := range vim.Keys(step.keys) {
			c.e.Feed(k)
		}
		c.draw()
		left := step.text - c.e.Windows()[0].Gutter
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
	for _, k := range vim.Keys(":set color_column 200<CR>:center<CR>") {
		c.e.Feed(k)
	}
	if !strings.Contains(c.e.Message(), "too narrow") {
		t.Errorf("centering 200 columns on 120 said %q", c.e.Message())
	}
	// :restart keeps the windows centered.
	for _, k := range vim.Keys(":only<CR>:set color_column 60<CR>ZR") {
		c.e.Feed(k)
	}
	t.Setenv(editor.RestartEnv, c.e.(*editor.Session).Restart())
	c = startWith(t, "")
	c.e.(*editor.Session).Use(windowmod.New())
	c.e.(*editor.Session).Use(thememod.New())
	c.draw()
	if s, _, _ := c.s.Get(30, 0); s != "t" {
		t.Errorf("after :restart the text starts with %q at column 30", s)
	}
}

// B23, for the line between windows side by side
func TestB23Separator(t *testing.T) {
	c := startWith(t, "")
	c.e.(*editor.Session).Use(windowmod.New())
	for _, k := range vim.Keys(":vsplit<CR>") {
		c.e.Feed(k)
	}
	c.draw()
	x := c.e.Windows()[1].X - 1
	if s, style, _ := c.s.Get(x, 0); s != "│" || style.GetForeground() != themes["dark"].border {
		t.Errorf("between the windows is %q in %v, want the border colour", s, style.GetForeground())
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
	c := startWith(t, "menu_height = 20\n", path)
	c.e.(*editor.Session).Use(completionmod.New())
	for _, k := range vim.Keys("o w<C-x><C-o>" + strings.Repeat("<C-n>", 24)) {
		c.e.Feed(k)
	}
	// selected draws a frame and returns the item in the text and the
	// screen row that shows it, or -1.
	selected := func() (string, int) {
		c.draw()
		buf := c.e.(*editor.Session).Text()
		chosen := strings.TrimSpace(buf.Slice(buf.LineStart(1), buf.Len()))
		for y := range 40 {
			var row strings.Builder
			for x := range 120 {
				s, _, _ := c.s.Get(x, y)
				row.WriteString(s)
			}
			if strings.Contains(row.String(), chosen+"  [buffer]") {
				return chosen, y
			}
		}
		return chosen, -1
	}
	chosen, y := selected()
	if y < 0 {
		t.Fatalf("the 25th item, %s, is not in the menu on screen", chosen)
	}
	// Going back up moves the selection, not the rows.
	c.e.Feed(vim.Keys("<C-p>")[0])
	if up, at := selected(); at != y-1 {
		t.Errorf("after <C-p> the 24th item, %s, is on row %d, want %d", up, at, y-1)
	}
}

// B345
func TestB345ModuleStyles(t *testing.T) {
	full, err := filepath.Abs("../../plugin/testdata/full/full.wasm")
	if err != nil {
		t.Fatal(err)
	}
	c := startWith(t, fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [\"ui\"]\n", full), "a.txt")
	c.e.Load()
	select {
	case f := <-c.e.Queue():
		f()
	case <-time.After(editortest.Patience):
		t.Fatal("the plugin never loaded")
	}
	for _, k := range vim.Keys("ia<Esc>:Paint<CR>") {
		c.e.Feed(k)
	}
	c.draw()
	if _, style, _ := c.s.Get(0, 0); style.GetForeground() != color.NewHexColor(0xff0000) || !style.HasBold() {
		t.Errorf("the byte in full.hot is drawn in %v, bold %t, want #ff0000 and bold", style.GetForeground(), style.HasBold())
	}
}

// B208
func TestB208Colours(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a #ff0000 rgb(0, 255, 0) hsl(240, 100%, 50%) #12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The cursor's line keeps the colours.
	c := startWith(t, "cursor_line = true\n", path)
	c.e.(*editor.Session).Use(colourmod.New())
	// The module's colours land through the loop.
	for deadline := time.After(editortest.Patience); ; {
		if c.draw(); func() bool { _, style, _ := c.s.Get(2, 0); return style.GetBackground() == color.NewHexColor(0xff0000) }() {
			break
		}
		select {
		case f := <-c.e.Queue():
			f()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("the colours never showed")
		}
	}
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
	c = startWith(t, "[module.colours]\nenabled = false\n", path)
	c.e.(*editor.Session).Use(colourmod.New())
	c.draw()
	if _, style, _ := c.s.Get(2, 0); style.GetBackground() != themes["dark"].bg {
		t.Errorf("with colours.enabled off, #ff0000 is drawn on %v", style.GetBackground())
	}
}

// heading is a module that shows "# " starting the second line as "§".
type heading struct{}

func (heading) Name() string                   { return "heading" }
func (heading) Registered() []api.Registration { return nil }
func (heading) Subscribed() []string           { return nil }
func (heading) Init(_ context.Context, s api.Scope) (api.Result, error) {
	p := s.Text.LineStart(1)
	return api.Result{Decorate: []api.Decoration{{ID: 1, Start: p, End: p + 2, Style: "markup.heading", Note: "§", Replace: true}}}, nil
}

func (heading) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

func (heading) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (heading) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (heading) Work(context.Context, api.Job) (string, error) { return "", nil }
func (heading) Close(context.Context) error                   { return nil }

// B295, drawn
func TestB295DrawReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(path, []byte("a\n# "+strings.Repeat("x", 125)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	c.e.(*editor.Session).Use(heading{})
	c.draw()
	row := func(y int) string {
		var b strings.Builder
		for x := range 120 {
			s, _, _ := c.s.Get(x, y)
			b.WriteString(s)
		}
		return strings.TrimRight(b.String(), " ")
	}
	// § takes the place of "# ", and the line wraps where § and 119 x's
	// fill the first row.
	if first, second := row(1), row(2); first != "§"+strings.Repeat("x", 119) || second != strings.Repeat("x", 6) {
		t.Errorf("the line draws as %q and %q", first, second)
	}
}

// stopped is a module that shades the second line as a debugger's stop.
type stopped struct{ heading }

func (stopped) Init(_ context.Context, s api.Scope) (api.Result, error) {
	p := s.Text.LineStart(1)
	return api.Result{Decorate: []api.Decoration{{ID: 1, Start: p, End: p + 1, Style: "debug.line", Line: true}}}, nil
}

// B217, B295: a pinned line shows its replacements.
func TestB217PinnedReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package p\nfunc F() {\n"+strings.Repeat("\tx()\n", 100)+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	c.e.(*editor.Session).Use(heading{})
	c.e.(*editor.Session).Use(stickymod.New())
	// The client sizes the window as it draws, which zt needs first.
	c.draw()
	for _, k := range vim.Keys("60Gzt3j") {
		c.e.Feed(k)
	}
	// The pinned line shows once the parse arrives through the loop.
	for deadline := time.After(editortest.Patience); ; {
		c.draw()
		var row strings.Builder
		for x := range 10 {
			s, _, _ := c.s.Get(x, 0)
			row.WriteString(s)
		}
		if row.String() == "§nc F() { " {
			break
		}
		select {
		case f := <-c.e.Queue():
			f()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("the pinned row shows %q", row.String())
		}
	}
}

// ghosted is a module that shows ghost text before the first line's second
// byte.
type ghosted struct{ heading }

func (ghosted) Init(context.Context, api.Scope) (api.Result, error) {
	return api.Result{Decorate: []api.Decoration{{ID: 1, Start: 1, End: 1, Style: "proposal", Note: "xy", Insert: true}}}, nil
}

// B384, B216: the cursor shows before ghost text at it in insert mode, and
// on the character after it in normal mode.
func TestB384DrawGhostCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("ab\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for keys, want := range map[string]int{"a": 1, "l": 3} {
		mt := vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40})
		c := start(t, path)
		c.s = screenOn(t, mt)
		c.e.(*editor.Session).Use(ghosted{})
		for _, k := range vim.Keys(keys) {
			c.e.Feed(k)
		}
		c.draw()
		if row := c.row(0); !strings.HasPrefix(row, "axyb") {
			t.Fatalf("the line draws as %q", row)
		}
		if got := mt.Pos(); int(got.X) != want || got.Y != 0 {
			t.Errorf("after %q the cursor shows at %v, want column %d", keys, got, want)
		}
	}
}

// spaced is a module that draws a row of its own below the second line.
type spaced struct{ heading }

func (spaced) Init(_ context.Context, s api.Scope) (api.Result, error) {
	p := s.Text.LineStart(1)
	return api.Result{Decorate: []api.Decoration{{ID: 1, Start: p, End: p, Style: "markdown.rule", Lines: []string{"──", "\t─"}}}}, nil
}

// B299, drawn
func TestB299DrawVirtualRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	c.e.(*editor.Session).Use(spaced{})
	c.draw()
	// The rows draw between the lines, and the third line goes down two. A
	// tab in a row takes the columns to the next tab stop.
	for y, want := range []string{"a", "b", "──", strings.Repeat(" ", 8) + "─", "c"} {
		var row strings.Builder
		for x := range 10 {
			s, _, _ := c.s.Get(x, y)
			row.WriteString(s)
		}
		if strings.TrimRight(row.String(), " ") != want {
			t.Errorf("row %d shows %q, want %q", y, row.String(), want)
		}
	}
}

// pixels is a terminal with cells of 8 by 16 pixels.
type pixels struct{ tcell.Tty }

func (pixels) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: 120, Height: 40, PixelWidth: 960, PixelHeight: 640}, nil
}

// framed is a module that shows the image at path below the first line.
type framed struct {
	heading
	path string
}

func (m framed) Init(context.Context, api.Scope) (api.Result, error) {
	return api.Result{Decorate: []api.Decoration{{ID: 1, Image: m.path}}}, nil
}

// B303, drawn
func TestB303DrawImage(t *testing.T) {
	dir := t.TempDir()
	pic := filepath.Join(dir, "a.png")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pic, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	var sent bytes.Buffer
	c.img = images{tty: pixels{}, out: &sent, mode: placeholders}
	c.e.(*editor.Session).Use(framed{path: pic})
	c.e.(*editor.Session).Use(buffersmod.New())
	// The image loads off the loop, and its rows show once it lands.
	cell := func(x, y int) (string, tcell.Style) { s, style, _ := c.s.Get(x, y); return s, style }
	for deadline := time.After(editortest.Patience); ; {
		if c.draw(); strings.HasPrefix(func() string { s, _ := cell(0, 1); return s }(), string(kitty.Placeholder)) {
			break
		}
		select {
		case f := <-c.e.Queue():
			f()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("the image never showed")
		}
	}
	// Its 80 by 32 pixels take 10 columns and two rows of placeholders, in
	// the colour of its id, and the terminal got it once, sized so.
	id := c.img.ids[[3]int{c.e.Windows()[0].Virtual[0][0].Image, 2, 10}]
	ink := color.NewRGBColor(int32(id>>16&0xff), int32(id>>8&0xff), int32(id&0xff))
	for _, at := range []struct{ x, y, row, col int }{{0, 1, 0, 0}, {9, 2, 1, 9}} {
		want := string([]rune{kitty.Placeholder, kitty.Diacritic(at.row), kitty.Diacritic(at.col)})
		if s, style := cell(at.x, at.y); s != want || style.GetForeground() != ink {
			t.Errorf("the cell at %d,%d holds %q in %v", at.x, at.y, s, style.GetForeground())
		}
	}
	if s, _ := cell(10, 1); strings.HasPrefix(s, string(kitty.Placeholder)) {
		t.Error("the image is wider than 10 columns")
	}
	if got := sent.String(); strings.Count(got, "a=T") != 1 || !strings.Contains(got, fmt.Sprintf("i=%d,c=10,r=2", id)) || !strings.Contains(got, "U=1") {
		t.Errorf("the terminal got %q", got[:min(len(got), 120)])
	}
	if c.draw(); strings.Count(sent.String(), "a=T") != 1 {
		t.Error("the next frame sent the image again")
	}
	// Line b shows under the image's rows.
	if s, _ := cell(0, 3); s != "b" {
		t.Errorf("under the image the row shows %q", s)
	}
	// Closing the buffer has the terminal drop the image (B306).
	for _, k := range vim.Keys(":bd<CR>") {
		c.e.Feed(k)
	}
	if c.draw(); !strings.Contains(sent.String(), fmt.Sprintf("a=d,d=I,i=%d", id)) {
		t.Error("after :bd the terminal did not drop the image")
	}
}

// B305, drawn
func TestB305DrawPreviewImage(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.png"), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	c := startWith(t, "", "a.png")
	c.e.(*editor.Session).Use(filesmod.New())
	c.img = images{tty: pixels{}, out: new(bytes.Buffer), mode: placeholders}
	c.draw()
	for _, k := range vim.Keys(":pick files<CR>") {
		c.e.Feed(k)
	}
	// The image's first cell is two columns into the preview's box, four
	// rows down, under the path, its rule and the line naming the image.
	for deadline := time.After(editortest.Patience); ; {
		c.draw()
		f, _ := c.e.Menu()
		if s, _, _ := c.s.Get(f.X+f.W+2, f.Y+4); f.DocImage != nil && strings.HasPrefix(s, string(kitty.Placeholder)) {
			break
		}
		select {
		case fn := <-c.e.Queue():
			fn()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("the preview never showed the image")
		}
	}
}

// hovered is a module whose :Hover shows markdown with an image in a float.
type hovered struct{ heading }

func (hovered) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "Hover", Help: "shows an image in a float", ID: 1}}
}

func (hovered) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (hovered) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{Float: &api.Float{Text: "see ![pic](a.png)"}}, nil
}

// B309, drawn
func TestB309DrawFloatImage(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.png"), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	c.img = images{tty: pixels{}, out: new(bytes.Buffer), mode: placeholders}
	c.e.(*editor.Session).Use(hovered{})
	c.draw()
	for _, k := range vim.Keys(":Hover<CR>") {
		c.e.Feed(k)
	}
	// The float opens under the cursor's line, its text on its first row
	// and the image's first cell under it, two columns in.
	for deadline := time.After(editortest.Patience); ; {
		c.draw()
		if s, _, _ := c.s.Get(2, 3); strings.HasPrefix(s, string(kitty.Placeholder)) {
			break
		}
		select {
		case fn := <-c.e.Queue():
			fn()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			var row strings.Builder
			for x := range 20 {
				s, _, _ := c.s.Get(x, 3)
				row.WriteString(s)
			}
			t.Fatalf("under the float's text the row shows %q", row.String())
		}
	}
}

// B310
func TestB310PaintImage(t *testing.T) {
	dir := t.TempDir()
	pic := filepath.Join(dir, "a.png")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pic, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		mode       int
		sent, show string
	}{
		{inline, "", "\x1b]1337;File=inline=1;"},
		{placements, "a=t,f=100", "\x1b_Ga=p,i="},
	} {
		cl := startWith(t, "", path)
		var out bytes.Buffer
		cl.img = images{tty: pixels{}, out: &out, mode: c.mode}
		cl.e.(*editor.Session).Use(framed{path: pic})
		// The image paints at its first cell, under line a, once its rows
		// show, and the frames after leave it.
		for deadline := time.After(editortest.Patience); !strings.Contains(out.String(), c.show); {
			cl.draw()
			select {
			case f := <-cl.e.Queue():
				f()
			case <-time.After(10 * time.Millisecond):
			case <-deadline:
				t.Fatalf("in mode %d the image never painted: %q", c.mode, out.String()[:min(out.Len(), 120)])
			}
		}
		got := out.String()
		at := strings.Index(got, c.show)
		if !strings.HasSuffix(got[:at], "\x1b7\x1b[2;1H") || !strings.Contains(got[at:], "2") || !strings.Contains(got, c.sent) {
			t.Errorf("in mode %d the terminal got %q", c.mode, got[:min(len(got), 200)])
		}
		if cl.draw(); strings.Count(out.String(), c.show) != 1 {
			t.Errorf("in mode %d the next frame painted the image again", c.mode)
		}
		// A float over the image unlocks its cells, so the float draws over
		// it.
		cl.e.(*editor.Session).Use(hovered{})
		for _, k := range vim.Keys(":Hover<CR>") {
			cl.e.Feed(k)
		}
		for deadline := time.After(editortest.Patience); slices.ContainsFunc(cl.img.painted, func(o overlay) bool { return o.x == 0 && o.y == 1 }); {
			cl.draw()
			select {
			case f := <-cl.e.Queue():
				f()
			case <-time.After(10 * time.Millisecond):
			case <-deadline:
				t.Fatalf("in mode %d the image stayed painted under the float", c.mode)
			}
		}
	}
}

// B296
func TestB296LineGround(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a\nbc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	c.e.(*editor.Session).Use(stopped{})
	c.draw()
	// The ground is under the text and reaches the window's edge.
	for _, x := range []int{0, 100} {
		if _, style, _ := c.s.Get(x, 1); style.GetBackground() != th.selection {
			t.Errorf("column %d of the shaded line is on %v", x, style.GetBackground())
		}
	}
	if _, style, _ := c.s.Get(100, 0); style.GetBackground() != th.bg {
		t.Errorf("the line above is on %v", style.GetBackground())
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
	deadline := time.After(editortest.Patience)
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

// B294
func TestB294Colors(t *testing.T) {
	cl := startWith(t, "[colors]\nbg = \"#123456\"\n")
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
	if ground() != color.NewHexColor(0x123456) {
		t.Fatalf("with bg set the ground is %v", ground())
	}
	// :config reload takes a new colour, and a value that is not one changes
	// nothing and says where it is.
	file := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "config.toml")
	for _, c := range []struct {
		src  string
		want int32
	}{{"[colors]\nbg = \"#654321\"\n", 0x654321}, {"[colors]\nbg = \"red\"\n", 0x654321}} {
		if err := os.WriteFile(file, []byte(c.src), 0o600); err != nil {
			t.Fatal(err)
		}
		if feed(":config reload<CR>"); ground() != color.NewHexColor(c.want) {
			t.Errorf("after reloading %q the ground is %v, saying %q", c.src, ground(), cl.e.Message())
		}
	}
	if msg := cl.e.Message(); !strings.Contains(msg, "line 2") || !strings.Contains(msg, "colors.bg") {
		t.Errorf("a colour that is not one said %q", msg)
	}
	// :set takes a colour by its role too.
	if feed(":set colors.bg #abcdef<CR>"); ground() != color.NewHexColor(0xabcdef) {
		t.Errorf("after :set colors.bg #abcdef the ground is %v", ground())
	}
	if feed(":set colors.bg red<CR>"); cl.e.Message() != `colors.bg takes #rrggbb, not "red"` || ground() != color.NewHexColor(0xabcdef) {
		t.Errorf(":set colors.bg red said %q, leaving the ground %v", cl.e.Message(), ground())
	}
	// A file that no longer sets it gives back the theme's.
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if feed(":config reload<CR>"); ground() != themes["dark"].bg {
		t.Errorf("after reloading a file without [colors] the ground is %v", ground())
	}
}

// B147
func TestB147StatusLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl := start(t, path)
	cl.e.(*editor.Session).Use(windowmod.New())
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
	// Another window's status line has the same ground.
	feed(":vsplit<CR>")
	cl.draw()
	_, h := cl.s.Size()
	if _, style, _ := cl.s.Get(1, h-1); style.GetBackground() != themes["dark"].surface {
		t.Errorf("another window's status line is on %v", style.GetBackground())
	}
	// Each wide character of a name takes two cells. When the name leaves no
	// room, the status line leaves out its right side instead of drawing it
	// over the name.
	dir := t.TempDir()
	t.Chdir(dir)
	wide := strings.Repeat("世", 60) + ".txt"
	if err := os.WriteFile(filepath.Join(dir, wide), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl = start(t, wide)
	if got := row(); strings.Contains(got, "1:1") {
		t.Errorf("the right side drew over a wide name: %q", got)
	}
}

// B148, B149
func TestB148CommandLine(t *testing.T) {
	cl := start(t)
	cl.e.(*editor.Session).Use(timelinemod.New())
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
	feed("<Esc>:set number<CR>")
	if rows = strings.Split(screen(), "\n"); !strings.HasSuffix(strings.TrimRight(rows[1], " │"), "number false") || len(rows[1]) < w {
		t.Fatalf("the toast is not in the top right:\n%s", strings.Join(rows[:3], "\n"))
	}
	// A long message wraps onto more lines instead of being cut.
	long := strings.Repeat("word ", 30) + "end"
	feed(":" + long + "<CR>")
	if s := screen(); !strings.Contains(s, "word end") {
		t.Fatalf("the long message lost its end:\n%s", strings.Join(strings.Split(s, "\n")[:6], "\n"))
	}
	// The cursor stands after a wide character as after two narrow ones.
	term := vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40})
	s, err := tcell.NewTerminfoScreenFromTty(term)
	if err == nil {
		err = s.Init()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Fini)
	cl.s = s
	cursor := func(keys string) vt.Coord {
		feed("<Esc>" + keys)
		cl.draw()
		return term.Pos()
	}
	if narrow, wide := cursor(":ab"), cursor(":世"); narrow != wide {
		t.Errorf("after :世 the cursor is at %v, and after :ab at %v", wide, narrow)
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
	cl := startWith(t, "[module.hints]\ndelay = 1\n")
	cl.e.(*editor.Session).Use(hintsmod.New())
	cl.draw()
	_, before, _ := cl.s.Get(0, 0)
	// The key hints leave the windows lit.
	cl.e.Feed("g")
	for deadline := time.After(editortest.Patience); ; {
		if _, ok := cl.e.Menu(); ok {
			break
		}
		select {
		case f := <-cl.e.Queue():
			f()
		case <-deadline:
			t.Fatal("no key hints after g")
		}
	}
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

// B362
func TestB362Confirm(t *testing.T) {
	// A short name keeps the question on one row, which a long temporary
	// path, such as Windows', would wrap.
	t.Chdir(t.TempDir())
	path := "f"
	editortest.Change(t, path, "one\n")
	cl := start(t, path)
	feed := func(keys string) {
		for _, k := range vim.Keys(keys) {
			cl.e.Feed(k)
		}
	}
	feed("x")
	editortest.Change(t, path, "disk\n")
	feed(":w<CR>")
	// The question is in a box in the middle of the screen, with its keys.
	cl.draw()
	w, h := cl.s.Size()
	rows := make([]string, h)
	for y := range h {
		for x := range w {
			s, _, _ := cl.s.Get(x, y)
			rows[y] += s
		}
	}
	at := slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, "write anyway?") })
	if at < h/4 || at > h*3/4 || !strings.Contains(rows[at+2], "y yes   n no") {
		t.Fatalf("the question is on row %d of:\n%s", at, strings.Join(rows, "\n"))
	}
	// Only y and n answer it.
	if feed("x<Esc>:q<CR>"); cl.e.Confirm() == "" || cl.e.Done() {
		t.Fatal("a key other than y or n answered the question")
	}
	if feed("n"); cl.e.Confirm() != "" {
		t.Fatalf("n left the question %q up", cl.e.Confirm())
	}
}

// B153
func TestB153IndentGuides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("a\n\tb\n\n\tc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl := start(t, path)
	cl.e.(*editor.Session).Use(guidesmod.New())
	cell := func(x, y int) string { s, _, _ := cl.s.Get(x, y); return s }
	// An indented line and the blank line before the next one show a guide,
	// once the module's answer comes through the loop.
	for deadline := time.After(editortest.Patience); ; {
		if cl.draw(); cell(0, 1) == "│" && cell(0, 2) == "│" && cell(0, 0) == "a" {
			break
		}
		select {
		case f := <-cl.e.Queue():
			f()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("the first column reads %q %q %q", cell(0, 0), cell(0, 1), cell(0, 2))
		}
	}
	// list hides them.
	for _, k := range vim.Keys(":set list true<CR>") {
		cl.e.Feed(k)
	}
	if cl.draw(); cell(0, 2) != " " {
		t.Fatalf("with list on the blank line has %q", cell(0, 2))
	}
}

// B158
func TestB158SwitchTheme(t *testing.T) {
	cl := start(t)
	cl.e.(*editor.Session).Use(thememod.New())
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
	cl.e.(*editor.Session).Use(lspmod.New())
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
		t.Fatal("the panel does not show its first server")
	}
	// The float has the focus, so its bottom border holds its status line
	// and the mode, which no other window shows (B364).
	if !shows("╰ NORMAL ─") {
		t.Error("the panel's bottom border does not show its status line")
	}
	for _, k := range vim.Keys("G") {
		cl.e.Feed(k)
	}
	// It scrolls as any window does, and the keys stay at its foot.
	if !shows("t19 files") || !shows("q close") {
		t.Error("the panel does not scroll to its last server with the keys at its foot")
	}
}

// serve serves e on a socket, running its loop off the test's goroutine.
// It returns the socket, a way to run a func on that loop, and a way to run
// a func as the loop's last, as fude's loop ends once its terminal does.
// The loop stops before the test's other cleanups run.
func serve(t *testing.T, e *editor.Session) (string, func(func()), func(func())) {
	t.Helper()
	// A socket path must be short, so it goes in a directory of its own.
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	t.Cleanup(e.Serve(path))
	do, last, stop, stopped := make(chan func()), make(chan func()), make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-stopped })
	go func() {
		defer close(stopped)
		for {
			select {
			case f := <-e.Queue():
				f()
			case f := <-do:
				f()
			case f := <-last:
				f()
				return
			case <-stop:
				return
			}
		}
	}()
	return path, func(f func()) {
			ran := make(chan struct{})
			do <- func() { f(); close(ran) }
			<-ran
		}, func(f func()) {
			last <- f
			<-stopped
		}
}

// shows runs what the frames bring and draws, until the screen shows want
// on row y.
func (c *client) shows(t *testing.T, y int, want string) {
	t.Helper()
	deadline := time.After(editortest.Patience)
	for {
		c.draw()
		if strings.Contains(c.row(y), want) {
			return
		}
		select {
		case f := <-c.e.Queue():
			f()
		case <-deadline:
			t.Fatalf("row %d is %q, want %q in it", y, c.row(y), want)
		}
	}
}

// row returns the text row y of the screen shows.
func (c *client) row(y int) string {
	var b strings.Builder
	for x := range 120 {
		s, _, _ := c.s.Get(x, y)
		b.WriteString(s)
	}
	return b.String()
}

// B239, B242
func TestB239Attach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := startWith(t, "", path).e.(*editor.Session)
	socket, on, halt := serve(t, host)
	a, err := fudeclient.Attach(socket, "pat")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	c := &client{s: mock(t), e: a}
	// It draws the session as fude's own terminal does, and its keys edit
	// fude's buffer.
	c.shows(t, 0, "hello")
	c.shows(t, 39, "a.txt")
	for _, k := range vim.Keys("A!<Esc>") {
		a.Feed(k)
	}
	c.shows(t, 0, "hello!")
	var text string
	on(func() { s := host.Text(); text = s.Slice(0, s.Len()) })
	if text != "hello!" {
		t.Errorf("the attached client's keys left fude's buffer %q", text)
	}
	// A client whose connection breaks ends, and says why.
	broken, err := fudeclient.Attach(socket, "pat")
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	for deadline := time.After(editortest.Patience); !broken.Done(); {
		select {
		case f := <-broken.Queue():
			f()
		case <-deadline:
			t.Fatal("a client whose connection broke never ended")
		}
	}
	if !strings.Contains(broken.Ended(), "the connection to fude broke") {
		t.Errorf("a client whose connection broke said %q", broken.Ended())
	}
	// When fude quits, it says so, though its loop ends with its terminal.
	halt(func() {
		for _, k := range vim.Keys(":qa!<CR>") {
			host.Feed(k)
		}
	})
	host.Finish(time.Second)
	for deadline := time.After(editortest.Patience); !a.Done(); {
		select {
		case f := <-a.Queue():
			f()
		case <-deadline:
			t.Fatal("the attached client never ended")
		}
	}
	if a.Ended() != "fude quit" {
		t.Errorf("when fude quit, the client said %q", a.Ended())
	}
}

// share makes host share on a port of the loopback, or through the relay
// at via, with a clipboard of its own, and returns its share string.
func share(t *testing.T, host *editor.Session, on func(func()), via string) string {
	t.Helper()
	board := filepath.Join(t.TempDir(), "board")
	t.Cleanup(clipboard.Fake(board))
	var unshare func()
	var err error
	var msg string
	on(func() {
		if via == "" {
			unshare, err = host.Share("127.0.0.1:0")
		} else {
			unshare, err = host.ShareRelay(via)
		}
		msg = host.Message()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unshare)
	s := shareString(t, msg)
	if copied, _ := os.ReadFile(board); string(copied) != "fude join "+s {
		t.Errorf("sharing put %q on the clipboard, want the command that joins", copied)
	}
	return s
}

// shareString returns the share string a message of sharing shows.
func shareString(t *testing.T, msg string) string {
	t.Helper()
	s, ok := strings.CutPrefix(msg, "copied, guests join with: fude join ")
	if !ok || strings.ContainsAny(s, " '\"#&;?*$") {
		t.Fatalf("sharing said %q, want the share string as one word", msg)
	}
	return s
}

// closedPort returns an address of the loopback that refuses connections.
func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	return l.Addr().String()
}

// asked waits until the host's fude asks to let a guest in.
func asked(t *testing.T, host *editor.Session, on func(func()), name string) {
	t.Helper()
	for deadline := time.Now().Add(editortest.Patience); ; time.Sleep(time.Millisecond) {
		var msg string
		on(func() { msg = host.Confirm() })
		if msg == name+" wants to join, let them in?" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the host said %q, want it to ask about %s", msg, name)
		}
	}
}

// joining joins with share off the test's goroutine, as the join waits
// for the host's answer.
func joining(share string) <-chan joined {
	ch := make(chan joined, 1)
	go func() {
		a, err := fudeclient.Join(share, os.Getenv("USER"), editor.Own{}, wire.Theme{}, false)
		ch <- joined{a, err}
	}()
	return ch
}

// refused joins with s and wants the join to end saying why.
func refused(t *testing.T, s, why string) {
	t.Helper()
	select {
	case j := <-joining(s):
		if j.err == nil || !strings.HasSuffix(j.err.Error(), why) {
			t.Errorf("a join said %v, want it to end with %q", j.err, why)
		}
	case <-time.After(editortest.Patience):
		t.Fatalf("a join waited for the host, want it to end with %q", why)
	}
}

type joined struct {
	a   *fudeclient.Attached
	err error
}

// B248
func TestB248Share(t *testing.T) {
	// The guest sees only the project's files (B245).
	t.Chdir(t.TempDir())
	path := "a.txt"
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "kyle")
	host := startWith(t, "", path).e.(*editor.Session)
	host.Use(terminalmod.New())
	_, on, _ := serve(t, host)
	// A guest races the string's addresses, so one that never answers and
	// one that refuses do not keep it from the host.
	join := joining("192.0.2.1:9," + closedPort(t) + "," + share(t, host, on, ""))
	// The host is asked, and y lets the guest in to a guest's session.
	asked(t, host, on, "kyle")
	on(func() { host.Feed("y") })
	j := <-join
	if j.err != nil {
		t.Fatal(j.err)
	}
	defer j.a.Close()
	c := &client{s: mock(t), e: j.a}
	c.shows(t, 0, "hello")
	for _, k := range vim.Keys(":terminal<CR>") {
		j.a.Feed(k)
	}
	for deadline := time.After(editortest.Patience); j.a.Message() != "a guest may not run :terminal"; {
		select {
		case f := <-j.a.Queue():
			f()
		case <-deadline:
			t.Fatalf(":terminal in the guest said %q", j.a.Message())
		}
	}
	// Quitting fude ends the guest's session.
	on(func() {
		for _, k := range vim.Keys(":qa!<CR>") {
			host.Feed(k)
		}
	})
	for deadline := time.After(editortest.Patience); !j.a.Done(); {
		select {
		case f := <-j.a.Queue():
			f()
		case <-deadline:
			t.Fatal("the guest's client never ended")
		}
	}
	if j.a.Ended() != "fude quit" {
		t.Errorf("when fude quit, the guest's client said %q", j.a.Ended())
	}
}

// B249
func TestB249ShareOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "kyle")
	host := startWith(t, "", "a.txt").e.(*editor.Session)
	_, on, _ := serve(t, host)
	str := share(t, host, on, "")
	refused := func(s, why string) { refused(t, s, why) }
	feed := func(keys string) {
		on(func() {
			for _, k := range vim.Keys(keys) {
				host.Feed(k)
			}
		})
	}
	// A string whose fingerprint or secret is not the share's lets no one
	// in, and the host is never asked.
	secretAt := strings.LastIndexByte(str, '/') + 1
	printAt := strings.LastIndexByte(str[:secretAt-1], '/') + 1
	flip := func(at int) string {
		c := byte('A')
		if str[at] == c {
			c = 'B'
		}
		return str[:at] + string(c) + str[at+1:]
	}
	refused(flip(printAt), "its certificate is not the one the share string names")
	dead := closedPort(t)
	// Each system words a refused connection its own way.
	_, err := net.Dial("tcp", dead)
	refused(dead+str[printAt-1:], "no address of the host answered, "+err.Error())
	refused(flip(secretAt), "the share string is wrong or was used, so ask the host for a new one")
	// n keeps the guest out.
	waiting := joining(str)
	asked(t, host, on, "kyle")
	// One join waits for the host at a time.
	refused(str, "another guest is waiting for the host, so try again soon")
	feed("n")
	if j := <-waiting; j.err == nil || !strings.HasSuffix(j.err.Error(), "the host said no") {
		t.Errorf("a join the host said no to said %v", j.err)
	}
	// A join while the host answers another question waits for neither.
	if err := os.WriteFile("a.txt", []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes("a.txt", later, later); err != nil {
		t.Fatal(err)
	}
	feed("x:w<CR>")
	refused(str, "the host is answering another question, so try again soon")
	feed("n")
	// y lets one guest in, and the string then works no more.
	waiting = joining(str)
	asked(t, host, on, "kyle")
	feed("y")
	if j := <-waiting; j.err != nil {
		t.Fatal(j.err)
	} else {
		defer j.a.Close()
	}
	refused(str, "the share string is wrong or was used, so ask the host for a new one")
	// :share makes a new string, which lets one more guest in.
	var msg string
	on(func() {
		for _, k := range vim.Keys(":share<CR>") {
			host.Feed(k)
		}
		msg = host.Message()
	})
	again := shareString(t, msg)
	waiting = joining(again)
	asked(t, host, on, "kyle")
	feed("y")
	if j := <-waiting; j.err != nil {
		t.Errorf("the string :share made said %v", j.err)
	} else {
		j.a.Close()
	}
}

// B250
func TestB250WhoAndKick(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "kyle")
	host := startWith(t, "", "a.txt").e.(*editor.Session)
	host.Use(sessionsmod.New())
	host.SetName("host")
	_, on, _ := serve(t, host)
	ex := func(line string) string {
		var msg string
		on(func() {
			for _, k := range vim.Keys(":" + line + "<CR>") {
				host.Feed(k)
			}
			msg = host.Message()
		})
		return msg
	}
	// Two guests called kyle are kyle and kyle2.
	str := share(t, host, on, "")
	var guests []*fudeclient.Attached
	for range 2 {
		waiting := joining(str)
		asked(t, host, on, "kyle")
		on(func() { host.Feed("y") })
		j := <-waiting
		if j.err != nil {
			t.Fatal(j.err)
		}
		defer j.a.Close()
		guests = append(guests, j.a)
		str = shareString(t, ex("share"))
	}
	if who := ex("who"); who != "kyle  kyle2" {
		t.Errorf(":who said %q", who)
	}
	// :kick ends the one guest's session, and its client says the host
	// ended it.
	ex("kick kyle2")
	for deadline := time.After(editortest.Patience); !guests[1].Done(); {
		select {
		case f := <-guests[1].Queue():
			f()
		case <-deadline:
			t.Fatal("the kicked guest's client never ended")
		}
	}
	if guests[1].Ended() != "the host ended your session" || guests[0].Done() {
		t.Errorf("the kicked guest said %q, and the other ended %v", guests[1].Ended(), guests[0].Done())
	}
	if who := ex("who"); who != "kyle" {
		t.Errorf(":who after :kick said %q", who)
	}
}

// relaying runs a relay on addr until the test ends, and returns its
// address and a func that stops it.
func relaying(t *testing.T, addr string) (string, func()) {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() {
		relay.Serve(l)
		close(served)
	}()
	stop := func() {
		l.Close()
		<-served
	}
	t.Cleanup(func() { l.Close() })
	return l.Addr().String(), stop
}

// B251
func TestB251Relay(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "kyle")
	host := startWith(t, "", "a.txt").e.(*editor.Session)
	_, on, _ := serve(t, host)
	ex := func(line string) string {
		var msg string
		on(func() {
			for _, k := range vim.Keys(":" + line + "<CR>") {
				host.Feed(k)
			}
			msg = host.Message()
		})
		return msg
	}
	let := func(str string) {
		t.Helper()
		waiting := joining(str)
		asked(t, host, on, "kyle")
		on(func() { host.Feed("y") })
		j := <-waiting
		if j.err != nil {
			t.Fatal(j.err)
		}
		t.Cleanup(func() { j.a.Close() })
		c := &client{s: mock(t), e: j.a}
		c.shows(t, 0, "hello")
	}
	// A relay that cannot be reached is an error.
	dead, stopDead := relaying(t, "127.0.0.1:0")
	stopDead()
	var err error
	on(func() { _, err = host.ShareRelay(dead) })
	if err == nil {
		t.Error("sharing through a relay no one runs gave no error")
	}
	// The share string names the relay and a room, and a guest joins
	// through it, the TLS end to end.
	addr, stop := relaying(t, "127.0.0.1:0")
	str := share(t, host, on, addr)
	if !strings.HasPrefix(str, addr+"/") || strings.Count(str, "/") != 3 {
		t.Fatalf("the share string %q does not name the relay and a room", str)
	}
	printAt := strings.LastIndexByte(str[:strings.LastIndexByte(str, '/')], '/') + 1
	flipped := str[:printAt] + string("AB"[strings.IndexByte("A", str[printAt])+1]) + str[printAt+1:]
	refused(t, flipped, "its certificate is not the one the share string names")
	let(str)
	// The host holds a new place for the next guest.
	let(shareString(t, ex("share")))
	room := str[len(addr)+1 : printAt-1]
	refused(t, strings.Replace(str, room, "nobody", 1), "no one shares in that room")
	// When the relay goes away the host says so, and :share holds a new
	// place once it is back.
	stop()
	for deadline := time.Now().Add(editortest.Patience); ; time.Sleep(time.Millisecond) {
		var msg string
		on(func() { msg = host.Message() })
		if strings.HasPrefix(msg, "lost the relay at "+addr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the host said %q when the relay went away", msg)
		}
	}
	relaying(t, addr)
	if msg := ex("share"); msg != "holding a new place at the relay" {
		t.Fatalf(":share after the relay went away said %q", msg)
	}
	for deadline := time.Now().Add(editortest.Patience); ; time.Sleep(time.Millisecond) {
		var msg string
		on(func() { msg = host.Message() })
		if strings.HasPrefix(msg, "copied") {
			let(shareString(t, msg))
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf(":share after the relay came back said %q", msg)
		}
	}
}

// B252
func TestB252Names(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "kyle")
	host := startWith(t, "", path).e.(*editor.Session)
	socket, on, _ := serve(t, host)
	a, err := fudeclient.Attach(socket, "pat")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	// A session that sends no name is named by its origin.
	on(func() { host.Attach("agent", editor.Host) })
	names := func(v []wire.Window) (out []string) {
		for _, o := range v[0].Others {
			out = append(out, fmt.Sprintf("%s %d", o.Name, o.Colour))
		}
		return out
	}
	var seen []string
	on(func() { seen = names(host.Windows()) })
	if want := []string{"pat 1", "agent 2"}; !slices.Equal(seen, want) {
		t.Errorf("the host sees %q, want %q", seen, want)
	}
	// The attached client sees the host by $USER.
	for deadline := time.After(editortest.Patience); !slices.Equal(names(a.Windows()), []string{"kyle 0", "agent 2"}); {
		select {
		case f := <-a.Queue():
			f()
		case <-deadline:
			t.Fatalf("the attached client sees %q", names(a.Windows()))
		}
	}
}

// B253
func TestB253Presence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello world\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	host := c.e.(*editor.Session)
	other := host.Attach("agent", editor.Host)
	for _, k := range vim.Keys("jwvl") {
		other.Feed(k)
	}
	c.draw()
	// The other session's cursor is a cell in its colour, and its selection
	// is underlined in it, on a line other than the host's cursor.
	colour := presence(1)
	if _, s, _ := c.s.Get(8, 1); s.GetBackground() != colour {
		t.Errorf("the other session's cursor has the ground %v, want %v", s.GetBackground(), colour)
	}
	if _, s, _ := c.s.Get(7, 1); s.GetUnderlineStyle() == tcell.UnderlineStyleNone || s.GetUnderlineColor() != colour {
		t.Errorf("the other session's selection is underlined %v in %v, want %v", s.GetUnderlineStyle(), s.GetUnderlineColor(), colour)
	}
	// The status line names it.
	if row := c.row(39); !strings.Contains(row, "agent") {
		t.Errorf("the status line is %q, want it to name the other session", row)
	}
	// A cursor after the line's end shows there.
	for _, k := range vim.Keys("<Esc>A") {
		other.Feed(k)
	}
	c.draw()
	if _, s, _ := c.s.Get(11, 1); s.GetBackground() != colour {
		t.Errorf("the other session's cursor after the line has the ground %v, want %v", s.GetBackground(), colour)
	}
}

// B254
func TestB254OwnConfig(t *testing.T) {
	kit, err := filepath.Abs("../../plugin/testdata/kit/kit.wasm")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "kyle")
	host := startWith(t, fmt.Sprintf("[keymap.normal]\nQ = \"ihost <Esc>\"\n[[plugin]]\npath = %q\ncapabilities = [\"buffer.read\", \"buffer.write\"]\n", kit), "a.txt").e.(*editor.Session)
	host.Load()
	_, on, _ := serve(t, host)
	text := func() string {
		var s string
		on(func() { t := host.Text(); s = t.Slice(0, t.Len()) })
		return s
	}
	// The plugin has loaded once its command types on the host.
	for deadline := time.Now().Add(editortest.Patience); text() != "!hello"; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the plugin never loaded, leaving %q", text())
		}
		on(func() {
			if text := host.Text(); text.Slice(0, text.Len()) == "hello" {
				for _, k := range vim.Keys(":Shout<CR>") {
					host.Feed(k)
				}
			}
		})
	}
	on(func() {
		for _, k := range vim.Keys("u") {
			host.Feed(k)
		}
	})
	str := share(t, host, on, "")
	// config.toml on the guest's machine.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fude"), 0o700); err != nil {
		t.Fatal(err)
	}
	guestConfig := "leader = \",\"\nnumber = true\ntheme = \"light\"\n[keymap.normal]\n\"<leader>q\" = \"iguest <Esc>\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fude", "config.toml"), []byte(guestConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	own, theme, err := editor.OwnConfig()
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan joined, 1)
	go func() {
		a, err := fudeclient.Join(str, "kyle", own, theme, false)
		waiting <- joined{a, err}
	}()
	asked(t, host, on, "kyle")
	on(func() { host.Feed("y") })
	j := <-waiting
	if j.err != nil {
		t.Fatal(j.err)
	}
	defer j.a.Close()
	// The guest's keys are its own, and the host's are not the guest's.
	for _, k := range vim.Keys("Q,q") {
		j.a.Feed(k)
	}
	for deadline := time.Now().Add(editortest.Patience); text() != "guest hello"; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the guest's Q,q left %q, want its own mapping typed alone", text())
		}
	}
	// A plugin's leader key takes the guest's leader.
	for _, k := range vim.Keys(",!") {
		j.a.Feed(k)
	}
	for deadline := time.Now().Add(editortest.Patience); strings.Count(text(), "!") != 1; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the guest's ,! left %q, want the plugin's key to type", text())
		}
	}
	// Its windows take its options, and it draws in its theme.
	for deadline := time.After(editortest.Patience); !j.a.Windows()[0].Number; {
		select {
		case f := <-j.a.Queue():
			f()
		case <-deadline:
			t.Fatal("the guest's window shows no line numbers")
		}
	}
	var hostNumber bool
	on(func() { hostNumber = host.Windows()[0].Number })
	if name, _, _ := j.a.Theme(); name != "light" || hostNumber {
		t.Errorf("the guest draws in %q, and the host shows numbers %v", name, hostNumber)
	}
}

// B255
func TestB255JoinReadOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := startWith(t, "", "a.txt").e.(*editor.Session)
	_, on, _ := serve(t, host)
	str := share(t, host, on, "")
	waiting := make(chan joined, 1)
	go func() {
		a, err := fudeclient.Join(str, "kyle", editor.Own{}, wire.Theme{}, true)
		waiting <- joined{a, err}
	}()
	// The host's question says the guest asks to be read-only.
	for deadline := time.Now().Add(editortest.Patience); ; time.Sleep(time.Millisecond) {
		var msg string
		on(func() { msg = host.Confirm() })
		if msg == "kyle wants to join read-only, let them in?" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the host was asked %q", msg)
		}
	}
	on(func() { host.Feed("y") })
	j := <-waiting
	if j.err != nil {
		t.Fatal(j.err)
	}
	defer j.a.Close()
	j.a.Feed("x")
	for deadline := time.After(editortest.Patience); j.a.Message() != "cannot make changes, the session is read-only"; {
		select {
		case f := <-j.a.Queue():
			f()
		case <-deadline:
			t.Fatalf("x in a read-only guest said %q", j.a.Message())
		}
	}
	var text string
	on(func() { s := host.Text(); text = s.Slice(0, s.Len()) })
	if text != "hello" {
		t.Errorf("a read-only guest's x left %q", text)
	}
}

// B258
func TestB258BufferLine(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, f := range []string{"a.txt", "b.txt", "one/x.txt", "two/x.txt"} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(f+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// lined is a client with the buffer line's module, and the one :b is in.
	lined := func(c *client) *client {
		c.e.(*editor.Session).Use(bufferlinemod.New())
		c.e.(*editor.Session).Use(buffersmod.New())
		return c
	}
	// One file shows the line too.
	c := lined(startWith(t, "", "a.txt"))
	if c.draw(); !strings.HasPrefix(c.row(0), " 1 a.txt ") {
		t.Errorf("with one file the top row is %q", c.row(0))
	}
	// Two show it, the current one lit and a modified one marked.
	c = lined(startWith(t, "", "a.txt", "b.txt"))
	for _, k := range vim.Keys("x") {
		c.e.Feed(k)
	}
	c.draw()
	if row := c.row(0); !strings.HasPrefix(row, " 1 a.txt ● ") || !strings.Contains(row, " 2 b.txt ") {
		t.Errorf("the buffer line is %q", row)
	}
	if _, s, _ := c.s.Get(3, 0); s.GetAttributes()&tcell.AttrBold == 0 {
		t.Error("the current buffer does not stand out")
	}
	if !strings.HasPrefix(c.row(1), ".txt") {
		t.Errorf("the window starts with %q, want its text below the line", c.row(1))
	}
	// Two files of one name show their folders.
	c = lined(startWith(t, "", "one/x.txt", "two/x.txt"))
	if c.draw(); !strings.Contains(c.row(0), filepath.Join("one", "x.txt")) || !strings.Contains(c.row(0), filepath.Join("two", "x.txt")) {
		t.Errorf("files of one name show as %q", c.row(0))
	}
	// bufferline.enabled false hides it.
	c = lined(startWith(t, "[module.bufferline]\nenabled = false\n", "a.txt", "b.txt"))
	if c.draw(); !strings.HasPrefix(c.row(0), "a.txt") {
		t.Errorf("with bufferline.enabled off the top row is %q", c.row(0))
	}
	// Too many to fit shows the part around the current one, cut with marks.
	var names []string
	for i := range 30 {
		name := fmt.Sprintf("file-with-a-long-name-%02d.txt", i)
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	c = lined(startWith(t, "", names...))
	for _, k := range vim.Keys(":b 15<CR>") {
		c.e.Feed(k)
	}
	if c.draw(); !strings.HasPrefix(c.row(0), "‹") || !strings.Contains(c.row(0), "15 file-with-a-long-name-14.txt") || !strings.HasSuffix(strings.TrimRight(c.row(0), " "), "›") {
		t.Errorf("thirty files show as %q", c.row(0))
	}
	// A guest's line lists only the buffers it may show.
	away := filepath.Join(t.TempDir(), "away.txt")
	if err := os.WriteFile(away, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c = lined(startWith(t, "", "a.txt", "b.txt", away))
	if tabs := c.e.(*editor.Session).Attach("guest", editor.Guest).Buffers(); len(tabs) != 2 {
		t.Errorf("a guest's buffer line lists %v, want the two files in the project", tabs)
	}
}

// B269
func TestB269ShareStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "kyle")
	c := startWith(t, "", "a.txt")
	host := c.e.(*editor.Session)
	_, on, _ := serve(t, host)
	status := func() string {
		var row string
		on(func() { c.draw(); row = c.row(39) })
		return row
	}
	if strings.Contains(status(), "sharing") {
		t.Errorf("before sharing the status line is %q", status())
	}
	str := share(t, host, on, "")
	if !strings.Contains(status(), "sharing  ") {
		t.Errorf("sharing, the status line is %q", status())
	}
	waiting := joining(str)
	asked(t, host, on, "kyle")
	on(func() { host.Feed("y") })
	j := <-waiting
	if j.err != nil {
		t.Fatal(j.err)
	}
	defer j.a.Close()
	if !strings.Contains(status(), "sharing with 1") {
		t.Errorf("with a guest in, the status line is %q", status())
	}
	g := &client{s: mock(t), e: j.a}
	g.shows(t, 39, "guest")
}

// B401
func TestB401Cursors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("ab cd\nef gh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "", path)
	c.draw()
	for _, k := range vim.Keys("Qjve") {
		c.e.Feed(k)
	}
	c.draw()
	// The multicursor draws as a block in the mode's colour, at the end of
	// its own selection.
	if _, s, _ := c.s.Get(1, 0); s.GetBackground() != th.visual {
		t.Errorf("the multicursor in visual mode is on %v, want %v", s.GetBackground(), th.visual)
	}
	if _, s, _ := c.s.Get(0, 0); s.GetBackground() != th.selection {
		t.Errorf("the selection at the cursor Q placed is on %v", s.GetBackground())
	}
	if _, s, _ := c.s.Get(3, 0); s.GetBackground() == th.selection {
		t.Error("text outside every selection draws selected")
	}
	// Where the cursor is an underline, so is a multicursor.
	for _, k := range vim.Keys("<Esc>d") {
		c.e.Feed(k)
	}
	c.draw()
	if _, s, _ := c.s.Get(1, 0); s.GetUnderlineStyle() == tcell.UnderlineStyleNone || s.GetUnderlineColor() != th.normal || s.GetBackground() == th.normal {
		t.Error("while d waits, the multicursor does not draw as an underline in the mode's colour")
	}

	// A terminal that answers that it draws cursors of its own, as kitty
	// does, gets one at each multicursor, in the cursor's shape. The answer
	// never reaches the keys, even cut across two reads.
	in := make(chan []byte, 2)
	var extra atomic.Bool
	s, err := screen(typed{in}, &extra)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	in <- []byte("\x1b[>1;2;3;29;30")
	in <- []byte(";40;100;101 qx")
	for read := false; !read; {
		select {
		case ev := <-s.EventQ():
			k, ok := ev.(*tcell.EventKey)
			if !ok {
				continue
			}
			if !slices.Equal(keys(k), []vim.Key{"x"}) {
				t.Fatalf("the answer and x read as %v", keys(k))
			}
			read = true
		case <-time.After(editortest.Patience):
			t.Fatal("x after the answer read as nothing")
		}
	}
	if !extra.Load() {
		t.Fatal("the answer did not say the terminal draws cursors")
	}
	var out strings.Builder
	c = start(t, path)
	c.extra, c.out = &extra, &out
	c.draw()
	for _, k := range vim.Keys("QjA") {
		c.e.Feed(k)
	}
	c.draw()
	r, g, b := th.insert.RGB()
	if want := fmt.Sprintf("\x1b[>0;4 q\x1b[>40;2:%d:%d:%d q\x1b[>29;2:1:6 q", r, g, b); !strings.HasSuffix(out.String(), want) {
		t.Errorf("the client wrote %q, want it to end in %q", out.String(), want)
	}
	if _, s, _ := c.s.Get(5, 0); s.GetBackground() == th.insert {
		t.Error("a multicursor the terminal draws draws as a cell too")
	}
	// Removing them, which changes no cell, takes the terminal's off.
	for _, k := range vim.Keys("<Esc>") {
		c.e.Feed(k)
	}
	c.draw()
	c.e.Feed("<C-l>")
	c.draw()
	if !strings.HasSuffix(out.String(), "q\x1b[>0;4 q") {
		t.Errorf("after <C-l> the client wrote %q, want the cursors cleared", out.String())
	}
}

// B402
func TestB402CursorShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("ab cd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		keys string
		want vt.CursorStyle
	}{
		{"", vt.SteadyBlock},
		{"v", vt.SteadyBlock},
		{"i", vt.SteadyBar},
		{"R", vt.SteadyUnderline},
		{"d", vt.SteadyUnderline},
		{"2c", vt.SteadyUnderline},
		{"r", vt.SteadyUnderline},
		{"rx", vt.SteadyBlock},
	} {
		mt := vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40})
		cl := start(t, path)
		cl.s = screenOn(t, mt)
		for _, k := range vim.Keys(c.keys) {
			cl.e.Feed(k)
		}
		cl.draw()
		if got := mt.Backend().GetCursor(); got != c.want {
			t.Errorf("after %q the cursor is %v, want %v", c.keys, got, c.want)
		}
	}
}

// B452
func TestB452LineBreak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("abcdef ", 20)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 17 words fill 119 of the 120 columns, so the 18th starts in the last.
	for config, want := range map[string]string{"": "bcdef abcdef", "line_break = true\n": "abcdef abcdef"} {
		c := startWith(t, config, path)
		c.draw()
		if row := c.row(1); !strings.HasPrefix(row, want) {
			t.Errorf("with %q the second row is %q, want it to start %q", config, row, want)
		}
	}
}

// B480
func TestB480Title(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for config, want := range map[string]string{"": "", "title = true\n": "notes.txt"} {
		c := startWith(t, config, path)
		mt := vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40})
		c.s = screenOn(t, mt)
		c.draw()
		if got := mt.GetTitle(); got != want {
			t.Errorf("with %q the title is %q, want %q", config, got, want)
		}
	}
}

// B481
func TestB481MenuHeight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	var words []string
	for i := range 30 {
		words = append(words, fmt.Sprintf("w%02d", i))
	}
	if err := os.WriteFile(path, []byte(strings.Join(words, " ")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for config, want := range map[string]int{"": 30, "menu_height = 5\n": 5} {
		c := startWith(t, config, path)
		c.e.(*editor.Session).Use(completionmod.New())
		for _, k := range vim.Keys("o w<C-x><C-o>") {
			c.e.Feed(k)
		}
		c.draw()
		rows := 0
		for y := range 40 {
			if strings.Contains(c.row(y), "[buffer]") {
				rows++
			}
		}
		if rows != want {
			t.Errorf("with %q the menu shows %d rows, want %d", config, rows, want)
		}
	}
}

// B483
func TestB483CursorColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("text\n\nmore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "cursor_column = true\n", path)
	c.e.Feed(vim.Keys("l")[0])
	c.draw()
	ground := func(x, y int) color.Color {
		_, style, _ := c.s.Get(x, y)
		return style.GetBackground()
	}
	for y := range 3 {
		if ground(1, y) != themes["dark"].cursorLine || ground(2, y) != themes["dark"].bg {
			t.Errorf("on line %d the cursor's column is %v and the column after it %v", y+1, ground(1, y), ground(2, y))
		}
	}
	if ground(1, 3) != themes["dark"].bg {
		t.Errorf("past the text the cursor's column is %v", ground(1, 3))
	}
}

// B486, B118
func TestB486ListChars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("\tab  \n  x\u00a0y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for config, want := range map[string][2]string{
		"list = true\n": {">       ab--", "  x+y"},
		"list = true\n[list_chars]\ntab = \"<->\"\nspace = \".\"\nlead = \"_\"\ntrail = \"~\"\nnbsp = \"%\"\neol = \"$\"\n": {"<------>ab~~$", "__x%y$"},
	} {
		c := startWith(t, config, path)
		c.draw()
		for y, row := range want {
			if got := strings.TrimRight(c.row(y), " "); got != row {
				t.Errorf("with %q row %d is %q, want %q", config, y, got, row)
			}
		}
	}
	// Without wrap, a line cut at either edge shows extends and precedes.
	long := filepath.Join(t.TempDir(), "b.txt")
	if err := os.WriteFile(long, []byte(strings.Repeat("x", 130)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startWith(t, "list = true\nwrap = false\n[list_chars]\nextends = \">\"\nprecedes = \"<\"\n", long)
	c.draw()
	if got := c.row(0); got != strings.Repeat("x", 119)+">" {
		t.Errorf("the first row is %q, want it cut with >", got)
	}
	c.e.Feed(vim.Keys("$")[0])
	c.draw()
	if got := c.row(0); !strings.HasPrefix(got, "<x") {
		t.Errorf("at the line's end the first row is %q, want it to start with <", got)
	}
}

// B540
func TestB540ControlCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a\x01b\x1b[Hc\x7f\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40})
	c := start(t, path)
	c.s = screenOn(t, mt)
	// What the terminal shows, so a byte it acted on shows up too.
	shown := func(y int) string {
		var b strings.Builder
		for x := range 120 {
			b.WriteString(mt.GetCell(vt.Coord{X: vt.Col(x), Y: vt.Row(y)}).C)
		}
		return strings.TrimRight(b.String(), " ")
	}
	c.draw()
	if got := shown(0); got != "a^Ab^[[Hc^?" {
		t.Errorf("the line shows %q, want a^Ab^[[Hc^?", got)
	}
	c.e.Feed(vim.Keys("$")[0])
	c.draw()
	if got := mt.Pos(); got.X != 9 || got.Y != 0 {
		t.Errorf("after $ the cursor shows at %v, want column 9", got)
	}
	for _, k := range vim.Keys(":<C-v><C-a>") {
		c.e.Feed(k)
	}
	c.draw()
	found := false
	for y := range 40 {
		found = found || strings.Contains(shown(y), "│ : ^A  ")
	}
	if !found {
		t.Errorf("the command line does not show ^A")
	}
}

// B487
func TestB487FillChars(t *testing.T) {
	c := startWith(t, "[fill_chars]\neob = \"@\"\nvert = \"!\"\n")
	c.e.(*editor.Session).Use(windowmod.New())
	for _, k := range vim.Keys(":vsplit<CR>") {
		c.e.Feed(k)
	}
	c.draw()
	x := c.e.Windows()[1].X - 1
	if s, _, _ := c.s.Get(x, 0); s != "!" {
		t.Errorf("between the windows is %q, want !", s)
	}
	if s, _, _ := c.s.Get(0, 1); s != "@" {
		t.Errorf("past the text is %q, want @", s)
	}
	// A value fill_chars does not take changes nothing.
	for _, k := range vim.Keys(":set fill_chars.eob ab<CR>") {
		c.e.Feed(k)
	}
	if msg := c.e.Message(); msg != `fill_chars.eob takes one character one column wide, not "ab"` {
		t.Errorf(":set fill_chars.eob ab said %q", msg)
	}
}

// B489
func TestB489BreakIndent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("    "+strings.Repeat("x", 130)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The second row keeps the line's 4 columns of indent, then shows "> ".
	c := startWith(t, "break_indent = true\nshow_break = \"> \"\n", path)
	c.draw()
	if got, want := strings.TrimRight(c.row(1), " "), "    > "+strings.Repeat("x", 14); got != want {
		t.Errorf("the second row is %q, want %q", got, want)
	}
	// gj keeps the screen column, so it lands 6 columns into the row's text.
	for _, k := range vim.Keys("10lgj") {
		c.e.Feed(k)
	}
	if got := c.e.(*editor.Session).Cursor(); got != 124 {
		t.Errorf("gj from column 10 went to %d, want 124", got)
	}
}

// B490
func TestB490VirtualEditCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("ab\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for config, want := range map[string]int{"": 1, "virtual_edit = \"all\"\n": 5} {
		mt := vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40})
		c := startWith(t, config, path)
		c.s = screenOn(t, mt)
		for _, k := range vim.Keys("5l") {
			c.e.Feed(k)
		}
		c.draw()
		if got := mt.Pos(); int(got.X) != want || got.Y != 0 {
			t.Errorf("with %q after 5l the cursor shows at %v, want column %d", config, got, want)
		}
	}
}
