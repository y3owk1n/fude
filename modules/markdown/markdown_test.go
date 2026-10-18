package markdown_test

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/markdown"
	"github.com/y3owk1n/fude/wire"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m, "markdown", "markdown_inline")) }

// B298
func TestB298Render(t *testing.T) {
	// The cursor starts on the blank first line, which shows as it is.
	src := "\n# Title\n- a\n  - b\n- [ ] todo\n> quote\n---\n```go\nx := 1\n```\nSome *em* and [link](http://x.y).\n\n| a | bb |\n|---|---:|\n| *ccc* | d |\n\n![pic](pic.png)\n"
	// An image at an address shows too.
	var web bytes.Buffer
	if err := png.Encode(&web, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(web.Bytes()) }))
	defer srv.Close()
	src += "\n![web](" + srv.URL + "/w.png)\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	editortest.Change(t, path, src)
	// The image beside the file is 80 by 32 pixels.
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), pic.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	e := editortest.Open(t, path)
	e.SetSize(80, 40)
	e.SetCell(8, 16)
	e.Use(markdown.New())
	at := func(s string) int { return strings.Index(src, s) }
	want := []wire.Ghost{
		{Pos: at("# "), End: at("# ") + 2, Style: "markdown.h1"},
		{Pos: at("- a"), End: at("- a") + 1, Text: "•", Style: "markdown.bullet"},
		{Pos: at("- b"), End: at("- b") + 1, Text: "◦", Style: "markdown.bullet"},
		{Pos: at("[ ]"), End: at("[ ]") + 3, Text: "☐", Style: "markdown.unchecked"},
		{Pos: at("> "), End: at("> ") + 1, Text: "▌", Style: "markdown.quote"},
		{Pos: at("---"), End: at("---") + 3, Text: "───", Style: "markdown.rule"},
		{Pos: at("```go"), End: at("```go") + 3},
		{Pos: at("*em"), End: at("*em") + 1},
		{Pos: at("[link"), End: at("[link") + 1},
		{Pos: at("http"), End: at("http") + len("http://x.y")},
		// The first column is as wide as *ccc* shows, five columns, so a's
		// pipe pads by two. The second aligns right, so d pads on its left.
		{Pos: at("| bb"), End: at("| bb") + 1, Text: "  │", Style: "markdown.table"},
		{Pos: at("|---"), End: at("|---") + 1, Text: "├", Style: "markdown.table"},
		{Pos: at("---|"), End: at("---|") + 3, Text: "─────", Style: "markdown.table"},
		{Pos: at("| d"), End: at("| d") + 1, Text: "│ ", Style: "markdown.table"},
		{Pos: at("| d") + 4, End: at("| d") + 5, Text: "│", Style: "markdown.table"},
	}
	shown := func() bool {
		v := e.Views()[0]
		for _, g := range want {
			if !slices.Contains(v.Ghosts, g) {
				return false
			}
		}
		// The borders above and below the table take the columns' widths.
		top := wire.Virtual{Text: "┌─────┬────┐", Style: "markdown.table", Above: true}
		bottom := wire.Virtual{Text: "└─────┴────┘", Style: "markdown.table"}
		if !slices.Contains(v.Virtual[e.Text().LineOf(at("| a"))], top) || !slices.Contains(v.Virtual[e.Text().LineOf(at("| *ccc"))], bottom) {
			return false
		}
		// The image shows below its line, two rows of 10 columns.
		for _, img := range []string{"![pic", "![web"} {
			if rows := v.Virtual[e.Text().LineOf(at(img))]; len(rows) != 2 || rows[0].Image == 0 || rows[0].Cols != 10 {
				return false
			}
		}
		line := e.Text().LineOf(at("x := 1"))
		return v.Grounds[1] == "markdown.h1" && v.Grounds[line] == "markdown.code"
	}
	editortest.Settle(t, e, shown)
	// With the cursor on one of its rows, the whole table shows its text,
	// without its borders.
	table := e.Text().LineOf(at("| a"))
	editortest.Feed(e, strconv.Itoa(table+3)+"G")
	for _, g := range e.Views()[0].Ghosts {
		if g.Pos >= at("| a") && g.Style == "markdown.table" {
			t.Errorf("with the cursor in the table, it shows %+v", g)
		}
	}
	if v := e.Views()[0]; len(v.Virtual[table]) != 0 || len(v.Virtual[table+2]) != 0 {
		t.Errorf("with the cursor in the table, its borders show: %v", v.Virtual)
	}
	editortest.Feed(e, "gg")
	// A new bullet draws a moment after it is typed, once the cursor leaves
	// its line.
	editortest.Feed(e, "Go- new<Esc>gg")
	editortest.Settle(t, e, func() bool {
		at := strings.Index(editortest.Text(e), "- new")
		return slices.Contains(e.Views()[0].Ghosts, wire.Ghost{Pos: at, End: at + 1, Text: "•", Style: "markdown.bullet"})
	})
	// render false shows the text as it is.
	editortest.Feed(e, ":set render false<CR>")
	editortest.Settle(t, e, func() bool { v := e.Views()[0]; return len(v.Ghosts) == 0 && len(v.Grounds) == 0 })
}
