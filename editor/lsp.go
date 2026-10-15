package editor

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/lsp"
	"github.com/y3owk1n/fude/vim"
)

// server is a running language server and the buffers attached to it.
type server struct {
	client    *lsp.Client
	command   []string
	root      string
	restarted bool // it crashed once, and the next crash leaves it off
	closing   bool // fude asked it to exit
	bufs      []*buffer
}

// attach connects b to the language server its filetype names, starting one
// for b's directory when none runs (B60).
func (e *Editor) attach(b *buffer) {
	command := e.cfg.Filetype[b.filetype()].LSP
	if len(command) == 0 || b.file.path == "" || b.title != "" {
		return
	}
	abs, err := filepath.Abs(b.file.path)
	if err != nil {
		e.message = err.Error()
		return
	}
	root := filepath.Dir(abs)
	key := strings.Join(append(slices.Clone(command), root), "\x00")
	s := e.servers[key]
	if s == nil {
		s = &server{command: command, root: root}
		if err := e.start(s); err != nil {
			e.message = err.Error()
			return
		}
		e.servers[key] = s
	}
	s.bufs = append(s.bufs, b)
	b.server, b.version = s, 0
	s.client.Open(lsp.URI(b.file.path), b.filetype(), b.text.Snapshot())
}

// detach disconnects b from its language server.
func (e *Editor) detach(b *buffer) {
	if s := b.server; s != nil {
		s.client.CloseDocument(lsp.URI(b.file.path))
		s.bufs = slices.DeleteFunc(s.bufs, func(o *buffer) bool { return o == b })
		b.server = nil
	}
}

func (e *Editor) start(s *server) error {
	c, err := lsp.Start(s.command, s.root, e.queue, logLine,
		func(method string, params json.RawMessage) { e.notified(s, method, params) },
		func(err error) { e.exited(s, err) })
	if err != nil {
		return err
	}
	s.client = c
	return nil
}

// exited restarts a server that stopped without being asked to, once. Its
// buffers open in the new one. After a second crash it stays off (B63).
func (e *Editor) exited(s *server, err error) {
	if s.closing {
		return
	}
	name := filepath.Base(s.command[0])
	if s.restarted {
		e.message = fmt.Sprintf("language server %s exited again (%v), so it stays off", name, err)
		e.stop(s)
		return
	}
	s.restarted = true
	if err := e.start(s); err != nil {
		e.message = err.Error()
		e.stop(s)
		return
	}
	for _, b := range s.bufs {
		b.version = 0
		s.client.Open(lsp.URI(b.file.path), b.filetype(), b.text.Snapshot())
	}
	e.message = fmt.Sprintf("language server %s exited (%v) and was restarted", name, err)
}

// stop detaches every buffer from s and forgets it.
func (e *Editor) stop(s *server) {
	for _, b := range s.bufs {
		b.server = nil
	}
	for k, o := range e.servers {
		if o == s {
			delete(e.servers, k)
		}
	}
}

// severities are the styles of diagnostics, by the protocol's severity.
var severities = map[int]string{1: "diagnostic.error", 2: "diagnostic.warn", 3: "diagnostic.info", 4: "diagnostic.hint"}

