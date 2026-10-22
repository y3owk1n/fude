// Command fude is a modal text editor.
package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/y3owk1n/fude/client"
	"github.com/y3owk1n/fude/distribution"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/internal/relay"
	"github.com/y3owk1n/fude/plugin"
	"github.com/y3owk1n/fude/ui/term"
)

func main() {
	err := run(os.Args[1:])
	var code exitCode
	if errors.As(err, &code) {
		os.Exit(int(code))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fude:", err)
		os.Exit(1)
	}
}

// exitCode is the code :cquit asked fude to exit with (B423).
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("exit %d", int(c)) }

// run runs the subcommand args name, or else opens the files they name.
func run(args []string) error {
	cmd, words, flags, err := parse(args)
	if err != nil {
		return err
	}
	// --clean reads no config (B270).
	remove := func() {}
	if _, ok := flags["--clean"]; ok {
		if remove, err = editor.Clean(); err != nil {
			return err
		}
	}
	defer remove()
	switch cmd {
	case "help", "version":
		if len(words) > 0 {
			return usage(cmd)
		}
		if cmd == "version" {
			fmt.Println(version())
			return nil
		}
		help()
	case "man":
		if len(words) > 1 {
			return usage(cmd)
		}
		page, err := editor.Man(strings.Join(words, ""), distribution.Modules()...)
		if err != nil {
			return err
		}
		fmt.Print(page)
	case "plugin":
		return pluginCommand(words)
	case "storage":
		_, force := flags["--force"]
		return storage(words, force)
	case "relay":
		return relays(words)
	case "tail":
		if len(words) > 0 {
			return usage(cmd)
		}
		path, err := socket(flags)
		if err != nil {
			return err
		}
		_, asJSON := flags["--json"]
		return editor.Tail(os.Stdout, path, asJSON)
	case "attach":
		if len(words) > 0 {
			return usage(cmd)
		}
		return attach(flags)
	case "join":
		return join(words, flags)
	case "open", "keys", "state":
		return remote(cmd, words, flags)
	default:
		return edit(cmd == "share", words, flags, remove)
	}
	return nil
}

// parse returns the subcommand args name, "" for none, the words after it
// and the flags they set, each with its value. -h and --help anywhere ask
// for help, and -v and --version for the version (B350).
func parse(args []string) (string, []string, map[string]string, error) {
	subcommands := map[string]bool{}
	for _, c := range editor.Subcommands {
		subcommands[strings.Fields(c.Name)[0]] = true
	}
	cmd, flags, ended := "", map[string]string{}, false
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case ended || a == "-" || !strings.HasPrefix(a, "-"):
			if cmd == "" && len(words) == 0 && !ended && subcommands[a] {
				cmd = a
				continue
			}
			words = append(words, a)
		case a == "--":
			ended = true
		case a == "-h" || a == "--help":
			return "help", nil, nil, nil
		case a == "-v" || a == "--version":
			return "version", nil, nil, nil
		case takesValue(a):
			if i++; i == len(args) {
				return "", nil, nil, fmt.Errorf("%s needs a value", a)
			}
			flags[a] = args[i]
		default:
			flags[a] = ""
		}
	}
	for _, f := range slices.Sorted(maps.Keys(flags)) {
		if slices.ContainsFunc(flagsOf(cmd), func(spelled string) bool { return strings.Fields(spelled)[0] == f }) {
			continue
		}
		if cmd == "" {
			return "", nil, nil, fmt.Errorf("no flag %s, and fude help lists them", f)
		}
		return "", nil, nil, fmt.Errorf("%s takes no %s, and fude help lists its flags", cmd, f)
	}
	return cmd, words, flags, nil
}

// takesValue reports whether flag is one that a value follows.
func takesValue(flag string) bool {
	return slices.ContainsFunc(editor.Flags, func(f editor.Flag) bool { return strings.HasPrefix(f.Flag, flag+" <") })
}

// flagsOf returns the flags subcommand cmd takes, as they are spelled in
// editor.Flags, such as --name <name>.
func flagsOf(cmd string) []string {
	var flags []string
	for _, f := range editor.Flags {
		if slices.Contains(f.For, cmp.Or(cmd, "fude")) {
			flags = append(flags, f.Flag)
		}
	}
	return flags
}

// usage is the error for words cmd does not take, with how to run it.
func usage(cmd string) error {
	var lines []string
	for _, c := range editor.Subcommands {
		if strings.Fields(c.Name)[0] == cmd {
			line := "fude " + c.Name
			for _, f := range flagsOf(cmd) {
				line += " [" + f + "]"
			}
			lines = append(lines, strings.TrimSpace(line+" "+c.Args))
		}
	}
	return errors.New("usage: " + strings.Join(lines, ", or "))
}

