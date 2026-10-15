// Command fude is a modal text editor.
package main

import (
	"fmt"
	"os"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/ui/term"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fude <path>")
		os.Exit(2)
	}
	e, err := editor.Open(os.Args[1])
	if err == nil {
		err = term.Run(e)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fude:", err)
		os.Exit(1)
	}
}
