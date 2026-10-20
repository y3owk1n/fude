package editortest

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/internal/paths"
)

// fakeEnv names the folder of FakeProgram's programs, so the test binary
// knows when it runs as one of them.
const fakeEnv = "FUDE_FAKE_PROGRAMS"

// FakeProgram puts programs called names first on PATH until t ends, and
// returns the file they log to. Each is the test binary, which Main runs as
// the program. It appends its arguments to the log, a line each run. With
// FUDE_FAKE_CHILD set to a duration, it also leaves a child that holds its
// output that long.
func FakeProgram(t testing.TB, names ...string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range names {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(dir, name)
		if err := link(exe, path); err != nil {
			t.Fatal(err)
		}
		// Windows scans a new program the first time it runs, which can
		// take seconds, so it runs once here, before a test times it.
		if runtime.GOOS == "windows" {
			first := exec.Command(path)
			first.Env = append(os.Environ(), "FUDE_FAKE_SLEEP=0s")
			if out, err := first.CombinedOutput(); err != nil {
				t.Fatalf("run %s: %v\n%s", path, err, out)
			}
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakeEnv, dir)
	t.Setenv("FUDE_FAKE_ORIGIN", exe)
	return filepath.Join(dir, "log")
}

// link makes to the file from, as a hard link where it can, else a copy.
// Windows cannot delete a link to a running program, and the test binary
// runs, so there it copies.
func link(from, to string) error {
	if runtime.GOOS != "windows" && os.Link(from, to) == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// fakeProgram runs the test binary as one of FakeProgram's programs when it
// is one, and reports whether it was, with its exit code.
func fakeProgram() (int, bool) {
	if d := os.Getenv("FUDE_FAKE_SLEEP"); d != "" {
		wait, err := time.ParseDuration(d)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1, true
		}
		time.Sleep(wait)
		return 0, true
	}
	dir := os.Getenv(fakeEnv)
	exe, err := os.Executable()
	if dir == "" || err != nil || !sameDir(filepath.Dir(exe), dir) {
		return 0, false
	}
	f, err := os.OpenFile(filepath.Join(dir, "log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	if d := os.Getenv("FUDE_FAKE_CHILD"); d != "" {
		// The child runs from the test binary, as Windows cannot delete a
		// program that runs.
		child := exec.Command(os.Getenv("FUDE_FAKE_ORIGIN"))
		child.Env = append(os.Environ(), "FUDE_FAKE_SLEEP="+d)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1, true
		}
	}
	return 0, true
}

// sameDir reports whether the folders x and y are one, after symlinks.
func sameDir(x, y string) bool {
	rx, err1 := filepath.EvalSymlinks(x)
	ry, err2 := filepath.EvalSymlinks(y)
	return err1 == nil && err2 == nil && paths.Same(rx, ry)
}
