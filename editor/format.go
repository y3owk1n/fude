package editor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
)

// formatTimeout is how long formatting before a write may take.
const formatTimeout = time.Second

// formatOnSave formats b, then runs write: through its filetype's format
// commands in turn, or else through its language server. A formatter that
// fails, or takes longer than formatTimeout, leaves the text as it is, and
// write runs anyway (B136).
func (e *Session) formatOnSave(b *buffer, write func()) {
	cmds, s := e.cfg.Filetype[e.filetype(b)].Format, b.server
	if len(cmds) == 0 && (s == nil || !s.client.Supports("documentFormattingProvider")) {
		write()
		return
	}
	t := b.text.Snapshot()
	waiting := true
	var timer *time.Timer
	finish := func(t kernel.Snapshot, edits []kernel.Edit, failed string) {
		if !waiting {
			return
		}
		waiting = false
		timer.Stop()
		e.reformat(b, t, edits, "format")
		write()
		if failed != "" {
			e.message = failed + ", written as it was"
		}
	}
	timer = time.AfterFunc(formatTimeout, func() {
		e.queue <- func() { finish(t, nil, "formatting took over a second") }
	})
	if len(cmds) > 0 {
		old, path := t.Slice(0, t.Len()), b.file.path
		go func() {
			// A buffer holds no last line break, and a formatter wants one.
			out, err := runFormatters(cmds, old+"\n", path)
			e.queue <- func() {
				if err != nil {
					finish(t, nil, err.Error())
					return
				}
				finish(t, lineEdits(old, strings.TrimSuffix(out, "\n")), "")
			}
		}()
		return
	}
	e.formatting(b, 0, t.Len(), func(t kernel.Snapshot, edits []kernel.Edit) { finish(t, edits, "") })
}

// runFormatters passes text through each command in turn, from its stdin to
// its stdout, in path's directory and with {path} in its arguments replaced
// by path. It stops them after formatTimeout.
func runFormatters(cmds [][]string, text, path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), formatTimeout)
	defer cancel()
	for _, c := range cmds {
		if len(c) == 0 {
			continue
		}
		args := make([]string, len(c)-1)
		for i, a := range c[1:] {
			args[i] = strings.ReplaceAll(a, "{path}", path)
		}
		cmd := exec.CommandContext(ctx, c[0], args...)
		cmd.Dir, cmd.Stdin = filepath.Dir(path), strings.NewReader(text)
		out, err := cmd.Output()
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && len(exit.Stderr) > 0 {
			return "", fmt.Errorf("%s: %s", c[0], strings.TrimSpace(string(exit.Stderr)))
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", c[0], err)
		}
		text = string(out)
	}
	return text, nil
}

// lineEdits returns the edits that turn old into new, one for each run of
// changed lines, so marks on the lines that stay keep their place.
func lineEdits(old, new string) []kernel.Edit {
	var edits []kernel.Edit
	pos := 0 // where the next op's line starts in old
	ops := lineDiff(strings.SplitAfter(old, "\n"), strings.SplitAfter(new, "\n"))
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			pos, i = pos+len(ops[i].line), i+1
			continue
		}
		e := kernel.Edit{Start: pos}
		for ; i < len(ops) && ops[i].kind != ' '; i++ {
			if ops[i].kind == '-' {
				pos += len(ops[i].line)
			} else {
				e.Text += ops[i].line
			}
		}
		e.End = pos
		edits = append(edits, e)
	}
	return edits
}
