// Package main is a test plugin written with the sdk.
// init puts the style keyword on [0, 2), which needs ui, and registers the
// key gX, which panics, so the plugin unloads.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	sdk.Decorate(0, 2, "keyword")
	sdk.Key("gX", "panics", func(sdk.Call) {
		panic("gX")
	})
}

func main() {}
