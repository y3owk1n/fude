// Package main is a test plugin written with the sdk.
// init registers the command :Whole, which :wh runs too, and which types x
// at the start of the line through the keys its result runs. Needs keys.
package main

import (
	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Register(api.Registration{Kind: api.Command, Name: "Whole", Help: "types x at the start of the line", Short: []string{"wh"}}, func(_, _, _ int) int {
		if err := sdk.Do(api.Result{Keys: "0ix<Esc>"}); err != nil {
			sdk.Log(err.Error())
		}
		return 0
	})
}

func main() {}
