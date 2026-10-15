package editor

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// hasGit reports whether git is installed. Without it, files show no git
// signs.
func hasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// The signs of git changes, as gitsigns shows them, by kind.
var (
	gitAdd          = Sign{"┃", "git.add"}
	gitChange       = Sign{"┃", "git.change"}
	gitDelete       = Sign{"▁", "git.delete"} // under the line before deleted ones
	gitTopDelete    = Sign{"▔", "git.delete"} // over the first line when the first lines went
	gitChangeDelete = Sign{"~", "git.change"} // a change that also deleted lines
)

// The signs of a listing entry's git status, by status code (B126).
var statusSigns = map[string]Sign{
	" M": {"✹", "git.change"}, "M ": {"•", "git.change"}, "MM": {"≠", "git.change"}, "R ": {"→", "git.change"},
	"A ": {"+", "git.add"}, "??": {"?", "git.delete"}, "D ": {"-", "git.delete"},
}

// gitStatus returns the git status sign of each name in dir, or false
// outside a repository. A directory takes the status of the first change
// below it, and a name whose status has no sign maps to the zero Sign.
func gitStatus(dir string) (map[string]Sign, bool) {
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
	signs, pre := map[string]Sign{}, strings.TrimSuffix(string(prefix), "\n")
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

// gitBase reads path as git's index has it, the text gitsigns compares
// with, or false when the file is not in a repository's index.
func gitBase(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	out, err := exec.Command("git", "-C", filepath.Dir(abs), "show", ":./"+filepath.Base(abs)).Output()
	if err != nil {
		return "", false
	}
	// A buffer holds no final line break, as it reads files (N5).
	return strings.TrimSuffix(string(out), "\n"), true
}

// loadGit reads b's base from git off the editor loop, then diffs it
// (B123), or a listing's git status (B126).
func (ed *Editor) loadGit(b *buffer) {
	if b.listed != "" && hasGit() {
		dir, had := b.listed, b.status != nil
		go func() {
			if signs, ok := gitStatus(dir); ok || had {
				ed.queue <- func() { b.status = signs }
			}
		}()
		return
	}
	if b.file.path == "" || b.title != "" || !hasGit() {
		return
	}
	path, had := b.file.path, b.base != nil
	go func() {
		base, ok := gitBase(path)
		if !ok && !had {
			// Nothing to show or to clear, so the loop need not hear of it.
			return
		}
		ed.queue <- func() {
			if !ok {
				b.base, b.git = nil, nil
				return
			}
			b.base, b.gitStale = &base, true
		}
	}()
}

// diffGit diffs b's text with its base off the editor loop, when the text
// changed since the last diff. One diff runs at a time, and a change made
// while it runs starts another after it.
func (ed *Editor) diffGit(b *buffer) {
	if b.base == nil || !b.gitStale || b.gitBusy {
		return
	}
	b.gitStale, b.gitBusy = false, true
	base, t := *b.base, b.text.Snapshot()
	go func() {
		signs := gitSigns(base, t.Slice(0, t.Len()))
		ed.queue <- func() {
			b.git, b.gitBusy = signs, false
		}
	}()
}

// gitSigns returns the sign of each line of now that differs from base.
func gitSigns(base, now string) map[int]Sign {
	ops := lineDiff(strings.Split(base, "\n"), strings.Split(now, "\n"))
	out := map[int]Sign{}
	line := 0 // the line of now the next op is at
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			line, i = line+1, i+1
			continue
		}
		del, add := 0, 0
		for ; i < len(ops) && ops[i].kind != ' '; i++ {
			if ops[i].kind == '-' {
				del++
			} else {
				add++
			}
		}
		for j := range add {
			out[line+j] = gitAdd
			if j < del {
				out[line+j] = gitChange
			}
		}
		switch {
		case add == 0 && line == 0:
			out[0] = gitTopDelete
		case add == 0:
			out[line-1] = gitDelete
		case del > add:
			out[line+add-1] = gitChangeDelete
		}
		line += add
	}
	return out
}
