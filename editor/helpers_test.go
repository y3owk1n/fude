package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/syntax/syntaxtest"
	"github.com/y3owk1n/fude/vim"
)

// TestMain keeps every test's timeline logs, config and caches in a
// directory of its own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fude-test")
	if err != nil {
		panic(err)
	}
	// Go's grammar builds once into the user's cache, before the cache
	// directory moves.
	if err := syntaxtest.InstallGo(filepath.Join(dir, "data")); err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	os.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	// No test fetches a grammar.
	if err := os.MkdirAll(filepath.Join(dir, "config", "fude"), 0o700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "fude", "config.toml"), []byte("auto_install = false\n"), 0o600); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func open(t *testing.T, path string) *editor.Session {
	t.Helper()
	e, err := editor.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	quitAtEnd(t, e)
	return e
}

func feed(e *editor.Session, keys string) {
	for _, k := range vim.Keys(keys) {
		e.Feed(k)
	}
}

func text(e *editor.Session) string {
	s := e.Text()
	return s.Slice(0, s.Len())
}

func disk(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func change(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func windows(e *editor.Session) string {
	var out []string
	for _, v := range e.Views() {
		cur := ""
		if v.Current {
			cur = "*"
		}
		out = append(out, fmt.Sprintf("%s%d,%d %dx%d", cur, v.X, v.Y, v.W, v.H))
	}
	return strings.Join(out, " ")
}

func run(t *testing.T, cases [][3]string) {
	t.Helper()
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte(c[0]), 0o644); err != nil {
			t.Fatal(err)
		}
		e := open(t, path)
		feed(e, c[1])
		got := text(e)
		if got = got[:e.Cursor()] + "|" + got[e.Cursor():]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

func captures(t *testing.T, e *editor.Session) map[string]string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, v := range e.Views() {
			if v.Current && len(v.Highlights) > 0 {
				out := map[string]string{}
				for _, s := range v.Highlights {
					text := v.Text.Slice(s.Start, s.End)
					out[text] = strings.TrimSpace(out[text] + " " + s.Name)
				}
				return out
			}
		}
		select {
		case f := <-e.Queue():
			f()
		case <-deadline:
			t.Fatal("no highlights after 5s")
		}
	}
}
