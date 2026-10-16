// Command fude is a modal text editor.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/plugin"
	"github.com/y3owk1n/fude/ui/term"
)

// pluginCommand runs fude plugin new <name> or fude plugin build [dir]
// (B173).
func pluginCommand(sub string, args []string) error {
	switch {
	case sub == "new" && len(args) == 1:
		if err := plugin.New(args[0]); err != nil {
			return err
		}
		fmt.Printf("wrote %s, and fude plugin build in it builds the plugin\n", args[0])
		return nil
	case sub == "build" && len(args) <= 1:
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		wasm, err := plugin.Build(dir)
		if err != nil {
			return err
		}
		fmt.Printf("built %s\n", wasm)
		return nil
	}
	return errors.New("usage: fude plugin new <name>, or fude plugin build [dir]")
}

// tail runs fude --tail [--json] [socket] (B174).
func tail(args []string) error {
	asJSON := len(args) > 0 && args[0] == "--json"
	if asJSON {
		args = args[1:]
	}
	switch len(args) {
	case 0:
		return editor.Tail(os.Stdout, "", asJSON)
	case 1:
		return editor.Tail(os.Stdout, args[0], asJSON)
	}
	return errors.New("usage: fude --tail [--json] [socket]")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fude:", err)
		os.Exit(1)
	}
}

// run prints a man page for --man, fude(1) or the one named after it,
// writes or builds a plugin for plugin new and plugin build, prints what a
// running fude does for --tail, or opens args in the terminal. After
// :restart it runs fude again in its place, on the saved session (B170).
func run(args []string) error {
	if len(args) >= 2 && args[0] == "plugin" && (args[1] == "new" || args[1] == "build") {
		return pluginCommand(args[1], args[2:])
	}
	if len(args) > 0 && len(args) <= 2 && args[0] == "--man" {
		page, err := editor.Man(strings.Join(args[1:], ""))
		if err != nil {
			return err
		}
		fmt.Print(page)
		return nil
	}
	if len(args) > 0 && args[0] == "--tail" {
		return tail(args[1:])
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
