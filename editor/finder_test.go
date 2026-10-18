package editor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/modules/grep"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// B129
func TestB129Files(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"editor/editor.go", "editor/git.go", "editor/editor_test.go", "prd/decisions.md", "skip.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		change(t, filepath.Join(dir, f), f+"\n")
	}
	change(t, filepath.Join(dir, ".gitignore"), "skip.go\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "prd/decisions.md")
	e.SetSize(80, 24)
	results := func() []string {
		f, _ := e.Float()
		return slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })
	}
	// The finder lists the files git does not ignore, each by its name and
	// then its directory.
	feed(e, ":pick files<CR>")
	settle(t, e, func() bool { return len(results()) == 5 })
	feed(e, "edgo")
	settle(t, e, func() bool {
		return slices.Equal(results(), []string{"editor.go  editor", "git.go  editor", "editor_test.go  editor"})
	})
	if feed(e, "<C-n><CR>"); e.Views()[0].Name != "editor/git.go" || e.Mode() != vim.Normal {
		t.Fatalf("<C-n><CR> opened %q in %v mode", e.Views()[0].Name, e.Mode())
	}
	// A screen too short for a row of matches still shows the query.
	e.SetSize(80, 5)
	feed(e, ":pick files<CR>zzz")
	if f, _ := e.Float(); !strings.HasPrefix(f.Text, "› zzz") {
		t.Errorf("on a short screen the finder shows %q", f.Text)
	}
	feed(e, "<Esc>")
	e.SetSize(80, 24)
	// <Esc> closes the finder and opens nothing.
	feed(e, ":pick files<CR>x<Esc>")
	if _, ok := e.Float(); ok || e.Views()[0].Name != "editor/git.go" || text(e) != "editor/git.go" {
		t.Fatalf("after <Esc> the finder shows %v and the window has %q", ok, e.Views()[0].Name)
	}
}

// B130
func TestB130FilesPreview(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a"), "one\ntwo\n")
	change(t, filepath.Join(dir, "b"), "three\n")
	change(t, filepath.Join(dir, "c.go"), "package c\n")
	change(t, filepath.Join(dir, "d"), strings.Repeat("x", 10<<10)+"\x00")
	change(t, filepath.Join(dir, "e"), strings.Repeat("y", 1<<20)+"\n")
	t.Chdir(dir)
	e := open(t, "a")
	e.SetSize(100, 24)
	feed(e, ":pick files<CR>")
	doc := func() string { f, _ := e.Float(); return f.Doc }
	settle(t, e, func() bool { return doc() == "a\n\none\ntwo" })
	feed(e, "<C-n>")
	settle(t, e, func() bool { return doc() == "b\n\nthree" })
	// A file's preview has its filetype's highlights.
	feed(e, "<C-n>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return f.Doc == "c.go\n\npackage c" && slices.ContainsFunc(f.DocSpans, func(s syntax.Span) bool { return s.Name == "keyword" })
	})
	// A zero byte in the first 16KB makes a binary file, and a long line
	// shows its start.
	feed(e, "<C-n>")
	settle(t, e, func() bool { return doc() == "d\n\nbinary file" })
	feed(e, "<C-n>")
	settle(t, e, func() bool { return strings.HasPrefix(doc(), "e\n\nyyyy") && len(doc()) < 1000 })
	// A narrow screen has no room for it.
	if e.SetSize(50, 24); doc() != "" {
		t.Fatalf("on 50 columns the preview is %q", doc())
	}
}

