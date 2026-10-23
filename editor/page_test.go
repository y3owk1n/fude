package editor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
)

// pages is the session's tab pages by their buffers' names, the current
// one starred.
func pages(e *editor.Session) string {
	var out []string
	for _, p := range e.Pages() {
		name := filepath.Base(p.Name)
		if p.Current {
			name = "*" + name
		}
		out = append(out, name)
	}
	return strings.Join(out, " ")
}

// B544
func TestB544TabPages(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	change(t, a, "one\n")
	change(t, b, "two\n")
	t.Chdir(dir)
	e := withCommands(open(t, a))
	e.SetSize(80, 24)
	// A new page goes after the current one, on a new empty buffer or on a
	// file.
	for _, c := range [][2]string{
		{":tabnew<CR>", "a.txt *[No Name]"},
		{":tabnext 1<CR>:tabedit b.txt<CR>", "a.txt *b.txt [No Name]"},
		// gt goes round, {n}gt to page n, and gT back.
		{"gt", "a.txt b.txt *[No Name]"},
		{"gt", "*a.txt b.txt [No Name]"},
		{"3gt", "a.txt b.txt *[No Name]"},
		{"gT", "a.txt *b.txt [No Name]"},
		{"3gT", "a.txt *b.txt [No Name]"},
		// :tabmove puts the page after page n, first with 0, by +n or -n, or
		// last.
		{":tabmove 0<CR>", "*b.txt a.txt [No Name]"},
		{":tabmove<CR>", "a.txt [No Name] *b.txt"},
		{":tabmove -1<CR>", "a.txt *b.txt [No Name]"},
		{":tabmove 3<CR>", "a.txt [No Name] *b.txt"},
	} {
		if feed(e, c[0]); pages(e) != c[1] {
			t.Fatalf("%s left the pages %q, want %q", c[0], pages(e), c[1])
		}
	}
	if feed(e, ":tabmove +1<CR>"); e.Message() != "cannot move the tab page to +1" {
		t.Errorf(":tabmove +1 on the last page said %q", e.Message())
	}
	if feed(e, ":tabnext 9<CR>"); e.Message() != "no tab page 9" {
		t.Errorf(":tabnext 9 said %q", e.Message())
	}
	// Each page keeps its own windows and the one with the focus.
	feed(e, "1gt<C-w>v<C-w>h")
	if feed(e, "gt"); len(e.Windows()) != 1 {
		t.Fatalf("the second page shows %d windows, want its own one", len(e.Windows()))
	}
	if feed(e, "gT"); len(e.Windows()) != 2 || !e.Windows()[0].Current {
		t.Fatalf("back on the first page, %d windows show, the left current %v", len(e.Windows()), e.Windows()[0].Current)
	}
	// <C-w>T moves the window to a page of its own after this one.
	if feed(e, "<C-w>T"); pages(e) != "a.txt *a.txt [No Name] b.txt" || len(e.Windows()) != 1 {
		t.Fatalf("<C-w>T left the pages %q with %d windows", pages(e), len(e.Windows()))
	}
	if feed(e, "<C-w>T"); e.Message() != "already only one window" {
		t.Errorf("<C-w>T in the only window said %q", e.Message())
	}
	// Closing a page shows the one on its right, and :q in a page's last
	// window closes the page. Its buffers stay.
	if feed(e, ":tabclose<CR>"); pages(e) != "a.txt *[No Name] b.txt" {
		t.Fatalf(":tabclose left the pages %q", pages(e))
	}
	if feed(e, ":q<CR>"); pages(e) != "a.txt *b.txt" || e.Done() {
		t.Fatalf(":q in the page's last window left the pages %q, done %v", pages(e), e.Done())
	}
	if feed(e, ":tabonly<CR>"); pages(e) != "*b.txt" {
		t.Fatalf(":tabonly left the pages %q", pages(e))
	}
	if feed(e, ":tabclose<CR>"); e.Message() != "cannot close the last tab page" {
		t.Errorf(":tabclose on the last page said %q", e.Message())
	}
	if feed(e, ":b a.txt<CR>"); filepath.Base(e.Windows()[0].Name) != "a.txt" {
		t.Errorf("a.txt's buffer went with its pages")
	}
}

// B546
func TestB546SavedTabPages(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	change(t, a, "one\n")
	change(t, b, "two\n")
	e := start(t, a)
	e.SetSize(80, 20)
	feed(e, ":tabedit "+b+"<CR><C-w>v:tabedit "+a+"<CR>gT:restart<CR>")
	if !e.Done() || e.Restart() == "" {
		t.Fatal(":restart did not quit with a saved session")
	}
	t.Setenv(editor.RestartEnv, e.Restart())
	e = start(t)
	e.SetSize(80, 20)
	if pages(e) != "a.txt *b.txt a.txt" || len(e.Windows()) != 2 {
		t.Errorf("the restarted session has the pages %q, %d windows on the current one", pages(e), len(e.Windows()))
	}
}
