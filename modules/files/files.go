// Package files is :pick files, the working directory's files read off the
// loop, and :pick buffers, the open buffers that have a file (B129, B213).
package files

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// What files registers, by the ids Call runs them by, and the picks its
// lists show in. A walk's job has an id from walks up.
const (
	filesSrc uint32 = iota + 1
	buffersSrc
	walks uint32 = 100
)

// Module is files. It keeps the job of the newest walk asked for, and the
// files git tracks once that walk has listed them.
type Module struct {
	mu      sync.Mutex
	newest  uint32
	tracked []string
}

// New returns files.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "files" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Source, Name: "files", Help: "the working directory's files, those git does not ignore in a repository", ID: filesSrc},
		{Kind: api.Source, Name: "buffers", Help: "the open buffers that have a file", ID: buffersSrc},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call opens the finder on the working directory's files, which a job lists,
// or on the open buffers but the current one.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == buffersSrc {
		var paths []string
		for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
			// An explorer's path is its directory.
			if p := s.Buffers[id].Path; p != "" && strconv.Itoa(id) != s.About["buffer"] {
				if info, err := os.Stat(p); err != nil || !info.IsDir() {
					paths = append(paths, p)
				}
			}
		}
		return api.Result{Pick: &api.Pick{ID: buffersSrc, Title: "buffers", Items: paths, Files: true}}, nil
	}
	res := m.list(false)
	res.Pick = &api.Pick{ID: filesSrc, Title: "files", Files: true}
	return res, nil
}

// list starts a job that lists the files, the ignored ones too with all.
// Only the newest walk fills the finder.
func (m *Module) list(all bool) api.Result {
	m.newest, m.tracked = max(m.newest+1, walks), nil
	return api.Result{Jobs: []api.Job{{ID: m.newest, Argv: []string{strconv.FormatBool(all)}, Stream: true}}}
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback lists the files again when <C-g> turns the ignored ones on or
// off, and fills the finder with the newest walk.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == filesSrc {
		if all, ok := s.About["all"]; ok {
			return m.list(all == "true"), nil
		}
		return api.Result{}, nil
	}
	if id != m.newest {
		return api.Result{}, nil
	}
	if err := s.About["error"]; err != "" {
		return api.Result{Error: err}, nil
	}
	var paths []string
	if out := strings.TrimSuffix(s.About["output"], "\x00"); out != "" {
		paths = strings.Split(out, "\x00")
	}
	// The tracked files come first, emitted. The job's end adds the
	// untracked ones before them, in git's order.
	if s.About["done"] != "true" {
		m.tracked = paths
	} else {
		paths = append(paths, m.tracked...)
	}
	return api.Result{Pick: &api.Pick{ID: filesSrc, Items: paths, Files: true, Update: true}}, nil
}

// Work lists the files below the working directory. In a repository it
// emits those git tracks, then returns the untracked ones git does not
// ignore, which git finds by walking every folder. Elsewhere, or with all,
// it returns every file outside .git and unreadable directories.
func (m *Module) Work(_ context.Context, j api.Job) (string, error) {
	if j.Argv[0] != "true" {
		// Outside a repository, or without git, git fails and the walk lists
		// the files.
		// git writes / between folders on Windows too.
		if out, err := exec.Command("git", "ls-files", "-z", "--cached").Output(); err == nil {
			j.Emit(filepath.FromSlash(string(out)))
			out, err := exec.Command("git", "ls-files", "-z", "--others", "--exclude-standard").Output()
			if err != nil {
				return "", fmt.Errorf("git ls-files --others: %w", err)
			}
			return filepath.FromSlash(string(out)), nil
		}
	}
	var paths []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil && path == ".":
			return err
		case err != nil || d.IsDir() && d.Name() == ".git":
			return fs.SkipDir
		case !d.IsDir():
			paths = append(paths, path)
		}
		return nil
	})
	return strings.Join(paths, "\x00"), err
}

func (m *Module) Close(context.Context) error { return nil }
