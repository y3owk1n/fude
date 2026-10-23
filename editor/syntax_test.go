package editor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/modules/comment"
	grammarsmod "github.com/y3owk1n/fude/modules/grammars"
	listsmod "github.com/y3owk1n/fude/modules/lists"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/wire"
)

// B46, and B43 for the edit
func TestB46Highlighting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package main\n\nfunc main() {\n\tx := len(\"hi\") // note\n}\n")
	e := start(t, path)
	e.SetSize(80, 10)
	got := captures(t, e)
	// len is a call, a builtin and an identifier, and the identifier pattern
	// comes last in the query.
	for text, name := range map[string]string{"package": "keyword", "func": "keyword", `"hi"`: "string", "// note": "comment", "len": "variable"} {
		if got[text] != name {
			t.Errorf("%s shows as %q, want %q", text, got[text], name)
		}
	}
	// The edit lands before any reparse, and the highlights it shows are the
	// old parse's moved through it.
	feed(e, "Ox<Esc>")
	if got := captures(t, e); got["package"] != "keyword" {
		t.Errorf("after an edit above it, package shows as %q", got["package"])
	}
}

// B40
func TestB40SyntaxObjects(t *testing.T) {
	src := "package p\n\ntype T struct {\n\tA int\n\tB string\n}\n\nfunc F(a int, b string) error {\n\tx := g(a, b)\n\treturn nil\n}\n"
	typ := "type T struct {\n\tA int\n\tB string\n}"
	fn := "func F(a int, b string) error {\n\tx := g(a, b)\n\treturn nil\n}"
	nested := "package p\n\nfunc F() {\n\th := func() {\n\t\treturn\n\t}\n\th(g(ab), c)\n}\n"
	for _, c := range [][3]string{
		{src, "9G^dif", "package p\n\n" + typ + "\n\nfunc F(a int, b string) error {\n\t\n}"},
		{src, "9G^vifd", "package p\n\n" + typ + "\n\nfunc F(a int, b string) error {\n\t\n}"},
		{src, "9Gdaf", "package p\n\n" + typ + "\n\n"},
		{src, "4G^dic", "package p\n\ntype T struct {\n\t\n}\n\n" + fn},
		{src, "4Gdac", "package p\n\n\n\n" + fn},
		{src, "8Gf(ldia", "package p\n\n" + typ + "\n\nfunc F(, b string) error {\n\tx := g(a, b)\n\treturn nil\n}"},
		{src, "8Gf(ldaa", "package p\n\n" + typ + "\n\nfunc F(b string) error {\n\tx := g(a, b)\n\treturn nil\n}"},
		{src, "8Gfbdaa", "package p\n\n" + typ + "\n\nfunc F(a int) error {\n\tx := g(a, b)\n\treturn nil\n}"},
		// A count, or the object again in visual mode, takes the one around it.
		{nested, "5Gdaf", "package p\n\nfunc F() {\n\th := \n\th(g(ab), c)\n}"},
		{nested, "5G2daf", "package p\n\n"},
		{nested, "5Gvafafd", "package p\n\n"},
		{nested, "7Gfa2dia", "package p\n\nfunc F() {\n\th := func() {\n\t\treturn\n\t}\n\th(, c)\n}"},
	} {
		path := filepath.Join(t.TempDir(), "a.go")
		change(t, path, c[0])
		e := start(t, path)
		e.SetSize(80, 20)
		captures(t, e)
		if feed(e, c[1]); text(e) != c[2] {
			t.Errorf("%s left %q, want %q", c[1], text(e), c[2])
		}
	}
}

