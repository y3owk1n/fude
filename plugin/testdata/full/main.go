// Package main is a test plugin written with the sdk.
// init registers the command :Whole, which :wh runs too, and which types x
// at the start of the line through the keys its result runs, and :Marked,
// which types the buffer's marks as name:place. :Where types the cursor
// and how many windows there are, and gW in visual mode types the
// selection's kind and ends. :State types the last search, the registers,
// the last search typed and the macro being recorded. :Other types x at
// the start of every other buffer. :Act split, close, write, scratch or
// drop splits the window on the buffer, closes the window, writes the
// buffer, opens a scratch buffer holding hi, or closes the buffer. :Mach
// sets mark b at the cursor, register r to hi, the search to a and wrap
// off, and runs :s/a/z/, and :Wrap types the wrap option. :Paint defines
// the style full.hot and colours the first byte in it. :Heard types the
// names of the window, search, macro, selection and theme events it heard.
// It registers § as a Typed key, which needs buffer.read, explains :Whole
// at length and adds the guide page guide-full. Needs keys, and buffer.read
// and buffer.write for the others.
package main

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/sdk"
)

// heard are the events the plugin heard, by name.
var heard = map[string]bool{}

func init() {
	for _, name := range []string{"window.opened", "window.closed", "window.focused", "search.changed", "macro.recorded", "selection.changed", "theme.changed"} {
		sdk.On(name, func(ev sdk.Event) { heard[ev.Name] = true })
	}
	sdk.Register(api.Registration{Kind: api.Typed, Name: "§", Help: "lets § type itself"}, func(_, _, _ int) int { return 0 })
	sdk.Command("Heard", "types the events heard", func(sdk.Call) {
		sdk.Edit(0, 0, strings.Join(slices.Sorted(maps.Keys(heard)), ",")+"|")
	})
	sdk.Page("full", "# The full plugin\n\nIt tests what a plugin may do.")
	sdk.Register(api.Registration{Kind: api.Command, Name: "Whole", Help: "types x at the start of the line", Doc: "Types x at the start of the cursor's line.", Short: []string{"wh"}}, func(_, _, _ int) int {
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
	sdk.Command("Other", "types x at the start of every other buffer", func(sdk.Call) {
		bufs, _ := sdk.Buffers()
		var edits []api.Edit
		for _, b := range bufs {
			if b.ID != sdk.Current().ID {
				edits = append(edits, api.Edit{Text: "x", Buffer: b.ID})
			}
		}
		if err := sdk.Do(api.Result{Edits: edits}); err != nil {
			sdk.Log(err.Error())
		}
	})
	sdk.Command("Act", "acts on the buffer or the window", func(c sdk.Call) {
		id := sdk.Current().ID
		var r api.Result
		switch c.Args {
		case "split":
			r.Jump = &api.Jump{Buffer: id, Where: "right"}
		case "close":
			r.Windows = []api.WindowAction{{Do: "close"}}
		case "write":
			r.Write = []int{id}
		case "scratch":
			r.Scratch = &api.Scratch{Text: "hi"}
		case "drop":
			r.Close = []int{id}
		}
		if err := sdk.Do(r); err != nil {
			sdk.Log(err.Error())
		}
	})
	sdk.Command("Mach", "sets a mark, a register, the search and an option, and runs :s", func(c sdk.Call) {
		err := sdk.Do(api.Result{
			Marks:     []api.Mark{{Name: "b", Pos: c.Cursor}},
			Registers: []api.RegisterText{{Name: "r", Text: "hi"}},
			Search:    &api.Search{Pattern: "a", Forward: true},
			Set:       map[string]string{"wrap": "false"},
			Ex:        []string{"s/a/z/"},
		})
		if err != nil {
			sdk.Log(err.Error())
		}
	})
	sdk.Command("Wrap", "types the wrap option", func(sdk.Call) {
		v, _ := sdk.Option("wrap")
		sdk.Edit(0, 0, v+"|")
	})
	sdk.Command("Paint", "colours the first byte in a style of its own", func(sdk.Call) {
		err := sdk.Do(api.Result{
			Styles:   map[string]api.Style{"full.hot": {Fg: "#ff0000", Bold: true}},
			Decorate: []api.Decoration{{ID: sdk.ID(), Start: 0, End: 1, Style: "full.hot"}},
		})
		if err != nil {
			sdk.Log(err.Error())
		}
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
