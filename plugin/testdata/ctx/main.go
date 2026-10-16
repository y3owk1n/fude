// Package main is a test plugin written with the sdk.
// init registers the command :Ctx, which types at the cursor what its call
// was about: its args, bang, first, last and ranged, joined by |. :Where
// types the buffer's id, path and filetype, the mode, how many lines the
// text has, the cursor's line and where the last line starts. The insert
// mode key <C-j> types the mode it ran in, in brackets. :Set name types
// the setting name, or none. :Show text shows text in a window titled
// [shown], as Go. :Go pos moves the cursor to pos, and :Open path opens
// the file at path with the cursor at 2. Needs buffer.read, buffer.write
// and ui.
package main

import (
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Command("Ctx", "types what its call was about", func(c sdk.Call) {
		about := []string{c.Args, strconv.FormatBool(c.Bang), strconv.Itoa(c.First), strconv.Itoa(c.Last), strconv.FormatBool(c.Ranged)}
		sdk.Edit(c.Cursor, c.Cursor, strings.Join(about, "|"))
	})
	sdk.Insert("<C-j>", "types the mode", func(c sdk.Call) {
		sdk.Edit(c.Cursor, c.Cursor, "["+sdk.Mode()+"]")
	})
	sdk.Command("Set", "types a setting", func(c sdk.Call) {
		v, ok := sdk.Setting(c.Args)
		if !ok {
			v = "none"
		}
		sdk.Edit(c.Cursor, c.Cursor, v)
	})
	sdk.Command("Show", "shows its args in a window", func(c sdk.Call) {
		sdk.Show("[shown]", "go", c.Args)
	})
	sdk.Command("Go", "moves the cursor", func(c sdk.Call) {
		pos, _ := strconv.Atoi(c.Args)
		sdk.Jump(pos)
	})
	sdk.Command("Open", "opens a file", func(c sdk.Call) {
		sdk.Open(c.Args, 2)
	})
	sdk.Command("Where", "types where its call ran", func(c sdk.Call) {
		b := sdk.Current()
		lines, _ := sdk.Lines()
		line, _ := sdk.LineOf(c.Cursor)
		last, _ := sdk.LineStart(lines - 1)
		where := []string{strconv.Itoa(b.ID), b.Path, b.Filetype, sdk.Mode(), strconv.Itoa(lines), strconv.Itoa(line), strconv.Itoa(last)}
		sdk.Edit(c.Cursor, c.Cursor, strings.Join(where, "|"))
	})
}

func main() {}
