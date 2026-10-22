package editor

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/fsys"
)

// file is a path and the on-disk format of its text, which a write keeps.
type file struct {
	path     string
	crlf     bool // every line ended in \r\n
	eol      bool // the text ended in a newline
	isNew    bool
	readonly bool // not valid UTF-8
	image    bool // a PNG, JPEG or GIF, whose text is a line naming it (B304)

	// The file's modification time and size when fude last read or wrote it.
	mod    time.Time
	size   int64
	warned bool // fude warned that the file changed on disk since then
}

// read returns the file at path and its text. The text has \n line endings
// and no final newline.
func read(path string) (file, string, error) {
	f := file{path: path, eol: true}
	text, err := slurp(path)
	if errors.Is(err, fs.ErrNotExist) {
		f.isNew = true
		return f, "", nil
	}
	if err != nil {
		return f, "", fmt.Errorf("open %s: %w", path, err)
	}
	f.note()
	if !utf8.ValidString(text) {
		f.readonly = true
		// An image file reads as a line that names it, and shows the image
		// below (B304).
		if cfg, format, err := image.DecodeConfig(strings.NewReader(text)); err == nil {
			f.image = true
			text = fmt.Sprintf("%s, %d by %d pixels", strings.ToUpper(format), cfg.Width, cfg.Height)
		}
		return f, text, nil
	}
	if n := strings.Count(text, "\n"); n > 0 && strings.Count(text, "\r\n") == n {
		f.crlf = true
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	if text != "" {
		f.eol = strings.HasSuffix(text, "\n")
		text = strings.TrimSuffix(text, "\n")
	}
	return f, text, nil
}

// slurp reads the file at path into a string without copying it.
func slurp(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	if fi, err := f.Stat(); err == nil {
		b.Grow(int(fi.Size()))
	}
	_, err = io.Copy(&b, f)
	return b.String(), err
}

// write stores t at the file's path through a temp file and a rename, so a
// failed write leaves the old file unchanged. It returns the bytes written.
func (f *file) write(t kernel.Snapshot) (int, error) {
	path := f.path
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".fude-*")
	if _, serr := os.Stat(filepath.Dir(path)); err != nil && errors.Is(serr, fs.ErrNotExist) {
		return 0, fmt.Errorf("write %s: the folder %s does not exist", f.path, filepath.Dir(path))
	}
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", f.path, err)
	}
	defer os.Remove(tmp.Name())
	// It writes a chunk at a time, since the whole text would be a copy of
	// the file.
	const chunk = 1 << 20
	n, buf := 0, make([]byte, 0, chunk+1)
	for at := 0; ; at += chunk {
		end := min(at+chunk, t.Len())
		buf = t.AppendSlice(buf[:0], at, end)
		if end == t.Len() && f.eol {
			buf = append(buf, '\n')
		}
		out := buf
		if f.crlf {
			out = bytes.ReplaceAll(buf, []byte("\n"), []byte("\r\n"))
		}
		var w int
		w, err = tmp.Write(out)
		n += w
		if err != nil || end == t.Len() {
			break
		}
	}
	err = errors.Join(err, tmp.Chmod(mode), tmp.Sync(), tmp.Close())
	if err == nil {
		err = fsys.Replace(tmp.Name(), path)
	}
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", f.path, err)
	}
	f.note()
	return n, nil
}

// note records the file's modification time and size on disk.
func (f *file) note() {
	if fi, err := os.Stat(f.path); err == nil {
		f.mod, f.size, f.warned = fi.ModTime(), fi.Size(), false
	}
}

// changed reports whether the file on disk changed since fude last read or
// wrote it.
func (f *file) changed() bool {
	fi, err := os.Stat(f.path)
	return f.path != "" && err == nil && (!fi.ModTime().Equal(f.mod) || fi.Size() != f.size)
}

// under returns path from dir, and whether path is inside dir.
func under(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}
