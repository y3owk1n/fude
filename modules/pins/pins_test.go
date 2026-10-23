package pins_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/modules/buffers"
	"github.com/y3owk1n/fude/modules/explorer"
	"github.com/y3owk1n/fude/modules/pins"
	"github.com/y3owk1n/fude/modules/window"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// project makes the files under a new working directory, each holding two
// lines, and opens the first with pins.
func project(t *testing.T, files ...string) *editor.Session {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		editortest.Change(t, filepath.Join(dir, f), "first line\nsecond line\n")
	}
	t.Chdir(dir)
	return start(t, files[0])
}

// start opens path with pins.
func start(t *testing.T, path string) *editor.Session {
	t.Helper()
	e := editortest.Start(t, path)
	e.Use(pins.New())
	e.Use(buffers.New())
	e.SetSize(80, 24)
	return e
}

// says feeds keys and fails unless the message is then want.
func says(t *testing.T, e *editor.Session, keys, want string) {
	t.Helper()
	if editortest.Feed(e, keys); e.Message() != want {
		t.Errorf("%s said %q, want %q", keys, e.Message(), want)
	}
}

// shows fails unless the current window shows name.
func shows(t *testing.T, e *editor.Session, name string) {
	t.Helper()
	if got := current(e).Name; got != name {
		t.Errorf("the window shows %q, want %q", got, name)
	}
}

// current is the current window.
func current(e *editor.Session) wire.Window {
	vs := e.Windows()
	return vs[slices.IndexFunc(vs, func(v wire.Window) bool { return v.Current })]
}

// B356
func TestB356Pin(t *testing.T) {
	e := project(t, "a.txt", "b.txt", "c", "d", "e", "f", "g", "h", "i", "j")
	says(t, e, " a", "pin 1")
	says(t, e, ":pin<CR>", "already pin 1")
	says(t, e, ":e b.txt<CR> a", "pin 2")
	says(t, e, ":pin remove<CR>", "unpinned")
	says(t, e, ":pin remove<CR>", "this file is not pinned")
	says(t, e, ":enew<CR>:pin<CR>", "this buffer has no file to pin")
	for _, f := range []string{"b.txt", "c", "d", "e", "f", "g", "h", "i"} {
		editortest.Feed(e, ":e "+f+"<CR>:pin<CR>")
	}
	says(t, e, ":e j<CR>:pin<CR>", "9 pins already, :pin list takes one off")
	// They are kept across fude's runs.
	editortest.Feed(e, ":qa!<CR>")
	e = start(t, "j")
	editortest.Feed(e, ":pin list<CR>")
	if got := editortest.Text(e); got != "a.txt\nb.txt\nc\nd\ne\nf\ng\nh\ni\n" {
		t.Errorf("after a restart the pins are %q", got)
	}
}

// B357
func TestB357GoToPin(t *testing.T) {
	editortest.Config(t, "[keymap.normal]\nQ = \"<Plug>(pin.next)\"\nW = \"<Plug>(pin.prev)\"\n")
	e := project(t, "a.txt", "b.txt", "c.txt")
	editortest.Feed(e, " a:pin list<CR>Gob.txt<Esc>:w<CR>q")
	// The cursor moves in a.txt, then b.txt opens. The module hears the move
	// first, so once b.txt shows its pin the move is kept.
	editortest.Feed(e, "jll:e b.txt<CR>")
	editortest.Settle(t, e, func() bool { return slices.Equal(current(e).LeftSegments, []string{"2/2"}) })
	editortest.Feed(e, " 1")
	shows(t, e, "a.txt")
	if e.Cursor() != len("first line\n")+2 {
		t.Errorf(" 1 put the cursor at %d", e.Cursor())
	}
	editortest.Feed(e, "Q")
	shows(t, e, "b.txt")
	editortest.Feed(e, "Q")
	shows(t, e, "a.txt")
	editortest.Feed(e, "W")
	shows(t, e, "b.txt")
	editortest.Feed(e, ":e c.txt<CR>W")
	shows(t, e, "b.txt")
	says(t, e, " 3", "no pin 3")
	// The cursor is where it was even after fude quit.
	editortest.Feed(e, ":qa!<CR>")
	e = start(t, "c.txt")
	editortest.Feed(e, ":pin 1<CR>")
	if shows(t, e, "a.txt"); e.Cursor() != len("first line\n")+2 {
		t.Errorf("after a restart :pin 1 put the cursor at %d", e.Cursor())
	}
	// A pin whose file is gone stays.
	if err := os.Remove("b.txt"); err != nil {
		t.Fatal(err)
	}
	says(t, e, " 2", "pin 2, b.txt, is gone")
	editortest.Feed(e, ":pin list<CR>")
	if got := editortest.Text(e); got != "a.txt\nb.txt\n" {
		t.Errorf("a gone pin left the pins %q", got)
	}
}

// B358, B327, B343
func TestB358EditPins(t *testing.T) {
	e := project(t, "a.txt", "b.txt", "c.txt")
	editortest.Feed(e, " a:e b.txt<CR> a h")
	if got := editortest.Text(e); got != "a.txt\nb.txt\n" {
		t.Fatalf(" h listed %q", got)
	}
	editortest.Feed(e, "ddp")
	says(t, e, "q", "the pins have changes not written, :w writes them")
	says(t, e, ":w<CR>", "")
	editortest.Feed(e, "q")
	shows(t, e, "b.txt")
	if got := current(e).LeftSegments; !slices.Equal(got, []string{"1/2"}) {
		t.Errorf("after the swap b.txt shows %v", got)
	}
	// What stops the write names the line.
	editortest.Feed(e, " h")
	says(t, e, "Onone.txt<Esc>:w<CR>", "line 1: none.txt is no file")
	says(t, e, "ddyyp:w<CR>", "line 2: b.txt is pinned already")
	// A line typed pins its file, and one taken out unpins its.
	editortest.Feed(e, "ddGoc.txt<Esc>ggdd:w<CR>")
	// <CR> opens the file on the cursor's line and closes the pins.
	editortest.Feed(e, "G<CR>")
	shows(t, e, "c.txt")
	editortest.Settle(t, e, func() bool { return slices.Equal(current(e).LeftSegments, []string{"2/2"}) })
	says(t, e, ":e b.txt<CR> 2", "")
	shows(t, e, "c.txt")
}

