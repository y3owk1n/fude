package main

import (
	"os"
	"os/exec"
)

// restart runs exe with argv and env in this console until it quits, as
// Windows cannot replace a process.
func restart(exe string, argv, env []string) error {
	cmd := exec.Command(exe, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdin, os.Stdout, os.Stderr, env
	return cmd.Run()
}
