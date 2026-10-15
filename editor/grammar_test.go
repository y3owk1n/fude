package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B110: the vim module's grammar cases give the same result here as with
// its minimal host.
func TestB110GrammarCases(t *testing.T) {
	src, err := os.ReadFile("../vim/testdata/grammar.txt")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for i, l := range strings.Split(strings.TrimRight(string(src), "\n"), "\n") {
		if l == "" || strings.HasPrefix(l, "# ") {
			continue
		}
		var c [3]string
		if _, err := fmt.Sscanf(l, "%q %q %q", &c[0], &c[1], &c[2]); err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		path := filepath.Join(dir, fmt.Sprint(i))
		change(t, path, strings.Replace(c[0], "|", "", 1)+"\n")
		e := open(t, path)
		e.SetSize(80, 23)
		feed(e, fmt.Sprintf("%dgo%s", strings.Index(c[0], "|")+1, c[1]))
		got := text(e)
		if got = got[:e.Cursor()] + "|" + got[e.Cursor():]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}
