package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/internal/clipboard"
	buffersmod "github.com/y3owk1n/fude/modules/buffers"
	"github.com/y3owk1n/fude/modules/debug"
	explorermod "github.com/y3owk1n/fude/modules/explorer"
	filesmod "github.com/y3owk1n/fude/modules/files"
	"github.com/y3owk1n/fude/modules/format"
	gitmod "github.com/y3owk1n/fude/modules/git"
	grammarsmod "github.com/y3owk1n/fude/modules/grammars"
	listsmod "github.com/y3owk1n/fude/modules/lists"
	sessionsmod "github.com/y3owk1n/fude/modules/sessions"
	shellmod "github.com/y3owk1n/fude/modules/shell"
	"github.com/y3owk1n/fude/modules/task"
	terminalmod "github.com/y3owk1n/fude/modules/terminal"
	thememod "github.com/y3owk1n/fude/modules/theme"
	timelinemod "github.com/y3owk1n/fude/modules/timeline"
)

// B36, in a terminal with no clipboard
func TestB36TerminalClipboard(t *testing.T) {
	t.Cleanup(clipboard.Fake(""))
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab cd\n")
	e := open(t, path)
	feed(e, `"+yiw`)
	if text, ok := e.TerminalClipboard(); !ok || text != "ab" {
		t.Fatalf(`"+yiw gave the terminal %q`, text)
	}
	if feed(e, `$"*p`); text(e) != "ab cdab" {
		t.Fatalf(`"*p put %q`, text(e))
	}
	if _, ok := e.TerminalClipboard(); ok {
		t.Fatal("the terminal was given the copy twice")
	}
}

// B241
func TestB241AttachedClipboard(t *testing.T) {
	board := filepath.Join(t.TempDir(), "board")
	t.Cleanup(clipboard.Fake(board))
	change(t, board, "host")
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab cd\n")
	e := open(t, path)
	agent := e.Attach("agent", editor.Host)
	// Its copy goes to its client, and leaves the host's clipboard alone.
	feed(agent, `"+yiw`)
	if text, ok := agent.TerminalClipboard(); !ok || text != "ab" || disk(t, board) != "host" {
		t.Fatalf(`"+yiw in an attached session gave its client %q, and the host's clipboard holds %q`, text, disk(t, board))
	}
	// Its put takes its own copy, not the host's clipboard.
	if feed(agent, `$"+p`); text(e) != "ab cdab" {
		t.Fatalf(`"+p in an attached session put %q`, text(e))
	}
}

// B240
func TestB240AttachedLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "abc\n")
	e := open(t, path)
	// :qa leaves, and the change it made stays, unsaved, for the host.
	agent := e.Attach("agent", editor.Host)
	feed(agent, "x:qa<CR>")
	if !agent.Done() || e.Done() || text(e) != "bc" || !e.Windows()[0].Modified {
		t.Fatalf(":qa in an attached session: it done %v, fude done %v, text %q", agent.Done(), e.Done(), text(e))
	}
	for _, keys := range []string{":q<CR>", "ZZ"} {
		agent = e.Attach("agent", editor.Host)
		if feed(agent, keys); !agent.Done() || e.Done() {
			t.Errorf("%s in an attached session: it done %v, fude done %v", keys, agent.Done(), e.Done())
		}
	}
	agent = e.Attach("agent", editor.Host)
	if feed(agent, ":restart<CR>"); agent.Done() || !strings.Contains(agent.Message(), "only the terminal") {
		t.Errorf(":restart in an attached session said %q", agent.Message())
	}
}