// notified handles a notification from s. Diagnostics replace those of the
// buffer they are for, as decorations it owns, so they move with edits until
// the next ones come (B61).
func (e *Editor) notified(s *server, method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Range    lsp.Range `json:"range"`
			Severity int       `json:"severity"`
			Message  string    `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		logLine(fmt.Sprintf("diagnostics from %s: %v", filepath.Base(s.command[0]), err))
		return
	}
	i := slices.IndexFunc(s.bufs, func(b *buffer) bool { return lsp.URI(b.file.path) == p.URI })
	if i < 0 {
		return
	}
	b := s.bufs[i]
	for _, d := range e.decos[b] {
		d.remove()
	}
	t, enc := b.text.Snapshot(), s.client.Encoding()
	var ds []decoration
	for _, d := range p.Diagnostics {
		style := severities[d.Severity]
		if style == "" {
			style = severities[1]
		}
		start, end := lsp.Offset(t, d.Range.Start, enc), lsp.Offset(t, d.Range.End, enc)
		ds = append(ds, decoration{0, b, b.text.SetMark(start, kernel.Right), b.text.SetMark(end, kernel.Left), style, d.Message})
	}
	e.decos[b] = ds
}

// diagnostic moves the cursor to the nth diagnostic after it, or before it
// when n is negative, going round the ends, and shows its message.
func (e *Editor) diagnostic(n int) {
	b := e.win.buf
	var at []int
	notes := map[int]string{}
	for _, d := range e.decos[b] {
		p := b.text.Mark(d.start)
		at = append(at, p)
		notes[p] = d.note
	}
	if len(at) == 0 {
		e.message = "no diagnostics"
		return
	}
	slices.Sort(at)
	at = slices.Compact(at)
	c := e.Cursor()
	i, found := slices.BinarySearch(at, c)
	if n > 0 {
		if found {
			i++
		}
		i += n - 1
	} else {
		i += n
	}
	p := at[((i%len(at))+len(at))%len(at)]
	e.pushJump(c)
	e.setCursor(p)
	e.message = notes[p]
}

// request sends the request named, such as lsp.definition, about the
// cursor's place in the current buffer, and handles the answer on the editor
// loop when it comes (B62).
func (e *Editor) request(name string) {
	b := e.win.buf
	s := b.server
	if s == nil {
		e.message = "no language server"
		return
	}
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	method := map[string]string{"lsp.definition": "textDocument/definition", "lsp.hover": "textDocument/hover"}[name]
	s.client.Request(method, func(enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc)}
	}, func(res json.RawMessage) {
		switch name {
		case "lsp.definition":
			e.define(s, res)
		case "lsp.hover":
			e.hover(res)
		}
	})
}

// define jumps to the first place a definition answer names, opening its
// file when no buffer has it.
func (e *Editor) define(s *server, res json.RawMessage) {
	type location struct {
		URI       string    `json:"uri"`
		Range     lsp.Range `json:"range"`
		TargetURI string    `json:"targetUri"`
		Target    lsp.Range `json:"targetSelectionRange"`
	}
	var many []location
	if json.Unmarshal(res, &many) != nil {
		var one location
		if json.Unmarshal(res, &one) == nil {
			many = []location{one}
		}
	}
	if len(many) == 0 || many[0].URI == "" && many[0].TargetURI == "" {
		e.message = "no definition found"
		return
	}
	l := many[0]
	if l.TargetURI != "" {
		l.URI, l.Range = l.TargetURI, l.Target
	}
	u, err := url.Parse(l.URI)
	if err != nil {
		e.message = err.Error()
		return
	}
	// Moving to another buffer is a jump of its own.
	if e.win.buf.is(u.Path) {
		e.pushJump(e.Cursor())
	} else {
		e.edit(u.Path)
	}
	if e.win.buf.is(u.Path) {
		e.setCursor(lsp.Offset(e.Text(), l.Range.Start, s.client.Encoding()))
		e.reveal()
	}
}

// hover shows a hover answer's text in a float at the cursor, rendered from
// markdown.
func (e *Editor) hover(res json.RawMessage) {
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	json.Unmarshal(res, &h)
	// Contents is markup with a value, a string, or a list of either.
	var parts []json.RawMessage
	if json.Unmarshal(h.Contents, &parts) != nil {
		parts = []json.RawMessage{h.Contents}
	}
	var texts []string
	for _, p := range parts {
		var markup struct {
			Value string `json:"value"`
		}
		var plain string
		switch {
		case json.Unmarshal(p, &plain) == nil:
			texts = append(texts, plain)
		case json.Unmarshal(p, &markup) == nil && markup.Value != "":
			texts = append(texts, markup.Value)
		}
	}
	if len(texts) == 0 {
		e.message = "no hover information"
		return
	}
	e.float, e.floatSpans = e.markdown(strings.TrimSpace(strings.Join(texts, "\n\n")))
}

// format asks b's server to format the lines from start to end, as gq does,
// and applies the answer as one timeline node. For a server that formats
// only whole documents, fude asks for that and keeps the edits inside the
// lines. It drops an answer for text that has changed since (B65).
func (e *Editor) format(b *buffer, start, end int) {
	s := b.server
	switch {
	case s == nil:
		e.message = "no language server"
		return
	case b.file.readonly:
		e.message = "cannot make changes, the file is read-only"
		return
	}
	t, uri := b.text.Snapshot(), lsp.URI(b.file.path)
	from, to := vim.LineStart(t, start), vim.LineEnd(t, end)
	ranged := s.client.Supports("documentRangeFormattingProvider")
	method := "textDocument/formatting"
	if ranged {
		method = "textDocument/rangeFormatting"
	}
	tab := (host{e}).IndentWidth()
	s.client.Request(method, func(enc string) any {
		p := map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"options":      map[string]any{"tabSize": tab, "insertSpaces": false},
		}
		if ranged {
			p["range"] = lsp.Range{Start: lsp.Point(t, from, enc), End: lsp.Point(t, to, enc)}
		}
		return p
	}, func(res json.RawMessage) {
		var answer []struct {
			Range   lsp.Range `json:"range"`
			NewText string    `json:"newText"`
		}
		json.Unmarshal(res, &answer)
		enc := s.client.Encoding()
		tx := kernel.Transaction{Base: t.Version(), Origin: "user", Label: "gq", Time: e.now()}
		for _, a := range answer {
			start, end := lsp.Offset(t, a.Range.Start, enc), lsp.Offset(t, a.Range.End, enc)
			if start >= from && end <= to {
				tx.Edits = append(tx.Edits, kernel.Edit{Start: start, End: end, Text: a.NewText})
			}
		}
		if len(tx.Edits) == 0 {
			return
		}
		slices.SortStableFunc(tx.Edits, func(x, y kernel.Edit) int { return cmp.Compare(x.Start, y.Start) })
		if err := b.text.Apply(tx); err != nil {
			if errors.Is(err, kernel.ErrStale) {
				err = errors.New("the text changed while the server formatted it")
			}
			e.message = err.Error()
			return
		}
		b.head = b.text.Head()
		b.pushChange(tx.Edits[0].Start)
	})
}
