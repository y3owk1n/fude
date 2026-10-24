package editortest

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	// A child that FUDE_FAKE_CHILD leaves runs from the test binary.
	// Windows cannot delete a program while it runs, so this kills the
	// child when the test ends.
	t.Cleanup(func() {
		pids, err := os.ReadFile(filepath.Join(dir, "children"))
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err != nil {
			t.Error(err)
			return
		}
		for _, pid := range strings.Fields(string(pids)) {
			if err := end(pid); err != nil {
				t.Errorf("end child %s: %v", pid, err)
			}
		}
	})
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakeEnv, dir)
	t.Setenv("FUDE_FAKE_ORIGIN", exe)
	return filepath.Join(dir, "log")
}

// end kills the process pid. On Windows it also waits for the process to
// exit, so the program it ran can be deleted. Elsewhere Go cannot wait on
// a process this one did not start.
func end(pid string) error {
	n, err := strconv.Atoi(pid)
	if err != nil {
		return err
	}
	p, err := os.FindProcess(n)
	if err != nil {
		return err
	}
	if err := p.Kill(); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	_, err = p.Wait()
	return err
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
	if err := appendLine(filepath.Join(dir, "log"), strings.Join(os.Args[1:], " ")); err != nil {
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
		// FakeProgram kills it when the test ends.
		if err := appendLine(filepath.Join(dir, "children"), strconv.Itoa(child.Process.Pid)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1, true
		}
	}
	return 0, true
}

// appendLine adds line to the end of the file at path.
func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(f, line)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// sameDir reports whether the folders x and y are one, after symlinks.
func sameDir(x, y string) bool {
	rx, err1 := filepath.EvalSymlinks(x)
	ry, err2 := filepath.EvalSymlinks(y)
	return err1 == nil && err2 == nil && paths.Same(rx, ry)
}
