// Package root finds the folder a project's tool runs in, by the files
// that mark it.
package root

import (
	"os"
	"path/filepath"
	"strings"
)

// Of returns the nearest folder above the file at abs that holds the first
// of markers found, or the file's folder when none is, and whether one was
// found. A marker with *, ? or [ is a pattern of names (B167, B528).
func Of(abs string, markers []string) (string, bool) {
	for _, m := range markers {
		for dir := filepath.Dir(abs); ; {
			if holds(dir, m) {
				return dir, true
			}
			up := filepath.Dir(dir)
			if up == dir {
				break
			}
			dir = up
		}
	}
	return filepath.Dir(abs), false
}

// holds reports whether dir has an entry named marker, or matching it.
func holds(dir, marker string) bool {
	if !strings.ContainsAny(marker, "*?[") {
		_, err := os.Stat(filepath.Join(dir, marker))
		return err == nil
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if ok, _ := filepath.Match(marker, e.Name()); ok {
			return true
		}
	}
	return false
}
