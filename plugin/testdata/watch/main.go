// Package main is a test plugin written with the sdk.
// init subscribes to view.moved, and shows the cursor and the top of the
// view it heard of on the statusline, as "cursor top". Needs ui.
package main

import (
	"strconv"

	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.On("view.moved", func(e sdk.Event) {
		sdk.Status(strconv.Itoa(e.Cursor) + " " + strconv.Itoa(e.Top))
	})
}

func main() {}
