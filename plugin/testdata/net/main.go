// Package main is a test plugin written with the sdk.
// init gets the URL that is the whole text and logs the body, then logs
// refused when it may not get http://example.invalid/. Needs buffer.read
// and net.
package main

import "github.com/y3owk1n/fude/sdk"

func init() {
	n, _ := sdk.Len()
	url, _ := sdk.Text(0, n)
	if body, err := sdk.Get(url); err == nil {
		sdk.Log(body)
	}
	if _, err := sdk.Get("http://example.invalid/"); err != nil {
		sdk.Log("refused")
	}
}

func main() {}
