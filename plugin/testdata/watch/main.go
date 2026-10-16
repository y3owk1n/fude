// Package main is a test plugin written with the sdk.
// init subscribes to view.moved, and shows the cursor and the top of the
// view it heard of on the statusline, as "cursor top". It subscribes to
// buffer.changed too, and puts the style keyword on the range the change it
// heard of covers, in place of the one before. Needs ui.
package main

import (
	"strconv"

	"github.com/y3owk1n/fude/sdk"
)

// last is the decoration the last change put.
var last int

func init() {
	sdk.On("buffer.changed", func(e sdk.Event) {
		if last != 0 {
			sdk.Undecorate(last)
		}
		last, _ = sdk.Decorate(e.Start, e.End, "keyword")
	})
	sdk.On("view.moved", func(e sdk.Event) {
		sdk.Status(strconv.Itoa(e.Cursor) + " " + strconv.Itoa(e.Top))
	})
}

func main() {}
