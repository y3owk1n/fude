// Package api is what fude gives its modules and plugins (D21). Modules
// import it, never core or vim. A module or plugin owns what it registers.
// A call runs one of those things, reads a Scope and returns a Result.
package api

// Text is a buffer's text at one moment. It never changes and is safe to read
// on any goroutine. Positions are byte offsets, and lines count from 0.
type Text interface {
	Len() int
	Slice(start, end int) string
	Lines() int
	LineStart(line int) int
	LineOf(pos int) int
}

// Edit replaces [Start, End) with Text, in the buffer by its id, one of
// Scope.Buffers, or in the buffer the call is about for 0. A buffer's
// edits apply together, as one change (B342).
type Edit struct {
	Start, End int
	Text       string
	Buffer     int
}
