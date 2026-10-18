// Package main is a test plugin written with the sdk.
// init registers the command :Whole, which :wh runs too, and which types x
// at the start of the line through the keys its result runs, and :Marked,
// which types the buffer's marks as name:place. :Where types the cursor
// and how many windows there are, and gW in visual mode types the
// selection's kind and ends. :State types the last search, the registers,
// the last search typed and the macro being recorded. Needs keys, and buffer.read and buffer.write
// for the others.
package main

import (
	"strconv"
	"strings"

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
	sdk.Command("Where", "types the cursor and how many windows there are", func(c sdk.Call) {
		var wins []api.Window
		sdk.Read("Windows", nil, &wins)
		sdk.Edit(0, 0, sdk.About("cursor")+":"+strconv.Itoa(len(wins))+"|")
	})
	sdk.Register(api.Registration{Kind: api.Key, Name: "gW", Help: "types the selection", Visual: true}, func(_, _, _ int) int {
		var sel api.Selection
		var ok bool
		sdk.Read("Selection", nil, &sel, &ok)
		sdk.Edit(0, 0, sel.Kind+strconv.Itoa(sel.Start)+"-"+strconv.Itoa(sel.End)+strconv.FormatBool(ok)+"|")
		return 0
	})
	sdk.Command("State", "types the search, the registers and the macro", func(sdk.Call) {
		var pattern, recording string
		var forward bool
		var regs []api.RegisterText
		var searches []string
		sdk.Read("Search", nil, &pattern, &forward)
		sdk.Read("Registers", nil, &regs)
		sdk.Read("History", []any{"/"}, &searches)
		sdk.Read("Recording", nil, &recording)
		out := pattern + strconv.FormatBool(forward) + "|"
		for _, r := range regs {
			out += r.Name + ":" + r.Text + ","
		}
		sdk.Edit(0, 0, out+"|"+strings.Join(searches, ",")+"|"+recording+"|")
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
