package editor

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
)

// blamed is what git blame said of a buffer's line, for the text at a
// version (B220).
type blamed struct {
	version kernel.Version
	line    int
	text    string
	busy    bool
}

// blameNote returns the note :blame shows after the cursor's line of w's
// buffer: who last changed it, how long ago and the commit's summary. It
// asks git off the loop when the line or the text changed, and returns ""
// until git answers.
func (e *Session) blameNote(w *window) string {
	b := w.buf
	if b.base == nil {
		return ""
	}
	t := b.text.Snapshot()
	line, bl := t.LineOf(w.pos()), &b.blamed
	if bl.version == t.Version() && bl.line == line {
		return bl.text
	}
	if !bl.busy {
		bl.busy = true
		path, text := b.file.path, t.Slice(0, t.Len())
		go func() {
			note, err := blameLine(path, text, line)
			if err != nil {
				logLine(err.Error())
			}
			e.queue <- func() { *bl = blamed{t.Version(), line, note, false} }
		}()
	}
	return ""
}

// blameLine runs git blame on line, from 0, of path with text as its
// contents, so lines changed since the last write still line up.
func blameLine(path, text string, line int) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("blame %s: %w", path, err)
	}
	n := strconv.Itoa(line + 1)
	cmd := exec.Command("git", "-C", filepath.Dir(abs), "blame", "--porcelain", "-L", n+","+n, "--contents", "-", "--", filepath.Base(abs))
	cmd.Stdin = strings.NewReader(text + "\n")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("blame %s: %w", path, err)
	}
	var author, summary string
	var when time.Time
	for in := bufio.NewScanner(strings.NewReader(string(out))); in.Scan(); {
		key, value, _ := strings.Cut(in.Text(), " ")
		switch key {
		case "author":
			author = value
		case "author-time":
			if s, err := strconv.ParseInt(value, 10, 64); err == nil {
				when = time.Unix(s, 0)
			}
		case "summary":
			summary = value
		}
	}
	if author == "Not Committed Yet" || author == "External file (--contents)" {
		return "not committed yet", nil
	}
	return fmt.Sprintf("%s, %s · %s", author, ago(time.Since(when)), summary), nil
}

// ago says d in its largest whole unit, as "3 days ago".
func ago(d time.Duration) string {
	for _, u := range []struct {
		name string
		size time.Duration
	}{{"year", 365 * 24 * time.Hour}, {"month", 30 * 24 * time.Hour}, {"day", 24 * time.Hour}, {"hour", time.Hour}, {"minute", time.Minute}} {
		if n := int(d / u.size); n > 0 {
			if n == 1 {
				return "1 " + u.name + " ago"
			}
			return strconv.Itoa(n) + " " + u.name + "s ago"
		}
	}
	return "just now"
}
