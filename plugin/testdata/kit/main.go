// Package main is a test plugin written with the sdk.
// It registers the operator gs, which upper-cases its target, the object
// aL, which is the whole text, and the key gS and the command :Shout, which
// type "!" at the cursor. Needs buffer.read and buffer.write.
package main

import (
	"strings"

	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Operator("gs", "upper-cases the text a motion covers", func(r sdk.Range) {
		if text, err := sdk.Text(r.Start, r.End); err == nil {
			sdk.Edit(r.Start, r.End, strings.ToUpper(text))
		}
	})
	sdk.Object("aL", "the whole text", func(_, _ int) (int, int, bool) {
		n, err := sdk.Len()
		return 0, n, err == nil
	})
	shout := func(c sdk.Call) { sdk.Edit(c.Cursor, c.Cursor, "!") }
	sdk.Key("gS", "types ! at the cursor", shout)
	sdk.Command("Shout", "types ! at the cursor", shout)
}

func main() {}
