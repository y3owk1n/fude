//go:build !windows

package syntax

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os/exec"
	"unsafe"
)

// libExt ends the name of a grammar's library.
const libExt = ".so"

// compiler is the program that builds a grammar.
const compiler = "tree-sitter"

// compile is the command that builds the grammar in dir into the library
// out.
func compile(dir, out string) *exec.Cmd { return exec.Command("tree-sitter", "build", "-o", out, dir) }

// openLibrary loads the library at path, which fude never closes, and
// returns the address of its function sym.
func openLibrary(path, sym string) (uintptr, error) {
	cpath, csym := C.CString(path), C.CString(sym)
	defer C.free(unsafe.Pointer(cpath))
	defer C.free(unsafe.Pointer(csym))
	h := C.dlopen(cpath, C.RTLD_NOW|C.RTLD_LOCAL)
	if h == nil {
		return 0, errors.New(C.GoString(C.dlerror()))
	}
	f := C.dlsym(h, csym)
	if f == nil {
		C.dlclose(h)
		return 0, fmt.Errorf("%s has no %s", path, sym)
	}
	return uintptr(f), nil
}
