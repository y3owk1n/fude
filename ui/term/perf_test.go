//go:build !race

// The speed promises in prd/product.md, measured on an M-series Mac. The
// race detector slows code several times over, so these build only without
// it, and run with just perf. A promise times fude to the frame written,
// and a terminal parses that frame in a process of its own, so the tests'
// terminal throws it away.

package term

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/y3owk1n/fude/distribution"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/vim"
)

// code writes about size bytes of Go-like text, with a blank line every ten
// lines, and returns its path.
func code(t *testing.T, size int) string {
	t.Helper()
	block := strings.Repeat("\tif err := fox.Jump(over, the.lazy(dog)); err != nil { return err }\n", 9) + "\n"
	path := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(path, []byte(strings.Repeat(block, size/len(block))), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// realCode writes about size bytes of Go, the standard library's own source
// over and over, and returns its path. It has the nesting, long lines,
// comments and unicode of real code.
func realCode(t *testing.T, size int) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(filepath.Join(runtime.GOROOT(), "src"), func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == "testdata":
			return filepath.SkipDir
		case b.Len() >= size:
			return filepath.SkipAll
		case !strings.HasSuffix(path, ".go"):
			return nil
		}
		src, err := os.ReadFile(path)
		b.Write(src)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// written returns a screen on a mock terminal that throws away what fude
// writes once the screen is set up.
func written(t *testing.T) tcell.Screen {
	t.Helper()
	mt := &discard{MockTerm: vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40})}
	s := screenOn(t, mt)
	mt.quiet = true
	return s
}

// discard is a mock terminal that, once quiet, takes writes without
// parsing them.
type discard struct {
	vt.MockTerm
	quiet bool
}

func (d *discard) Write(b []byte) (int, error) {
	if d.quiet {
		return len(b), nil
	}
	return d.MockTerm.Write(b)
}

// press runs the editor loop for gap, as it runs between keys. It does the
// work other goroutines queue as it comes, such as a new syntax tree, and
// draws after each burst of it. Then keys arrive, and it feeds them and
// draws. It returns the time from their arrival to the frame, so keys that
// come while queued work runs wait for it. They wait only for the work
// running, as the loop then runs them first and draws their frame alone.
// It spins through the gap. A core left idle for 10ms draws the next frame
// about seven times slower, and the test would time that instead of fude.
func (c *client) press(keys string, gap time.Duration) time.Duration {
	arrive := time.Now().Add(gap)
	for time.Now().Before(arrive) {
		select {
		case f := <-c.e.Queue():
			f()
			arrived := func() bool { return !time.Now().Before(arrive) }
			if drain(c.e, arrived); !arrived() {
				c.draw()
			}
		default:
		}
	}
	for _, k := range vim.Keys(keys) {
		c.e.Feed(k)
	}
	c.draw()
	return time.Since(arrive)
}

// Cold start to first frame on an empty buffer under 5ms. It runs in
// process, from Start to the first frame written, so it leaves out exec and
// terminal setup. The best of three runs counts.
func TestPromiseColdStart(t *testing.T) {
	best := time.Hour
	for range 3 {
		at := time.Now()
		start(t).draw()
		best = min(best, time.Since(at))
	}
	t.Logf("first frame after %v", best)
	if best > 5*time.Millisecond {
		t.Errorf("first frame after %v, want under 5ms", best)
	}
}

// Key read to frame written under 1ms at p99, on 10MB of real Go with its
// syntax on. The test feeds over a thousand keys, 10ms apart, which is
// faster than key repeat. They mix motions, edits, scrolls and searches,
// one of them typed on past its last match. The work the edits queue, such
// as reparsing, runs between keys as the loop runs it, and a key that
// arrives while it runs waits for it.
func TestPromiseKeyLatency(t *testing.T) {
	c := startWith(t, "large_file = 20", realCode(t, 10<<20))
	c.s = written(t)
	c.draw()
	session := "jjjjkkwwbbeeWB}}{{50%ggG90000G/funcxyz<BS><BS><BS><CR>nnN*#xu.uddpyyPuuciwbar<Esc>u" +
		"ihello world<Esc>uoreturn nil<Esc>uA;<Esc>u<C-d><C-u><C-f><C-b>zzHML%:s/err/e/<CR>u"
	var times []time.Duration
	for len(times) < 1000 {
		for _, k := range vim.Keys(session) {
			times = append(times, c.press(string(k), 10*time.Millisecond))
		}
	}
	slices.Sort(times)
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	t.Logf("p50 %v, p99 %v, worst %v over %d keys", p50, p99, worst, len(times))
	if p99 > time.Millisecond {
		t.Errorf("p99 is %v, want under 1ms", p99)
	}
}

