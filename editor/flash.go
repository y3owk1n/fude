package editor

import (
	"time"

	"github.com/y3owk1n/fude/core/kernel"
)

// flash is text of b shown for a moment, as the last yank, put, undo or
// redo left it (B139).
type flash struct {
	b          *buffer
	start, end int
}

// flashOn shows start to end of b for flash_time milliseconds.
func (e *Session) flashOn(b *buffer, start, end int) {
	if start >= end || e.cfg.FlashTime == 0 {
		return
	}
	f := &flash{b, start, end}
	e.flash = f
	time.AfterFunc(time.Duration(e.cfg.FlashTime)*time.Millisecond, func() {
		e.queue <- func() {
			if e.flash == f {
				e.flash = nil
			}
		}
	})
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
