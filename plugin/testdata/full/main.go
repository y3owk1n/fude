// Package main is a test plugin written with the sdk.
// init registers the command :Whole, which :wh runs too, and which types x
// at the start of the line through the keys its result runs, and :Marked,
// which types the buffer's marks as name:place. Needs keys, and
// buffer.read and buffer.write for :Marked.
package main

import (
	"strconv"

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
	sdk.Command("Marked", "types the buffer's marks", func(c sdk.Call) {
		var marks []api.Mark
		if err := sdk.Read("Marks", nil, &marks); err != nil {
			sdk.Log(err.Error())
			return
		}
		for _, m := range marks {
			sdk.Edit(0, 0, m.Name+":"+strconv.Itoa(m.Pos)+"|")
		}
	})
}

func main() {}
