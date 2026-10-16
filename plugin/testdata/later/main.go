// Package main is a test plugin written with the sdk.
// init registers the command :Twice, which arms two timers that each type
// t at the start of the text. :Jobs runs sleep 0.1 and echo hi in the
// background, and types slept and what echo printed when each ends. :Fetch
// url gets the URL in the background and types its body. Needs
// buffer.write, process and net.
package main

import "github.com/y3owk1n/fude/sdk"

// typed types out at the start of the text, or the error.
func typed(out string, err error) {
	if err != nil {
		out = err.Error()
	}
	sdk.Edit(0, 0, out)
}

func init() {
	sdk.Command("Twice", "types t twice, a moment later", func(sdk.Call) {
		for range 2 {
			sdk.After(10, func() { sdk.Edit(0, 0, "t") })
		}
	})
	sdk.Command("Jobs", "runs two programs in the background", func(sdk.Call) {
		sdk.Start(func(out string, err error) { typed("slept"+out, err) }, "sleep", "0.1")
		sdk.Start(typed, "echo", "hi")
	})
	sdk.Command("Fetch", "gets a URL in the background", func(c sdk.Call) {
		sdk.Fetch(c.Args, typed)
	})
}

func main() {}
