package editor

import (
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// readIn puts a file's text below the line at last, as :r does. With !cmd
// the shell module's :! puts what cmd writes there (B156).
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
		shell, ok := e.commands["!"]
		if !ok {
			e.message = "not a command: !"
			return
		}
		shell(e, call{name: "!", arg: cmd, force: true, first: t.LineStart(t.LineOf(last)), last: last})
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
