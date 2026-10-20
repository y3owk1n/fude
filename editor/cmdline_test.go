package editor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	completionmod "github.com/y3owk1n/fude/modules/completion"
	"github.com/y3owk1n/fude/modules/debug"
	"github.com/y3owk1n/fude/modules/grep"
	helpmod "github.com/y3owk1n/fude/modules/help"
	listsmod "github.com/y3owk1n/fude/modules/lists"
	sessionsmod "github.com/y3owk1n/fude/modules/sessions"
	"github.com/y3owk1n/fude/modules/task"
	timelinemod "github.com/y3owk1n/fude/modules/timeline"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/wire"
)

// B131, B290
func TestB131CommandCompletion(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "editor"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "editor", "files.go"), "")
	change(t, filepath.Join(dir, "readme"), "")
	t.Chdir(dir)
	config(t, "autocomplete = true")
	e := start(t, "readme")
	e.Use(listsmod.New())
	e.Use(sessionsmod.New())
	e.Use(helpmod.New())
	e.Use(timelinemod.New())
	e.Use(task.New())
	e.Use(debug.New())
	e.SetSize(80, 24)
	for _, c := range []struct{ keys, line string }{
		// <Tab> puts the best match in the line, and <C-e> takes it back.
		{":ti<Tab>", ":timeline"},
		{"<C-e>", ":ti"},
		// A path completes in the directory typed so far.
		{"<Esc>:e ed<Tab>", ":e editor/"},
		{"f<Tab>", ":e editor/files.go"},
		// <S-Tab> goes back from the lit first match to the last.
		{"<Esc>:set _case<S-Tab>", ":set ignore_case"},
		// :set completes the settings too, and then their values.
		{"<Esc>:set highlight_c<Tab>", ":set highlight_colors"},
		{" f<Tab>", ":set highlight_colors false"},
		{"<Esc>:set completion_sources lsp,b<Tab>", ":set completion_sources lsp,buffer"},
	} {
		if feed(e, c.keys); e.Line() != c.line {
			t.Fatalf("after %q the line is %q, want %q", c.keys, e.Line(), c.line)
		}
	}
	lit := func(want string) bool {
		f, _ := e.Menu()
		return slices.ContainsFunc(f.Spans, func(s syntax.Span) bool { return s.Name == "menu.selected" && f.Text[s.Start:s.End] == want })
	}
	// <CR> on a name that is no command runs the menu's first, and on one
	// that is a command runs it as typed.
	if feed(e, "<Esc>:timel<CR>"); len(e.Windows()) != 2 {
		t.Fatalf(":timel<CR> left %d windows, want the timeline's too", len(e.Windows()))
	}
	feed(e, ":q<CR>")
	if feed(e, ":ls<CR>"); !strings.Contains(e.Message(), `"readme"`) {
		t.Errorf(":ls<CR> said %q, want :ls to list the buffers, not :lsp", e.Message())
	}
	// A command typed whole, or by a short name, shows its own item lit.
	if feed(e, ":w"); !lit("write") {
		t.Errorf(":w lights none of the menu, want write")
	}
	// An argument takes the menu's lit item too, and <C-e> keeps what is
	// typed, as for a new file.
	if feed(e, "<Esc>:debug sta<CR>"); !strings.HasPrefix(e.Message(), "no launch settings") {
		t.Errorf(":debug sta<CR> said %q, want :debug start to run", e.Message())
	}
	feed(e, ":e rea")
	settle(t, e, func() bool { return lit("readme") })
	if feed(e, "<CR>"); e.Windows()[0].Name != "readme" {
		t.Errorf(":e rea<CR> opened %s, want readme", e.Windows()[0].Name)
	}
	feed(e, ":e rea")
	settle(t, e, func() bool { return lit("readme") })
	if feed(e, "<C-e><CR>"); e.Windows()[0].Name != "rea" {
		t.Errorf(":e rea<C-e><CR> opened %s, want the new file rea", e.Windows()[0].Name)
	}
	feed(e, ":b readme<CR>")
	// A short name matches as its command, which shows its short names.
	feed(e, "<Esc>:ls")
	if f, _ := e.Menu(); !regexp.MustCompile(`(?m)^buffers +ls$`).MatchString(f.Text) || !lit("buffers") {
		t.Errorf(":ls shows %q, want buffers lit with ls beside it", f.Text)
	}
	feed(e, "<Esc>")
	// Other sessions complete for :follow and guests for :kick.
	e.Attach("pat", editor.Host)
	e.Attach("guest", editor.Guest).SetName("sam")
	for _, c := range []struct{ keys, line string }{
		{"<Esc>:follow p<Tab>", ":follow pat"},
		{"<Esc>:kick <Tab>", ":kick sam"},
	} {
		if feed(e, c.keys); e.Line() != c.line {
			t.Errorf("after %q the line is %q, want %q", c.keys, e.Line(), c.line)
		}
	}
	// Tasks complete off the loop as the line is typed.
	if err := os.MkdirAll(".fude", 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(".fude", "tasks.toml"), "[task.build]\ncommand = \"true\"\n")
	feed(e, "<Esc>:task b")
	settle(t, e, func() bool { f, _ := e.Menu(); return strings.HasPrefix(f.Text, "build") })
	// A menu that comes later lights its item too.
	if !lit("build") {
		t.Error(":task b lights none of its menu, want build")
	}

	// With autocomplete, the menu shows as the line is typed.
	feed(e, "<Esc>:bu")
	if f, ok := e.Menu(); !ok || !strings.HasPrefix(f.Text, "buffer") {
		t.Fatalf("typing :bu shows the menu %q", f.Text)
	}
	// A long menu shows its best matches first, lighting the first, and
	// <C-n> goes on to the second.
	feed(e, "<Esc>:h plug")
	f, _ := e.Menu()
	items := strings.Split(f.Text, "\n")
	if !lit(items[0]) {
		t.Errorf("the menu %q lights none of its first", items[:2])
	}
	if feed(e, "<C-n>"); e.Line() != ":h "+items[1] {
		t.Errorf("the menu starts with %q, and <C-n> put %q in the line", items[:2], e.Line())
	}
	// A path's menu shows once its directory is read.
	feed(e, "<Esc>:e ed")
	settle(t, e, func() bool { f, ok := e.Menu(); return ok && strings.HasPrefix(f.Text, "editor/\n") })
}

