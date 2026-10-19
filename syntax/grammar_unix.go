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
	"unsafe"
)

// libExt ends the name of a grammar's library.
const libExt = ".so"

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
