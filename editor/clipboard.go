package editor

import (
	"errors"
	"time"

	"github.com/y3owk1n/fude/clipboard"
)

// Clipboard reads the system clipboard. Without a clipboard program, and
// for any session but the first, it is the text the session last copied
// (B36, B241).
func (h host) Clipboard() (string, error) {
	if h.e != h.e.sessions[0] {
		return h.e.copied()
	}
	at := time.Now()
	text, err := clipboard.Read()
	traced("file", "read the clipboard", at, err)
	if !errors.Is(err, clipboard.ErrNone) {
		return text, err
	}
	return h.e.copied()
}

// copied returns the text the session last copied.
func (e *Session) copied() (string, error) {
	if e.clip == "" {
		return "", errors.New("nothing on the clipboard")
	}
	return e.clip, nil
}

// SetClipboard writes text to the system clipboard. Without a clipboard
// program, and for any session but the first, the session's client puts it
// on its own clipboard (B36, B241).
func (h host) SetClipboard(text string) error {
	if h.e != h.e.sessions[0] {
		h.e.clip, h.e.toTerminal = text, true
		return nil
	}
	at := time.Now()
	err := clipboard.Write(text)
	traced("file", "wrote the clipboard", at, err)
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
