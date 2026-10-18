package markdown_test

import (
	"os"
	"path/filepath"
	"slices"
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
	src := "\n# Title\n- a\n  - b\n- [ ] todo\n> quote\n---\n```go\nx := 1\n```\nSome *em* and [link](http://x.y).\n\n| a | bb |\n|---|----|\n| *ccc* | d |\n"
	path := filepath.Join(t.TempDir(), "notes.md")
	editortest.Change(t, path, src)
	e := editortest.Open(t, path)
	e.SetSize(80, 20)
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
		// pipe pads by two and d's by none.
		{Pos: at("| bb"), End: at("| bb") + 1, Text: "  │", Style: "markdown.table"},
		{Pos: at("|---"), End: at("|---") + 1, Text: "├", Style: "markdown.table"},
		{Pos: at("---|"), End: at("---|") + 3, Text: "─────", Style: "markdown.table"},
		{Pos: at("| d"), End: at("| d") + 1, Text: "│", Style: "markdown.table"},
		{Pos: at("| d") + 4, End: at("| d") + 5, Text: " │", Style: "markdown.table"},
	}
	shown := func() bool {
		v := e.Views()[0]
		for _, g := range want {
			if !slices.Contains(v.Ghosts, g) {
				return false
			}
		}
		line := e.Text().LineOf(at("x := 1"))
		return v.Grounds[1] == "markdown.h1" && v.Grounds[line] == "markdown.code"
	}
	editortest.Settle(t, e, shown)
	// render false shows the text as it is.
	editortest.Feed(e, ":set render false<CR>")
	editortest.Settle(t, e, func() bool { v := e.Views()[0]; return len(v.Ghosts) == 0 && len(v.Grounds) == 0 })
}
