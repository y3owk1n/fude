// Package paths compares file paths as the system does: exactly, and on
// Windows ignoring case.
package paths

import (
	"runtime"
	"strings"
)

// Same reports whether the absolute paths x and y name one file.
func Same(x, y string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(x, y)
	}
	return x == y
}

// Key is the absolute path p as a map key or a hash's input. Paths that
// are Same have one Key.
func Key(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
