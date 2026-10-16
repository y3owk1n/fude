// Package main is a test plugin written with the sdk.
// init registers the key gP, which shows **hi** in a float. Needs ui.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	sdk.Key("gP", "shows hi in a float", func(sdk.Call) {
		sdk.Float("**hi**")
	})
}

func main() {}
