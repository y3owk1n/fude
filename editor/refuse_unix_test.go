//go:build !windows

package editor_test

import (
	"os"
	"path/filepath"
	"testing"
)

// refuseWrite makes a write of the file at path fail until the returned
// func, as its folder takes no new file.
func refuseWrite(t *testing.T, path string) (release func()) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	return func() { os.Chmod(dir, 0o700) }
}
