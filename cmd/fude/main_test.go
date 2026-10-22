package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/distribution"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/plugin"
	"github.com/y3owk1n/fude/plugin/plugintest"
)

// TestMain runs fude itself when a test runs this binary as the CLI.
func TestMain(m *testing.M) {
	if os.Getenv("FUDE_TEST_CLI") == "1" {
		main()
		return
	}
	os.Exit(editortest.Main(m))
}

// fude runs the fude command in dir with args, outside any fude's
// terminal, and returns what it printed.
func fude(dir string, args ...string) (string, error) {
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir, cmd.Env = dir, append(os.Environ(), "FUDE_TEST_CLI=1", "FUDE_SOCKET=", "FUDE_SESSION=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// serve starts fude on args and serves it on a socket, as the fude command
// does, and returns the socket.
func serve(t *testing.T, args ...string) string {
	t.Helper()
	e := editortest.Start(t, args...)
	// A socket path must be short, so it goes in a directory of its own.
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s")
	unserve := e.Serve(path)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case f := <-e.Queue():
				f()
			case <-stop:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-stopped
		unserve()
		os.RemoveAll(dir)
	})
	return path
}

// state is what fude state prints of the fude at socket.
type state struct {
	Name   string `json:"name"`
	Text   string `json:"text"`
	Cursor int    `json:"cursor"`
	Mode   string `json:"mode"`
}

// stateOf runs fude state on the fude at socket.
func stateOf(t *testing.T, socket string) state {
	t.Helper()
	out, err := fude(t.TempDir(), "state", "--socket", socket)
	var st state
	if err != nil || json.Unmarshal([]byte(out), &st) != nil {
		t.Fatalf("fude state: %v: %s", err, out)
	}
	return st
}