// B244
func TestB244GuestRefuses(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	e, log := withServerAt(t, filepath.Join(project, "a.go"), "b\na\n", "utf-8")
	e.Use(debug.New())
	e.Use(task.New())
	e.Use(gitmod.New())
	e.Use(shellmod.New())
	e.Use(sessionsmod.New())
	e.Use(grammarsmod.New())
	e.Use(terminalmod.New())
	e.Use(buffersmod.New())
	e.Use(thememod.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	guest := e.Attach("guest", editor.Guest)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	theme, _, _ := e.Theme()
	ran := filepath.Join(t.TempDir(), "ran")
	for _, keys := range []string{
		":!touch " + ran + "<CR>", ":r !touch " + ran + "<CR>", ":%!sort<CR>", ":terminal<CR>", ":task<CR>",
		":pick tasks<CR>", ":restart<CR>", ":cd /<CR>", ":plugin reload<CR>", ":grammar install go<CR>",
		":grammar update<CR>", ":lsp stop<CR>", ":theme light<CR>", ":share<CR>", ":kick x<CR>", ":debug start<CR>", "ZR", "ghj", "gHj", "<Space>db", ":set autocomplete true<CR>", ":config reload<CR>",
	} {
		if feed(guest, keys); !strings.HasPrefix(guest.Message(), "a guest may not") {
			t.Errorf("%s in a guest said %q", keys, guest.Message())
		}
	}
	now, _ := os.Getwd()
	if name, _, _ := e.Theme(); now != wd || name != theme || text(e) != "b\na" || guest.Done() || len(guest.Windows()) != 1 {
		t.Errorf("a guest's refused commands left the directory %s, the theme %s, the text %q, it done %v, %d windows", now, name, text(e), guest.Done(), len(guest.Windows()))
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("a guest ran a program")
	}
	// The guest takes a code action that edits, and not one that runs a
	// command.
	feed(guest, "gra")
	settle(t, guest, func() bool { _, ok := guest.Menu(); return ok })
	feed(guest, "2")
	settle(t, guest, func() bool { return guest.Message() != "" })
	if guest.Message() != "a guest may not take a code action that runs a command" || text(e) != "b\na" {
		t.Errorf("a guest's code action that runs a command said %q and left %q", guest.Message(), text(e))
	}
	feed(guest, "gra")
	settle(t, guest, func() bool { _, ok := guest.Menu(); return ok })
	feed(guest, "1")
	settle(t, guest, func() bool { return text(e) != "b\na" })
	if text(e) != "hi b\na" {
		t.Errorf("a guest's code action that edits left %q", text(e))
	}
	for _, keys := range []string{":pick files<CR><Esc>", ":lsp<CR>"} {
		if feed(guest, keys); strings.Contains(guest.Message(), "guest") {
			t.Errorf("%s in a guest said %q", keys, guest.Message())
		}
	}
	// A guest shows no terminal, whose keys would run on the host.
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	feed(e, `:terminal cat<CR><C-\><C-n>`)
	wins := e.Windows()
	term := wins[len(wins)-1]
	if feed(guest, fmt.Sprintf(":b %d<CR>", term.Buffer)); guest.Message() != "a guest may not use a terminal" || guest.Windows()[0].Name == term.Name {
		t.Errorf(":b on the host's terminal in a guest said %q and shows %s", guest.Message(), guest.Windows()[0].Name)
	}
}

// B245
func TestB245GuestProject(t *testing.T) {
	project, away := t.TempDir(), t.TempDir()
	t.Chdir(project)
	change(t, "a.txt", "a\n")
	change(t, filepath.Join(away, "out.txt"), "out\n")
	out := filepath.Join(away, "out.txt")
	if err := os.Mkdir(".git", 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(".git", "config"), "")
	for link, to := range map[string]string{"link": away, "outlink": out} {
		if err := os.Symlink(to, link); err != nil {
			t.Fatal(err)
		}
	}
	e := withCommands(open(t, "a.txt"))
	e.Use(listsmod.New())
	e.Use(explorermod.New())
	e.Use(filesmod.New())
	e.Use(shellmod.New())
	// A guest that joins while the host shows a file outside starts on one
	// inside.
	feed(e, ":e "+out+"<CR>")
	guest := e.Attach("guest", editor.Guest)
	guest.SetSize(80, 24)
	if name := guest.Windows()[0].Name; name != "a.txt" {
		t.Fatalf("a guest joined showing %s", name)
	}
	r := editor.Request{Buffer: e.Windows()[0].Buffer, Command: editor.Command{Op: "delete", Target: editor.Target{Name: "lines"}}}
	if err := guest.Request(r); err == nil || err.Error() != out+" is outside the project" {
		t.Errorf("a guest's request naming a buffer outside said %v", err)
	}
	feed(e, ":ls<CR>")
	buffers := e.Message()
	for _, keys := range []string{
		":e " + out + "<CR>", ":e link/out.txt<CR>", ":b out<CR>", ":split " + out + "<CR>",
		":r " + out + "<CR>", ":w " + filepath.Join(away, "new.txt") + "<CR>",
		":e .<CR>/^outlink<CR><CR>", ":e .<CR>Go../made.txt<Esc>:w<CR>", "-",
		":e .git/config<CR>", ":w .git/config<CR>",
		// A file system that ignores case, as macOS's and Windows' do, reaches
		// .git as .GIT too.
		":w .GIT/config<CR>",
	} {
		if feed(guest, keys); !strings.HasSuffix(guest.Message(), "is outside the project") {
			t.Errorf("%s in a guest said %q", keys, guest.Message())
		}
	}
	feed(guest, "u:b a.txt<CR>")
	if v := guest.Windows(); len(v) != 1 || v[0].Name != "a.txt" || text(guest) != "a" {
		t.Errorf("a guest's paths outside left %d windows, on %q holding %q", len(v), v[0].Name, text(guest))
	}
	// The only buffer the guest's paths opened is the project's explorer,
	// which :ls passes as it has no number (B373).
	if feed(e, ":ls<CR>"); e.Message() != buffers {
		t.Errorf("a guest's paths outside left the buffers %s, from %s", e.Message(), buffers)
	}
	for _, path := range []string{filepath.Join(away, "new.txt"), filepath.Join(project, "..", "made.txt")} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("a guest made %s", path)
		}
	}
	// A file not made yet in the project opens.
	if feed(guest, ":e sub/new.txt<CR>"); guest.Windows()[0].Name != filepath.Join("sub", "new.txt") {
		t.Errorf(":e sub/new.txt in a guest said %q", guest.Message())
	}
	// The finder previews only files in the project.
	feed(guest, ":pick buffers<CR>out")
	settle(t, guest, func() bool {
		f, _ := guest.Menu()
		return strings.HasSuffix(f.Doc, "\n\n"+out+" is outside the project")
	})
	feed(guest, "<Esc>")
	// The finder and grep find only in the project.
	feed(e, ":cd "+away+"<CR>")
	for _, keys := range []string{":pick files<CR>", ":pick grep out<CR>"} {
		if feed(guest, keys); guest.Message() != "the working directory is outside the project" {
			t.Errorf("%s in a guest, with the working directory outside, said %q", keys, guest.Message())
		}
	}
	feed(e, ":cd "+project+"<CR>")
	// A server's rename edits no file outside, even through a link in the
	// project.
	change(t, filepath.Join(away, "other.go"), "package p\n")
	if err := os.Symlink(filepath.Join(away, "other.go"), "other.go"); err != nil {
		t.Fatal(err)
	}
	served, log := withServerAt(t, filepath.Join(project, "a.go"), "a target\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	renamer := served.Attach("guest", editor.Guest)
	feed(renamer, "wgrn<BS><BS><BS><BS><BS><BS>goal<CR>")
	settle(t, renamer, func() bool {
		return strings.Contains(renamer.Message(), "renamed") || strings.Contains(renamer.Message(), "outside")
	})
	feed(served, ":ls<CR>")
	if text(served) != "a goal" || !strings.HasSuffix(renamer.Message(), "other.go is outside the project") || strings.Contains(served.Message(), "other.go") {
		t.Errorf("a guest's rename left %q, said %q, with the buffers %s", text(served), renamer.Message(), served.Message())
	}
}