// B257
func TestB257Cdo(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "foo one\nfoo two\nkeep\n")
	change(t, filepath.Join(dir, "b.txt"), "foo three\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := withCommands(open(t, "a.txt"))
	e.Use(grep.New())
	e.SetSize(100, 24)
	feed(e, ":pick grep foo<CR>")
	// The query and three matches.
	settle(t, e, func() bool { f, _ := e.Menu(); return strings.Count(f.Text, "foo") == 4 })
	feed(e, "<C-q>:cclose<CR>")
	texts := func() (out []string) {
		for _, name := range []string{"a.txt", "b.txt"} {
			feed(e, ":b "+name+"<CR>")
			out = append(out, text(e))
		}
		return out
	}
	if feed(e, ":cfdo<CR>"); e.Message() != "run what? :cfdo cmd" {
		t.Errorf(":cfdo alone said %q", e.Message())
	}
	// :cfdo runs once in each file, at its first place.
	if feed(e, ":cfdo normal Ix<CR>"); e.Message() != "ran :normal Ix in 2 files" {
		t.Errorf(":cfdo said %q", e.Message())
	}
	if got := texts(); !slices.Equal(got, []string{"xfoo one\nfoo two\nkeep", "xfoo three"}) {
		t.Errorf(":cfdo left %q", got)
	}
	// A command that fails stops there and says why.
	if feed(e, ":cdo s/nowhere/x/<CR>"); strings.HasPrefix(e.Message(), "ran") {
		t.Errorf(":cdo with a failing command said %q", e.Message())
	}
	// So does one of fude's own commands, as :cd to no folder, and a
	// module's, as :b naming no buffer.
	if feed(e, ":cdo cd nowhere<CR>"); !strings.HasPrefix(e.Message(), "cd nowhere: ") {
		t.Errorf(":cdo with a failing :cd said %q", e.Message())
	}
	if feed(e, ":cdo b nowhere<CR>"); strings.HasPrefix(e.Message(), "ran") {
		t.Errorf(":cdo with a failing :b said %q", e.Message())
	}
	// :cdo runs at each place, which stays on its text as the lines before
	// it go.
	if feed(e, ":cdo d<CR>"); e.Message() != "ran :d at 3 places in 2 files" {
		t.Errorf(":cdo said %q", e.Message())
	}
	if got := texts(); !slices.Equal(got, []string{"keep", ""}) {
		t.Errorf(":cdo d left %q", got)
	}
}

