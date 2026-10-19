//go:build !windows

package shell

import (
	"cmp"
	"os"
)

// program is SHELL, or else sh.
func program() string { return cmp.Or(os.Getenv("SHELL"), "/bin/sh") }

// run is what the shell takes to run cmd.
func run(_, cmd string) []string { return []string{"-c", cmd} }
