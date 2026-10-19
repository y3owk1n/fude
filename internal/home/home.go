// Package home expands the ~ that starts a path.
package home

import (
	"os"
	"path/filepath"
	"strings"
)

// Expand returns path with a leading ~/ made the home directory, and on
// Windows a leading ~\ too. Any other path comes back as it is.
func Expand(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok && filepath.Separator != '/' {
		rest, ok = strings.CutPrefix(path, "~"+string(filepath.Separator))
	}
	if !ok {
		return path, nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, rest), nil
}
