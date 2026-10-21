package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// revert puts the text of b's file on disk in b, as one undo step.
func (e *Session) revert(b *buffer) error {
	if err := e.mayEdit(); err != nil {
		return err
	}
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
func (e *Session) reread(force bool) error {
	b := e.win.buf
	switch {
	case b.listed != "" && b.modified() && !force:
		return fmt.Errorf("\"%s\" has unsaved changes, :e! drops them", b.title)
	case b.listed != "":
		// An explorer reads its directory again (B124).
		e.reload(b)
		return nil
	case b.file.path == "" || b.title != "":
		return errors.New("no file name")
	case b.modified() && !force:
		return fmt.Errorf("\"%s\" has unsaved changes, :e! drops them", b.file.path)
	}
	return e.revert(b)
}

// writeTo writes b's text to path, leaving b on its own file, as :w path
// does. It refuses to write over a file that exists unless force (B155).
func (e *Session) writeTo(b *buffer, path string, force bool) error {
	_, err := os.Stat(path)
	if err == nil && !force {
		return fmt.Errorf("\"%s\" exists, :w! writes over it", path)
	}
	kind := fileEdited
	if err != nil {
		kind = fileCreated
	}
	f := b.file
	f.path = path
	t := b.text.Snapshot()
	n, err := f.write(t.Slice(0, t.Len()))
	if err != nil {
		return err
	}
	e.filesChanged(fileChange{path, kind})
	e.message = fmt.Sprintf("\"%s\" %dB written", path, n)
	return nil
}

// rename makes path b's file, which b is not yet written to, as :file
// path does, with the filetype and language servers of the new name.
// Writing over a file already there asks first (B426).
func (e *Session) rename(b *buffer, path string) error {
	// The servers hear of the close under the old name.
	e.detach(b)
	old := b.file.path
	b.file.path = path
	_, err := os.Stat(path)
	b.file.isNew = err != nil
	e.emit("buffer.renamed", old, b)
	return e.retype(b)
}

// saveAs makes path the current buffer's file and writes it there, as
// :saveas does. It refuses a file that exists unless force (B427).
func (e *Session) saveAs(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("\"%s\" exists, :saveas! writes over it", path)
	}
	if err := e.rename(e.win.buf, path); err != nil {
		return err
	}
	// The file there is written over without asking.
	e.win.buf.file.note()
	return e.write(e.win.buf, force, nil)
}

// fileInfo says what <C-g> says of the current buffer: its name, whether
// it is modified or new, how many lines it has and how far down the cursor
// is. A count gives the full path, and one over 1 the buffer's number too
// (B426).
func (e *Session) fileInfo(count int) string {
	b, t := e.win.buf, e.Text()
	name := b.name()
	if abs, err := filepath.Abs(name); err == nil && count > 0 && b.title == "" && b.file.path != "" {
		name = abs
	}
	var s strings.Builder
	if count > 1 {
		fmt.Fprintf(&s, "buf %d: ", b.id)
	}
	s.WriteString("\"" + name + "\" ")
	flags := ""
	if b.modified() {
		flags += "[Modified]"
	}
	if b.file.isNew && b.file.path != "" {
		flags += "[New]"
	}
	if b.file.readonly {
		flags += "[readonly]"
	}
	if flags != "" {
		s.WriteString(flags + " ")
	}
	switch n := t.Lines(); {
	case t.Len() == 0:
		s.WriteString("--No lines in buffer--")
	case n == 1:
		s.WriteString("1 line --100%--")
	default:
		fmt.Fprintf(&s, "%d lines --%d%%--", n, (t.LineOf(e.Cursor())+1)*100/n)
	}
	return s.String()
}

// cd makes dir the working directory, or the home directory for "". The
// buffers' paths become absolute first, so they keep naming their files
// (B155).
func (e *Session) cd(dir string) error {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = home
	}
	for _, b := range e.bufs {
		if b.file.path == "" || filepath.IsAbs(b.file.path) {
			continue
		}
		abs, err := filepath.Abs(b.file.path)
		if err != nil {
			return err
		}
		b.file.path = abs
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("cd %s: %w", dir, err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	e.message, e.wd = wd, wd
	e.emit("cwd.changed", wd, e.win.buf)
	return nil
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
		case ch == '~' && (i == 0 || arg[i-1] == ' ') && (i+1 == len(arg) || arg[i+1] == '/' || arg[i+1] == filepath.Separator || arg[i+1] == ' '):
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

// write writes b to its file, then runs then. Without force it asks first
// when the file changed on disk since fude read it.
func (e *Session) write(b *buffer, force bool, then func()) error {
	if _, ok := e.writers[b.kind()]; ok || b.listed != "" {
		e.writeKind(b, then)
		return nil
	}
	switch {
	case b.file.path == "":
		return errors.New("no file name")
	case b.file.readonly:
		return fmt.Errorf("\"%s\" is read-only", b.file.path)
	case !force && b.file.changed():
		e.confirm(fmt.Sprintf("\"%s\" changed on disk since fude read it, write anyway?", b.file.path), func(yes bool) {
			if yes {
				e.report(e.write(b, true, then))
			}
		})
		return nil
	}
	// The modules that hear buffer.saving change the text first (B282).
	e.saving(b, func() { e.writeOut(b, then) })
	return nil
}

// writeOut writes b's text to its file, then runs then.
func (e *Session) writeOut(b *buffer, then func()) {
	s, at := b.text.Snapshot(), time.Now()
	text := s.Slice(0, s.Len())
	n, err := b.file.write(text)
	trace.Since("file", fmt.Sprintf("wrote %q, %d bytes", b.file.path, n), at, err)
	if err != nil {
		e.message = err.Error()
		return
	}
	tag, kind := "", fileEdited
	if b.file.isNew {
		tag, kind = "[New] ", fileCreated
	}
	e.filesChanged(fileChange{b.file.path, kind})
	b.file.isNew = false
	b.markSaved(text)
	for _, srv := range b.servers {
		srv.client.Save(lsp.URI(b.file.path), s)
	}
	e.emit("buffer.saved", "", b)
	e.message = fmt.Sprintf("\"%s\" %s%dB written", b.file.path, tag, n)
	if then != nil {
		then()
	}
}

// writeAll writes each modified buffer in bs, then runs then. It stops at
// the first buffer that fails to write.
func (e *Session) writeAll(bs []*buffer, force bool, then func()) error {
	for i, b := range bs {
		if b.modified() {
			return e.write(b, force, func() { e.report(e.writeAll(bs[i+1:], force, then)) })
		}
	}
	if then != nil {
		then()
	}
	return nil
}

// Focus reloads each unmodified buffer whose file changed on disk, as one
// undo step. A modified one keeps its text, and fude warns once.
func (e *Session) Focus() {
	e.emit("editor.focused", "", e.win.buf)
	e.recheck()
	for _, b := range e.bufs {
		// A directory may have changed while fude was away.
		if b.listed != "" {
			e.emit("explorer.read", "", b)
		}
		if b.title != "" || b.file.warned || !b.file.changed() {
			continue
		}
		e.filesChanged(fileChange{b.file.path, fileEdited})
		if b.modified() {
			e.message = fmt.Sprintf("\"%s\" changed on disk and in fude, keeping fude's text", b.file.path)
			b.file.warned = true
			continue
		}
		if err := e.revert(b); err != nil {
			e.message = err.Error()
			continue
		}
		e.message = fmt.Sprintf("\"%s\" changed on disk, reloaded", b.file.path)
	}
	e.reveal()
}