// help prints each way to run fude (B348).
func help() {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprint(w, `fude is a modal text editor.

usage:
  fude [flags] [+<cmd>...] [<file>[:<line>[:<byte>]]...]
  fude <command> [flags] [<args>]

commands:
`)
	for _, c := range editor.Subcommands {
		fmt.Fprintf(w, "  %s\t%s\n", strings.TrimSpace(c.Name+" "+c.Args), c.Help)
	}
	fmt.Fprint(w, "\nflags:\n")
	for _, f := range editor.Flags {
		fmt.Fprintf(w, "  %s\t%s", f.Flag, f.Help)
		if f.For != nil {
			fmt.Fprintf(w, "\t(%s)", strings.Join(f.For, ", "))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprint(w, `
open, keys, state, attach and tail reach the one fude running, or the one
--socket names. In a fude's :terminal they reach that fude without
--socket, so fude open notes.md there opens notes.md in it.
`)
	w.Flush()
}

// build is the name of a build that knows neither its module version nor its
// commit. oku's builds set it with -ldflags "-X main.build=...".
var build string

// version is build when set, else the release fude was installed as, else
// the date and commit it was built from (B349).
func version() string {
	if build != "" {
		return "fude " + build
	}
	vcs := map[string]string{}
	if info, ok := debug.ReadBuildInfo(); ok {
		// A build by go install of a release, as the release workflow makes,
		// knows its version.
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return "fude " + v
		}
		for _, s := range info.Settings {
			vcs[s.Key] = s.Value
		}
	}
	rev, at := vcs["vcs.revision"], vcs["vcs.time"]
	if len(rev) < 7 || len(at) < 10 {
		return "fude (devel)"
	}
	v := "fude " + strings.ReplaceAll(at[:10], "-", ".") + " (" + rev[:7]
	if vcs["vcs.modified"] == "true" {
		v += ", modified"
	}
	return v + ")"
}

// name is the session's name others see (B252).
func name(flags map[string]string) string {
	// Windows names the user in USERNAME.
	return cmp.Or(flags["--name"], os.Getenv("USER"), os.Getenv("USERNAME"))
}

// socket is the socket of the fude a subcommand reaches (B352).
func socket(flags map[string]string) (string, error) {
	if path := cmp.Or(flags["--socket"], os.Getenv("FUDE_SOCKET")); path != "" {
		return path, nil
	}
	return editor.FindSocket()
}

// remote runs fude open, keys or state on the session B352 finds.
func remote(cmd string, words []string, flags map[string]string) error {
	var params any
	switch {
	case cmd == "open" && len(words) > 0:
		// The paths are from here, not from where fude runs (B351). Abs would
		// read the a: of a:2 as a Windows drive, so a relative path joins the
		// working directory instead.
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("open: %w", err)
		}
		paths := make([]string, len(words))
		for i, w := range words {
			paths[i] = w
			if !filepath.IsAbs(w) {
				paths[i] = filepath.Join(wd, w)
			}
		}
		params = map[string]any{"paths": paths}
	case cmd == "keys" && len(words) == 1:
		params = map[string]any{"keys": words[0]}
	case cmd != "state" || len(words) > 0:
		return usage(cmd)
	}
	path, err := socket(flags)
	if err != nil {
		return err
	}
	c, err := client.Dial(path)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Call("session", map[string]string{"name": cmp.Or(flags["--session"], os.Getenv("FUDE_SESSION"))}, nil); err != nil {
		return err
	}
	var state json.RawMessage
	if err := c.Call(cmd, params, &state); err != nil {
		return err
	}
	if cmd == "state" {
		fmt.Printf("%s\n", state)
	}
	return nil
}

// pluginCommand runs fude plugin new <name> or fude plugin build [dir]
// (B173).
func pluginCommand(words []string) error {
	switch {
	case len(words) == 2 && words[0] == "new":
		if err := plugin.New(words[1]); err != nil {
			return err
		}
		fmt.Printf("wrote %s, and fude plugin build in it builds the plugin\n", words[1])
		return nil
	case len(words) >= 1 && len(words) <= 2 && words[0] == "build":
		dir := "."
		if len(words) == 2 {
			dir = words[1]
		}
		wasm, err := plugin.Build(dir)
		if err != nil {
			return err
		}
		fmt.Printf("built %s\n", wasm)
		return nil
	}
	return usage("plugin")
}

