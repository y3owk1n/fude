package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/distribution"
	"github.com/y3owk1n/fude/editor/editortest"
)

// Promise: editing leaves nothing behind, so fude with every module it
// adds is as big after many files opened, edited, scrolled and closed as
// after a few (product.md, Stable).
func TestPromiseEditingLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("package p\n")
	for i := range 40 {
		fmt.Fprintf(&src, "\nfunc f%d() {\n\tif true {\n\t\tg := func() {\n\t\t\tx()\n\t\t}\n\t\tg()\n\t}\n}\n", i)
	}
	t.Chdir(dir)
	e := editortest.Start(t)
	for _, m := range distribution.Modules() {
		e.Use(m)
	}
	e.Load()
	e.SetSize(100, 30)
	feed := func(keys string) {
		editortest.Feed(e, keys)
		editortest.Quiet(t, e)
	}
	// Each cycle opens a new file, so no file's timeline grows with them.
	n := 0
	cycles := func(count int) {
		for range count {
			n++
			name := fmt.Sprintf("f%d.go", n)
			editortest.Change(t, filepath.Join(dir, name), src.String())
			feed(":e " + name + "<CR>")
			feed("100Gzt<C-d><C-d>")
			feed("Oy := 1<Esc>")
			feed("u:vsplit<CR>gg<C-w>c")
			feed(":h<CR>")
			feed(":q<CR>")
			feed(":bd!<CR>")
		}
	}
	measure := func() (goroutines int, heap, rss uint64) {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return runtime.NumGoroutine(), m.HeapAlloc, residentSize(t)
	}
	cycles(20)
	g0, h0, r0 := measure()
	cycles(100)
	g1, h1, r1 := measure()
	t.Logf("goroutines %d to %d, heap %.1fMB to %.1fMB, resident %.1fMB to %.1fMB", g0, g1, mb(h0), mb(h1), mb(r0), mb(r1))
	// The allocator, and the race detector's, move the resident size by a
	// few megabytes. A file kept after it closes grows it by far more.
	if g1 > g0+2 || h1 > h0+2<<20 || r1 > r0+32<<20 {
		t.Errorf("100 more files took goroutines from %d to %d, the heap from %.1fMB to %.1fMB and the resident size from %.1fMB to %.1fMB", g0, g1, mb(h0), mb(h1), mb(r0), mb(r1))
	}
}

// residentSize returns the memory the test process holds, the grammars'
// included, which the Go heap leaves out, or 0 where ps cannot say.
func residentSize(t *testing.T) uint64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatalf("ps said %q", out)
	}
	return kb << 10
}

func mb(n uint64) float64 { return float64(n) / (1 << 20) }
