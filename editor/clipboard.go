package editor

import (
	"errors"
	"time"

	"github.com/y3owk1n/fude/frontend/clipboard"
	"github.com/y3owk1n/fude/internal/trace"
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
	trace.Since("file", "read the clipboard", at, err)
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
	trace.Since("file", "wrote the clipboard", at, err)
	if errors.Is(err, clipboard.ErrNone) {
		h.e.clip, h.e.toTerminal = text, true
		return nil
	}
	return err
}

// Clipboard returns text a copy asks the frontend to put on its own
// clipboard, as a terminal does with OSC 52, or false when there is none.
// It returns each copy once.
func (e *Session) Clipboard() (string, bool) {
	ok := e.toTerminal
	e.toTerminal = false
	return e.clip, ok
}

// Alert returns, once, a message a module asked the frontend to notify the
// user of while they are away (B564).
func (e *Session) Alert() (string, bool) {
	a := e.alert
	e.alert = nil
	if a == nil {
		return "", false
	}
	return *a, true
}
