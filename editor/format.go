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

// run is a formatter's command and the folder it runs in.
type run struct {
	name    string
	command []string
	dir     string
}

// formatters returns the formatters of b's filetype that apply to it, in
// order. One applies when its program is installed and any root it requires
// is found. With format_first, only the first that applies is returned.
// missing names those not installed (B136, B171).
func (e *Session) formatters(b *buffer) (runs []run, missing []string, err error) {
	ft := e.cfg.Filetype[e.filetype(b)]
	abs, err := filepath.Abs(b.file.path)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range ft.Format {
		f := e.cfg.Formatter[name]
		command, err := commandOf(name, f.Command)
		if err != nil {
			return nil, nil, fmt.Errorf("format with %s: %w", name, err)
		}
		if _, err := exec.LookPath(command[0]); err != nil {
			if !ft.FormatFirst {
				missing = append(missing, name)
			}
			continue
		}
		dir, found := rootOf(abs, f.Root)
		if !found && f.Workspace {
			continue
		}
		runs = append(runs, run{name, command, dir})
		if ft.FormatFirst {
			break
		}
	}
	return runs, missing, nil
}

// commandOf returns the command config.toml gives name, or the name alone,
// with a leading ~/ in each word made the home directory.
func commandOf(name string, command []string) ([]string, error) {
	if len(command) == 0 {
		return []string{name}, nil
	}
	out := make([]string, len(command))
	for i, word := range command {
		var err error
		if out[i], err = expandHome(word); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// formatOnSave formats b, then runs write: through its filetype's
// formatters in turn, or else through its language server when none
// applies. A formatter that fails, or takes longer than format_timeout,
// leaves the text as it is, and write runs anyway (B136, B171).
func (e *Session) formatOnSave(b *buffer, write func()) {
	runs, missing, err := e.formatters(b)
	if err != nil {
		e.message = err.Error()
		write()
		return
	}
	// The write's message names the formatters that are not installed.
	written := func() {
		write()
		if len(missing) > 0 {
			e.message += ", " + strings.Join(missing, ", ") + " not installed"
		}
	}
	s := formatter(b)
	if len(runs) == 0 && (s == nil || !s.client.Supports("documentFormattingProvider")) {
		written()
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
		written()
		if failed != "" {
			e.message = failed + ", written as it was"
		}
	}
	timeout := time.Duration(e.cfg.FormatTimeout) * time.Millisecond
	timer = time.AfterFunc(timeout, func() {
		e.queue <- func() { finish(t, nil, "formatting took over "+timeout.String()) }
	})
	if len(runs) > 0 {
		old, path := t.Slice(0, t.Len()), b.file.path
		go func() {
			// A buffer holds no last line break, and a formatter wants one.
			out, err := runFormatters(runs, old+"\n", path, timeout)
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

// runFormatters passes text through each formatter in turn, from its stdin
// to its stdout, in its folder and with {path} in its arguments replaced by
// path. It stops them after timeout.
func runFormatters(runs []run, text, path string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for _, r := range runs {
		args := make([]string, len(r.command)-1)
		for i, a := range r.command[1:] {
			args[i] = strings.ReplaceAll(a, "{path}", path)
		}
		cmd := exec.CommandContext(ctx, r.command[0], args...)
		cmd.Dir, cmd.Stdin = r.dir, strings.NewReader(text)
		at := time.Now()
		out, err := cmd.Output()
		traced("file", "ran formatter "+r.name+" on "+path, at, err)
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && len(exit.Stderr) > 0 {
			return "", fmt.Errorf("%s: %s", r.name, strings.TrimSpace(string(exit.Stderr)))
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", r.name, err)
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
