// Package main is a test plugin written with the sdk.
// init subscribes to view.moved, and shows the cursor and the top of the
// view it heard of on the statusline, as "cursor top". It subscribes to
// buffer.changed too, and puts the style keyword on the range the change it
// heard of covers, the sign + over a sign put under, and the note changed
// on its first line, in place of the ones before. Needs ui.
package main

import (
	"strconv"

	"github.com/y3owk1n/fude/sdk"
)

// last are the decorations the last change put.
var last []int

func init() {
	sdk.On("buffer.changed", func(e sdk.Event) {
		for _, id := range last {
			sdk.Undecorate(id)
		}
		style, _ := sdk.Decorate(e.Start, e.End, "keyword")
		under, _ := sdk.SignUnder(e.Start, "-", "git.delete")
		sign, _ := sdk.Sign(e.Start, "+", "git.add")
		note, _ := sdk.Note(e.Start, "changed")
		last = []int{style, under, sign, note}
	})
	sdk.On("view.moved", func(e sdk.Event) {
		sdk.Status(strconv.Itoa(e.Cursor) + " " + strconv.Itoa(e.Top))
	})
}

func main() {}