// Key read to frame written under 5ms at p99 through fude attach on the
// local socket, on the file and keys of TestPromiseKeyLatency. fude runs its
// loop on a goroutine of its own, as a fude in another terminal does, and
// the attached client draws each key's frame once it comes.
func TestPromiseAttachLatency(t *testing.T) {
	host := startWith(t, "large_file = 20", realCode(t, 10<<20)).e.(*editor.Session)
	socket, _, _ := serve(t, host)
	a, err := Attach(socket, "pat")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	c := &client{s: written(t), e: a}
	// upTo runs the frames that come, drawing after each, until fude has
	// handled n of the client's notices.
	upTo := func(n int) {
		for deadline := time.After(editortest.Patience); a.f.Input < n; {
			select {
			case f := <-a.Queue():
				f()
				c.draw()
			case <-deadline:
				t.Fatalf("fude handled %d notices of %d", a.f.Input, n)
			}
		}
	}
	c.draw()
	upTo(1) // the screen's size
	session := "jjjjkkwwbbeeWB}}{{50%ggG90000G/func<CR>nnN*#xu.uddpyyPuuciwbar<Esc>u" +
		"ihello world<Esc>uoreturn nil<Esc>uA;<Esc>u<C-d><C-u><C-f><C-b>zzHML%:s/err/e/<CR>u"
	var times []time.Duration
	for len(times) < 1000 {
		for _, k := range vim.Keys(session) {
			// Frames of fude's own work, such as a new syntax tree, come
			// between keys and the test draws them. It spins, as press does.
			for gap := time.Now().Add(10 * time.Millisecond); time.Now().Before(gap); {
				select {
				case f := <-a.Queue():
					f()
					c.draw()
				default:
				}
			}
			at := time.Now()
			a.Feed(k)
			upTo(a.f.Input + 1)
			times = append(times, time.Since(at))
		}
	}
	slices.Sort(times)
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	t.Logf("p50 %v, p99 %v, worst %v over %d keys", p50, p99, worst, len(times))
	if p99 > 5*time.Millisecond {
		t.Errorf("p99 is %v, want under 5ms", p99)
	}
}

// Key read to frame written under 2ms at p99, as TestPromiseKeyLatency
// measures it, with fude set up as for work. Every module fude adds runs.
// The 10MB of real Go is in a git repository, so its signs diff, and shows
// in two windows. A language server publishes diagnostics after each change
// and offers completions as keys are typed, and a plugin counts words on
// each change.
func TestPromiseKeyLatencyFullSetup(t *testing.T) {
	path := committed(t, realCode(t, 10<<20))
	count, err := filepath.Abs("../../plugin/testdata/count/count.wasm")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUDE_PERF_LSP", "1")
	c := startWith(t, fmt.Sprintf(`autocomplete = true
large_file = 20
lsp.busy.command = [%q, "-test.run=^TestPerfServer$"]
[filetype.go]
lsp = ["busy"]
[[plugin]]
path = %q
capabilities = ["buffer.read", "ui"]
`, os.Args[0], count), path)
	c.s = written(t)
	for _, m := range distribution.Modules() {
		c.e.(*editor.Session).Use(m)
	}
	c.draw()
	c.e.Load()
	c.press("<C-w>v", 0)
	session := "jjjjkkwwbbeeWB}}{{50%ggG90000G/func<CR>nnN*#xu.uddpyyPuuciwbar<Esc>u" +
		"ihello world<Esc>uoreturn nil<Esc>uA;<Esc>u<C-d><C-u><C-f><C-b>zzHML%:s/err/e/<CR>u" +
		"ofmt.Println(err.Error())<Esc>uA // fix this later<Esc>u<C-w>wjjk<C-w>w"
	var times []time.Duration
	for len(times) < 1000 {
		for _, k := range vim.Keys(session) {
			times = append(times, c.press(string(k), 10*time.Millisecond))
		}
	}
	slices.Sort(times)
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	t.Logf("p50 %v, p99 %v, worst %v over %d keys", p50, p99, worst, len(times))
	if p99 > 2*time.Millisecond {
		t.Errorf("p99 is %v, want under 2ms", p99)
	}
}

