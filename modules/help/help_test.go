package help_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/help"
	"github.com/y3owk1n/fude/vim"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B162
func TestB162Help(t *testing.T) {
	e := editortest.Open(t, "")
	e.Use(help.New())
	e.SetSize(100, 40)
	line := func() string {
		t := e.Text()
		ls := t.LineStart(t.LineOf(e.Cursor()))
		return t.Slice(ls, vim.LineEnd(t, ls))
	}
	// Help opens on its index, and the reference comes from the tables.
	if editortest.Feed(e, ":h<CR>"); !strings.HasPrefix(editortest.Text(e), "# fude help") || !strings.Contains(editortest.Text(e), "`:write [file]  :w`\n  writes the buffer") {
		t.Fatalf(":h opened %q", editortest.Text(e)[:min(200, len(editortest.Text(e)))])
	}
	for topic, want := range map[string]string{":w": "`:write [file]  :w`", "w": "`w`", "theme": "`theme = \"dark\"`", "colors": "`[colors]`", "guide-start": "# Getting started"} {
		if editortest.Feed(e, ":h "+topic+"<CR>"); line() != want {
			t.Errorf(":h %s went to %q, want %q", topic, line(), want)
		}
	}
	if editortest.Feed(e, ":h nope<CR>"); e.Message() != "no help for nope" {
		t.Errorf(":h nope said %q", e.Message())
	}
	// <CR> on a name goes to it, from a description or the index.
	if editortest.Feed(e, ":h ;<CR>jff<CR>"); line() != "`f{char}`" {
		t.Errorf("<CR> on f went to %q", line())
	}
	if editortest.Feed(e, ":h<CR>/guide-config<CR><CR>"); line() != "# Configuring fude" {
		t.Errorf("<CR> on guide-config went to %q", line())
	}
	if editortest.Feed(e, ":h<CR>/restart)`,<CR><CR>"); line() != "`<Plug>(restart)`" {
		t.Errorf("<CR> on <Plug>(restart) went to %q, said %q", line(), e.Message())
	}
}

// written is a module with the command :hello, whose Doc is doc, and the
// guide page guide-written.
type written struct{ doc string }

func (written) Name() string         { return "written" }
func (written) Subscribed() []string { return nil }

func (w written) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "hello", Help: "says hello", Doc: w.doc, ID: 1},
		{Kind: api.Page, Name: "written", Doc: "# Writing it down\n\nWhat :hello is for."},
	}
}

func (written) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }
func (written) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

func (written) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (written) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}
func (written) Work(context.Context, api.Job) (string, error) { return "", nil }
func (written) Close(context.Context) error                   { return nil }

// B375
func TestB375ModuleDoc(t *testing.T) {
	e := editortest.Open(t, "")
	e.Use(help.New())
	e.Use(written{"Says hello, at length."})
	e.SetSize(100, 40)
	editortest.Feed(e, ":h :hello<CR>")
	if text := editortest.Text(e); !strings.Contains(text, "`:hello`\n  says hello\n\n  Says hello, at length.") {
		t.Errorf(":h :hello did not show the module's Doc")
	}
	// A module's entry with no Doc and no explanation of fude's is a gap.
	if missing, _, err := editor.HelpGaps(written{}); err != nil || !slices.Equal(missing, []string{"commands: :hello has no explanation"}) {
		t.Errorf("HelpGaps of an undocumented :hello gave %q, %v", missing, err)
	}
	if missing, _, err := editor.HelpGaps(written{"Says hello."}); err != nil || len(missing) != 0 {
		t.Errorf("HelpGaps of a documented :hello gave %q, %v", missing, err)
	}
}

// B376
func TestB376ModulePages(t *testing.T) {
	e := editortest.Open(t, "")
	e.Use(help.New())
	e.Use(written{})
	e.SetSize(100, 40)
	if editortest.Feed(e, ":h<CR>"); !strings.Contains(editortest.Text(e), "- Writing it down  `guide-written`") {
		t.Errorf("help's index lists no page of the module")
	}
	editortest.Feed(e, ":h guide-written<CR>")
	text, line := editortest.Text(e), e.Text().LineOf(e.Cursor())
	if strings.Split(text, "\n")[line] != "# Writing it down" {
		t.Errorf(":h guide-written went to %q", strings.Split(text, "\n")[line])
	}
	// fude man guide prints it after fude's own pages.
	if man, err := editor.Man("guide", written{}); err != nil || !strings.Contains(man, "What :hello is for.") {
		t.Errorf("fude man guide printed no page of the module: %v", err)
	}
}