// B154
func TestB154SubstitutePreview(t *testing.T) {
	e := open(t, "")
	e.SetSize(80, 24)
	feed(e, "ifoo bar foo<CR>foo<Esc>gg")
	// While the pattern is typed, its match on the line shows.
	if feed(e, ":s/foo"); !slices.Equal(e.Windows()[0].Matches, [][2]int{{0, 3}}) {
		t.Fatalf("typing :s/foo shows matches %v", e.Windows()[0].Matches)
	}
	// With a replacement, each match is struck out before what replaces it,
	// and the text stays as it is.
	feed(e, "/x")
	v := e.Windows()[0]
	if len(v.Ghosts) != 1 || v.Ghosts[0] != (wire.Ghost{Pos: 3, Text: "x", Style: "sub.new"}) || text(e) != "foo bar foo\nfoo" {
		t.Fatalf("typing :s/foo/x shows %v over %q", v.Ghosts, text(e))
	}
	if feed(e, "<Esc>:%s/foo/y/g"); len(e.Windows()[0].Ghosts) != 3 {
		t.Fatalf(":%%s/foo/y/g previews %d replacements, want 3", len(e.Windows()[0].Ghosts))
	}
	if feed(e, "<Esc>"); len(e.Windows()[0].Ghosts) != 0 || text(e) != "foo bar foo\nfoo" {
		t.Fatalf("after <Esc> the preview left %v over %q", e.Windows()[0].Ghosts, text(e))
	}
}

// B206
func TestB206CompletionSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "apple apricot\n")
	change(t, filepath.Join(dir, "notes.md"), "")
	e := open(t, path)
	e.Use(completionmod.New())
	// The nearest word comes first, and the menu names where it came from.
	feed(e, "o ap<C-x><C-o>")
	if f, _ := e.Menu(); text(e) != "apple apricot\n apricot" || !strings.Contains(f.Text, "apple  [buffer]") {
		t.Fatalf("<C-x><C-o> after ap made %q with the menu %q", text(e), f.Text)
	}
	feed(e, "<Esc>o./no<C-x><C-o>")
	settle(t, e, func() bool { return strings.HasSuffix(text(e), "./notes.md") })

	config(t, `completion_sources = ["buffer"]`)
	e = start(t, path)
	e.Use(completionmod.New())
	if feed(e, "o./no<C-x><C-o>"); e.Message() != "no completions" {
		t.Errorf("with only buffer words, a path completed to %q", text(e))
	}
	config(t, `completion_sources = ["nope"]`)
	if e = start(t, path); !strings.Contains(e.Message(), `no source "nope"`) {
		t.Errorf("an unknown source said %q", e.Message())
	}
}

// B207
func TestB207Snippets(t *testing.T) {
	config(t, "")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "snippets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	snippets := `{"for loop": {"prefix": "for", "body": ["for ${1:i} := 0; $1 < ${2:n}; $1++ {", "\t$0", "}"]}}`
	change(t, filepath.Join(dir, "all.json"), snippets)
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "x\n")
	e := start(t, path)
	e.Use(completionmod.New())
	feed(e, "ofor<C-x><C-o>")
	if text(e) != "x\nfor i := 0; i < n; i++ {\n\t\n}" {
		t.Fatalf("the snippet went in as %q", text(e))
	}
	// Typing replaces a stop's placeholder everywhere it repeats as it is
	// typed. <Tab> goes on to the next stop, <S-Tab> back, and the last
	// <Tab> to the end.
	for _, step := range []struct{ keys, want string }{
		{"<C-y>j", "x\nfor j := 0; j < n; j++ {\n\t\n}"},
		{"<Tab>m<S-Tab>k", "x\nfor k := 0; k < m; k++ {\n\t\n}"},
		{"<Tab><Tab>x", "x\nfor k := 0; k < m; k++ {\n\tx\n}"},
	} {
		if feed(e, step.keys); text(e) != step.want {
			t.Errorf("%s made %q, want %q", step.keys, text(e), step.want)
		}
	}
	_, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"snippetSupport":true`, 1)
}
