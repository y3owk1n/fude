// Package main is a test plugin written with the sdk.
// init subscribes to buffer.changed, buffer.saving, editor.focused,
// lsp.attached and its own listen.ping, and registers the key gZ, which
// emits ping. Each event logs its name and puts the style keyword on the
// whole text in place of the one before, so a test sees which text it
// heard of. Once the text is over 8 bytes, it loops until its budget runs
// out. Needs buffer.read and ui.
package main

import "github.com/y3owk1n/fude/sdk"

// last is the decoration the last event put.
var last int

func heard(e sdk.Event) {
	sdk.Log(e.Name)
	n, _ := sdk.Len()
	for n > 8 {
	}
	if last != 0 {
		sdk.Undecorate(last)
	}
	last, _ = sdk.Decorate(0, n, "keyword")
}

func init() {
	sdk.On("buffer.changed", heard)
	sdk.On("listen.ping", heard)
	sdk.On("editor.focused", heard)
	sdk.On("buffer.saving", heard)
	sdk.On("lsp.attached", heard)
	sdk.Key("ping", "gZ", "emits listen.ping", func(sdk.Call) {
		sdk.Emit("ping")
	})
}

func main() {}
