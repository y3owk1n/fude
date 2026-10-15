// Package api is what fude gives its modules. Modules import it, never core
// or vim.
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

// Edit replaces [Start, End) with Text.
type Edit struct {
	Start, End int
	Text       string
}
