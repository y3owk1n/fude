package editor_test

import (
	"testing"

	win "golang.org/x/sys/windows"
)

// refuseWrite makes a write of the file at path fail until the returned
// func, as it holds the file open to no one else. A folder's read-only flag
// does not stop new files on Windows.
func refuseWrite(t *testing.T, path string) (release func()) {
	t.Helper()
	name, err := win.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := win.CreateFile(name, win.GENERIC_READ, 0, nil, win.OPEN_EXISTING, win.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() { win.CloseHandle(h) }
}
