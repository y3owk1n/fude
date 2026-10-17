package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// runShell runs cmd in the user's shell with stdin, and returns what it
// writes, or an error with what it said on stderr.
func runShell(cmd, stdin string) (string, error) {
	c := exec.Command(cmp.Or(os.Getenv("SHELL"), "sh"), "-c", cmd)
	c.Stdin = strings.NewReader(stdin)
	var errOut bytes.Buffer
	c.Stderr = &errOut
	at := time.Now()
	out, err := c.Output()
	traced("file", "ran "+cmd, at, err)
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(errOut.String()))
	}
	return string(out), nil
}

// shell runs cmd off the loop, as :! does (B156). With a range it filters
// those lines through cmd, and the output replaces them unless the text
// changed meanwhile. Without one it shows what cmd writes, in a window
// when it is long.
func (e *Session) shell(cmd string, first, last int, ranged bool) {
	if strings.TrimSpace(cmd) == "" {
		e.message = "no command"
		return
	}
	b, t := e.win.buf, e.win.buf.text.Snapshot()
	end := vim.LineEnd(t, last)
	in := ""
	if ranged {
		in = t.Slice(first, end) + "\n"
	}
	go func() {
		out, err := runShell(cmd, in)
		e.queue <- func() {
			switch {
			case err != nil:
				e.message = err.Error()
			case ranged:
				e.reformat(b, t, []kernel.Edit{{Start: first, End: end, Text: strings.TrimSuffix(out, "\n")}}, ":!")
			case strings.Count(out, "\n") > 10:
				e.scratch("[!"+cmd+"]", strings.TrimSuffix(out, "\n"), b, nil)
			default:
				e.message = strings.TrimSuffix(out, "\n")
			}
		}
	}()
}

// readIn puts a file's text, or with !cmd what cmd writes, below the line
// at last, as :r does (B156).
func (e *Session) readIn(arg string, last int) {
	b, t := e.win.buf, e.win.buf.text.Snapshot()
	at := vim.LineEnd(t, last)
	put := func(text string) {
		if text = strings.TrimSuffix(text, "\n"); text != "" {
			e.reformat(b, t, []kernel.Edit{{Start: at, End: at, Text: "\n" + text}}, ":r")
		}
	}
	cmd, run := strings.CutPrefix(arg, "!")
	switch {
	case arg == "":
		e.message = "no file name"
	case run:
		go func() {
			out, err := runShell(cmd, "")
			e.queue <- func() {
				if err != nil {
					e.message = err.Error()
					return
				}
				put(out)
			}
		}()
	default:
		text, err := os.ReadFile(arg)
		if err != nil {
			e.message = err.Error()
			return
		}
		put(string(text))
	}
}

// reformat applies edits for text t to b as one timeline node, and drops
// them when b has changed since t or has closed.
func (e *Session) reformat(b *buffer, t kernel.Snapshot, edits []kernel.Edit, label string) {
	if len(edits) == 0 || !slices.Contains(e.bufs, b) {
		return
	}
	if err := e.mayEdit(); err != nil {
		e.message = err.Error()
		return
	}
	if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: label, Time: e.now(), Edits: edits}); err != nil {
		if errors.Is(err, kernel.ErrStale) {
			err = errors.New("the text changed while it was formatted")
		}
		e.message = err.Error()
		return
	}
	b.head = b.text.Head()
	b.pushChange(edits[0].Start)
}
