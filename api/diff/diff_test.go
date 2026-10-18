package diff_test

import (
	"strings"
	"testing"

	"github.com/y3owk1n/fude/api/diff"
)

// B222, B227: the lines that differ show removed after - and added after +,
// with lines of context around them.
func TestUnifiedShowsTheChangedLines(t *testing.T) {
	a := strings.Split("a\nb\nc\nd\ne", "\n")
	b := strings.Split("a\nB\nc\nd\ne\nf", "\n")
	got := diff.Unified(diff.Lines(a, b), 1)
	for _, want := range []string{"\n-b\n", "\n+B\n", "\n c\n", "\n+f"} {
		if !strings.Contains(got, want) {
			t.Errorf("the diff lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, " d\n") {
		t.Errorf("the diff shows d, outside one line of context:\n%s", got)
	}
}

// The edits between two texts turn the old one into the new.
func TestEditsTurnOldIntoNew(t *testing.T) {
	old, new := "one\ntwo\nthree\n", "one\n2\nthree\nfour\n"
	text := old
	edits := diff.Edits(old, new)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		text = text[:e.Start] + e.Text + text[e.End:]
	}
	if text != new {
		t.Errorf("the edits made %q, want %q", text, new)
	}
}
