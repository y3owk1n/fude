//go:build !race

// The speed promises in prd/product.md, measured on an M-series Mac. The
// race detector slows code several times over, so these build only without
// it, and run with just perf. The mock terminal parses each frame fude
// writes, so frame times here are higher than in a real terminal.

package term

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

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

// press runs the editor loop for gap, as it runs between keys: it does the
// work other goroutines queue as it comes, such as a new syntax tree, and
// draws after each. Then keys arrive, and it feeds them and draws. When
// queued work was still running as the keys arrived, it returns the time
// from their arrival to the frame, since the keys waited for that work.
// Otherwise it counts from when the wait ends, because the test's timer
// fires late, and a real key would not wait for it.
func (c *client) press(keys string, gap time.Duration) time.Duration {
	arrive := time.Now().Add(gap)
	busy := false // work ran past the keys' arrival
	for wait := time.Until(arrive); wait > 0; wait = time.Until(arrive) {
		select {
		case f := <-c.e.Queue():
			f()
			c.draw()
			busy = time.Now().After(arrive)
		case <-time.After(wait):
		}
	}
	at := time.Now()
	if busy {
		at = arrive
	}
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

// Key read to frame written under 1ms at p99, on 10MB of real Go with its
// syntax on. The test feeds a mix of motions, edits, searches and scrolls,
// over a thousand keys, 10ms apart, which is faster than key repeat. The
// work the edits queue, such as reparsing, runs between keys as the loop
// runs it, and a key that arrives while it runs waits for it.
func TestPromiseKeyLatency(t *testing.T) {
	c := start(t, realCode(t, 10<<20))
	c.draw()
	session := "jjjjkkwwbbeeWB}}{{50%ggG90000G/func<CR>nnN*#xu.uddpyyPuuciwbar<Esc>u" +
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
		d := c.press(k, 0)
		slowest = max(slowest, d)
		if d > 16*time.Millisecond {
			t.Errorf("%s took %v, want under 16ms", k, d)
		}
	}
	t.Logf("slowest scroll %v", slowest)
}
