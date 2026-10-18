package editor

import (
	"fmt"

	"github.com/y3owk1n/fude/core/kernel"
)

// touch tells the modules a key yanked, put, undid or redid the text of b
// from start to end, as kind, which flash shows for a moment (B139, B313).
func (e *Session) touch(b *buffer, kind string, start, end int) {
	if start < end {
		e.emit("text.touched", fmt.Sprintf("%s %d %d", kind, start, end), b)
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
