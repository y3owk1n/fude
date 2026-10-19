// Package shell finds the user's shell, which runs :! commands and
// :terminal.
package shell

// Command returns the program and arguments that run cmd through the
// user's shell, or that start the shell alone when cmd is "".
func Command(cmd string) []string {
	sh := program()
	if cmd == "" {
		return []string{sh}
	}
	return append([]string{sh}, run(sh, cmd)...)
}
