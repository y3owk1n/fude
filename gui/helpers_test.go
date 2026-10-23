package gui

import (
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor/editortest"
)

// onLoop runs fn on w's loop, where the session may be read.
func (w *window) onLoop(fn func()) {
	done := make(chan struct{})
	w.do <- func() { fn(); close(done) }
	<-done
}

// until waits for ok, failing t once it waited too long for what.
func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(editortest.Patience); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("still waiting for %s", what)
		}
	}
}
