package diff_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/frontend/wire"
	diffmod "github.com/y3owk1n/fude/modules/diff"
	"github.com/y3owk1n/fude/modules/hints"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// files writes each text to a file of its own, named a, b and on, and
// returns their paths.
func files(t *testing.T, texts ...string) []string {
	dir := t.TempDir()
	var paths []string
	for i, text := range texts {
		path := filepath.Join(dir, string(rune('a'+i)))
		editortest.Change(t, path, text)
		paths = append(paths, path)
	}
	return paths
}

// diffed opens the texts' files side by side, each window in diff mode as
// :diffthis puts it, with the first one's window current.
func diffed(t *testing.T, texts ...string) *editor.Session {
	e := editortest.Start(t, files(t, texts...)...)
	e.Use(diffmod.New())
	e.SetSize(80, 20)
	e.Diff()
	return e
}

// window returns the window that shows the file named name.
func window(t *testing.T, e *editor.Session, name string) wire.Window {
	t.Helper()
	for _, w := range e.Windows() {
		if filepath.Base(w.Name) == name {
			return w
		}
	}
	t.Fatalf("no window shows %s", name)
	return wire.Window{}
}

// line is the text of line n of w.
func line(w wire.Window, n int) string {
	start := w.Text.LineStart(n)
	end := w.Text.Len()
	if n+1 < w.Text.Lines() {
		end = w.Text.LineStart(n+1) - 1
	}
	return w.Text.Slice(start, end)
}

// B520, B522
func TestB520DiffMode(t *testing.T) {
	paths := files(t, "one\ntwo\nthree\nfour", "one\ntwo!\nthree\nnew\nfour")
	e := editortest.Open(t, paths[0])
	e.Use(diffmod.New())
	e.SetSize(80, 20)
	editortest.Feed(e, ":diffthis<CR><C-w>v:e "+paths[1]+"<CR>:diffthis<CR>")
	a, b := window(t, e, "a"), window(t, e, "b")
	// The changed line shows as changed on both sides, with the text that
	// differs lit, and the added line as added.
	if want := map[int]string{1: "diff.change"}; !maps.Equal(a.Grounds, want) {
		t.Errorf("a's lines show %v, want %v", a.Grounds, want)
	}
	if want := map[int]string{1: "diff.change", 3: "diff.add"}; !maps.Equal(b.Grounds, want) {
		t.Errorf("b's lines show %v, want %v", b.Grounds, want)
	}
	if lit := editortest.Decorations(e); !slices.Equal(lit, []string{"! diff.text"}) {
		t.Errorf("the changed text lit is %q", lit)
	}
	// A row of fill stands in a for b's added line, above four, so four
	// stays beside four.
	if rows := a.Virtual[3]; len(rows) != 1 || !rows[0].Above || rows[0].Style != "diff.delete" || !strings.HasPrefix(rows[0].Text, "---") {
		t.Errorf("a draws %v above four, want a row of fill", rows)
	}
	// :diffoff leaves the current window's diff, and the other shows none
	// once it is alone.
	editortest.Feed(e, ":diffoff<CR>")
	if a, b := window(t, e, "a"), window(t, e, "b"); a.Grounds != nil || len(a.Virtual) != 0 || b.Grounds != nil || !b.Wrap {
		t.Errorf("after :diffoff a shows %v and %v, b %v, b wraps %v", a.Grounds, a.Virtual, b.Grounds, b.Wrap)
	}
	// :diffoff! takes every window out, so a shows none either.
	editortest.Feed(e, ":diffthis<CR>:diffoff!<CR>")
	if a, b := window(t, e, "a"), window(t, e, "b"); a.Grounds != nil || b.Grounds != nil || !a.Wrap || !b.Wrap {
		t.Errorf("after :diffoff! a shows %v, b %v", a.Grounds, b.Grounds)
	}
	// :diffsplit opens the file below, both windows in diff mode.
	editortest.Feed(e, "<C-w>o:diffsplit "+paths[0]+"<CR>")
	if a, b := window(t, e, "a"), window(t, e, "b"); a.Y <= b.Y || a.X != b.X || !maps.Equal(b.Grounds, map[int]string{1: "diff.change", 3: "diff.add"}) {
		t.Errorf(":diffsplit put a at %d,%d and b at %d,%d, b showing %v", a.X, a.Y, b.X, b.Y, b.Grounds)
	}
	// The last window in diff mode leaves it once the other closes.
	if editortest.Feed(e, ":q<CR>"); e.Message() != "" {
		t.Fatal(e.Message())
	}
	editortest.Settle(t, e, func() bool { b := window(t, e, "b"); return b.Grounds == nil && b.Wrap })
}

// B521
func TestB521DiffFollowsEdits(t *testing.T) {
	e := diffed(t, "one\ntwo\nthree", "one\ntwo!\nthree")
	editortest.Feed(e, "<C-w>ljA?<Esc>")
	editortest.Settle(t, e, func() bool {
		return slices.Equal(editortest.Decorations(e), []string{"!? diff.text"})
	})
	// Making the lines the same leaves no change to show.
	// a's cursor went to that line with b's.
	editortest.Feed(e, "<C-w>hA!?<Esc>")
	editortest.Settle(t, e, func() bool { return window(t, e, "a").Grounds == nil && window(t, e, "b").Grounds == nil })
}

