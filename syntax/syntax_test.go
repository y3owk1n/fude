package syntax_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/syntax/syntaxtest"
)

// TestMain builds Go's grammar, once into the user's cache, and loads
// grammars from where it put it.
func TestMain(m *testing.M) {
	data, err := os.MkdirTemp("", "fude-syntax")
	if err != nil {
		panic(err)
	}
	if err := syntaxtest.InstallGo(data); err != nil {
		panic(err)
	}
	syntax.Use(filepath.Join(data, "fude", "grammars"), nil)
	code := m.Run()
	os.RemoveAll(data)
	os.Exit(code)
}

// text is a string as a syntax.Text.
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

// named returns the capture over each span of src, by its text.
func named(src string, spans []syntax.Span) map[string]string {
	out := map[string]string{}
	for _, s := range spans {
		out[src[s.Start:s.End]] = s.Name
	}
	return out
}

// B46
func TestHighlight(t *testing.T) {
	src := "package main\n\nfunc main() {}\n"
	spans, err := syntax.Highlight("go", "", src)
	if err != nil {
		t.Fatal(err)
	}
	if got := named(src, spans); got["package"] != "keyword" || got["func"] != "keyword" {
		t.Errorf("highlighted %v, want package and func as keywords", got)
	}
	// A filetype with no grammar built has none, and no error.
	if spans, err := syntax.Highlight("cobol", "", "x"); spans != nil || err != nil {
		t.Errorf("a filetype with no grammar gave %v, %v", spans, err)
	}
}

// B40, B46: a buffer parses on its own goroutine, and its highlights and
// text objects come back through the queue.
func TestBuffer(t *testing.T) {
	queue := make(chan func(), 16)
	parsed := make(chan struct{}, 1)
	h, ok, err := syntax.New("go", "", queue, func() { parsed <- struct{}{} }, func(string, error) {}, func(string, time.Duration) {})
	if err != nil || !ok {
		t.Fatalf("New gave %v, %v", ok, err)
	}
	defer h.Close()
	src := text("package p\n\nfunc f(a int, b int) {\n\treturn\n}\n")
	h.Changed(text(""), []syntax.Edit{{Start: 0, End: 0, Text: string(src)}})
	deadline := time.After(30 * time.Second)
	var spans []syntax.Span
	for len(spans) == 0 {
		spans = h.Show(src, "view", 0, src.Len())
		select {
		case f := <-queue:
			f()
		case <-parsed:
		case <-deadline:
			t.Fatal("no highlights after 30s")
		}
	}
	if got := named(string(src), spans); got["func"] != "keyword" {
		t.Errorf("the buffer shows %v, want func as a keyword", got)
	}
	// af selects the function around the cursor, and ia the argument.
	if start, end, ok := h.Object(src, "af", 30, 31); !ok || src.Slice(start, end) != "func f(a int, b int) {\n\treturn\n}" {
		t.Errorf("af selected %q", src.Slice(start, end))
	}
	if start, end, ok := h.Object(src, "ia", 25, 26); !ok || src.Slice(start, end) != "b int" {
		t.Errorf("ia selected %q", src.Slice(start, end))
	}
}

// B41: a query in the queries directory replaces the shipped one, and one
// that fails to compile names its file and line.
func TestQueryOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "go"), 0o700); err != nil {
		t.Fatal(err)
	}
	highlights := filepath.Join(dir, "go", "highlights.scm")
	if err := os.WriteFile(highlights, []byte("(package_identifier) @type\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := "package p\n"
	spans, err := syntax.Highlight("go", dir, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := named(src, spans); len(got) != 1 || got["p"] != "type" {
		t.Errorf("with an override, highlighted %v, want p alone as a type", got)
	}
	if err := os.WriteFile(highlights, []byte("(package_identifier) @type\n(nothing_like_this) @x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := syntax.Highlight("go", dir, src); err == nil || !strings.Contains(err.Error(), highlights+" line 2") {
		t.Errorf("a query that does not compile said %v, want its file and line 2", err)
	}
}

// B44
func TestComment(t *testing.T) {
	if c, ok := syntax.Comment("go"); !ok || c != "// %s" {
		t.Errorf("go comments as %q, %v", c, ok)
	}
	if _, ok := syntax.Comment("no such filetype"); ok {
		t.Error("an unknown filetype has a comment syntax")
	}
}
