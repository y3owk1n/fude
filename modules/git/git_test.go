package git_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	gitmod "github.com/y3owk1n/fude/modules/git"
	"github.com/y3owk1n/fude/wire"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B123
func TestB123GitSigns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	editortest.Change(t, path, "a\nb\nc\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "f"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	e := editortest.Open(t, path)
	e.Use(gitmod.New())
	e.SetSize(40, 10)
	// a becomes xa and b goes, a change that also deleted, and new is added.
	editortest.Feed(e, "ix<Esc>jddGonew<Esc>")
	want := map[int]wire.Sign{0: {Text: "~", Name: "git.change"}, 2: {Text: "┃", Name: "git.add"}}
	editortest.Settle(t, e, func() bool { return maps.Equal(e.Views()[0].Signs, want) })
	// Turning git_signs off removes the signs, and turning it on brings them back.
	editortest.Feed(e, ":set git_signs false<CR>")
	editortest.Settle(t, e, func() bool { return len(e.Views()[0].Signs) == 0 })
	editortest.Feed(e, ":set git_signs true<CR>")
	editortest.Settle(t, e, func() bool { return maps.Equal(e.Views()[0].Signs, want) })

	// Without git installed, the file shows no git signs.
	t.Setenv("PATH", "")
	e = editortest.Open(t, path)
	e.Use(gitmod.New())
	if editortest.Feed(e, "ix<Esc>"); len(e.Views()[0].Signs) != 0 {
		t.Fatalf("without git the signs are %v", e.Views()[0].Signs)
	}
}

// B126, B311
func TestB126ListingGitStatus(t *testing.T) {
	dir := t.TempDir()
	top := filepath.Join(dir, "top")
	if err := os.MkdirAll(filepath.Join(top, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"mod", "same"} {
		editortest.Change(t, filepath.Join(top, f), f+"\n")
	}
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	editortest.Change(t, filepath.Join(top, "mod"), "changed\n")
	editortest.Change(t, filepath.Join(top, "added"), "added\n")
	git("add", "top/added")
	editortest.Change(t, filepath.Join(top, "sub", "new"), "new\n")
	e := editortest.Open(t, top)
	e.SetSize(40, 10)
	e.Use(gitmod.New())
	// A listing below the repository's root, with an untracked file below sub/.
	want := map[int]wire.Sign{0: {Text: "+", Name: "git.add"}, 1: {Text: "✹", Name: "git.change"}, 3: {Text: "?", Name: "git.delete"}}
	editortest.Settle(t, e, func() bool { return maps.Equal(e.Views()[0].Signs, want) })

	// Without git installed, the listing shows no git signs.
	t.Setenv("PATH", "")
	e = editortest.Open(t, top)
	if e.Use(gitmod.New()); len(e.Views()[0].Signs) != 0 {
		t.Fatalf("without git the signs are %v", e.Views()[0].Signs)
	}
}

// B147, B312
func TestB147Branch(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "trunk").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	path := filepath.Join(dir, "f")
	editortest.Change(t, path, "a\n")
	e := editortest.Open(t, path)
	e.Use(gitmod.New())
	editortest.Settle(t, e, func() bool { return slices.Equal(e.Views()[0].LeftSegments, []string{"trunk"}) })
}

// B221
func TestB221StageHunk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	editortest.Change(t, path, "a\nb\nc\n")
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	e := editortest.Open(t, path)
	e.Use(gitmod.New())
	e.SetSize(80, 10)
	editortest.Feed(e, "rAGrC")
	editortest.Settle(t, e, func() bool { return len(e.Views()[0].Signs) == 2 })
	// ghh stages the first line's hunk, and the last stays as it was.
	editortest.Feed(e, "ggghh")
	editortest.Settle(t, e, func() bool { return git("show", ":a.txt") == "A\nb\nc\n" && len(e.Views()[0].Signs) == 1 })
}

// B222
func TestB222GitDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	editortest.Change(t, path, "a\nb\n")
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	e := editortest.Open(t, path)
	e.Use(gitmod.New())
	e.SetSize(80, 10)
	editortest.Settle(t, e, func() bool { editortest.Feed(e, ":gitdiff<CR>"); return e.Message() == "no changes from git's index" })
	editortest.Feed(e, "jrB:gitdiff<CR>")
	if got := editortest.Text(e); !strings.Contains(got, "-b\n+B") {
		t.Errorf(":gitdiff shows %q", got)
	}
	// Removed lines show in the diff.minus colour and added ones in diff.plus.
	if got := editortest.Decorations(e); !slices.Contains(got, "-b diff.minus") || !slices.Contains(got, "+B diff.plus") {
		t.Errorf(":gitdiff colours %q", got)
	}
}

// B138
func TestB138Hunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	editortest.Change(t, path, "a\nb\nc\nd\ne\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "f"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	e := editortest.Open(t, path)
	e.Use(gitmod.New())
	e.SetSize(40, 10)
	editortest.Feed(e, "jrBjjrD")
	editortest.Settle(t, e, func() bool { return len(e.Views()[0].Signs) == 2 })
	line := func() int { return e.Text().LineOf(e.Cursor()) }
	for _, c := range []struct {
		keys string
		line int
	}{{"gg]h", 1}, {"]h", 3}, {"[h", 1}, {"]H", 3}, {"[H", 1}, {"gg2]h", 3}} {
		if editortest.Feed(e, c.keys); line() != c.line {
			t.Errorf("%s went to line %d, want %d", c.keys, line(), c.line)
		}
	}
	// The preview shows the hunk's old and new lines.
	editortest.Feed(e, "[h<Plug>(git.preview)")
	if f, ok := e.Float(); !ok || f.Text != "-b\n+B" {
		t.Fatalf("the preview is %q", f.Text)
	}
	// gH puts the hunks in range back as the index has them.
	if editortest.Feed(e, "<Esc>]hVgH"); editortest.Text(e) != "a\nB\nc\nd\ne" {
		t.Fatalf("VgH on line 4 left %q", editortest.Text(e))
	}
}
