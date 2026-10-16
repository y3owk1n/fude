// Package main is a test plugin written with the sdk.
// init shows ready on the statusline of the buffer it read. Needs ui.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	sdk.Status("ready")
}

func main() {}
