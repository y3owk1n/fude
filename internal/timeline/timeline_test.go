package timeline_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/timeline"
)

// B33, B34
func TestLogKeepsTheTimelineAndTheUnsavedEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	log, _, err := timeline.Open(path, "")
	if err != nil {
		t.Fatal(err)
	}
	b := kernel.New("", log)
	if err := b.Apply(kernel.Transaction{Edits: []kernel.Edit{{Text: "ab"}}}); err != nil {
		t.Fatal(err)
	}
	log.Saved(b.Head(), b.Snapshot())
	if err := b.Apply(kernel.Transaction{Base: b.Snapshot().Version(), Edits: []kernel.Edit{{Start: 2, End: 2, Text: "c"}}}); err != nil {
		t.Fatal(err)
	}
	// The process ends without a clean close, so the last edit is unsaved.
	for log.Busy() {
		runtime.Gosched()
	}
	_, l, err := timeline.Open(path, "ab")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Nodes) != 2 || l.Unsaved != b.Head() || len(l.Saves) != 1 {
		t.Errorf("reopened with %d nodes, unsaved %v and %d saves, want 2, the head and 1", len(l.Nodes), l.Unsaved, len(l.Saves))
	}
	// Text that no longer matches starts a new log.
	log, l, err = timeline.Open(path, "other")
	if err != nil || len(l.Nodes) != 0 {
		t.Errorf("a changed text reopened with %d nodes and %v", len(l.Nodes), err)
	}
	for log.Busy() {
		runtime.Gosched()
	}
}
