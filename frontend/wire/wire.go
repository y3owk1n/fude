// Package wire is what fude and its clients send each other over the
// socket (D47).
package wire

import "github.com/y3owk1n/fude/core/kernel"

// Edit replaces [Start, End) with Text, as a replica pulls and pushes it.
type Edit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// Pulled is a buffer's edits since the version a replica holds, or its
// whole text when the version is too old or not given (B79).
type Pulled struct {
	Buffer  int            `json:"buffer"`
	Version kernel.Version `json:"version"`
	Edits   [][]Edit       `json:"edits,omitempty"`
	Text    *string        `json:"text,omitempty"`
	Indent  int            `json:"indent"` // the columns > and < shift by in the buffer
	Tab     int            `json:"tab"`    // the columns a tab shows as in the buffer
}

// Pushed is whether a replica's edits applied, and the buffer's version.
// Edits made against an older version do not apply, and the replica pulls
// and pushes them again (B79).
type Pushed struct {
	Applied bool           `json:"applied"`
	Version kernel.Version `json:"version"`
}

// Hello is the first line a joining guest sends.
type Hello struct {
	Secret string `json:"secret"`
	Name   string `json:"name"`
	Own    any    `json:"own"` // what the guest's session takes from its config.toml (B254)
	// ReadOnly asks for a read-only guest's session (B255).
	ReadOnly bool `json:"read_only,omitempty"`
}

// Theme is what a client draws in: dark, light or a base16 scheme's name,
// that scheme's colours, and the colours config.toml sets.
type Theme struct {
	Name   string
	Base16 *[16]int32
	Colors *Colors
}

// Font is the font a window frontend draws the text in: families, of
// which it takes the first the system has, and a size in points. None and
// 0 mean the system's (B584).
type Font struct {
	Families []string `json:",omitempty"`
	Size     uint     `json:",omitempty"`
}
