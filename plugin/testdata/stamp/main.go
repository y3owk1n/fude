// Package main is a test plugin written with the sdk. :Stamp types "stamp"
// at the cursor, the operator zu upper-cases its target, and each change
// to the text shows "changed" on the statusline. Needs buffer.read,
// buffer.write and ui.
package main

import (
	"strings"

	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Command("Stamp", "types stamp at the cursor", func(c sdk.Call) {
		sdk.Edit(c.Cursor, c.Cursor, "stamp")
	})
	sdk.Operator("zu", "upper-cases the text a motion or object covers", func(r sdk.Range) {
		text, err := sdk.Text(r.Start, r.End)
		if err != nil {
			sdk.Log(err.Error())
			return
		}
		sdk.Edit(r.Start, r.End, strings.ToUpper(text))
	})
	sdk.On("buffer.changed", func(sdk.Event) {
		sdk.Status("changed")
	})
}

func main() {}
