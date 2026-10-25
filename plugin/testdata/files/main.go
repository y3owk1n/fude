// Package main is a test plugin written with the sdk. Its commands read,
// list, stat, write, make, move, remove and open what their arguments name,
// and show what they found, or the error. Needs fs.read, fs.write and ui.
package main

import (
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/sdk"
)

// command adds :name, which shows what f returns for its arguments.
func command(name string, f func(args []string) (string, error)) {
	sdk.Command(name, "", func(c sdk.Call) {
		out, err := f(strings.Fields(c.Args))
		if err != nil {
			out = "error " + err.Error()
		}
		sdk.Message(out)
	})
}

func init() {
	command("Read", func(a []string) (string, error) { return sdk.ReadFile(a[0]) })
	command("List", func(a []string) (string, error) {
		entries, err := sdk.List(a[0])
		var names []string
		for _, e := range entries {
			if e.Dir {
				e.Name += "/"
			}
			names = append(names, e.Name)
		}
		return strings.Join(names, " "), err
	})
	command("Stat", func(a []string) (string, error) {
		f, ok, err := sdk.Stat(a[0])
		switch {
		case err != nil:
			return "", err
		case !ok:
			return "none", nil
		case f.Dir:
			return "dir", nil
		}
		return strconv.FormatInt(f.Size, 10) + " " + strconv.FormatInt(f.Changed.UnixNano(), 10), nil
	})
	command("Write", func(a []string) (string, error) { return "wrote", sdk.WriteFile(a[0], strings.Join(a[1:], " ")) })
	command("Mkdir", func(a []string) (string, error) { return "made", sdk.MakeDir(a[0]) })
	command("Mv", func(a []string) (string, error) { return "moved", sdk.Rename(a[0], a[1]) })
	command("Rm", func(a []string) (string, error) { return "removed", sdk.Remove(a[0]) })
	command("Open", func(a []string) (string, error) { return "opened", sdk.Open(a[0], 0) })
}

func main() {}
