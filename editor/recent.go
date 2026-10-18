package editor

import (
	"path/filepath"
	"sync"

	"github.com/y3owk1n/fude/internal/keep"
)

// recent is the files shown last, kept in fude's state directory (B213).
var recent = sync.OnceValue(func() *keep.Recent { return keep.NewRecent(recentPath, logLine) })

// recentPath is where the recent files are kept.
func recentPath() (string, error) {
	dir, err := keep.Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fude", "recent"), nil
}
