// Package main is a test plugin written with the sdk.
// init logs what echo hi prints, and refused when it may not run cat. Then
// it runs sleep 5, which its budget stops. Needs process.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	if out, err := sdk.Run("echo", "hi"); err == nil {
		sdk.Log(out)
	}
	if _, err := sdk.Run("cat"); err != nil {
		sdk.Log("refused")
	}
	sdk.Run("sleep", "5")
}

func main() {}
