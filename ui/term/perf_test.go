//go:build !race

// The speed promises in prd/product.md, measured on an M-series Mac. The
// race detector slows code several times over, so these build only without
// it, and run with just perf. The mock terminal parses each frame fude
// writes, so frame times here are higher than in a real terminal.

package term

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

// start opens args in an editor drawn on a mock terminal, with its config
// and timeline logs in temp directories.
func start(t *testing.T, args ...string) *client {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	e, err := editor.Start(args)
	if err != nil {
		t.Fatal(err)
	}
	s, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 120, Y: 40}))
	if err == nil {
		err = s.Init()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Fini)
	return &client{s: s, e: e}
}

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

// press feeds keys and draws one frame, and returns how long that took.
func (c *client) press(keys string) time.Duration {
	at := time.Now()
	for _, k := range vim.Keys(keys) {
		c.e.Feed(k)
	}
	c.draw()
	return time.Since(at)
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

// Key read to frame written under 1ms at p99, on a 10MB file. The test feeds
// and draws each key of a mix of motions, edits, searches and scrolls, over a
// thousand keys.
func TestPromiseKeyLatency(t *testing.T) {
	c := start(t, code(t, 10<<20))
	c.draw()
	session := "jjjjkkwwbbeeWB}}{{50%ggG90000G/fox<CR>nnN*#xu.uddpyyPuuciwbar<Esc>u" +
		"ihello world<Esc>uoreturn nil<Esc>uA;<Esc>u<C-d><C-u><C-f><C-b>zzHML%:s/fox/cat/<CR>u"
	var times []time.Duration
	for len(times) < 1000 {
		for _, k := range vim.Keys(session) {
			times = append(times, c.press(string(k)))
		}
	}
	slices.Sort(times)
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	t.Logf("p50 %v, p99 %v, worst %v over %d keys", p50, p99, worst, len(times))
	if p99 > time.Millisecond {
		t.Errorf("p99 is %v, want under 1ms", p99)
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
		at := time.Now()
		c = start(t, path)
		c.draw()
		best = min(best, time.Since(at))
	}
	t.Logf("first frame after %v", best)
	if best > time.Second {
		t.Errorf("first frame after %v, want under 1s", best)
	}
	slowest := time.Duration(0)
	for _, k := range []string{"<C-f>", "<C-d>", "<C-b>", "<C-u>", "j", "k", "G", "gg", "5000000G", "}", "{"} {
		d := c.press(k)
		slowest = max(slowest, d)
		if d > 16*time.Millisecond {
			t.Errorf("%s took %v, want under 16ms", k, d)
		}
	}
	t.Logf("slowest scroll %v", slowest)
}
