package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// projectPath returns where the session of the working directory is kept
// (B226).
func projectPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	state, err := xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	sum := sha256.Sum256([]byte(wd))
	return filepath.Join(state, "fude", "sessions", hex.EncodeToString(sum[:16])+".json"), nil
}

// saveProject keeps the windows on files, their cursors and options, for
// the start screen to restore in this directory. With no window on a file,
// it keeps the last session (B226).
func (e *Session) saveProject() error {
	s := e.session()
	if len(s.Buffers) == 0 {
		return nil
	}
	path, err := projectPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := os.WriteFile(path+".new", data, 0o600); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// hasProject reports whether this directory has a session kept.
func hasProject() bool {
	path, err := projectPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// restoreProject opens the files of this directory's session and builds its
// windows, each with its cursor and options. A file that no longer opens is
// left out, and the message says why.
func (e *Session) restoreProject() {
	path, err := projectPath()
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
			if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
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
