package editor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// grepLimit is the most lines a search shows.
const grepLimit = 1000

// grepFiles opens the finder as a live grep with query typed (B134).
func (e *Session) grepFiles(query string) {
	e.openFinder(&finder{grep: true, hint: "type to search the files' lines"}, query)
}

// grep returns the lines of the working directory's files that hold query,
// as path:line:column:text, at most grepLimit of them. It runs rg when it
// is installed, and git grep in a repository otherwise. A query without
// capitals ignores case. With all, it searches the files git ignores and
// hidden ones too.
func grep(query string, all bool) ([]string, error) {
	if query == "" {
		return nil, nil
	}
	var cmd *exec.Cmd
	if _, err := exec.LookPath("rg"); err == nil {
		args := []string{"--line-number", "--column", "--no-heading", "--color=never", "--smart-case", "--fixed-strings"}
		if all {
			args = append(args, "--no-ignore", "--hidden", "--glob=!.git")
		}
		cmd = exec.Command("rg", append(args, "--", query)...)
	} else if hasGit() {
		ignores := "--exclude-standard"
		if all {
			ignores = "--no-exclude-standard"
		}
		args := []string{"grep", "-n", "--column", "-I", "--untracked", ignores, "-F"}
		if strings.ToLower(query) == query {
			args = append(args, "-i")
		}
		cmd = exec.Command("git", append(args, "-e", query)...)
	} else {
		return nil, errors.New("grep needs rg or git")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmd.Path, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", cmd.Path, err)
	}
	var lines []string
	in := bufio.NewReaderSize(out, 64<<10)
	for len(lines) < grepLimit && err == nil {
		var line []byte
		var more bool
		if line, more, err = in.ReadLine(); err != nil {
			break
		}
		lines = append(lines, string(line))
		// A line longer than the reader's buffer shows its start.
		for more && err == nil {
			_, more, err = in.ReadLine()
		}
	}
	if len(lines) == grepLimit || err != nil && !errors.Is(err, io.EOF) {
		// The rest are not shown, or cannot be read, so the search stops.
		cmd.Process.Kill()
		cmd.Wait()
		if len(lines) < grepLimit {
			return nil, fmt.Errorf("%s: %w", cmd.Path, err)
		}
		return lines, nil
	}
	// Both exit with 1 when nothing matches.
	if err := cmd.Wait(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("%s: %w", cmd.Path, err)
		}
	}
	return lines, nil
}

// grepSpot splits a grep match into its path, line and column, from 1,
// and text. A file finder's match is a path, with line and column 0.
func grepSpot(match string) (string, int, int, string) {
	parts := strings.SplitN(match, ":", 4)
	if len(parts) < 4 {
		return match, 0, 0, ""
	}
	line, err1 := strconv.Atoi(parts[1])
	col, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return match, 0, 0, ""
	}
	return parts[0], line, col, parts[3]
}
