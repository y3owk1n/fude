// Command fude is a modal text editor.
package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/internal/relay"
	"github.com/y3owk1n/fude/modules/blame"
	"github.com/y3owk1n/fude/modules/colours"
	"github.com/y3owk1n/fude/modules/comment"
	"github.com/y3owk1n/fude/modules/debug"
	"github.com/y3owk1n/fude/modules/editorconfig"
	"github.com/y3owk1n/fude/modules/fold"
	"github.com/y3owk1n/fude/modules/format"
	"github.com/y3owk1n/fude/modules/git"
	"github.com/y3owk1n/fude/modules/grep"
	"github.com/y3owk1n/fude/modules/inlay"
	"github.com/y3owk1n/fude/modules/links"
	lspmod "github.com/y3owk1n/fude/modules/lsp"
	"github.com/y3owk1n/fude/modules/markdown"
	"github.com/y3owk1n/fude/modules/spell"
	"github.com/y3owk1n/fude/modules/task"
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

// attach runs fude --attach [socket], a terminal client on a session
// called name of a running fude, and says why the session ended once the
// screen is gone (B239, B242).
func attach(args []string, name string) error {
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
	a, err := term.Attach(socket, name)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := term.Run(a); err != nil {
		return err
	}
	// The reason is a whole message, such as "fude quit".
	if why := a.Ended(); why != "" {
		fmt.Println(why)
	}
	return nil
}

// join runs fude --join [--read-only] string, a terminal client on a guest
// session called name of the fude that shares with string, and says why
// the session ended once the screen is gone (B248, B249, B255).
func join(args []string, name string) error {
	readOnly := len(args) == 2 && args[0] == "--read-only"
	if readOnly {
		args = args[1:]
	}
	if len(args) != 1 {
		return errors.New("usage: fude --join [--read-only] string")
	}
	// A guest still joins with what it read of a config that fails (B254).
	own, err := editor.OwnConfig()
	if err != nil {
		fmt.Println("fude:", err)
	}
	fmt.Println("waiting for the host to let you in")
	a, err := term.Join(args[0], name, own, readOnly)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := term.Run(a); err != nil {
		return err
	}
	// The reason is a whole message, such as "fude quit".
	if why := a.Ended(); why != "" {
		fmt.Println(why)
	}
	return nil
}

// relays runs fude relay [address], which passes shares through until the
// process ends (B251).
func relays(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: fude relay [address]")
	}
	addr := ""
	if len(args) == 1 {
		addr = args[0]
	}
	l, err := net.Listen("tcp", relay.WithPort(addr))
	if err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	fmt.Println("fude relays on", l.Addr())
	return relay.Serve(l)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fude:", err)
		os.Exit(1)
	}
}

// modules returns the modules this distribution adds to the editor (D49).
func modules() []api.Module {
	words := ""
	if dir, err := editor.ConfigDir(); err == nil {
		words = filepath.Join(dir, "words.txt")
	}
	return []api.Module{blame.New(), colours.New(), comment.New(), debug.New(), editorconfig.New(), fold.New(), format.New(), git.New(), grep.New(), inlay.New(), links.New(), lspmod.New(), markdown.New(), spell.New(words), task.New()}
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
		page, err := editor.Man(strings.Join(args[1:], ""), modules()...)
		if err != nil {
			return err
		}
		fmt.Print(page)
		return nil
	}
	if len(args) > 0 && args[0] == "--tail" {
		return tail(args[1:])
	}
	// --clean reads no config (B270).
	clean := len(args) > 0 && args[0] == "--clean"
	remove := func() {}
	if clean {
		var err error
		if remove, err = editor.Clean(); err != nil {
			return err
		}
		args = args[1:]
	}
	defer remove()
	// --name names this machine's session, or else $USER does (B252).
	name := os.Getenv("USER")
	if len(args) > 1 && args[0] == "--name" {
		name, args = args[1], args[2:]
	}
	if len(args) > 0 && args[0] == "--attach" {
		return attach(args[1:], name)
	}
	if len(args) > 0 && args[0] == "--join" {
		return join(args[1:], name)
	}
	if len(args) > 0 && args[0] == "relay" {
		return relays(args[1:])
	}
	// --share takes an address when the word after it is one (B248), or
	// --relay and the relay's (B251).
	sharing, address, via := len(args) > 0 && args[0] == "--share", ":0", ""
	if sharing {
		args = args[1:]
		switch {
		case len(args) > 1 && args[0] == "--relay":
			via, args = args[1], args[2:]
		case len(args) > 0:
			if _, _, err := net.SplitHostPort(args[0]); err == nil {
				address, args = args[0], args[1:]
			}
		}
	}
	// Start reads RestartEnv and unsets it.
	restarted := os.Getenv(editor.RestartEnv) != ""
	e, err := editor.Start(args)
	if err != nil {
		return err
	}
	for _, m := range modules() {
		e.Use(m)
	}
	e.SetName(name)
	if len(args) == 0 && !restarted {
		e.Welcome()
	}
	stop := e.Serve(e.SocketPath())
	if sharing {
		share := e.Share
		if via != "" {
			address, share = via, e.ShareRelay
		}
		unshare, err := share(address)
		if err != nil {
			stop()
			return err
		}
		defer unshare()
	}
	err = term.Run(e)
	e.Finish(time.Second)
	stop()
	if path := e.Restart(); err == nil && path != "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("restart: %w", err)
		}
		argv := os.Args[:1]
		if clean {
			argv = []string{os.Args[0], "--clean"}
		}
		// Exec replaces the process, so deferred calls never run.
		remove()
		return fmt.Errorf("restart: %w", syscall.Exec(exe, argv, append(os.Environ(), editor.RestartEnv+"="+path)))
	}
	return err
}
