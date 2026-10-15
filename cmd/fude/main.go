// Command fude is a modal text editor.
package main

import (
	"fmt"
	"os"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/ui/term"
)

func main() {
	e, err := editor.Start(os.Args[1:])
	if err == nil {
		stop := e.Serve(e.SocketPath())
		err = term.Run(e)
		stop()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fude:", err)
		os.Exit(1)
	}
}
