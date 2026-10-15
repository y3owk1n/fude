package editor

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
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
	e.syncKeys()
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
	e.syncKeys()
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

// requests are the language server requests that commands send, by name.
var requests = map[string]string{
	"lsp.definition":     "textDocument/definition",
	"lsp.typeDefinition": "textDocument/typeDefinition",
	"lsp.implementation": "textDocument/implementation",
	"lsp.references":     "textDocument/references",
	"lsp.symbols":        "textDocument/documentSymbol",
	"lsp.hover":          "textDocument/hover",
}

// attached are the keys that exist only while the current buffer has a
// language server, and the commands they run (N3).
var attached = map[string]string{
	"grr": "lsp.references", "gri": "lsp.implementation", "grt": "lsp.typeDefinition", "gO": "lsp.symbols",
}

// syncKeys maps the attached keys while the current buffer has a language
// server and removes them otherwise, so gr waits for nothing without one.
func (e *Editor) syncKeys() {
	if want := e.win.buf.server != nil; want != e.lspKeys {
		for keys, name := range attached {
			if want {
				e.vim.Map(vim.Normal, keys, "<Plug>("+name+")")
			} else {
				e.vim.Unmap(vim.Normal, keys)
			}
		}
		e.lspKeys = want
	}
}

// request sends the request named, such as lsp.definition, about the
// cursor's place in the current buffer, and handles the answer on the editor
// loop when it comes (B62, B66).
func (e *Editor) request(name string) {
	b := e.win.buf
	s := b.server
	if s == nil {
		e.message = "no language server"
		return
	}
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	s.client.Request(requests[name], func(enc string) any {
		p := map[string]any{"textDocument": map[string]any{"uri": uri}}
		if name != "lsp.symbols" {
			p["position"] = lsp.Point(t, c, enc)
		}
		if name == "lsp.references" {
			p["context"] = map[string]any{"includeDeclaration": true}
		}
		return p
	}, func(res json.RawMessage) {
		switch name {
		case "lsp.hover":
			e.hover(res)
		case "lsp.symbols":
			e.symbols(s, b, res)
		default:
			e.locate(s, name, res)
		}
	})
}

// spot is a place in a file that a server named.
type spot struct {
	path string
	pos  lsp.Position
	enc  string // the encoding pos counts in
}

// locate jumps to the one place an answer names, or lists them in a window
// when it names several, as references always are.
func (e *Editor) locate(s *server, name string, res json.RawMessage) {
	type location struct {
		URI       string    `json:"uri"`
		Range     lsp.Range `json:"range"`
		TargetURI string    `json:"targetUri"`
		Target    lsp.Range `json:"targetSelectionRange"`
	}
	var locs []location
	if json.Unmarshal(res, &locs) != nil {
		var one location
		if json.Unmarshal(res, &one) == nil && (one.URI != "" || one.TargetURI != "") {
			locs = []location{one}
		}
	}
	var spots []spot
	for _, l := range locs {
		if l.TargetURI != "" {
			l.URI, l.Range = l.TargetURI, l.Target
		}
		if u, err := url.Parse(l.URI); err == nil && u.Path != "" {
			spots = append(spots, spot{u.Path, l.Range.Start, s.client.Encoding()})
		}
	}
	what := strings.TrimPrefix(name, "lsp.")
	switch {
	case len(spots) == 0:
		e.message = "no " + what + " found"
	case len(spots) == 1 && name != "lsp.references":
		e.visit(spots[0])
	default:
		// Paths under the working directory show relative to it.
		wd, _ := os.Getwd()
		lines := make([]string, len(spots))
		for i, sp := range spots {
			path := sp.path
			if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
				path = rel
			}
			lines[i] = fmt.Sprintf("%s:%d:%d: %s", path, sp.pos.Line+1, sp.pos.Character+1, e.lineAt(sp))
		}
		e.showList("["+what+"]", lines, spots)
	}
}

