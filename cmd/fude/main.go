// Command fude is a modal text editor.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/ui/term"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fude:", err)
		os.Exit(1)
	}
}

// run prints a man page for --man, fude(1) or the one named after it, or
// opens args in the terminal. After :restart it runs fude again in its
// place, on the saved session (B170).
func run(args []string) error {
	if len(args) > 0 && len(args) <= 2 && args[0] == "--man" {
		page, err := editor.Man(strings.Join(args[1:], ""))
		if err != nil {
			return err
		}
		fmt.Print(page)
		return nil
	}
	e, err := editor.Start(args)
	if err != nil {
		return err
	}
	stop := e.Serve(e.SocketPath())
	err = term.Run(e)
	stop()
	if path := e.Restart(); err == nil && path != "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("restart: %w", err)
		}
		return fmt.Errorf("restart: %w", syscall.Exec(exe, os.Args[:1], append(os.Environ(), editor.RestartEnv+"="+path)))
	}
	return err
}