// B247
func TestB247ReadOnlyGuest(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	e, log := withServerAt(t, filepath.Join(project, "a.go"), `say "hi" target`+"\n", "utf-8")
	e.Use(listsmod.New())
	e.Use(format.New())
	e.Use(timelinemod.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	agent := e.Attach("agent", editor.Host)
	viewer := e.Attach("viewer", editor.ReadOnly)
	proposeQuote(t, agent, 5, "hello")
	feed(e, "A!<Esc>")
	want := text(e)
	readOnly := func() bool {
		return strings.Contains(viewer.Message(), "cannot make changes, the session is read-only")
	}
	// The server answers formatting and rename off the loop.
	for _, keys := range []string{"gqq", "$bgrn<BS><BS><BS><BS><BS><BS>goal<CR>"} {
		feed(viewer, keys)
		settle(t, viewer, readOnly)
		feed(viewer, ":<Esc>")
	}
	for _, keys := range []string{"x", "<Esc>ihello", "<Esc>dd", ":accept<CR>", ":e!<CR>", ":restore 0<CR>"} {
		if feed(viewer, keys); !readOnly() {
			t.Errorf("%s in a read-only guest said %q", keys, viewer.Message())
		}
	}
	feed(viewer, "<Esc>")
	r := editor.Request{Command: editor.Command{Op: "delete", Target: editor.Target{Name: "lines"}}}
	if err := viewer.Request(r); err == nil || err.Error() != "cannot make changes, the session is read-only" {
		t.Errorf("a read-only guest's request said %v", err)
	}
	if text(e) != want {
		t.Errorf("a read-only guest left %q, want %q", text(e), want)
	}
	// It still moves, searches, yanks and proposes.
	feed(viewer, "0/hi<CR>yiw:reg 0<CR>")
	if viewer.Cursor() != 5 || !strings.Contains(viewer.Message(), "hi") {
		t.Errorf("a read-only guest's search and yank left the cursor at %d and said %q", viewer.Cursor(), viewer.Message())
	}
	proposeQuote(t, viewer, 5, "bye")
	if g := e.Windows()[0].Ghosts; len(g) != 2 {
		t.Errorf("a read-only guest's proposal left the ghosts %v", g)
	}
}

// B256
func TestB256Follow(t *testing.T) {
	project, away := t.TempDir(), t.TempDir()
	t.Chdir(project)
	change(t, "a.txt", "a\n")
	change(t, "b.txt", strings.Repeat("b\n", 100))
	change(t, "c.txt", strings.Repeat("c", 300)+"\n")
	change(t, filepath.Join(away, "out.txt"), "out\n")
	config(t, "wrap = false\n")
	e := withCommands(start(t, "a.txt"))
	e.Use(sessionsmod.New())
	e.SetName("host")
	e.SetSize(80, 20)
	pat := e.Attach("pat", editor.Host)
	pat.SetSize(80, 20)
	feed(pat, ":e b.txt<CR>50G")
	// The window shows what pat's shows, and keeps up as pat moves.
	same := func() bool {
		v, w := e.Windows()[0], pat.Windows()[0]
		return v.Name == w.Name && v.Cursor == w.Cursor && v.Top == w.Top
	}
	if feed(e, ":follow pat<CR>"); !same() {
		t.Fatalf(":follow pat shows %s at %d, want pat's %s at %d", e.Windows()[0].Name, e.Windows()[0].Cursor, pat.Windows()[0].Name, pat.Windows()[0].Cursor)
	}
	if feed(pat, "90G"); !same() {
		t.Errorf("after pat moved, the follower is at %d, want %d", e.Windows()[0].Cursor, pat.Windows()[0].Cursor)
	}
	// It copies the layout, which window is current, and each window's left
	// column.
	feed(pat, ":vsplit c.txt<CR>$10h")
	vs, ws := e.Windows(), pat.Windows()
	if len(vs) != len(ws) {
		t.Fatalf("the follower has %d windows, pat %d", len(vs), len(ws))
	}
	for i := range vs {
		if vs[i].Name != ws[i].Name || vs[i].Current != ws[i].Current || vs[i].Left != ws[i].Left || vs[i].Cursor != ws[i].Cursor {
			t.Errorf("window %d of the follower is %s at %d, left %d, current %v, want pat's %s at %d, left %d, current %v", i, vs[i].Name, vs[i].Cursor, vs[i].Left, vs[i].Current, ws[i].Name, ws[i].Cursor, ws[i].Left, ws[i].Current)
		}
	}
	if ws[1].Left == 0 {
		t.Fatal("pat's window on the long line does not scroll, so the test proves nothing")
	}
	// A window too short for pat's top line scrolls to show the cursor.
	feed(pat, "<C-w>w95G")
	e.SetSize(80, 8)
	for _, v := range e.Windows() {
		if v.Name == "b.txt" && (v.Cursor/2 < v.Top/2 || v.Cursor/2 >= v.Top/2+v.H) {
			t.Errorf("the follower's short window shows lines %d on, the cursor on %d", v.Top/2, v.Cursor/2)
		}
	}
	e.SetSize(80, 20)
	// A key typed stops following.
	feed(e, "k")
	if feed(pat, "10G"); same() || e.Message() != "stopped following pat" {
		t.Errorf("a key typed left the follower following, saying %q", e.Message())
	}
	// So does a paste.
	feed(e, ":follow pat<CR>")
	if e.Paste("x"); e.Message() != "stopped following pat" {
		t.Errorf("a paste left the follower saying %q", e.Message())
	}
	// pat leaving stops it too.
	feed(e, ":follow pat<CR>")
	feed(pat, ":qa<CR>")
	if e.Windows(); e.Message() != "pat left, so following stopped" {
		t.Errorf("when pat left, the follower said %q", e.Message())
	}
	// A key's own message shows over that of following's end.
	reader := e.Attach("reader", editor.ReadOnly)
	feed(reader, ":follow host<CR>")
	if feed(reader, "x"); reader.Message() != "cannot make changes, the session is read-only" {
		t.Errorf("x in a following read-only session said %q", reader.Message())
	}
	// A guest follows only into files it may show.
	guest := e.Attach("guest", editor.Guest)
	feed(guest, ":follow host<CR>")
	feed(e, ":e "+filepath.Join(away, "out.txt")+"<CR>")
	if name := guest.Windows()[0].Name; name != "b.txt" {
		t.Errorf("a guest followed the host to %s", name)
	}
}

// typosIn returns the words spell underlines in e's first window.
