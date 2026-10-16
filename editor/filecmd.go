package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	b.markSaved(text)
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

// takesPath reports whether c's argument is a file or a directory, which
// expand expands.
func takesPath(c *command) bool {
	return strings.Contains(c.Args, "file]") || strings.Contains(c.Args, "file}") || strings.Contains(c.Args, "[dir]")
}

// expand returns arg with % made the current file's path and # the
// alternate file's, each changed by the modifiers after it: :p the full
// path, :h its directory, :t its name, :r it without its extension and :e
// the extension. A ~ that starts a word is the home directory, and \% and
// \# are % and # (B204).
func (e *Session) expand(arg string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(arg); i++ {
		ch := arg[i]
		switch {
		case ch == '\\' && i+1 < len(arg) && (arg[i+1] == '%' || arg[i+1] == '#'):
			i++
			b.WriteByte(arg[i])
		case ch == '%' || ch == '#':
			path := e.win.buf.file.path
			if ch == '#' {
				path = ""
				if e.win.alt != nil {
					path = e.win.alt.file.path
				}
			}
			if path == "" {
				return "", fmt.Errorf("no file for %c", ch)
			}
			for i+2 < len(arg) && arg[i+1] == ':' && strings.IndexByte("phtre", arg[i+2]) >= 0 {
				var err error
				if path, err = modify(path, arg[i+2]); err != nil {
					return "", err
				}
				i += 2
			}
			b.WriteString(path)
		case ch == '~' && (i == 0 || arg[i-1] == ' ') && (i+1 == len(arg) || arg[i+1] == '/' || arg[i+1] == ' '):
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("expand ~: %w", err)
			}
			b.WriteString(home)
		default:
			b.WriteByte(ch)
		}
	}
	return b.String(), nil
}

// modify returns path changed by the modifier m of expand, one of phtre.
func modify(path string, m byte) (string, error) {
	switch m {
	case 'p':
		return filepath.Abs(path)
	case 'h':
		return filepath.Dir(path), nil
	case 't':
		return filepath.Base(path), nil
	case 'r':
		return strings.TrimSuffix(path, filepath.Ext(path)), nil
	}
	return strings.TrimPrefix(filepath.Ext(path), "."), nil
}
