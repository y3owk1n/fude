// Package main is a test plugin written with the sdk.
// init makes an edit, then panics, so the edit must not land. Needs
// buffer.write.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	sdk.Edit(0, 0, "x")
	panic("trap")
}

func main() {}
