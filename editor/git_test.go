package editor_test

import (
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/modules/blame"
	gitmod "github.com/y3owk1n/fude/modules/git"
	"github.com/y3owk1n/fude/wire"
)

// B123
func TestB123GitSigns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	change(t, path, "a\nb\nc\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "f"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	e := open(t, path)
	e.Use(gitmod.New())
	e.SetSize(40, 10)
	// a becomes xa and b goes, a change that also deleted, and new is added.
	feed(e, "ix<Esc>jddGonew<Esc>")
	want := map[int]wire.Sign{0: {Text: "~", Name: "git.change"}, 2: {Text: "┃", Name: "git.add"}}
	settle(t, e, func() bool { return maps.Equal(e.Views()[0].Signs, want) })

	// Without git installed, the file shows no git signs.
	t.Setenv("PATH", "")
	e = open(t, path)
	e.Use(gitmod.New())
	if feed(e, "ix<Esc>"); len(e.Views()[0].Signs) != 0 {
		t.Fatalf("without git the signs are %v", e.Views()[0].Signs)
	}
}

// B221
func TestB221StageHunk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "a\nb\nc\n")
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
	e := open(t, path)
	e.Use(gitmod.New())
	e.SetSize(80, 10)
	feed(e, "rAGrC")
	settle(t, e, func() bool { return len(e.Views()[0].Signs) == 2 })
	// ghh stages the first line's hunk, and the last stays as it was.
	feed(e, "ggghh")
	settle(t, e, func() bool { return git("show", ":a.txt") == "A\nb\nc\n" && len(e.Views()[0].Signs) == 1 })
}

// B222
func TestB222GitDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "a\nb\n")
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	e := open(t, path)
	e.Use(gitmod.New())
	e.SetSize(80, 10)
	settle(t, e, func() bool { feed(e, ":gitdiff<CR>"); return e.Message() == "no changes from git's index" })
	feed(e, "jrB:gitdiff<CR>")
	if got := text(e); !strings.Contains(got, "-b\n+B") {
		t.Errorf(":gitdiff shows %q", got)
	}
	// Removed lines show in the diff.minus colour and added ones in diff.plus.
	if got := decorations(e); !slices.Contains(got, "-b diff.minus") || !slices.Contains(got, "+B diff.plus") {
		t.Errorf(":gitdiff colours %q", got)
	}
}

// B220
func TestB220Blame(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "one\ntwo\n")
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Ann", "-c", "user.email=a@a"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "first words")
	e := open(t, path)
	e.Use(blame.New())
	e.SetSize(80, 10)
	note := func(want string) func() bool {
		return func() bool { return strings.Contains(e.Views()[0].Notes[e.Text().LineOf(e.Cursor())], want) }
	}
	// :blame shows who changed the cursor's line, and a line changed since
	// shows it is not committed.
	feed(e, "j:blame<CR>")
	settle(t, e, note("Ann, just now · first words"))
	feed(e, "Ax<Esc>")
	settle(t, e, note("not committed yet"))
	if feed(e, ":blame<CR>"); e.Views()[0].Notes[1] != "" {
		t.Errorf("after :blame again the line shows %q", e.Views()[0].Notes[1])
	}
}

// B138
func TestB138Hunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	change(t, path, "a\nb\nc\nd\ne\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "f"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	e := open(t, path)
	e.Use(gitmod.New())
	e.SetSize(40, 10)
	feed(e, "jrBjjrD")
	settle(t, e, func() bool { return len(e.Views()[0].Signs) == 2 })
	line := func() int { return e.Text().LineOf(e.Cursor()) }
	for _, c := range []struct {
		keys string
		line int
	}{{"gg]h", 1}, {"]h", 3}, {"[h", 1}, {"]H", 3}, {"[H", 1}, {"gg2]h", 3}} {
		if feed(e, c.keys); line() != c.line {
			t.Errorf("%s went to line %d, want %d", c.keys, line(), c.line)
		}
	}
	// The preview shows the hunk's old and new lines.
	feed(e, "[h<Plug>(git.preview)")
	if f, ok := e.Float(); !ok || f.Text != "-b\n+B" {
		t.Fatalf("the preview is %q", f.Text)
	}
	// gH puts the hunks in range back as the index has them.
	if feed(e, "<Esc>]hVgH"); text(e) != "a\nB\nc\nd\ne" {
		t.Fatalf("VgH on line 4 left %q", text(e))
	}
}