// B195
func TestB195FinderLists(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.go"), "one\n  two Needle\n")
	change(t, filepath.Join(dir, "b.txt"), "needles\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "b.txt")
	e.Use(grep.New())
	e.SetSize(100, 24)
	shown := func() int {
		f, _ := e.Float()
		return len(slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" }))
	}
	visited := func() []string {
		feed(e, ":cclose<CR>:cfirst<CR>")
		names := []string{e.Views()[0].Name}
		feed(e, ":cnext<CR>")
		return append(names, e.Views()[0].Name)
	}
	// Grep shows each match under a row for its file.
	for find, rows := range map[string]int{":pick grep needle<CR>": 4, ":pick files<CR>": 2} {
		feed(e, find)
		settle(t, e, func() bool { return shown() == rows })
		// <C-q> sends both matches to a list, which is the last list.
		if feed(e, "<C-q>"); len(e.Views()) != 2 {
			t.Fatalf("<C-q> after %s left %d windows", find, len(e.Views()))
		}
		if got := visited(); !slices.Contains(got, "a.go") || !slices.Contains(got, "b.txt") {
			t.Errorf("the list of %s went to %q", find, got)
		}
	}
}

// B202, for the finder's query
func TestB202FinderQuery(t *testing.T) {
	e := open(t, "")
	e.Use(grep.New())
	e.SetSize(100, 24)
	feed(e, ":pick grep<CR>f(")
	if f, _ := e.Float(); !strings.HasPrefix(f.Text, "› f( ") {
		t.Fatalf("typing f( in the finder makes the line %q", strings.SplitN(f.Text, "\n", 2)[0])
	}
}

// B213
func TestB213Pick(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\n")
	change(t, filepath.Join(dir, "b.txt"), "b\n")
	t.Chdir(dir)
	e := open(t, "a.txt")
	e.Use(grep.New())
	e.SetSize(100, 30)
	feed(e, ":e b.txt<CR>")
	rows := func() []string {
		f, _ := e.Float()
		return slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })
	}
	// recent waits for the recent files to name each of paths.
	recent := func(paths ...string) {
		settle(t, e, func() bool {
			r, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fude", "recent"))
			for _, p := range paths {
				if abs, _ := filepath.Abs(p); !strings.Contains(string(r), abs) {
					return false
				}
			}
			return true
		})
	}
	recent("a.txt", "b.txt")
	// Buffers and recent files list the files but the current one, and
	// <CR> opens the one picked.
	for _, source := range []string{"buffers", "recent"} {
		feed(e, ":pick "+source+"<CR>")
		settle(t, e, func() bool { return slices.Contains(rows(), "a.txt") && !slices.Contains(rows(), "b.txt") })
		if feed(e, "a.txt<CR>"); e.Views()[0].Name != "a.txt" {
			t.Fatalf("<CR> in :pick %s opened %q", source, e.Views()[0].Name)
		}
		feed(e, ":e b.txt<CR>")
	}
	// A recent file outside the working directory comes after its own, even
	// when it is newer.
	out := filepath.Join(t.TempDir(), "o.txt")
	change(t, out, "o\n")
	feed(e, ":e "+out+"<CR>:e b.txt<CR>")
	recent(out)
	feed(e, ":pick recent<CR>")
	settle(t, e, func() bool {
		r := rows()
		at := func(name string) int {
			return slices.IndexFunc(r, func(l string) bool { return strings.HasPrefix(l, name) })
		}
		return at("a.txt") >= 0 && at("a.txt") < at("o.txt")
	})
	feed(e, "<Esc>")
	// A name that starts with .. is still under the working directory.
	change(t, filepath.Join(dir, "..dots"), "d\n")
	feed(e, ":e ..dots<CR>:e b.txt<CR>")
	recent("..dots")
	feed(e, ":pick recent<CR>")
	settle(t, e, func() bool { return slices.Contains(rows(), "..dots") })
	feed(e, "<Esc>")
	// Files and grep are sources too.
	feed(e, ":pick grep b<CR>")
	settle(t, e, func() bool { return slices.Contains(rows(), "b.txt") })
}

// B266
func TestB266PickCommands(t *testing.T) {
	e := start(t)
	e.SetSize(100, 30)
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	// Typing what a command does finds it, and the one picked runs.
	feed(e, ":pick commands<CR>opens a window below with a new empty")
	settle(t, e, func() bool { return shows(":new") })
	if feed(e, "<CR>"); len(e.Views()) != 2 {
		t.Fatalf("picking :new left %d windows", len(e.Views()))
	}
	// One that needs an argument waits on the command line.
	feed(e, ":pick commands<CR>:cdo")
	settle(t, e, func() bool { return shows(":cdo") })
	if feed(e, "<CR>"); e.Line() != ":cdo " {
		t.Errorf("picking :cdo left the line %q", e.Line())
	}
}

