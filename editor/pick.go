package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pickSources are the sources :pick finds in, by name.
var pickSources = []string{"files", "grep", "buffers", "recent"}

// openPick opens the finder on the items of source. grep takes the text
// after it as its query (B213).
func (e *Session) openPick(arg string) {
	source, rest, _ := strings.Cut(arg, " ")
	switch source {
	case "files":
		e.files()
	case "grep":
		e.grepFiles(strings.TrimSpace(rest))
	case "buffers":
		var paths []string
		for _, b := range e.bufs {
			if b.file.path != "" && b != e.win.buf && b.kind() == "" {
				paths = append(paths, b.file.path)
			}
		}
		e.openFinder(&finder{paths: paths, stale: true, title: "buffers"}, "")
	case "recent":
		f, current := &finder{title: "recent files"}, e.win.buf.file.path
		e.openFinder(f, "")
		go func() {
			paths, err := recentFiles(current)
			e.queue <- func() {
				if err != nil {
					e.message = err.Error()
				}
				f.paths, f.stale = paths, true
			}
		}()
	default:
		e.message = fmt.Sprintf("no source named %q, only %s", source, strings.Join(pickSources, ", "))
	}
}

// recentFiles returns the recent files that still exist, but current, each
// relative to the working directory when it is under it.
func recentFiles(current string) ([]string, error) {
	paths, err := readRecent()
	if err != nil {
		return nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	cur, _ := filepath.Abs(current)
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil || p == cur {
			continue
		}
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = rel
		}
		out = append(out, p)
	}
	return out, nil
}
