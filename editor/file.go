package editor

import (
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

// write stores text at the file's path through a temp file and a rename, so
// a failed write leaves the old file unchanged. It returns the bytes written.
func (f *file) write(text string) (int, error) {
	// The final line break goes in a write of its own, as adding it would
	// copy the text.
	eol := ""
	if f.eol {
		eol = "\n"
	}
	if f.crlf {
		eol = strings.ReplaceAll(eol, "\n", "\r\n")
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	path := f.path
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".fude-*")
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", f.path, err)
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(text)
	if err == nil {
		_, err = tmp.WriteString(eol)
	}
	err = errors.Join(err, tmp.Chmod(mode), tmp.Sync(), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", f.path, err)
	}
	f.note()
	return len(text) + len(eol), nil
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
