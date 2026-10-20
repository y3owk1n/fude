package editor_test

import (
	"testing"
)

// B118
func TestB118List(t *testing.T) {
	e := open(t, "")
	if feed(e, ":set list true<CR>"); !e.Windows()[0].List {
		t.Fatal(":set list true left the window without it")
	}
}
