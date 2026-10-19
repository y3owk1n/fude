// Package keep is what fude keeps between runs, under the XDG directories:
// where they are, writing a file whole, the recent files and where a
// session and the pins are kept.
package keep

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Clean is a directory read as the config directory instead of the user's,
// while fude runs with --clean, or "" (B270).
var Clean string

// Dir returns the directory the XDG variable env names, or fallback under
// the home directory. With Clean set, the config directory is Clean.
func Dir(env, fallback string) (string, error) {
	if env == "XDG_CONFIG_HOME" && Clean != "" {
		return Clean, nil
	}
	if dir := os.Getenv(env); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}

// Write replaces the file at path with data, whole, so a reader never
// sees half of it. It makes the directories on the way.
func Write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path+".new", data, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

// RecentCap is how many files Recent remembers (B213).
const RecentCap = 100

// Recent is the file of the files shown last, newest first. One goroutine
// writes it, so notes keep their order and stay off the caller's loop.
type Recent struct {
	path  func() (string, error) // where the file is, asked at each read and write
	log   func(string)           // takes a write that failed
	notes chan note
	once  sync.Once
}

// note is a file shown. With done set, it asks for done to close once the
// notes before it are written.
type note struct {
	path string
	done chan<- struct{}
}

// NewRecent returns the recent files kept at the file path returns, telling
// log of a write that fails.
func NewRecent(path func() (string, error), log func(string)) *Recent {
	return &Recent{path: path, log: log, notes: make(chan note, 64)}
}

// Note records that a window shows the file at path. It drops the note
// when the queue is full, as a burst of window switches notes the same
// files again.
func (r *Recent) Note(path string) {
	if path == "" {
		return
	}
	r.once.Do(func() { go r.write() })
	select {
	case r.notes <- note{path: path}:
	default:
	}
}

// Flush returns once every file noted before it is in the file, so
// quitting keeps the last ones.
func (r *Recent) Flush() {
	r.once.Do(func() { go r.write() })
	done := make(chan struct{})
	r.notes <- note{done: done}
	<-done
}

// write puts each noted file first in the file, which it replaces whole.
func (r *Recent) write() {
	paths, err := r.Read()
	if err != nil {
		r.log(err.Error())
	}
	for n := range r.notes {
		if n.done != nil {
			close(n.done)
			continue
		}
		abs, err := filepath.Abs(n.path)
		if err != nil {
			r.log(fmt.Sprintf("recent files: %v", err))
			continue
		}
		paths = slices.Insert(slices.DeleteFunc(paths, func(p string) bool { return p == abs }), 0, abs)
		paths = paths[:min(len(paths), RecentCap)]
		if err := r.save(paths); err != nil {
			r.log(err.Error())
		}
	}
}

// save replaces the file with paths.
func (r *Recent) save(paths []string) error {
	file, err := r.path()
	if err == nil {
		err = Write(file, []byte(strings.Join(paths, "\n")+"\n"))
	}
	if err != nil {
		return fmt.Errorf("recent files: %w", err)
	}
	return nil
}

// Read returns the recent files, newest first.
func (r *Recent) Read() ([]string, error) {
	file, err := r.path()
	if err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	f, err := os.Open(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	defer f.Close()
	var paths []string
	in := bufio.NewScanner(f)
	for in.Scan() {
		if line := in.Text(); line != "" {
			paths = append(paths, line)
		}
	}
	if err := in.Err(); err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	return paths, nil
}

// SessionPath returns where the session of the working directory is kept
// (B226).
func SessionPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	path, err := byDir("sessions", wd)
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	return path, nil
}

// PinsPath returns where the pins of the working directory wd are kept
// (B356).
func PinsPath(wd string) (string, error) { return byDir("pins", wd) }

// byDir returns the file of dir among what fude keeps in the state
// directory's folder kind, named for a hash of dir.
func byDir(kind, dir string) (string, error) {
	state, err := Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(state, "fude", kind, hex.EncodeToString(sum[:16])+".json"), nil
}

// RecentPath is where the recent files are kept (B213).
func RecentPath() (string, error) {
	dir, err := Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fude", "recent"), nil
}

// Files returns the recent files that still exist, but current, newest
// first: those under dir, relative to it, and the others (B213).
func (r *Recent) Files(current, dir string) (here, elsewhere []string, err error) {
	paths, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	cur, _ := filepath.Abs(current)
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil || p == cur {
			continue
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			elsewhere = append(elsewhere, p)
			continue
		}
		here = append(here, rel)
	}
	return here, elsewhere, nil
}
