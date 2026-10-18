package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/y3owk1n/fude/internal/keep"
)

// saveProject keeps the windows on files, their cursors and options, for
// the start screen to restore in this directory. With no window on a file,
// it keeps the last session (B226).
func (e *Session) saveProject() error {
	s := e.session()
	if len(s.Buffers) == 0 {
		return nil
	}
	path, err := keep.SessionPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := keep.Write(path, data); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// restoreProject opens the files of this directory's session and builds its
// windows, each with its cursor and options. A file that no longer opens is
// left out, and the message says why.
func (e *Session) restoreProject() {
	path, err := keep.SessionPath()
	if err != nil {
		e.message = err.Error()
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		e.message = "no session here"
		return
	}
	if err != nil {
		e.message = fmt.Sprintf("session: %v", err)
		return
	}
	var s saved
	if err := json.Unmarshal(data, &s); err != nil {
		e.message = fmt.Sprintf("session: %v", err)
		return
	}
	var problems []error
	wd, _ := os.Getwd()
	for _, sb := range s.Buffers {
		if !slices.ContainsFunc(e.bufs, func(b *buffer) bool { return b.is(sb.Path) }) {
			// A file under the working directory opens by its relative path,
			// as typed.
			path := sb.Path
			if rel, ok := under(wd, path); ok {
				path = rel
			}
			if _, err := e.add(path); err != nil {
				problems = append(problems, err)
			}
		}
	}
	e.restoreLayout(s)
	if err := errors.Join(problems...); err != nil {
		e.message = err.Error()
	}
}
