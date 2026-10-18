package keep_test

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/y3owk1n/fude/internal/keep"
)

// B213
func TestRecentKeepsTheNewestFirst(t *testing.T) {
	dir := t.TempDir()
	path := func() (string, error) { return filepath.Join(dir, "fude", "recent"), nil }
	r := keep.NewRecent(path, func(s string) { t.Error(s) })
	for i := range keep.RecentCap + 1 {
		r.Note(filepath.Join(dir, strconv.Itoa(i)))
		// A burst drops notes, so each is written before the next.
		r.Flush()
	}
	r.Note(filepath.Join(dir, "1"))
	r.Flush()
	paths, err := keep.NewRecent(path, nil).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != keep.RecentCap || paths[0] != filepath.Join(dir, "1") || paths[1] != filepath.Join(dir, strconv.Itoa(keep.RecentCap)) {
		t.Errorf("recent holds %d paths starting %v", len(paths), paths[:2])
	}
	if slices.Contains(paths, filepath.Join(dir, "0")) {
		t.Error("the oldest file is still there")
	}
}
