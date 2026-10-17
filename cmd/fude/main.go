// Command fude is a modal text editor.
package main

import (
	"errors"
	"fmt"
	"net"
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

// attach runs fude --attach [socket], a terminal client on a session of a
// running fude, and says why the session ended once the screen is gone
// (B239, B242).
func attach(args []string) error {
	var socket string
	switch len(args) {
	case 0:
		found, err := editor.FindSocket()
		if err != nil {
			return err
		}
		socket = found
	case 1:
		socket = args[0]
	default:
		return errors.New("usage: fude --attach [socket]")
	}
	a, err := term.Attach(socket)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := term.Run(a); err != nil {
		return err
	}
	if why := a.Ended(); why != "" {
		fmt.Println("fude:", why)
	}
	return nil
}

// join runs fude --join string, a terminal client on a guest session of
// the fude that shares with string, and says why the session ended once
// the screen is gone (B248, B249).
func join(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: fude --join string")
	}
	fmt.Println("waiting for the host to let you in")
	a, err := term.Join(args[0])
	if err != nil {
		return err
	}
	defer a.Close()
	if err := term.Run(a); err != nil {
		return err
	}
	if why := a.Ended(); why != "" {
		fmt.Println("fude:", why)
	}
	return nil
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
	if len(args) > 0 && args[0] == "--attach" {
		return attach(args[1:])
	}
	if len(args) > 0 && args[0] == "--join" {
		return join(args[1:])
	}
	// --share takes an address when the word after it is one (B248).
	sharing, address := len(args) > 0 && args[0] == "--share", ":0"
	if sharing {
		args = args[1:]
		if len(args) > 0 {
			if _, _, err := net.SplitHostPort(args[0]); err == nil {
				address, args = args[0], args[1:]
			}
		}
	}
	e, err := editor.Start(args)
	if err != nil {
		return err
	}
	if len(args) == 0 && os.Getenv(editor.RestartEnv) == "" {
		e.Welcome()
	}
	stop := e.Serve(e.SocketPath())
	if sharing {
		unshare, err := e.Share(address)
		if err != nil {
			stop()
			return err
		}
		defer unshare()
	}
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
