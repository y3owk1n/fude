package editor

import (
	"errors"

	"github.com/y3owk1n/fude/clipboard"
)

// Clipboard reads the system clipboard. Without a clipboard program it is
// the text the session last copied (B36).
func (h host) Clipboard() (string, error) {
	text, err := clipboard.Read()
	if !errors.Is(err, clipboard.ErrNone) {
		return text, err
	}
	if h.e.clip == "" {
		return "", errors.New("nothing on the clipboard")
	}
	return h.e.clip, nil
}

// SetClipboard writes text to the system clipboard. Without a clipboard
// program, the client puts it on the terminal's clipboard (B36).
func (h host) SetClipboard(text string) error {
	err := clipboard.Write(text)
	if errors.Is(err, clipboard.ErrNone) {
		h.e.clip, h.e.toTerminal = text, true
		return nil
	}
	return err
}

// TerminalClipboard returns text the client should put on its terminal's
// clipboard with OSC 52, or false when there is none. It returns each copy
// once.
func (e *Session) TerminalClipboard() (string, bool) {
	ok := e.toTerminal
	e.toTerminal = false
	return e.clip, ok
}
