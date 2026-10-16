// Package main is a test plugin written with the sdk.
// init types "hi" at the start of the buffer, which needs buffer.write.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	sdk.Edit(0, 0, "hi")
}

func main() {}
