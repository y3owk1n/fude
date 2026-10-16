// Package main is a test plugin written with the sdk.
// init registers the command :Twice, which arms two timers that each type
// t at the start of the text. Needs buffer.write.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	sdk.Command("Twice", "types t twice, a moment later", func(sdk.Call) {
		for range 2 {
			sdk.After(10, func() { sdk.Edit(0, 0, "t") })
		}
	})
}

func main() {}
