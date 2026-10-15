package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// clipboard returns the commands that write and read the system clipboard,
// or false when this system has none installed.
func clipboard() (write, read []string, ok bool) {
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

// Clipboard reads the system clipboard. Without a clipboard command it is
// the text the session last copied (B36).
func (h host) Clipboard() (string, error) {
	_, read, ok := clipboard()
	if !ok {
		if h.e.clip == "" {
			return "", errors.New("nothing on the clipboard")
		}
		return h.e.clip, nil
	}
	out, err := exec.Command(read[0], read[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("read the clipboard with %s: %w", read[0], err)
	}
	return string(out), nil
}

// SetClipboard writes text to the system clipboard. Without a clipboard
// command, the client puts it on the terminal's clipboard (B36).
func (h host) SetClipboard(text string) error {
	write, _, ok := clipboard()
	if !ok {
		h.e.clip, h.e.toTerminal = text, true
		return nil
	}
	cmd := exec.Command(write[0], write[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write the clipboard with %s: %w", write[0], err)
	}
	return nil
}

// TerminalClipboard returns text the client should put on its terminal's
// clipboard with OSC 52, or false when there is none. It returns each copy
// once.
func (e *Session) TerminalClipboard() (string, bool) {
	ok := e.toTerminal
	e.toTerminal = false
	return e.clip, ok
}
