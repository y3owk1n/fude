// Package main is a test plugin written with the sdk.
// init never returns.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	sdk.Log("spinning")
	for {
	}
}

func main() {}
