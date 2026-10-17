package editor

import (
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/y3owk1n/fude/wire"
)

// hasGit reports whether git is installed. Without it, files show no git
// signs.
func hasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// The signs of a listing entry's git status, by status code (B126).
var statusSigns = map[string]wire.Sign{
	" M": {Text: "✹", Name: "git.change"}, "M ": {Text: "•", Name: "git.change"}, "MM": {Text: "≠", Name: "git.change"}, "R ": {Text: "→", Name: "git.change"},
	"A ": {Text: "+", Name: "git.add"}, "??": {Text: "?", Name: "git.delete"}, "D ": {Text: "-", Name: "git.delete"},
}

// gitStatus returns the git status sign of each name in dir, or false
// outside a repository. A directory takes the status of the first change
// below it, and a name whose status has no sign maps to the zero Sign.
func gitStatus(dir string) (map[string]wire.Sign, bool) {
	// git names paths from the repository's root, so each starts with dir's
	// prefix.
	prefix, err := exec.Command("git", "-C", dir, "rev-parse", "--show-prefix").Output()
	if err != nil {
		return nil, false
	}
	out, err := exec.Command("git", "-C", dir, "--no-optional-locks", "status", "--porcelain", "-z", "--", ".").Output()
	if err != nil {
		return nil, false
	}
	signs, pre := map[string]wire.Sign{}, strings.TrimSuffix(string(prefix), "\n")
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		if len(fields[i]) < 4 {
			continue
		}
		code, path := fields[i][:2], fields[i][3:]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // the old name
		}
		name := strings.TrimPrefix(path, pre)
		if top, _, below := strings.Cut(name, "/"); below {
			name = top + "/"
		}
		if _, seen := signs[name]; !seen {
			signs[name] = statusSigns[code]
		}
	}
	return signs, true
}

// gitBranch returns the branch checked out in dir's repository, or "" for
// none, outside one or on a detached head (B147).
func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// loadGit reads a listing's git status off the editor loop (B126), or a
// file's branch (B147).
func (ed *Editor) loadGit(b *buffer) {
	if b.listed != "" && hasGit() {
		dir, had := b.listed, b.status != nil || b.branch != ""
		go func() {
			at := time.Now()
			signs, ok := gitStatus(dir)
			branch := gitBranch(dir)
			traced("file", "read the git status of "+dir, at, nil)
			if ok || had {
				ed.queue <- func() { b.status, b.branch = signs, branch }
			}
		}()
		return
	}
	if b.file.path == "" || b.title != "" || !hasGit() {
		return
	}
	path, had := b.file.path, b.branch != ""
	go func() {
		at := time.Now()
		branch := gitBranch(filepath.Dir(path))
		traced("file", "read the git branch of "+path, at, nil)
		if branch == "" && !had {
			// Nothing to show or to clear, so the loop need not hear of it.
			return
		}
		ed.queue <- func() { b.branch = branch }
	}()
}
