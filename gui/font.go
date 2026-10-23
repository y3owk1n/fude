package gui

import (
	"cmp"
	"slices"

	"github.com/y3owk1n/fude/frontend/wire"
)

// wanted is the font the session last asked for. The text takes the first
// of its families that the system has (B584).
var wanted wire.Font

// askFont makes the text f's family and size once the session asks for
// another font.
func askFont(f wire.Font) {
	if f.Size == wanted.Size && slices.Equal(f.Families, wanted.Families) {
		return
	}
	wanted = f
	sizeText(firstSize())
}

// firstSize is the text's size before Command or Control with = or minus
// change it: the session's, or the system's.
func firstSize() float64 { return cmp.Or(float64(wanted.Size), systemSize) }
