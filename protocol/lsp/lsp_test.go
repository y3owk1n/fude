package lsp_test

import (
	"strings"
	"testing"

	"github.com/y3owk1n/fude/protocol/lsp"
)

// text is a string as an lsp.Text.
type text string

func (t text) Len() int                    { return len(t) }
func (t text) Slice(start, end int) string { return string(t[start:end]) }
func (t text) Lines() int                  { return strings.Count(string(t), "\n") + 1 }
func (t text) LineOf(pos int) int          { return strings.Count(string(t[:pos]), "\n") }
func (t text) LineStart(line int) int {
	at := 0
	for ; line > 0; line-- {
		at += strings.IndexByte(string(t[at:]), '\n') + 1
	}
	return at
}

// B60: positions go to a server in the encoding it chose, and come back.
func TestPointsInEachEncoding(t *testing.T) {
	// é is two bytes and one UTF-16 unit, 😀 four bytes and two units.
	doc := text("é😀x\ny")
	for _, c := range []struct {
		enc  string
		char int
	}{{"utf-8", 6}, {"utf-16", 3}} {
		p := lsp.Point(doc, 6, c.enc)
		if p.Line != 0 || p.Character != c.char {
			t.Errorf("%s put x at %d:%d, want 0:%d", c.enc, p.Line, p.Character, c.char)
		}
		if at := lsp.Offset(doc, p, c.enc); at != 6 {
			t.Errorf("%s brought %v back to %d, want 6", c.enc, p, at)
		}
	}
	if at := lsp.Offset(doc, lsp.Position{Line: 1, Character: 0}, "utf-16"); at != 8 {
		t.Errorf("line 2 starts at %d, want 8", at)
	}
}

// B207: a completion's snippet expands with its placeholders, choices and
// variables, and says where its stops are.
func TestExpandSnippet(t *testing.T) {
	got, stops := lsp.ExpandSnippet("for ${1:i} := range ${2|xs,ys|} {\n\t$0\n}", "  ", "\t", map[string]string{"TM_FILENAME": "a.go"})
	if want := "for i := range xs {\n  \t\n  }"; got != want {
		t.Errorf("expanded to %q, want %q", got, want)
	}
	if len(stops) != 3 || stops[0].N != 1 || got[stops[0].Start:stops[0].End] != "i" || stops[2].N != 0 {
		t.Errorf("the stops are %+v", stops)
	}
	if got, _ := lsp.ExpandSnippet("$TM_FILENAME ${UNKNOWN:none}", "", "\t", map[string]string{"TM_FILENAME": "a.go"}); got != "a.go none" {
		t.Errorf("variables expanded to %q", got)
	}
}