// B162: with the modules fude adds, every entry of help has a longer
// explanation, and every explanation names an entry.
func TestB162HelpComplete(t *testing.T) {
	missing, unused, err := editor.HelpGaps(distribution.Modules()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range append(missing, unused...) {
		t.Errorf("help: %s", g)
	}
}

// B348
func TestB348Help(t *testing.T) {
	want, err := fude(t.TempDir(), "help")
	if err != nil {
		t.Fatalf("fude help: %v: %s", err, want)
	}
	for _, c := range editor.Subcommands {
		if !strings.Contains(want, "  "+c.Name+" ") {
			t.Errorf("fude help lists no %s", c.Name)
		}
	}
	for _, f := range editor.Flags {
		if !strings.Contains(want, "  "+f.Flag+" ") {
			t.Errorf("fude help lists no %s", f.Flag)
		}
	}
	// -h and --help ask for help anywhere.
	for _, flag := range []string{"-h", "--help"} {
		if out, err := fude(t.TempDir(), "tail", flag); err != nil || out != want {
			t.Errorf("fude tail %s printed %q, %v", flag, out, err)
		}
	}
}

// B383
func TestB383Storage(t *testing.T) {
	home := t.TempDir()
	for _, v := range []string{"XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, filepath.Join(home, v))
	}
	stores, err := editor.Stores()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stores {
		if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.Path, []byte("12345"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := fude(home, "storage"); err != nil || !strings.Contains(out, "timeline   5B") || !strings.Contains(out, "total      40B") {
		t.Fatalf("fude storage printed %q, %v", out, err)
	}
	// clean asks, and anything but y keeps them all, as does clean with
	// --force.
	kept := func() (out []string) {
		for _, s := range stores {
			if _, err := os.Stat(s.Path); err == nil {
				out = append(out, s.Name)
			}
		}
		return out
	}
	clean := func(answer string, args ...string) string {
		cmd := exec.Command(os.Args[0], append([]string{"storage", "clean"}, args...)...)
		cmd.Dir, cmd.Env, cmd.Stdin = home, append(os.Environ(), "FUDE_TEST_CLI=1"), strings.NewReader(answer)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fude storage clean %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	user := []string{"timeline", "sessions", "pins", "recent", "memory"}
	if out := clean(""); !strings.Contains(out, "freed 15B") || !slices.Equal(kept(), user) {
		t.Errorf("fude storage clean printed %q and kept %v, want %v", out, kept(), user)
	}
	if out := clean("n\n", "--force"); !strings.Contains(out, "deleting it loses undo") || !slices.Equal(kept(), user) {
		t.Errorf("declining fude storage clean --force printed %q and kept %v", out, kept())
	}
	if out := clean("y\n", "--force"); !strings.Contains(out, "freed 25B") || len(kept()) > 0 {
		t.Errorf("fude storage clean --force printed %q and kept %v", out, kept())
	}
}

// B349
func TestB349Version(t *testing.T) {
	// A test binary is a build that does not know its commit.
	for _, args := range [][]string{{"version"}, {"-v"}, {"--version"}} {
		if out, err := fude(t.TempDir(), args...); err != nil || out != "fude (devel)\n" {
			t.Errorf("fude %s printed %q, %v", args[0], out, err)
		}
	}
}

// B350
func TestB350Flags(t *testing.T) {
	// Each error names the flag.
	for flag, args := range map[string][]string{"--bogus": {"--bogus", "a.txt"}, "--clean": {"tail", "--clean"}, "--socket": {"open", "a.txt", "--socket"}} {
		if out, err := fude(t.TempDir(), args...); err == nil || !strings.Contains(out, flag) {
			t.Errorf("fude %s printed %q, %v", strings.Join(args, " "), out, err)
		}
	}
	// After --, a flag is a file.
	socket := serve(t)
	if out, err := fude(t.TempDir(), "open", "--socket", socket, "--", "--help"); err != nil {
		t.Fatalf("fude open -- --help: %v: %s", err, out)
	}
	if st := stateOf(t, socket); filepath.Base(st.Name) != "--help" {
		t.Errorf("fude open -- --help shows %q", st.Name)
	}
}

// B351
func TestB351Open(t *testing.T) {
	socket := serve(t)
	dir := t.TempDir()
	for name, text := range map[string]string{"a": "one\n", "b": "two\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The paths are from the directory fude open runs in.
	if out, err := fude(dir, "open", "--socket", socket, "a", "b"); err != nil {
		t.Fatalf("fude open a b: %v: %s", err, out)
	}
	if st := stateOf(t, socket); st.Text != "one" {
		t.Fatalf("after fude open a b, fude shows %q", st.Text)
	}
	if out, err := fude(dir, "keys", "--socket", socket, ":e #<CR>"); err != nil {
		t.Fatalf("fude keys: %v: %s", err, out)
	}
	if st := stateOf(t, socket); st.Text != "two" {
		t.Errorf("b is not the other buffer, as :e # shows %q", st.Text)
	}
}

// B353
func TestB353Keys(t *testing.T) {
	socket := serve(t)
	if out, err := fude(t.TempDir(), "keys", "--socket", socket, "ihi"); err != nil {
		t.Fatalf("fude keys ihi: %v: %s", err, out)
	}
	if st := stateOf(t, socket); st.Mode != "insert" || !strings.HasPrefix(st.Text, "hi") {
		t.Errorf("after fude keys ihi, fude is in %s mode with %q", st.Mode, st.Text)
	}
}

// B354
func TestB354State(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket := serve(t, path+":2")
	out, err := fude(t.TempDir(), "state", "--socket", socket)
	var st state
	if err != nil || strings.Count(out, "\n") != 1 || json.Unmarshal([]byte(out), &st) != nil {
		t.Fatalf("fude state printed %q, %v", out, err)
	}
	if filepath.Base(st.Name) != "a.txt" || st.Text != "one\ntwo" || st.Cursor != 4 || st.Mode != "normal" {
		t.Errorf("fude state printed %+v", st)
	}
}

// B173
func TestB173PluginNewAndBuild(t *testing.T) {
	dir := t.TempDir()
	// The new plugin takes the sdk through the module proxy.
	if out, err := fude(dir, "plugin", "new", "words"); err != nil {
		if strings.Contains(out, "dial tcp") || strings.Contains(out, "no such host") {
			t.Skipf("the module proxy is out of reach: %s", out)
		}
		t.Fatalf("fude plugin new words: %v: %s", err, out)
	}
	for _, name := range []string{"go.mod", "main.go"} {
		if _, err := os.Stat(filepath.Join(dir, "words", name)); err != nil {
			t.Fatalf("plugin new wrote no %s: %v", name, err)
		}
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed, so the plugin is not built")
	}
	if out, err := fude(dir, "plugin", "build", "words"); err != nil {
		t.Fatalf("fude plugin build words: %v: %s", err, out)
	}
	// A build that is not a release takes the newest sdk, which may be older
	// than this fude's ABI (D51).
	if p, err := plugin.Load(context.Background(), filepath.Join(dir, "words", "words.wasm"), plugin.Grants{}, t.TempDir(), func(string) {}, func(string, time.Duration) {}); err != nil {
		if strings.Contains(err.Error(), "import func[fude.") {
			t.Skipf("the published sdk is older than this fude: %v", err)
		}
	} else {
		p.Close(context.Background())
	}
	// The plugin it built runs :Words as any plugin's command runs.
	words := plugintest.Load(t, filepath.Join(dir, "words", "words.wasm"), plugin.Grants{Capabilities: []string{"buffer.read", "ui"}}, "")
	if res := words.Command("Words", "", "one two three", 0); res.Status == nil || *res.Status != "3 words" {
		t.Errorf(":Words showed %v, want 3 words", res.Status)
	}
}
