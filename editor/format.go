package editor

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/vim"
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

// formatOnSave formats b, then runs write. A formatter that fails leaves
// the text as it is, and write runs anyway (B136, B171).
func (e *Session) formatOnSave(b *buffer, write func()) {
	runs, missing, err := e.formatters(b)
	// The write's message names the formatters that are not installed.
	written := func(failed string) {
		write()
		if failed != "" {
			e.message = failed + ", written as it was"
		} else if len(missing) > 0 {
			e.message += ", " + strings.Join(missing, ", ") + " not installed"
		}
	}
	switch {
	case err != nil:
		written(err.Error())
	case !e.formatWith(b, 0, b.text.Snapshot().Len(), runs, written):
		written("")
	}
}

// formatsOnSave reports whether a write formats b, as its filetype's
// format_on_save says or else the one at the top of config.toml (B136).
func (e *Session) formatsOnSave(b *buffer) bool {
	if on := e.cfg.Filetype[e.filetype(b)].FormatOnSave; on != nil {
		return *on
	}
	return e.cfg.FormatOnSave
}

// format formats b's lines from start to end as a write would, without
// writing them, for gq and :format (B65, B234).
func (e *Session) format(b *buffer, start, end int) {
	if b.file.readonly {
		e.message = "cannot make changes, the file is read-only"
		return
	}
	runs, missing, err := e.formatters(b)
	if err != nil {
		e.message = err.Error()
		return
	}
	said := func(failed string) {
		if failed != "" {
			e.message = failed
		} else if len(missing) > 0 {
			e.message = strings.Join(missing, ", ") + " not installed"
		}
	}
	if !e.formatWith(b, start, end, runs, said) {
		e.message = "nothing formats this file"
		said("")
	}
}

// formatWith formats b's lines from start to end through runs in turn, or
// else through its language server when runs is empty. A formatter sees the
// whole text, and only its edits inside the lines apply. The formatting is
// one timeline node. It then passes done why it failed, or "" when it did
// not. A formatter that fails, or takes longer than format_timeout, leaves
// the text as it is. It returns false, without calling done, when nothing
// formats b.
func (e *Session) formatWith(b *buffer, start, end int, runs []run, done func(failed string)) bool {
	s := formatter(b)
	if len(runs) == 0 && s == nil {
		return false
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
		done(failed)
	}
	timeout := time.Duration(e.cfg.FormatTimeout) * time.Millisecond
	timer = time.AfterFunc(timeout, func() {
		e.queue <- func() { finish(t, nil, "formatting took over "+timeout.String()) }
	})
	if len(runs) > 0 {
		old, path := t.Slice(0, t.Len()), b.file.path
		from, to := vim.LineStart(t, start), vim.LineEnd(t, end)
		go func() {
			// A buffer holds no last line break, and a formatter wants one.
			out, err := runFormatters(runs, old+"\n", path, timeout)
			e.queue <- func() {
				if err != nil {
					finish(t, nil, err.Error())
					return
				}
				edits := slices.DeleteFunc(lineEdits(old, strings.TrimSuffix(out, "\n")), func(ed kernel.Edit) bool {
					return ed.Start < from || ed.End > to
				})
				finish(t, edits, "")
			}
		}()
		return true
	}
	e.formatting(b, s, start, end, func(t kernel.Snapshot, edits []kernel.Edit) { finish(t, edits, "") })
	return true
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
// changed lines, so marks on the lines that stay keep their place. When a
// run has as many new lines as old ones, each line changes on its own and
// keeps its line break, so a mark on it stays on its line.
func lineEdits(old, new string) []kernel.Edit {
	var edits []kernel.Edit
	pos := 0 // where the next op's line starts in old
	ops := lineDiff(strings.SplitAfter(old, "\n"), strings.SplitAfter(new, "\n"))
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			pos, i = pos+len(ops[i].line), i+1
			continue
		}
		var gone, added []string
		for ; i < len(ops) && ops[i].kind != ' '; i++ {
			if ops[i].kind == '-' {
				gone = append(gone, ops[i].line)
			} else {
				added = append(added, ops[i].line)
			}
		}
		if len(gone) == len(added) {
			for k, line := range gone {
				e := kernel.Edit{Start: pos, End: pos + len(line), Text: added[k]}
				if strings.HasSuffix(line, "\n") && strings.HasSuffix(e.Text, "\n") {
					e.End, e.Text = e.End-1, strings.TrimSuffix(e.Text, "\n")
				}
				edits = append(edits, e)
				pos += len(line)
			}
			continue
		}
		e := kernel.Edit{Start: pos, Text: strings.Join(added, "")}
		for _, line := range gone {
			pos += len(line)
		}
		e.End = pos
		edits = append(edits, e)
	}
	return edits
}

// formatter returns the first of b's servers that formats, or nil (N22).
func formatter(b *buffer) *server {
	i := slices.IndexFunc(b.servers, func(s *server) bool {
		return s.client.Supports("documentFormattingProvider") || s.client.Supports("documentRangeFormattingProvider")
	})
	if i < 0 {
		return nil
	}
	return b.servers[i]
}

// formatting asks s for the edits that format b's lines from start to end,
// and passes them to answer with the text they are for. For a server that
// formats only whole documents, fude asks for that and keeps the edits
// inside the lines.
func (e *Session) formatting(b *buffer, s *server, start, end int, answer func(kernel.Snapshot, []kernel.Edit)) {
	t, uri := b.text.Snapshot(), lsp.URI(b.file.path)
	from, to := vim.LineStart(t, start), vim.LineEnd(t, end)
	ranged := s.client.Supports("documentRangeFormattingProvider")
	method := "textDocument/formatting"
	if ranged {
		method = "textDocument/rangeFormatting"
	}
	tab, spaces := e.indentWidth(b), e.expandTab(b)
	s.client.Request(method, func(enc string) any {
		p := map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"options":      map[string]any{"tabSize": tab, "insertSpaces": spaces},
		}
		if ranged {
			p["range"] = lsp.Range{Start: lsp.Point(t, from, enc), End: lsp.Point(t, to, enc)}
		}
		return p
	}, func(res json.RawMessage) {
		var answers []textEdit
		json.Unmarshal(res, &answers)
		enc := s.client.Encoding()
		var edits []kernel.Edit
		for _, a := range answers {
			start, end := lsp.Offset(t, a.Range.Start, enc), lsp.Offset(t, a.Range.End, enc)
			// A buffer holds no last line break, so a server that adds one
			// adds the line break the file has.
			if end == t.Len() && b.file.eol {
				if a.NewText = strings.TrimSuffix(a.NewText, "\n"); start == end && a.NewText == "" {
					continue
				}
			}
			if start >= from && end <= to {
				edits = append(edits, kernel.Edit{Start: start, End: end, Text: a.NewText})
			}
		}
		slices.SortStableFunc(edits, func(x, y kernel.Edit) int { return cmp.Compare(x.Start, y.Start) })
		// A server may replace the whole text, which would take the cursor to
		// its end, so the edits become the lines they change.
		old := t.Slice(0, t.Len())
		var formatted strings.Builder
		at := 0
		for _, ed := range edits {
			// Edits that overlap go as they are, for the kernel to refuse.
			if ed.Start < at {
				answer(t, edits)
				return
			}
			formatted.WriteString(old[at:ed.Start])
			formatted.WriteString(ed.Text)
			at = ed.End
		}
		formatted.WriteString(old[at:])
		answer(t, lineEdits(old, formatted.String()))
	})
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