// B271
func TestB271Plug(t *testing.T) {
	e := withKit(t)
	e.SetSize(100, 30)
	// A plugin's key runs with no key typed.
	feed(e, ":plug kit.gS<CR>")
	settle(t, e, func() bool { return text(e) == "!ab cd\nef" })
	// A name that is no plug says so.
	if feed(e, ":plug nope<CR>"); e.Message() != "no plug is called nope" {
		t.Errorf(":plug nope said %q", e.Message())
	}
	// :pick commands finds a plug by what it does.
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	feed(e, ":pick commands<CR>lists the directory of the file")
	settle(t, e, func() bool { return shows(":plug listing.up") })
	feed(e, "<Esc><Esc>")
	// Names complete, and fude's plugs run.
	if feed(e, ":plug listing.u<Tab>"); e.Line() != ":plug listing.up" {
		t.Errorf("<Tab> left the line %q", e.Line())
	}
	feed(e, "<CR>")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "f") })
}

// B215
func TestB215PickLists(t *testing.T) {
	config(t, "[keymap.normal]\n\"z9\" = \":set number true<CR>\"\n")
	e := start(t)
	e.SetSize(100, 30)
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	// :pick alone lists the sources, and picking one opens its finder.
	feed(e, ":pick<CR>")
	settle(t, e, func() bool { return shows("keymaps") })
	feed(e, "keymaps<CR>z9")
	settle(t, e, func() bool { return shows("normal mapping  types :set<Space>number<Space>true<CR>") })
	// A mapping picked runs.
	if feed(e, "<CR>"); !e.Views()[0].Number {
		t.Error("picking z9 did not run it")
	}
	// It lists the machine's keys too, each with its kind and help, and a
	// motion picked runs, j here after insert mode's <Down>.
	feed(e, "ggix<CR>y<Esc>gg:pick keymaps<CR>moves down a line")
	settle(t, e, func() bool { return shows("motion          moves down a line") })
	if feed(e, "<C-n><CR>"); e.Text().LineOf(e.Cursor()) != 1 {
		t.Errorf("picking j left the cursor on line %d", e.Text().LineOf(e.Cursor()))
	}
	if feed(e, ":pick help<CR>guide-start<CR>"); !strings.HasPrefix(text(e), "# fude help") {
		t.Errorf("picking guide-start opened %q", e.Views()[len(e.Views())-1].Name)
	}
}

// B212
func TestB212QCloses(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Chdir(dir)
	e := open(t, "a.txt")
	e.SetSize(100, 30)
	feed(e, "ix<Esc>")
	// A list of places, a timeline and a window fude wrote each close.
	feed(e, ":pick files<CR>")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "a.txt") })
	for _, keys := range []string{"<C-q>", ":timeline<CR>", ":grammar<CR>"} {
		if feed(e, keys); len(e.Views()) != 2 {
			t.Fatalf("%s made %d windows", keys, len(e.Views()))
		}
		if feed(e, "q"); len(e.Views()) != 1 || e.Recording() != 0 {
			t.Errorf("q after %s left %d windows, recording %q", keys, len(e.Views()), e.Recording())
		}
	}
}

// B210
func TestB210FinderIgnored(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.go"), "needle\n")
	change(t, filepath.Join(dir, "skip.go"), "needle\n")
	change(t, filepath.Join(dir, ".gitignore"), "skip.go\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "a.go")
	e.Use(grep.New())
	e.SetSize(100, 24)
	// shown is the finder's rows, sorted, as the search's order may vary.
	shown := func() []string {
		f, _ := e.Float()
		return slices.Sorted(slices.Values(slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })))
	}
	for _, c := range []struct {
		find          string
		without, with []string
	}{
		{":pick grep needle<CR>", []string{"1  needle", "a.go"}, []string{"1  needle", "1  needle", "a.go", "skip.go"}},
		{":pick files<CR>", []string{".gitignore", "a.go"}, []string{".gitignore", "a.go", "skip.go"}},
	} {
		feed(e, c.find)
		settle(t, e, func() bool { return slices.Equal(shown(), c.without) })
		feed(e, "<C-g>")
		settle(t, e, func() bool { return slices.Equal(shown(), c.with) })
		feed(e, "<C-g>")
		settle(t, e, func() bool { return slices.Equal(shown(), c.without) })
		feed(e, "<Esc>")
	}
}
