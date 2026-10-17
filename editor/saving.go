package editor

import (
	"slices"
	"strings"
	"time"
)

// saving is a write that waits for the modules that hear buffer.saving.
// Each handler counts, and so does each callback a handler's result asks
// for. After format_timeout the write runs anyway (B282).
type saving struct {
	b       *buffer
	pending int      // the handlers and callbacks the write waits for
	done    bool     // the write ran, so a late result is dropped
	notes   []string // the handlers' messages, which follow the write's
	write   func()
}

// saving runs write once the modules that hear buffer.saving about b have
// handled it, or at once when none does.
func (e *Session) saving(b *buffer, write func()) {
	if e.saves[b] != nil {
		e.message = "a write of the file is still waiting"
		return
	}
	s := &saving{b: b, write: write}
	if s.pending = e.emitTo(event{name: "buffer.saving", buf: b, save: s}); s.pending == 0 {
		write()
		return
	}
	e.saves[b] = s
	timeout := time.Duration(e.cfg.FormatTimeout) * time.Millisecond
	time.AfterFunc(timeout, func() {
		e.queue <- func() { s.finish(e, "a module took over "+timeout.String()+", written as it was") }
	})
}

// finish writes, once, with late said after the write's message when the
// wait ran out.
func (s *saving) finish(e *Session, late string) {
	if s.done {
		return
	}
	s.done = true
	delete(e.saves, s.b)
	if !slices.Contains(e.bufs, s.b) {
		return
	}
	s.write()
	if late != "" {
		s.notes = append(s.notes, late)
	}
	if len(s.notes) > 0 {
		e.message = strings.Join(append([]string{e.message}, s.notes...), ", ")
	}
}

// handledOne counts a handler or callback of s as done, and writes once
// all are.
func (s *saving) handledOne(e *Session) {
	if s.pending--; s.pending == 0 {
		s.finish(e, "")
	}
}