// A scroll's frame shows everything fude's modules draw for the window's
// new view under 5ms at p99, from the key's arrival, on the full setup's
// 10MB of real Go in a git repository with every module fude adds: the
// pinned lines, indent guides, git signs, colours and spelling of the lines
// it scrolled to. Keys come 10ms apart.
func TestPromiseScrollSettles(t *testing.T) {
	c := startWith(t, "large_file = 20\n[module.spell]\nenabled = true", committed(t, realCode(t, 10<<20)))
	c.s = written(t)
	for _, m := range distribution.Modules() {
		c.e.(*editor.Session).Use(m)
	}
	c.draw()
	c.e.Load()
	settled := func() {
		for !c.e.(*editor.Session).Idle() {
			(<-c.e.Queue())()
		}
		c.draw()
	}
	// The file's syntax comes first, as it would before a person scrolls.
	for deadline := time.After(editortest.Patience); len(c.e.(*editor.Session).Windows()[0].Highlights) == 0; {
		select {
		case f := <-c.e.Queue():
			f()
		case <-deadline:
			t.Fatal("the file never parsed")
		}
	}
	settled()
	// Keys that wait for another, such as the g of gg, wait for key hints
	// on purpose, so the scrolls are one key each.
	session := "<C-d><C-d><C-d><C-u>jjjjjjjjjjkkkkk<C-f><C-b>G<C-u><C-u>HLM"
	var times []time.Duration
	for len(times) < 500 {
		for _, k := range vim.Keys(session) {
			// press returns the time since the key arrived.
			first := c.press(string(k), 10*time.Millisecond)
			arrive := time.Now().Add(-first)
			settled()
			times = append(times, time.Since(arrive))
		}
	}
	slices.Sort(times)
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	t.Logf("p50 %v, p99 %v, worst %v over %d keys", p50, p99, worst, len(times))
	if p99 > 5*time.Millisecond {
		t.Errorf("p99 is %v, want under 5ms", p99)
	}
}

// committed makes the folder of path a git repository with the file
// committed, so git signs diff against it, and returns path.
func committed(t *testing.T, path string) string {
	t.Helper()
	dir := filepath.Dir(path)
	for _, args := range [][]string{{"init", "-q"}, {"add", filepath.Base(path)}, {"-c", "user.name=fude", "-c", "user.email=fude@localhost", "commit", "-qm", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", args[0], err, out)
		}
	}
	return path
}

// perfDiagnostics and perfItems are how many diagnostics TestPerfServer
// publishes after each change, and how many completions it offers.
const perfDiagnostics, perfItems = 100, 300

