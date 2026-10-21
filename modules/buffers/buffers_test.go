package buffers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/buffers"
	"github.com/y3owk1n/fude/modules/help"
	"github.com/y3owk1n/fude/modules/lists"
	"github.com/y3owk1n/fude/modules/window"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

var (
	feed = editortest.Feed
	text = editortest.Text
)

// open opens path with the modules the tests need: buffers, help for :h,
// and window for :vnew, :vsplit and :only.
func open(t *testing.T, path string) *editor.Session {
	t.Helper()
	e := editortest.Open(t, path)
	e.Use(buffers.New())
	e.Use(help.New())
	e.Use(window.New())
	return e
}

// B89
func TestB89Buffers(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, a)
	e.Use(lists.New())
	feed(e, "x:e "+b+"<CR>")
	if text(e) != "bbb" {
		t.Fatalf(":e showed %q", text(e))
	}
	feed(e, ":ls<CR>")
	if want := `1+ "` + a + `"  2% "` + b + `"`; e.Message() != want {
		t.Errorf(":ls said %q, want %q", e.Message(), want)
	}
	if feed(e, ":b<CR>"); e.Message() != "which buffer? :buffer n" {
		t.Errorf(":b alone said %q", e.Message())
	}
	feed(e, ":b 1<CR>")
	if text(e) != "aa" {
		t.Fatalf(":b 1 showed %q, want the modified a", text(e))
	}
	feed(e, ":bp<CR>")
	if text(e) != "bbb" {
		t.Fatalf(":bp showed %q", text(e))
	}
	feed(e, ":bd 1<CR>")
	if !strings.Contains(e.Message(), a) {
		t.Errorf(":bd of a modified buffer said %q, want it refused", e.Message())
	}
	feed(e, ":q<CR>")
	if e.Done() || !strings.Contains(e.Message(), a) {
		t.Errorf(":q with a modified hidden buffer: done %v, message %q", e.Done(), e.Message())
	}
	feed(e, ":bd! 1<CR>:ls<CR>")
	if want := `2% "` + b + `"`; e.Message() != want {
		t.Errorf("after :bd! 1, :ls said %q, want %q", e.Message(), want)
	}
	// The first file opened takes the number of the empty buffer fude began
	// with.
	e = open(t, "")
	e.Use(lists.New())
	if feed(e, ":e "+a+"<CR>:e "+b+"<CR>:ls<CR>"); e.Message() != `1 "`+a+`"  2% "`+b+`"` {
		t.Errorf("after opening two files from an empty buffer, :ls said %q", e.Message())
	}
	// Help takes no number, so the next file is 2 and :b names no help
	// (B373).
	e = open(t, a)
	e.Use(lists.New())
	if feed(e, ":h<CR>:q<CR>:e "+b+"<CR>:ls<CR>"); e.Message() != `1 "`+a+`"  2% "`+b+`"` {
		t.Errorf("after :h and :e, :ls said %q", e.Message())
	}
	if feed(e, ":b help<CR>"); !strings.HasPrefix(e.Message(), "no buffer") {
		t.Errorf(":b help said %q", e.Message())
	}
	// :bn and :bp go through the buffers being edited, past help, wrapping
	// at either end, and :bd moves the window to one of them.
	e = open(t, a)
	feed(e, ":e "+b+"<CR>:h<CR>:only<CR>")
	name := func() string { return filepath.Base(e.Windows()[0].Name) }
	for _, c := range []struct{ keys, want string }{
		{":bn<CR>", "a"}, {":bn<CR>", "b"}, {":bn<CR>", "a"}, {":bp<CR>", "b"}, {":bp<CR>", "a"}, {":bn<CR>", "b"}, {":bd<CR>", "a"},
	} {
		if feed(e, c.keys); name() != c.want {
			t.Errorf("%s showed %s, want %s", c.keys, name(), c.want)
		}
	}
	// A buffer completes as its number, then its path from the working
	// directory, and that names the buffer.
	t.Chdir(dir)
	e = open(t, a)
	e.SetSize(80, 24)
	feed(e, ":e "+b+"<CR>:b <Tab>")
	if e.Line() != ":b 1 a" {
		t.Errorf(":b <Tab> made the line %q, want :b 1 a", e.Line())
	}
	if feed(e, "<CR>"); filepath.Base(e.Windows()[0].Name) != "a" {
		t.Errorf(":b 1 a showed %s", e.Windows()[0].Name)
	}
	// :bd of the buffer showing goes back to the one shown before, not to
	// an empty buffer opened after it.
	e = open(t, a)
	e.SetSize(80, 24)
	feed(e, ":e "+b+"<CR>:vnew<CR><C-w>h:bd<CR>")
	if v := e.Windows(); len(v) != 2 || filepath.Base(v[0].Name) != "a" {
		t.Errorf(":bd from b showed %v, want a", v)
	}
	// A window with no buffer shown before, as a new split, passes an empty
	// buffer too.
	e = open(t, a)
	e.SetSize(80, 24)
	feed(e, ":e "+b+"<CR>:enew<CR>:b 2<CR>:vsplit<CR>:bd<CR>")
	for _, v := range e.Windows() {
		if filepath.Base(v.Name) != "a" {
			t.Errorf(":bd in a new split showed %s, want a", v.Name)
		}
	}
	// Text in a file's own name names it, though another's directory holds
	// it too.
	sub := filepath.Join(dir, "data")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	e = open(t, a)
	if feed(e, ":e "+filepath.Join(sub, "b")+"<CR>:b a<CR>"); filepath.Base(e.Windows()[0].Name) != "a" {
		t.Errorf(":b a showed %s, with message %q", e.Windows()[0].Name, e.Message())
	}
}

// B428
func TestB428BracketBuffers(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a", "b", "c", "d"} {
		paths = append(paths, filepath.Join(dir, name))
		editortest.Change(t, paths[len(paths)-1], name+"\n")
	}
	e := editortest.Start(t, paths...)
	e.Use(buffers.New())
	name := func() string { return filepath.Base(e.Windows()[0].Name) }
	for _, c := range []struct{ keys, want string }{
		{"]b", "b"}, {"2]b", "d"}, {"]b", "a"}, {"[b", "d"}, {"3[b", "a"}, {"]B", "d"}, {"[B", "a"},
	} {
		if feed(e, c.keys); name() != c.want {
			t.Errorf("%s showed %s, want %s", c.keys, name(), c.want)
		}
	}
}