// B359, B312
func TestB359PinOnStatusLine(t *testing.T) {
	e := project(t, "a.txt", "b.txt", "c.txt")
	e.Use(window.New())
	editortest.Feed(e, " a:vsplit<CR>:e b.txt<CR> a:vsplit<CR>:e c.txt<CR>")
	// c.txt, not pinned, shows none.
	segs := func() []string {
		var out []string
		for _, v := range e.Windows() {
			out = append(out, v.Name+" "+strings.Join(v.LeftSegments, " "))
		}
		return out
	}
	if want := []string{"a.txt 1/2", "b.txt 2/2", "c.txt "}; !slices.Equal(segs(), want) {
		t.Errorf("the status lines show %q, want %q", segs(), want)
	}
	// Every window's count changes as soon as the pins do.
	editortest.Feed(e, ":e a.txt<CR>:pin remove<CR>")
	if want := []string{"a.txt ", "b.txt 1/1", "a.txt "}; !slices.Equal(segs(), want) {
		t.Errorf("after :pin remove the status lines show %q, want %q", segs(), want)
	}
}

// B327, B142
func TestB327ConfigKeysWin(t *testing.T) {
	editortest.Config(t, "[keymap.normal]\n\"<Leader>a\" = \"x\"\nQ = \"<Plug>(pin.add)\"\n[keymap.pins]\nZ = \"<Plug>(pin.close)\"\n")
	e := project(t, "a.txt")
	// config.toml's mapping of a module's key wins, and its plug runs from
	// any key.
	editortest.Feed(e, " a")
	if got := editortest.Text(e); got != "irst line\nsecond line" {
		t.Errorf("<Leader>a, mapped to x, left %q", got)
	}
	says(t, e, "Q", "pin 1")
	// A module's kind of buffer takes a table of its own.
	editortest.Feed(e, ":pin list<CR>Z")
	shows(t, e, "a.txt")
	// Without its mapping, :config reload gives the module's key back.
	editortest.Config(t, "")
	says(t, e, ":config reload<CR>", "reloaded config.toml")
	says(t, e, " a", "already pin 1")
}

// B360, B346
func TestB360PinFollowsRename(t *testing.T) {
	e := project(t, "b.txt", "a.txt", "sub/x.txt")
	e.Use(explorer.New())
	// a.txt and sub/x.txt are pinned and not open.
	editortest.Feed(e, ":pin list<CR>ia.txt<CR>sub/x.txt<Esc>:w<CR>q")
	// One write renames a.txt to z.txt and sub/ to new/.
	editortest.Feed(e, ":e .<CR>gg/a.txt<CR>cwz<Esc>/sub<CR>cwnew<Esc>:w<CR>y")
	// goes feeds :pin n until it shows name, since the module hears the
	// moves after the write.
	goes := func(n, name string) {
		editortest.Settle(t, e, func() bool {
			editortest.Feed(e, ":pin "+n+"<CR>")
			return current(e).Name == name
		})
	}
	goes("1", "z.txt")
	goes("2", filepath.Join("new", "x.txt"))
}

// B361, B332
func TestB361GuestPins(t *testing.T) {
	e := project(t, "a.txt", "b.txt", "c.txt")
	outside := filepath.Join(t.TempDir(), "o.txt")
	editortest.Change(t, outside, "o\n")
	editortest.Feed(e, " a:e c.txt<CR> a:e a.txt<CR>")
	guest := e.Attach("guest", editor.Guest)
	guest.SetSize(80, 24)
	// A guest's pins start empty and are its own.
	editortest.Feed(guest, ":pin list<CR>")
	if got := editortest.Text(guest); got != "" {
		t.Errorf("the guest's pins start as %q", got)
	}
	says(t, guest, "q:e b.txt<CR> a", "pin 1")
	if got := current(guest).LeftSegments; !slices.Equal(got, []string{"1/1"}) {
		t.Errorf("the guest's b.txt shows %v", got)
	}
	if got := current(e).LeftSegments; !slices.Equal(got, []string{"1/2"}) {
		t.Errorf("the host's a.txt shows %v", got)
	}
	editortest.Feed(e, ":e b.txt<CR>")
	editortest.Settle(t, e, func() bool { return current(e).Name == "b.txt" })
	if got := current(e).LeftSegments; len(got) != 0 {
		t.Errorf("the host's b.txt shows the guest's pin %v", got)
	}
	says(t, guest, " h", "")
	says(t, guest, "O"+outside+"<Esc>:w<CR>", "line 1: a guest may not pin a file outside the project")
	// Another session's pins start empty, as the guest's are never kept.
	editortest.Feed(guest, ":q!<CR>")
	again := e.Attach("guest", editor.Guest)
	again.SetSize(80, 24)
	editortest.Feed(again, ":pin list<CR>")
	if got := editortest.Text(again); got != "" {
		t.Errorf("a guest joining again has the pins %q", got)
	}
}