// TestPerfServer is a language server when FUDE_PERF_LSP is set, which the
// full setup runs as a process of its own. It acts as a busy gopls does:
// after each change it publishes perfDiagnostics diagnostics spread over the
// text, it offers perfItems completions, and it answers every other request
// with null.
func TestPerfServer(t *testing.T) {
	if os.Getenv("FUDE_PERF_LSP") == "" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	send := func(v map[string]any) {
		v["jsonrpc"] = "2.0"
		body, _ := json.Marshal(v)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	lines := 1
	for {
		n := 0
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				os.Exit(0)
			}
			if line = strings.TrimSpace(line); line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
				n, _ = strconv.Atoi(v)
			}
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			os.Exit(0)
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				TextDocument struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"textDocument"`
			} `json:"params"`
		}
		json.Unmarshal(body, &m)
		switch m.Method {
		case "initialize":
			send(map[string]any{"id": m.ID, "result": map[string]any{"capabilities": map[string]any{
				"positionEncoding": "utf-8", "completionProvider": map[string]any{"triggerCharacters": []string{"."}},
			}}})
		case "textDocument/didOpen", "textDocument/didChange":
			if m.Method == "textDocument/didOpen" {
				lines = strings.Count(m.Params.TextDocument.Text, "\n") + 1
			}
			var ds []any
			for i := range perfDiagnostics {
				at := map[string]int{"line": i * lines / perfDiagnostics, "character": 0}
				ds = append(ds, map[string]any{"range": map[string]any{"start": at, "end": at}, "severity": 1 + i%4, "message": "a problem the server found"})
			}
			send(map[string]any{"method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": m.Params.TextDocument.URI, "diagnostics": ds}})
		case "textDocument/completion":
			var items []any
			for i := range perfItems {
				items = append(items, map[string]any{"label": fmt.Sprintf("item%03d", i), "kind": 3, "detail": "func()"})
			}
			send(map[string]any{"id": m.ID, "result": map[string]any{"isIncomplete": true, "items": items}})
		case "exit":
			os.Exit(0)
		default:
			if m.ID != nil {
				send(map[string]any{"id": m.ID, "result": nil})
			}
		}
	}
}

// Undo and redo, from u or <C-r> read to frame written, under 1ms at p99,
// as TestPromiseKeyLatency measures it, on 10MB of real Go whose timeline
// holds 10,000 steps.
func TestPromiseUndo(t *testing.T) {
	c := startWith(t, "large_file = 20", realCode(t, 10<<20))
	c.s = written(t)
	c.draw()
	// Each x is a step of its own, on a line of its own, so each deletes a
	// character.
	for range 10000 {
		c.e.Feed(vim.Key("x"))
		c.e.Feed(vim.Key("j"))
	}
	c.draw()
	var times []time.Duration
	for len(times) < 1000 {
		for _, k := range vim.Keys("uuuuu<C-r><C-r><C-r><C-r><C-r>") {
			times = append(times, c.press(string(k), 10*time.Millisecond))
		}
	}
	slices.Sort(times)
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	t.Logf("p50 %v, p99 %v, worst %v over %d keys", p50, p99, worst, len(times))
	if p99 > time.Millisecond {
		t.Errorf("p99 is %v, want under 1ms", p99)
	}
}

// A key draws under 1ms at p99 just after 10,000 edits arrive at once, as
// a paste of keys or a macro sends them, while their parse runs. The text is
// 10MB of real Go, with every module fude adds. The test feeds the burst as
// the loop feeds keys from the terminal. It runs one queued result between
// keys and draws no frame until the keys stop.
func TestPromiseBurst(t *testing.T) {
	c := startWith(t, "large_file = 20", realCode(t, 10<<20))
	c.s = written(t)
	for _, m := range distribution.Modules() {
		c.e.(*editor.Session).Use(m)
	}
	c.draw()
	c.e.Load()
	for range 10000 {
		for _, k := range vim.Keys("xj") {
			c.e.Feed(k)
			select {
			case f := <-c.e.Queue():
				f()
			default:
			}
		}
	}
	c.draw()
	session := "jjjjkkwwbbeeWB}}{{xu.uddpyyPuuciwbar<Esc>uihello world<Esc>uoreturn nil<Esc>uA;<Esc>u<C-d><C-u>zzHML"
	var times []time.Duration
	for len(times) < 300 {
		for _, k := range vim.Keys(session) {
			times = append(times, c.press(string(k), 10*time.Millisecond))
		}
	}
	slices.Sort(times)
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	t.Logf("p50 %v, p99 %v, worst %v over %d keys", p50, p99, worst, len(times))
	if p99 > time.Millisecond {
		t.Errorf("p99 is %v, want under 1ms", p99)
	}
}

// :%s/err/e/g over 10MB of real Go, from <CR> read to the frame that shows
// it, under 50ms. The median of ten runs counts, and the test undoes each
// run before the next.
func TestPromiseSubstitute(t *testing.T) {
	c := startWith(t, "large_file = 20", realCode(t, 10<<20))
	c.s = written(t)
	c.draw()
	e := c.e.(*editor.Session)
	size := e.Text().Len()
	var times []time.Duration
	for range 10 {
		c.press(":%s/err/e/g", 10*time.Millisecond)
		times = append(times, c.press("<CR>", 10*time.Millisecond))
		if e.Text().Len() >= size {
			t.Fatalf(":%%s left the text %d bytes, want fewer than %d", e.Text().Len(), size)
		}
		c.press("u", 10*time.Millisecond)
	}
	slices.Sort(times)
	t.Logf("median %v, worst %v over %d runs", times[len(times)/2], times[len(times)-1], len(times))
	if times[len(times)/2] > 50*time.Millisecond {
		t.Errorf("the median is %v, want under 50ms", times[len(times)/2])
	}
}

// A 1GB file shows its first frame in under 1s and scrolls at frame rate,
// which is a frame every 16ms. The best of three opens counts.
func TestPromiseHugeFile(t *testing.T) {
	path := code(t, 1<<30)
	var c *client
	best := time.Hour
	for range 3 {
		c = nil
		runtime.GC()
		screen := written(t)
		at := time.Now()
		c = start(t, path)
		c.s = screen
		c.draw()
		best = min(best, time.Since(at))
	}
	t.Logf("first frame after %v", best)
	if best > time.Second {
		t.Errorf("first frame after %v, want under 1s", best)
	}
	slowest := time.Duration(0)
	for _, k := range []string{"<C-f>", "<C-d>", "<C-b>", "<C-u>", "j", "k", "G", "gg", "5000000G", "}", "{"} {
		d := c.press(k, 0)
		slowest = max(slowest, d)
		if d > 16*time.Millisecond {
			t.Errorf("%s took %v, want under 16ms", k, d)
		}
	}
	t.Logf("slowest scroll %v", slowest)
}
