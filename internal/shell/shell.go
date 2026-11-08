// Package shell finds the user's shell, which runs :! commands and
// :terminal, and the system's opener.
package shell

import "runtime"

// Command returns the program and arguments that run cmd through the
// user's shell, or that start the shell alone when cmd is "".
func Command(cmd string) []string {
	sh := program()
	if cmd == "" {
		return []string{sh}
	}
	return append([]string{sh}, run(sh, cmd)...)
}

// Opener returns the command that opens target, a link or file, as the
// system would.
func Opener(target string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"open", target}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", target}
	}
	return []string{"xdg-open", target}
}
