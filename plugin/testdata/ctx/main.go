// Package main is a test plugin written with the sdk.
// init registers the command :Ctx, which types at the cursor what its call
// was about: its args, bang, first, last and ranged, joined by |. :Where
// types the buffer's id, path and filetype, the mode, how many lines the
// text has, the cursor's line and where the last line starts. The insert
// mode key <C-j> types the mode it ran in, in brackets. Needs buffer.read
// and buffer.write.
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
