// Package clipboard reads and writes the system clipboard through the
// program the system has for it: pbcopy on macOS, and wl-copy or xclip on
// Linux.
package clipboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNone is what Read and Write return on a system with no clipboard
// program.
var ErrNone = errors.New("no clipboard on this system")

// commands returns the commands that write and read the clipboard, or
// false when this system has none installed.
func commands() (write, read []string, ok bool) {
	switch {
	case runtime.GOOS == "darwin":
		write, read = []string{"pbcopy"}, []string{"pbpaste"}
	case os.Getenv("WAYLAND_DISPLAY") != "":
		write, read = []string{"wl-copy"}, []string{"wl-paste", "--no-newline"}
	case os.Getenv("DISPLAY") != "":
		write, read = []string{"xclip", "-selection", "clipboard"}, []string{"xclip", "-selection", "clipboard", "-o"}
	}
	if write == nil {
		return nil, nil, false
	}
	_, err1 := exec.LookPath(write[0])
	_, err2 := exec.LookPath(read[0])
	return write, read, err1 == nil && err2 == nil
}

// Read returns the clipboard's text.
func Read() (string, error) {
	_, read, ok := commands()
	if !ok {
		return "", ErrNone
	}
	out, err := exec.Command(read[0], read[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("read the clipboard with %s: %w", read[0], err)
	}
	return string(out), nil
}

// Write puts text on the clipboard.
func Write(text string) error {
	write, _, ok := commands()
	if !ok {
		return ErrNone
	}
	cmd := exec.Command(write[0], write[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write the clipboard with %s: %w", write[0], err)
	}
	return nil
}
