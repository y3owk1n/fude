package editor_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/modules/autopairs"
	filesmod "github.com/y3owk1n/fude/modules/files"
	grammarsmod "github.com/y3owk1n/fude/modules/grammars"
	"github.com/y3owk1n/fude/modules/grep"
	listingmod "github.com/y3owk1n/fude/modules/listing"
	pickmod "github.com/y3owk1n/fude/modules/pick"
	timelinemod "github.com/y3owk1n/fude/modules/timeline"
	"github.com/y3owk1n/fude/syntax"
)

// B305
func TestB305PreviewImage(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.png"), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	e := open(t, "a.png")
	e.Use(filesmod.New())
	e.SetSize(100, 24)
	e.SetCell(8, 16)
	feed(e, ":pick files<CR>")
	// The preview names the image, and the image shows under that, in two
	// rows of 10 columns.
	settle(t, e, func() bool {
		f, _ := e.Float()
		return f.Doc == "a.png\n\nPNG, 80 by 32 pixels" && f.DocImage != nil && f.DocImage.Row == 3 && f.DocImage.Rows == 2 && f.DocImage.Cols == 10
	})
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
	e.Use(filesmod.New())
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
	e.Use(filesmod.New())
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
	e.Use(autopairs.New())
	e.Use(grep.New())
	e.SetSize(100, 24)
	feed(e, ":pick grep<CR>f(")
	if f, _ := e.Float(); !strings.HasPrefix(f.Text, "› f( ") {
		t.Fatalf("typing f( in the finder makes the line %q", strings.SplitN(f.Text, "\n", 2)[0])
	}
}

// B271
func TestB271Plug(t *testing.T) {
	e := withKit(t)
	e.Use(listingmod.New())
	e.Use(pickmod.New())
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

// B212
func TestB212QCloses(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Chdir(dir)
	e := open(t, "a.txt")
	e.Use(timelinemod.New())
	e.Use(grammarsmod.New())
	e.Use(filesmod.New())
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
	e.Use(filesmod.New())
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
