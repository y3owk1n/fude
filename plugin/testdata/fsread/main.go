// Package main is a test plugin written with the sdk.
// init logs data.txt from the project, refused when it may not read
// ../out.txt, and unlinked when it may not read link. Needs fs.read.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	if data, err := sdk.ReadFile("data.txt"); err == nil {
		sdk.Log(data)
	}
	if _, err := sdk.ReadFile("../out.txt"); err != nil {
		sdk.Log("refused")
	}
	if _, err := sdk.ReadFile("link"); err != nil {
		sdk.Log("unlinked")
	}
}

func main() {}
