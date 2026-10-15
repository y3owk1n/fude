package editor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/y3owk1n/fude/core/kernel"
)

// revert puts the text of b's file on disk in b, as one undo step.
func (e *Session) revert(b *buffer) error {
	f, text, err := read(b.file.path)
	if err != nil {
		return err
	}
	now := b.text.Snapshot()
	if old := now.Slice(0, now.Len()); old != text {
		t := kernel.Transaction{Base: now.Version(), Origin: e.origin, Label: "reload", Time: e.now(), Edits: []kernel.Edit{replace(old, text)}}
		if err := b.text.Apply(t); err != nil {
			return err
		}
		b.head = b.text.Head()
	}
	b.file = f
	b.markSaved()
	return nil
}

// reread reads the current buffer's file again, as :e does, and refuses
// while it has changes unless force drops them, as :e! does (B155).
func (e *Session) reread(force bool) {
	b := e.win.buf
	switch {
	case b.file.path == "" || b.title != "":
		e.message = "no file name"
	case b.modified() && !force:
		e.message = fmt.Sprintf("\"%s\" has unsaved changes, :e! drops them", b.file.path)
	default:
		if err := e.revert(b); err != nil {
			e.message = err.Error()
		}
	}
}

// writeTo writes b's text to path, leaving b on its own file, as :w path
// does. It refuses to write over a file that exists unless force (B155).
func (e *Session) writeTo(b *buffer, path string, force bool) {
	if _, err := os.Stat(path); err == nil && !force {
		e.message = fmt.Sprintf("\"%s\" exists, :w! writes over it", path)
		return
	}
	f := b.file
	f.path = path
	t := b.text.Snapshot()
	n, err := f.write(t.Slice(0, t.Len()))
	if err != nil {
		e.message = err.Error()
		return
	}
	e.message = fmt.Sprintf("\"%s\" %dB written", path, n)
}

// empty shows a new buffer with no file in the current window, as :enew
// does (B155).
func (e *Session) empty() {
	if _, err := e.add(""); err != nil {
		e.message = err.Error()
		return
	}
	e.show(e.bufs[len(e.bufs)-1])
}

// cd makes dir the working directory, or the home directory for "". The
// buffers' paths become absolute first, so they keep naming their files
// (B155).
func (e *Session) cd(dir string) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			e.message = err.Error()
			return
		}
		dir = home
	}
	for _, b := range e.bufs {
		if b.file.path == "" || filepath.IsAbs(b.file.path) {
			continue
		}
		abs, err := filepath.Abs(b.file.path)
		if err != nil {
			e.message = err.Error()
			return
		}
		b.file.path = abs
	}
	if err := os.Chdir(dir); err != nil {
		e.message = fmt.Sprintf("cd %s: %v", dir, err)
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		e.message = err.Error()
		return
	}
	e.message = wd
}
