// Package main is a test plugin written with the sdk.
// init subscribes to buffer.changed, and each change reads the first 64KB
// of the text and shows how many words it has on the statusline. Counting
// 10MB would run past the budget for an event. Needs buffer.read and ui.
package main

import (
	"strconv"

	"github.com/y3owk1n/fude/sdk"
)

func count(sdk.Event) {
	n, err := sdk.Len()
	if err != nil {
		sdk.Log(err.Error())
		return
	}
	text, err := sdk.Text(0, min(n, 64<<10))
	if err != nil {
		sdk.Log(err.Error())
		return
	}
	words, in := 0, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		blank := c == ' ' || c == '\t' || c == '\n'
		if !blank && !in {
			words++
		}
		in = !blank
	}
	sdk.Status(strconv.Itoa(words) + " words")
}

func init() {
	sdk.On("buffer.changed", count)
}

func main() {}
