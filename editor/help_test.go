package editor_test

import (
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
)

// B163
func TestB163Man(t *testing.T) {
	// Each page has its own part of help.
	for name, want := range map[string]string{
		"":       ".TP\n\\fB:write [file]  :w\\fR\nwrites the buffer",
		"config": ".TP\n\\fBtheme = \"dark\"\\fR",
		"guide":  ".SH GETTING STARTED",
	} {
		page, err := editor.Man(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(page, ".TH FUDE") || !strings.Contains(page, want) {
			t.Errorf("man page %q is %q...", name, page[:min(300, len(page))])
		}
	}
}
