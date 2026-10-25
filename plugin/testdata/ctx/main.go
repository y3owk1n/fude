// Package main is a test plugin written with the sdk.
// init registers the command :Ctx, which types at the cursor what its call
// was about: its args, bang, first, last and ranged, joined by |. :Where
// types the buffer's id, path and filetype, the mode, how many lines the
// text has, the cursor's line and where the last line starts. The insert
// mode key <C-j> types the mode it ran in, in brackets. It has the options
// depth and on. :Opt name types the option name and a |, :Cfg path
// the config.toml value at path as JSON and a |, :SetOpt name value sets
// the option, and :Indent types the indent and tab widths. :Show
// text shows text in a window titled [shown], as Go, :Styled text shows it
// there in the diff.plus style, and :Say text shows it in the message
// area. <Plug>(ctx.hi) types hi at the cursor. :pick letters lists the
// query typed with 1 and 2 after it, and types the one picked at the
// start. :Go pos moves the cursor to
// pos, and :Open path opens the file at path with the cursor at 2. :Bufs types the id and text of every open
// buffer, and :Node the kinds of the syntax node at the cursor and its
// parent. :Lang types the language at the cursor, the names of the
// captures over it and its comment syntax. :Hover asks the language server
// for hover at the cursor and types its answer, or the error, at the
// start. :Fold first last closes a fold over those lines, and :Decos types
// the plugin's decorations, each id:start-end:fold, and a |. :Replace line
// shows the first two bytes of that line as § in markdown.h1, and :Shade
// line shades it in markdown.code. :Rows line draws a row of ── below it,
// and :Block first last makes those lines one block. Needs buffer.read,
// buffer.write, ui and lsp.
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
	sdk.Define("depth", "0", "how deep it looks")
	sdk.Define("on", "false", "turns it on")
	sdk.Define("skip", "", "the filetypes it leaves alone")
	sdk.Command("Opt", "types an option", func(c sdk.Call) {
		v, ok := sdk.Option(c.Args)
		if !ok {
			v = "none"
		}
		sdk.Edit(c.Cursor, c.Cursor, v+"|")
	})
	sdk.Command("Cfg", "types a config.toml value", func(c sdk.Call) {
		v, ok := sdk.Config(c.Args)
		if !ok {
			v = "none"
		}
		sdk.Edit(c.Cursor, c.Cursor, v+"|")
	})
	sdk.Command("SetOpt", "sets the option its first arg names to its second", func(c sdk.Call) {
		name, value, _ := strings.Cut(c.Args, " ")
		if err := sdk.SetOption(name, value); err != nil {
			sdk.Log(err.Error())
		}
	})
	sdk.Command("Indent", "types the indent and tab widths", func(c sdk.Call) {
		sdk.Edit(c.Cursor, c.Cursor, strconv.Itoa(sdk.IndentWidth())+"|"+strconv.Itoa(sdk.TabWidth())+"|")
	})
	sdk.Command("Show", "shows its args in a window", func(c sdk.Call) {
		sdk.Show("[shown]", "go", c.Args)
	})
	sdk.Command("Styled", "shows its args in a window, in the diff.plus style", func(c sdk.Call) {
		sdk.ShowStyled("[shown]", "", c.Args, []sdk.Span{{Start: 0, End: len(c.Args), Name: "diff.plus"}})
	})
	sdk.Plug("hi", "types hi at the cursor", func(c sdk.Call) {
		sdk.Edit(c.Cursor, c.Cursor, "hi")
	})
	sdk.Source("letters", "lists the query with a digit after it", func(c sdk.Call) {
		sdk.Picker("letters", "type a letter", c.Args, false, func(q string, _ bool) []string {
			return []string{q + "1", q + "2"}
		}, func(item string, ok bool) {
			if ok {
				sdk.Edit(0, 0, item)
			}
		})
	})
	sdk.Command("Say", "shows its args in the message area", func(c sdk.Call) {
		sdk.Message(c.Args)
	})
	sdk.Command("Go", "moves the cursor", func(c sdk.Call) {
		pos, _ := strconv.Atoi(c.Args)
		sdk.Jump(pos)
	})
	sdk.Command("Open", "opens a file", func(c sdk.Call) {
		sdk.Open(c.Args, 2)
	})
	sdk.Command("Bufs", "types every buffer's text", func(c sdk.Call) {
		bufs, _ := sdk.Buffers()
		var all []string
		for _, b := range bufs {
			n, _ := sdk.LenOf(b.ID)
			text, _ := sdk.TextOf(b.ID, 0, n)
			all = append(all, strconv.Itoa(b.ID)+"="+text)
		}
		sdk.Edit(c.Cursor, c.Cursor, strings.Join(all, "|"))
	})
	sdk.Command("Node", "types the syntax nodes at the cursor", func(c sdk.Call) {
		n, _ := sdk.NodeAt(c.Cursor, 0)
		up, _ := sdk.NodeAt(c.Cursor, 1)
		sdk.Edit(c.Cursor, c.Cursor, n.Kind+"|"+up.Kind+"|")
	})
	sdk.Command("Lang", "types the language at the cursor, the captures over its byte and its comment syntax", func(c sdk.Call) {
		lang, _ := sdk.Language(c.Cursor)
		spans, _ := sdk.Spans(c.Cursor, c.Cursor+1)
		var names []string
		for _, s := range spans {
			names = append(names, s.Name)
		}
		cs, _ := sdk.Comment(c.Cursor)
		sdk.Edit(c.Cursor, c.Cursor, lang+"|"+strings.Join(names, ",")+"|"+cs+"|")
	})
	sdk.Command("Fold", "closes a fold over the lines its args name", func(c sdk.Call) {
		first, last, _ := strings.Cut(c.Args, " ")
		s, _ := strconv.Atoi(first)
		e, _ := strconv.Atoi(last)
		start, _ := sdk.LineStart(s)
		end, _ := sdk.LineStart(e)
		sdk.Fold(start, end)
	})
	sdk.Command("Replace", "shows the first two bytes of the line its arg names as §", func(c sdk.Call) {
		line, _ := strconv.Atoi(c.Args)
		start, _ := sdk.LineStart(line)
		sdk.Replace(start, start+2, "§", "markdown.h1")
	})
	sdk.Command("Shade", "shades the line its arg names", func(c sdk.Call) {
		line, _ := strconv.Atoi(c.Args)
		start, _ := sdk.LineStart(line)
		sdk.Shade(start, start, "markdown.code")
	})
	sdk.Command("Rows", "draws a row below the line its arg names", func(c sdk.Call) {
		line, _ := strconv.Atoi(c.Args)
		start, _ := sdk.LineStart(line)
		sdk.Rows(start, []string{"──"}, "markdown.rule", false)
	})
	sdk.Command("Block", "makes the lines its args name one block", func(c sdk.Call) {
		first, last, _ := strings.Cut(c.Args, " ")
		s, _ := strconv.Atoi(first)
		e, _ := strconv.Atoi(last)
		start, _ := sdk.LineStart(s)
		end, _ := sdk.LineStart(e)
		sdk.Block(start, end)
	})
	sdk.Command("Decos", "types the plugin's decorations", func(c sdk.Call) {
		ds, _ := sdk.Decorations()
		var out []string
		for _, d := range ds {
			out = append(out, strconv.Itoa(d.ID)+":"+strconv.Itoa(d.Start)+"-"+strconv.Itoa(d.End)+":"+strconv.FormatBool(d.Fold))
		}
		sdk.Edit(c.Cursor, c.Cursor, strings.Join(out, ",")+"|")
	})
	sdk.Command("Range", "asks the server to format the cursor's byte, and types its answer", func(c sdk.Call) {
		sdk.RequestRange("textDocument/formatting", c.Cursor, c.Cursor+1, "", func(out string, err error) {
			if err != nil {
				out = err.Error()
			}
			sdk.Edit(0, 0, out)
		})
	})
	sdk.Command("Hover", "types the server's hover", func(c sdk.Call) {
		sdk.Request("textDocument/hover", c.Cursor, "", func(out string, err error) {
			if err != nil {
				out = err.Error()
			}
			sdk.Edit(0, 0, out)
		})
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