// B523
func TestB523BoundWindows(t *testing.T) {
	var a, b []string
	for i := range 40 {
		a = append(a, "line "+string(rune('A'+i)))
	}
	b = slices.Concat(a[:10], []string{"new", "new", "new", "new", "new"}, a[10:])
	e := diffed(t, strings.Join(a, "\n"), strings.Join(b, "\n"))
	// The fill above a's eleventh line shows on the cursor's line too.
	editortest.Feed(e, "11G")
	if rows := window(t, e, "a").Virtual[10]; len(rows) != 5 {
		t.Fatalf("with the cursor on it, a's eleventh line draws %d rows above it, want 5", len(rows))
	}
	// Going down in a takes b down with it, the same lines level.
	editortest.Feed(e, "30G")
	wa, wb := window(t, e, "a"), window(t, e, "b")
	if top, btop := wa.Text.LineOf(wa.Top), wb.Text.LineOf(wb.Top); top == 0 || btop != top+5 {
		t.Errorf("a's top line is %d and b's %d, want b's 5 further", top, btop)
	}
	if line(wb, wb.Text.LineOf(wb.Cursor)) != "line "+string(rune('A'+29)) || wb.Wrap {
		t.Errorf("b's cursor is on %q, and b wraps %v", line(wb, wb.Text.LineOf(wb.Cursor)), wb.Wrap)
	}
	// Going up in b takes a up.
	editortest.Feed(e, "<C-w>lgg")
	if wa := window(t, e, "a"); wa.Top != 0 || wa.Cursor != 0 {
		t.Errorf("after gg in b, a's top is %d and its cursor %d", wa.Top, wa.Cursor)
	}
	// With b's third new line at its top, a starts at its third row of fill.
	editortest.Feed(e, "13Gzt")
	if wa := window(t, e, "a"); wa.Text.LineOf(wa.Top) != 10 || len(wa.Virtual[10]) != 3 {
		t.Errorf("a's top line is %d, with %d rows above it", wa.Text.LineOf(wa.Top), len(wa.Virtual[10]))
	}
}

// B524
func TestB524JumpsToChanges(t *testing.T) {
	e := diffed(t, "a\nb\nc\nd\ne\nf\ng", "a\nB\nc\nd\ne\nF\ng")
	at := func() int { return e.Text().LineOf(e.Cursor()) }
	for _, step := range []struct {
		keys string
		line int
	}{{"]c", 1}, {"]c", 5}, {"[c", 1}, {"G[c", 5}, {"gg2]c", 5}} {
		if editortest.Feed(e, step.keys); at() != step.line {
			t.Errorf("%s went to line %d, want %d", step.keys, at(), step.line)
		}
	}
	if editortest.Feed(e, "]c"); e.Message() != "no change there" {
		t.Errorf("]c past the last change said %q", e.Message())
	}
}

// B525
func TestB525GetAndPut(t *testing.T) {
	e := diffed(t, "a\nb\nc\nd\ne", "a\nB\nc\nD\ne\nf")
	editortest.Feed(e, "jdo")
	if got := editortest.Text(e); got != "a\nB\nc\nd\ne" {
		t.Errorf("do made a %q", got)
	}
	editortest.Feed(e, "2jdp")
	if got := line(window(t, e, "b"), 3); got != "d" {
		t.Errorf("dp made b's fourth line %q", got)
	}
	// d still deletes, as dd does.
	if editortest.Feed(e, "ggdd"); editortest.Text(e) != "B\nc\nd\ne" {
		t.Errorf("dd made %q", editortest.Text(e))
	}
	editortest.Feed(e, ":%diffget<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "a\nB\nc\nd\ne\nf" })
	editortest.Feed(e, ":diffput<CR>")
	if e.Message() != "no change here" {
		t.Errorf(":diffput with no change said %q", e.Message())
	}
	// With a third buffer, the other one needs a name.
	e = diffed(t, "a", "b", "c")
	if editortest.Feed(e, "do"); e.Message() != "more than two buffers in diff mode, so name one" {
		t.Errorf("do among three said %q", e.Message())
	}
	editortest.Feed(e, ":diffget c<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "c" })
	// do and dp are keys only in diff mode, so elsewhere d offers the
	// motions.
	e.Use(hints.New())
	offers := func(help string) bool { f, ok := e.Menu(); return ok && strings.Contains(f.Text, help) }
	editortest.Feed(e, "d")
	editortest.Settle(t, e, func() bool { return offers("takes the other") })
	editortest.Feed(e, "<Esc>:diffoff!<CR>d")
	editortest.Settle(t, e, func() bool { return offers("goes to the end of the line") })
	if offers("takes the other") {
		t.Error("d outside diff mode offers do")
	}
}

// B526
func TestB526DiffFlag(t *testing.T) {
	e := diffed(t, "a\nb", "a\nB", "x\nb")
	ws := e.Windows()
	if len(ws) != 3 || !ws[0].Current {
		t.Fatalf("fude -d shows %d windows, the first current %v", len(ws), ws[0].Current)
	}
	for i, w := range ws {
		if filepath.Base(w.Name) != string(rune('a'+i)) || w.Y != ws[0].Y || i > 0 && w.X <= ws[i-1].X || w.Grounds == nil {
			t.Errorf("window %d shows %s at %d,%d, with lines %v", i, w.Name, w.X, w.Y, w.Grounds)
		}
	}
}
