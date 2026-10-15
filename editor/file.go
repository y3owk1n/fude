package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// file is a path and the on-disk format of its text, which a write keeps.
type file struct {
	path     string
	crlf     bool // every line ended in \r\n
	eol      bool // the text ended in a newline
	isNew    bool
	readonly bool // not valid UTF-8
}

// read returns the file at path and its text. The text has \n line endings
// and no final newline.
func read(path string) (file, string, error) {
	f := file{path: path, eol: true}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		f.isNew = true
		return f, "", nil
	}
	if err != nil {
		return f, "", fmt.Errorf("open %s: %w", path, err)
	}
	text := string(data)
	if !utf8.ValidString(text) {
		f.readonly = true
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

// write stores text at the file's path through a temp file and a rename, so
// a failed write leaves the old file unchanged. It returns the bytes written.
func (f *file) write(text string) (int, error) {
	if f.eol {
		text += "\n"
	}
	if f.crlf {
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
	err = errors.Join(err, tmp.Chmod(mode), tmp.Sync(), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", f.path, err)
	}
	return len(text), nil
}
