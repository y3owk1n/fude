package syntax

import (
	"fmt"
	"syscall"
)

// libExt ends the name of a grammar's library.
const libExt = ".dll"

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
