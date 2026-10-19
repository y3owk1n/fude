package syntax

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// libExt ends the name of a grammar's library.
const libExt = ".dll"

// compiler is the program that builds a grammar.
const compiler = "gcc"

// compile is the command that builds the grammar in dir into the library
// out. On Windows tree-sitter build compiles for Microsoft's C runtime,
// whose headers llvm-mingw lacks. So fude runs llvm-mingw's gcc itself,
// with the flags tree-sitter uses.
func compile(dir, out string) *exec.Cmd {
	src := filepath.Join(dir, "src")
	args := []string{"-shared", "-O2", "-std=c11", "-I", src, "-o", out, filepath.Join(src, "parser.c")}
	if _, err := os.Stat(filepath.Join(src, "scanner.c")); err == nil {
		args = append(args, filepath.Join(src, "scanner.c"))
	}
	return exec.Command("gcc", args...)
}

// openLibrary loads the library at path, which fude never closes, and
// returns the address of its function sym.
func openLibrary(path, sym string) (uintptr, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return 0, err
	}
	f, err := dll.FindProc(sym)
	if err != nil {
		dll.Release()
		return 0, fmt.Errorf("%s has no %s", path, sym)
	}
	return f.Addr(), nil
}