// kinds are the names of the protocol's symbol kinds.
var kinds = []string{
	"", "File", "Module", "Namespace", "Package", "Class", "Method", "Property", "Field",
	"Constructor", "Enum", "Interface", "Function", "Variable", "Constant", "String", "Number", "Boolean",
	"Array", "Object", "Key", "Null", "EnumMember", "Struct", "Event", "Operator", "TypeParameter",
}

// symbols lists the symbols of b that a documentSymbol answer names, in a
// window where <CR> jumps to one. Nested symbols are indented under theirs.
func (e *Editor) symbols(s *server, b *buffer, res json.RawMessage) {
	type symbol struct {
		Name           string    `json:"name"`
		Kind           int       `json:"kind"`
		SelectionRange lsp.Range `json:"selectionRange"`
		Location       struct {
			Range lsp.Range `json:"range"`
		} `json:"location"`
		Children []json.RawMessage `json:"children"`
	}
	var lines []string
	var spots []spot
	var add func(raw []json.RawMessage, depth int)
	add = func(raw []json.RawMessage, depth int) {
		for _, r := range raw {
			var sym symbol
			if json.Unmarshal(r, &sym) != nil {
				continue
			}
			// A symbol has a selectionRange, or a location when the answer is flat.
			pos := sym.SelectionRange.Start
			if sym.SelectionRange == (lsp.Range{}) {
				pos = sym.Location.Range.Start
			}
			kind := ""
			if sym.Kind > 0 && sym.Kind < len(kinds) {
				kind = kinds[sym.Kind]
			}
			lines = append(lines, fmt.Sprintf("%d:%d: %s[%s] %s", pos.Line+1, pos.Character+1, strings.Repeat("  ", depth), kind, sym.Name))
			spots = append(spots, spot{b.file.path, pos, s.client.Encoding()})
			add(sym.Children, depth+1)
		}
	}
	var raw []json.RawMessage
	json.Unmarshal(res, &raw)
	add(raw, 0)
	if len(lines) == 0 {
		e.message = "no symbols found"
		return
	}
	e.showList("[symbols]", lines, spots)
}

// lineAt returns the text of the line sp is on, from its buffer, or from
// the file when no buffer has it.
func (e *Editor) lineAt(sp spot) string {
	text := ""
	if i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(sp.path) }); i >= 0 {
		t := e.bufs[i].text.Snapshot()
		text = t.Slice(0, t.Len())
	} else if data, err := os.ReadFile(sp.path); err == nil {
		text = string(data)
	}
	if lines := strings.Split(text, "\n"); sp.pos.Line < len(lines) {
		return strings.TrimSpace(lines[sp.pos.Line])
	}
	return ""
}

// showList opens a window of lines, each naming a spot, where <CR> jumps to the
// spot on the cursor's line in another window.
func (e *Editor) showList(title string, lines []string, spots []spot) {
	e.scratch(title, strings.Join(lines, "\n"), nil, nil)
	e.win.buf.spots = spots
}

// listKey runs <CR> in a list window, and reports whether it did. It jumps
// to the spot on the cursor's line in another window, keeping the list.
func (e *Editor) listKey(k vim.Key) bool {
	b := e.win.buf
	if k != "<CR>" {
		return false
	}
	sp := b.spots[e.Text().LineOf(e.Cursor())]
	for _, w := range e.root.windows() {
		if w.buf != b {
			e.win = w
			break
		}
	}
	e.visit(sp)
	return true
}

// visit jumps to sp, opening its file when no buffer has it.
func (e *Editor) visit(sp spot) {
	// Moving to another buffer is a jump of its own.
	if e.win.buf.is(sp.path) {
		e.pushJump(e.Cursor())
	} else {
		e.edit(sp.path)
	}
	if e.win.buf.is(sp.path) {
		e.setCursor(lsp.Offset(e.Text(), sp.pos, sp.enc))
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
