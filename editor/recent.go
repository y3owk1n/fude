package editor

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// recentCap is how many files :pick recent remembers (B213).
const recentCap = 100

var (
	// recentNotes are the files shown, for the one goroutine that writes
	// the recent file, so writes keep their order and stay off the loop.
	recentNotes = make(chan string, 64)
	recentOnce  sync.Once
)

// noteRecent records that a window shows the file at path. It drops the
// note when the queue is full, as a burst of window switches notes the same
// files again.
func noteRecent(path string) {
	if path == "" {
		return
	}
	recentOnce.Do(func() { go writeRecent() })
	select {
	case recentNotes <- path:
	default:
	}
}

// recentPath is where the files shown last are kept, newest first.
func recentPath() (string, error) {
	dir, err := xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", fmt.Errorf("recent files: %w", err)
	}
	return filepath.Join(dir, "fude", "recent"), nil
}

// writeRecent puts each noted file first in the recent file, which it
// replaces whole, so a reader never sees half of it.
func writeRecent() {
	paths, err := readRecent()
	if err != nil {
		logLine(err.Error())
	}
	for path := range recentNotes {
		abs, err := filepath.Abs(path)
		if err != nil {
			logLine(fmt.Sprintf("recent files: %v", err))
			continue
		}
		paths = slices.Insert(slices.DeleteFunc(paths, func(p string) bool { return p == abs }), 0, abs)
		paths = paths[:min(len(paths), recentCap)]
		if err := saveRecent(paths); err != nil {
			logLine(err.Error())
		}
	}
}

func saveRecent(paths []string) error {
	file, err := recentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return fmt.Errorf("recent files: %w", err)
	}
	tmp := file + ".new"
	if err := os.WriteFile(tmp, []byte(strings.Join(paths, "\n")+"\n"), 0o600); err != nil {
		return fmt.Errorf("recent files: %w", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		return fmt.Errorf("recent files: %w", err)
	}
	return nil
}

// readRecent returns the recent files, newest first.
func readRecent() ([]string, error) {
	file, err := recentPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	defer f.Close()
	var paths []string
	in := bufio.NewScanner(f)
	for in.Scan() {
		if line := in.Text(); line != "" {
			paths = append(paths, line)
		}
	}
	if err := in.Err(); err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	return paths, nil
}
