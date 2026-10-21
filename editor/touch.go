package editor

import (
	"fmt"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
)

// touch tells the modules a key yanked, put, undid or redid the text of b
// in spans, one at the cursor and one at each multicursor, as kind, which
// flash shows for a moment (B139, B313).
func (e *Session) touch(b *buffer, kind string, spans ...[2]int) {
	var detail strings.Builder
	detail.WriteString(kind)
	for _, s := range spans {
		if s[0] < s[1] {
			fmt.Fprintf(&detail, " %d %d", s[0], s[1])
		}
	}
	if detail.Len() > len(kind) {
		e.emit("text.touched", detail.String(), b)
	}
}

// touched returns where edits, sorted and apart, put their text, from the
// first one's start to the last one's end in the text after them.
func touched(edits []kernel.Edit) [2]int {
	if len(edits) == 0 {
		return [2]int{}
	}
	shift, end := 0, 0
	for _, ed := range edits {
		end = ed.Start + shift + len(ed.Text)
		shift += len(ed.Text) - (ed.End - ed.Start)
	}
	return [2]int{edits[0].Start, end}
}