// storage prints where fude keeps files between runs and how much each
// takes. With clean it deletes what fude makes again. With --force it also
// deletes the user's history, after it says what that loses and the user
// answers y (B383).
func storage(words []string, force bool) error {
	stores, err := editor.Stores()
	if err != nil {
		return err
	}
	switch {
	case len(words) == 0 && !force:
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		total := int64(0)
		for _, s := range stores {
			n, err := size(s.Path)
			if err != nil {
				return err
			}
			total += n
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, bytes(n), s.Path, s.Holds)
		}
		fmt.Fprintf(w, "total\t%s\n", bytes(total))
		return w.Flush()
	case len(words) != 1 || words[0] != "clean":
		return usage("storage")
	}
	if force {
		for _, s := range stores {
			if s.Loses == "" {
				continue
			}
			n, err := size(s.Path)
			if err != nil {
				return err
			}
			fmt.Printf("%s, %s at %s\n  deleting it loses %s\n", s.Name, bytes(n), s.Path, s.Loses)
		}
		fmt.Print("A fude running now keeps writing what it has open. Delete these too? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("deleted nothing")
			return nil
		}
	}
	freed := int64(0)
	for _, s := range stores {
		if s.Loses != "" && !force {
			continue
		}
		n, err := size(s.Path)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(s.Path); err != nil {
			return fmt.Errorf("delete %s: %w", s.Name, err)
		}
		freed += n
	}
	fmt.Printf("freed %s\n", bytes(freed))
	return nil
}

// size returns how many bytes the files under path take, 0 when there are
// none.
func size(path string) (int64, error) {
	n := int64(0)
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			n += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("size of %s: %w", path, err)
	}
	return n, nil
}

// bytes is n in the largest unit that keeps it at least 1, as 1.4MB.
func bytes(n int64) string {
	f, units := float64(n), []string{"B", "KB", "MB", "GB"}
	i := 0
	for ; f >= 1024 && i < len(units)-1; i++ {
		f /= 1024
	}
	if i == 0 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

// attach runs fude attach, a terminal client on a session of a running
// fude, and says why the session ended once the screen is gone (B239,
// B242).
func attach(flags map[string]string) error {
	path, err := socket(flags)
	if err != nil {
		return err
	}
	a, err := term.Attach(path, name(flags))
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

// join runs fude join, a terminal client on a guest session of the fude
// that shares with the string, and says why the session ended once the
// screen is gone (B248, B249, B255).
func join(words []string, flags map[string]string) error {
	if len(words) != 1 {
		return usage("join")
	}
	// A guest still joins with what it read of a config that fails (B254).
	own, err := editor.OwnConfig()
	if err != nil {
		fmt.Println("fude:", err)
	}
	fmt.Println("waiting for the host to let you in")
	_, readOnly := flags["--read-only"]
	a, err := term.Join(words[0], name(flags), own, readOnly)
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
func relays(words []string) error {
	if len(words) > 1 {
		return usage("relay")
	}
	l, err := net.Listen("tcp", relay.WithPort(strings.Join(words, "")))
	if err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	fmt.Println("fude relays on", l.Addr())
	return relay.Serve(l)
}

// edit opens the files in words in the terminal, and with sharing lets
// guests join (B248, B251). After :restart it runs fude again in its place,
// on the saved session (B170), and calls remove first.
func edit(sharing bool, words []string, flags map[string]string, remove func()) error {
	// A share's first word is where it listens when it is an address
	// (B248), unless it goes through a relay (B251).
	address, via := ":0", flags["--relay"]
	if sharing && via == "" && len(words) > 0 {
		if _, _, err := net.SplitHostPort(words[0]); err == nil {
			address, words = words[0], words[1:]
		}
	}
	// Start reads RestartEnv and unsets it.
	restarted := os.Getenv(editor.RestartEnv) != ""
	e, err := editor.Start(words)
	if err != nil {
		return err
	}
	for _, m := range distribution.Modules() {
		e.Use(m)
	}
	e.SetName(name(flags))
	if _, ok := flags["-d"]; ok {
		e.Diff()
	}
	if len(words) == 0 && !restarted {
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
		if _, clean := flags["--clean"]; clean {
			argv = []string{os.Args[0], "--clean"}
		}
		// On Unix the new fude replaces this process, so deferred calls
		// never run.
		remove()
		if err := restart(exe, argv, append(os.Environ(), editor.RestartEnv+"="+path)); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
	}
	if code := e.ExitCode(); err == nil && code != 0 {
		return exitCode(code)
	}
	return err
}