// B41
func TestB41QueryOverrides(t *testing.T) {
	config(t, "")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "queries", "go")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A plain #lua-match? is a #match?, and #set! "priority" and @spell
	// change nothing. fude leaves out a pattern with a predicate it does not
	// evaluate (B46).
	src := "((comment) @keyword @spell (#set! \"priority\" 95))\n" +
		"((package_identifier) @type (#lua-match? @type \"^p$\"))\n((package_identifier) @string (#lua-match? @string \"^q\"))\n" +
		"((package_identifier) @function (#has-ancestor? @function source_file))\n"
	if err := os.WriteFile(filepath.Join(dir, "highlights.scm"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p // note\n")
	e := start(t, path)
	e.SetSize(80, 10)
	if got := captures(t, e); len(got) != 2 || got["// note"] != "keyword" || got["p"] != "type" {
		t.Errorf("with an override, captures are %v, want the comment as a keyword and p as a type", got)
	}

	objects := filepath.Join(dir, "textobjects.scm")
	if err := os.WriteFile(objects, []byte("(comment) @x\n((identifier) @y\n  (#foo? @y \"z\"))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e = start(t, path)
	if want := objects + " line 2: "; !strings.Contains(e.Message(), want) {
		t.Errorf("an unknown predicate said %q, want %q", e.Message(), want)
	}
}

// B45
func TestB45SyntaxNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p\n\nfunc F(a int, b string) error {\n\tx := g(a, b)\n\treturn nil\n}\n")
	// Each case starts in visual mode on the a in g(a, b).
	for _, c := range [][2]string{
		{"any", "(a, b)"},
		{"ananany", "x := g(a, b)"},
		{"2any", "g(a, b)"},
		{"ananiny", "(a, b)"},
		{"ananininy", "a"},
		{"iny", "a"},
		{"]ny", "b"},
		{"[ny", "a"},
		{"an[ny", "g"},
		{"ananan]ny", "return nil"},
		{"anananany", "x := g(a, b)^J\treturn nil^J"},
	} {
		e := start(t, path)
		e.Use(listsmod.New())
		e.SetSize(80, 20)
		captures(t, e)
		feed(e, "4Gf(lv"+c[0]+`:reg "<CR>`)
		if want := `""   ` + c[1]; e.Message() != want {
			t.Errorf("v%s yanked %q, want %q", c[0], e.Message(), want)
		}
	}
}

// B44
func TestB44Comments(t *testing.T) {
	src := "func f() {\n\tx := 1\n\n\t\ty := 2\n}"
	for _, c := range [][3]string{
		// Each starts after "x :=", as j5l puts it.
		{src, "j5lgcc", "func f() {\n\t|// x := 1\n\n\t\ty := 2\n}"},
		{src, "j5lgcj", "func f() {\n\t// x| := 1\n\t//\n\t\ty := 2\n}"},
		{src, "j5lgc2j", "func f() {\n\t// x| := 1\n\t//\n\t// \ty := 2\n}"},
		{src, "j5lgccgcc", "func f() {\n\t|x := 1\n\n\t\ty := 2\n}"},
		{src, "j5lgc2jgc2j", "func f() {\n\tx :=| 1\n\n\t\ty := 2\n}"},
		{src, "j5lgcc.", "func f() {\n\t|x := 1\n\n\t\ty := 2\n}"},
		{src, "j5lvjgc", "func f() {\n\t// x| := 1\n\t//\n\t\ty := 2\n}"},
		{src, "j5lgcip", "|// func f() {\n// \tx := 1\n\n\t\ty := 2\n}"},
		{src, "j5lgcck.", "|// func f() {\n\t// x := 1\n\n\t\ty := 2\n}"},
		{src, "jjgcc", "func f() {\n\tx := 1\n|\n\t\ty := 2\n}"},
		{"a\n//b\n  //  c\n//", "gc3j", "|// a\n// //b\n//   //  c\n// //"},
		{"//a\n//b\n\n  //  c\n//", "gc4j", "|a\nb\n\n   c\n"},
		// After an operator, gc is the block of commented lines around the
		// cursor, up to a blank line.
		{"x\n// a\n\t// b\n\n// c", "jjdgc", "x\n|\n// c"},
		{"x\n// a\n// b\ny", "jcgcz<Esc>", "x\n|z\ny"},
		{"x\n\n// a\n// b", "Gdgc", "x\n|"},
		{"x\n// a", "dgc", "|x\n// a"},
		// gcgc uncomments that block, and does nothing outside one.
		{"x\n// a\n\t// b\n\n// c", "jjgcgc", "x\n|a\n\tb\n\n// c"},
		{"x\n// a", "gcgc", "|x\n// a"},
	} {
		path := filepath.Join(t.TempDir(), "a.go")
		change(t, path, c[0])
		e := open(t, path)
		e.Use(comment.New())
		feed(e, c[1])
		if got := text(e)[:e.Cursor()] + "|" + text(e)[e.Cursor():]; got != c[2] {
			t.Errorf("%s on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B42
func TestB42Select(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p\n\nfunc f() {\n\ta := b + c\n}\n")
	for _, c := range [][3]string{
		{"d", "package p\n\nfunc f() {\n\t :=  + \n}", ""},
		{"du", "package p\n\nfunc f() {\n\ta := b + c\n}", ""},
		{"cxy<Esc>", "package p\n\nfunc f() {\n\txy := xy + xy\n}", ""},
		{"cxy<Esc>u", "package p\n\nfunc f() {\n\ta := b + c\n}", ""},
	} {
		e := start(t, path)
		e.SetSize(80, 20)
		captures(t, e)
		feed(e, "4GV:select (identifier) @x<CR>"+c[0])
		if text(e) != c[1] {
			t.Errorf("%s on every identifier left %q, want %q", c[0], text(e), c[1])
		}
	}
	e := start(t, path)
	e.SetSize(80, 20)
	captures(t, e)
	for query, want := range map[string]string{
		"(identifier)":                     "the query captures nothing",
		`((identifier) @x (#foo? @x "y"))`: "query line 1: fude does not evaluate #foo?",
	} {
		feed(e, "4GV:select "+query+"<CR>")
		if e.Message() != want {
			t.Errorf(":select %s said %q, want %q", query, e.Message(), want)
		}
		feed(e, "<Esc>")
	}
}

// B164
func TestB164Filetypes(t *testing.T) {
	config(t, "[filetype.toml]\nfiles = [\"*.toml.tmpl\"]\n")
	dir := t.TempDir()
	for name, want := range map[string]string{"a.toml.tmpl": "toml", "a.yml": "yaml", "a.rs": "rust", ".bashrc": "bash", "a.nix": "nix"} {
		path := filepath.Join(dir, name)
		change(t, path, "x\n")
		if got := start(t, path).Windows()[0].Filetype; got != want {
			t.Errorf("%s has filetype %q, want %q", name, got, want)
		}
	}
	// A file whose name says nothing has the filetype its #! line names.
	for line, want := range map[string]string{"#!/usr/bin/env python3": "python", "#!/bin/sh -e": "bash", "#!/usr/bin/env -S node --flag": "javascript", "plain": ""} {
		path := filepath.Join(t.TempDir(), "script")
		change(t, path, line+"\nx\n")
		if got := start(t, path).Windows()[0].Filetype; got != want {
			t.Errorf("a file starting %q has filetype %q, want %q", line, got, want)
		}
	}
	// A table named after an extension applies to that extension's filetype.
	config(t, "[filetype.yml]\nindent_width = 3\n")
	path := filepath.Join(dir, "a.yaml")
	change(t, path, "x\n")
	e := start(t, path)
	if feed(e, ">>"); text(e) != "   x" {
		t.Errorf("[filetype.yml] gave >> in a.yaml %q, want three spaces", text(e))
	}
	// :set filetype gives a buffer another filetype, with its table and
	// grammar.
	path = filepath.Join(dir, "a.txt")
	change(t, path, "x\n")
	e = start(t, path)
	if feed(e, ":set filetype yaml<CR>>>"); e.Windows()[0].Filetype != "yaml" || text(e) != "   x" {
		t.Errorf(":set filetype yaml gave filetype %q and >> %q", e.Windows()[0].Filetype, text(e))
	}
	change(t, path, "package main\n")
	e = start(t, path)
	e.SetSize(80, 10)
	if feed(e, ":set filetype go<CR>"); captures(t, e)["package"] != "keyword" {
		t.Errorf("after :set filetype go, package shows as %q", captures(t, e)["package"])
	}
}

// B165, through a grammar built by hand: Go's, as the filetype gotest.
func TestB165BuiltGrammar(t *testing.T) {
	mod, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/tree-sitter/tree-sitter-go").Output()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "src")
	if err := os.CopyFS(src, os.DirFS(filepath.Join(strings.TrimSpace(string(mod)), "src"))); err != nil {
		t.Fatal(err)
	}
	for name, from := range map[string]string{"parser.c": "tree_sitter_go(", "grammar.json": `"name": "go"`} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		b = []byte(strings.Replace(string(b), from, strings.Replace(from, "go", "gotest", 1), 1))
		if err := os.WriteFile(filepath.Join(src, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Windows will not delete the library fude loads, so the folder may
	// outlive the test.
	data, err := os.MkdirTemp("", "fude-grammar")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(data) })
	t.Setenv("XDG_DATA_HOME", data)
	out := filepath.Join(data, "fude", "grammars", "gotest")
	if err := os.CopyFS(filepath.Join(out, "queries"), os.DirFS(filepath.Join(strings.TrimSpace(string(mod)), "queries"))); err != nil {
		t.Fatal(err)
	}
	if err := syntax.Compile(filepath.Dir(src), syntax.Library(filepath.Dir(out), "gotest")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.gotest")
	change(t, path, "package p\n")
	e := start(t, path)
	e.Use(grammarsmod.New())
	e.SetSize(80, 20)
	settle(t, e, func() bool {
		return slices.ContainsFunc(e.Windows()[0].Highlights, func(s wire.Span) bool { return s.Name == "keyword" })
	})

	// A grammar built at an older pin is outdated, and :grammar update
	// builds it again. Without git on the PATH the build fails at once.
	old := filepath.Join(data, "fude", "grammars", "toml")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(old, "0000000-1"+filepath.Ext(syntax.Library("", "toml"))), "")
	if feed(e, ":grammar<CR>"); !strings.Contains(text(e), "toml         outdated") {
		t.Fatalf(":grammar listed %q, want toml outdated", text(e))
	}
	t.Setenv("PATH", "")
	if feed(e, ":q<CR>:grammar update<CR>"); e.Message() != "building the toml grammar" {
		t.Fatalf(":grammar update said %q", e.Message())
	}
	settle(t, e, func() bool { return strings.Contains(e.Message(), "git is not installed") })
}

// B166, through an injection a query override makes: Go inside a Go raw
// string.
func TestB166Injections(t *testing.T) {
	config(t, "")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "queries", "go")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The second pattern's predicate is one fude does not evaluate, which
	// leaves that pattern out but keeps the query.
	src := "((raw_string_literal_content) @injection.content (#set! injection.language \"go\"))\n" +
		"((comment) @injection.content (#offset! @injection.content 0 1 0 0) (#set! injection.language \"go\"))\n"
	if err := os.WriteFile(filepath.Join(dir, "injections.scm"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	text := "package p\n\nvar s = `func f() {}`\n"
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, text)
	e := start(t, path)
	e.SetSize(80, 10)
	at := strings.Index(text, "`func") + 1
	settle(t, e, func() bool {
		return slices.ContainsFunc(e.Windows()[0].Highlights, func(s wire.Span) bool { return s.Name == "keyword" && s.Start == at })
	})
	if e.Message() != "" {
		t.Errorf("the injections said %q", e.Message())
	}
}

// querier is a module whose :Q says what the Go query typed after it
// captures.
type querier struct{ idle }

func (querier) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "Q", Help: "runs a query", ID: 1}}
}

func (querier) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	spans, err := s.Query(map[string]string{"go": s.About["args"]})
	if err != nil {
		return api.Result{Message: err.Error()}, nil
	}
	var got []string
	for _, sp := range spans {
		got = append(got, s.Text.Slice(sp.Start, sp.End)+":"+sp.Name)
	}
	return api.Result{Message: strings.Join(got, " ")}, nil
}

// B297, with Go inside a Go raw string as the injected language
func TestB297Query(t *testing.T) {
	config(t, "")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "queries", "go")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	src := "((raw_string_literal_content) @injection.content (#set! injection.language \"go\"))\n"
	if err := os.WriteFile(filepath.Join(dir, "injections.scm"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p\n\nfunc g() {}\n\nvar s = `func f() {}`\n")
	e := start(t, path)
	e.Use(querier{})
	feed(e, ":Q (function_declaration name: (identifier) @fn)<CR>")
	settle(t, e, func() bool { return e.Message() == "g:fn f:fn" })
	// A query that does not compile says so.
	feed(e, ":Q (nope) @x<CR>")
	settle(t, e, func() bool { return strings.HasPrefix(e.Message(), "query for go") })
}
