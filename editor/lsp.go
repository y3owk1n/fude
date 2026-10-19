package editor

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// server is a running language server and the buffers attached to it.
type server struct {
	client    *lsp.Client
	name      string // the name config.toml gives it
	command   []string
	root      string
	restarted bool // it crashed once, and the next crash leaves it off
	closing   bool // fude asked it to exit
	bufs      []*buffer
	versions  map[*buffer]int  // the changes it has heard of each, since it opened
	work      map[string]*work // by token, what it reports doing (B161)
}

// work is a piece of work a server reports progress on (B161).
type work struct {
	title, message string
	percent        *int
}

// progressed records the progress s reports on a piece of work, until the
// work ends (B161).
func (s *server) progressed(params json.RawMessage) {
	var p struct {
		Token json.RawMessage `json:"token"`
		Value struct {
			Kind, Title, Message string
			Percentage           *int
		} `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		logLine(fmt.Sprintf("progress from %s: %v", s.name, err))
		return
	}
	token, v := string(p.Token), p.Value
	switch v.Kind {
	case "begin":
		s.work[token] = &work{v.Title, v.Message, v.Percentage}
	case "report":
		if w := s.work[token]; w != nil {
			w.message = cmp.Or(v.Message, w.message)
			if v.Percentage != nil {
				w.percent = v.Percentage
			}
		}
	case "end":
		delete(s.work, token)
	}
}

// doing says what s is working on, the same piece of work each frame when
// it reports several, or is "" once it is ready (B161).
func (s *server) doing() string {
	if s.client.Encoding() == "" {
		return "starting"
	}
	if len(s.work) == 0 {
		return ""
	}
	w := s.work[slices.Min(slices.Collect(maps.Keys(s.work)))]
	text := strings.TrimSpace(w.title + " " + w.message)
	if w.percent != nil {
		text += fmt.Sprintf(" %d%%", *w.percent)
	}
	return text
}

// attach connects b to each language server its filetype names, and
// returns why any could not start (B60, B169).
func (e *Session) attach(b *buffer) error {
	var failed []string
	for _, name := range e.serverNames(b) {
		if err := e.attachTo(b, name); err != nil {
			failed = append(failed, err.Error())
		}
	}
	e.syncKeys()
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "  "))
	}
	return nil
}

// attachTo connects b to the language server name when it is for b and not
// attached yet, starting one for b's project root when none runs (B167,
// B169).
func (e *Session) attachTo(b *buffer, name string) error {
	if e.down[name] == disabled || e.down[name] == notInstalled || b.file.path == "" || b.title != "" ||
		slices.ContainsFunc(b.servers, func(s *server) bool { return s.name == name }) {
		return nil
	}
	abs, err := filepath.Abs(b.file.path)
	if err != nil {
		return err
	}
	c := e.cfg.LSP[name]
	root, found := rootOf(abs, c.Root)
	if !found && c.Workspace || len(c.Files) > 0 && !slices.ContainsFunc(c.Files, func(p string) bool { return matchEnd(p, abs) }) {
		return nil
	}
	key := name + "\x00" + root
	s := e.servers[key]
	if s == nil {
		command, err := commandOf(name, c.Command)
		if err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
		s = &server{name: name, command: command, root: root, versions: map[*buffer]int{}}
		if err := e.start(s); err != nil {
			e.serversChanged(name)
			e.down[name] = err.Error()
			// A program missing from PATH stays missing, so files opened
			// later do not try again.
			if errors.Is(err, exec.ErrNotFound) {
				e.down[name] = notInstalled
			}
			return err
		}
		e.servers[key] = s
		delete(e.down, name)
	}
	s.bufs, s.versions[b] = append(s.bufs, b), 0
	// A server attached again after :lsp restart keeps its place.
	names := e.serverNames(b)
	b.servers = append(b.servers, s)
	slices.SortStableFunc(b.servers, func(x, y *server) int {
		return cmp.Compare(slices.Index(names, x.name), slices.Index(names, y.name))
	})
	s.client.Open(lsp.URI(b.file.path), languageID(e.filetype(b)), b.text.Snapshot())
	return nil
}

// serverNames are the language servers of b's filetype, in order.
func (e *Editor) serverNames(b *buffer) []string {
	return e.cfg.Filetype[e.filetype(b)].LSP
}

// rootOf returns the project root of the file at abs: the nearest folder
// above it that holds the first of markers found, or the file's folder
// when it has none, and whether a marker was found (B167).
func rootOf(abs string, markers []string) (string, bool) {
	for _, m := range markers {
		for dir := filepath.Dir(abs); ; {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir, true
			}
			up := filepath.Dir(dir)
			if up == dir {
				break
			}
			dir = up
		}
	}
	return filepath.Dir(abs), false
}

// matchEnd reports whether pattern matches the file at abs: its name, or
// with a slash in pattern, as many of the path's last parts (B169).
func matchEnd(pattern, abs string) bool {
	parts := strings.Split(abs, string(filepath.Separator))
	n := strings.Count(pattern, "/") + 1
	ok, _ := filepath.Match(pattern, strings.Join(parts[max(len(parts)-n, 0):], "/"))
	return ok
}

// languageID is the protocol's name for filetype ft. It is ft except for
// these two.
func languageID(ft string) string {
	return cmp.Or(map[string]string{"bash": "shellscript", "tsx": "typescriptreact", "objc": "objective-c"}[ft], ft)
}

// detach disconnects b from its language servers.
func (e *Session) detach(b *buffer) {
	for _, s := range b.servers {
		s.client.Notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": lsp.URI(b.file.path)}})
		s.bufs = slices.DeleteFunc(s.bufs, func(o *buffer) bool { return o == b })
		delete(s.versions, b)
		e.emit("lsp.detached", s.name, b)
	}
	b.servers = nil
}

func (e *Session) start(s *server) error {
	cfg := e.cfg.LSP[s.name]
	c, err := lsp.Start(s.name, s.command, s.root, cfg.InitOptions, e.queue, logLine,
		func(text string, took time.Duration) { trace.Record("lsp", text, took) },
		func(method string, params json.RawMessage) { e.notified(s, method, params) },
		func(method string, params json.RawMessage) any { return e.served(s, method, params) },
		func(err error) { e.exited(s, err) })
	if err != nil {
		return err
	}
	trace.Record("lsp", "started "+s.name+" in "+s.root, 0)
	e.serversChanged(s.name)
	// A restarted server starts with none of its old process's work.
	s.client, s.work = c, map[string]*work{}
	if len(cfg.Settings) > 0 {
		c.Notify("workspace/didChangeConfiguration", map[string]any{"settings": cfg.Settings})
	}
	return nil
}

// exited restarts a server that stopped without being asked to, once. Its
// buffers open in the new one. After a second crash it stays off (B63).
func (e *Session) exited(s *server, err error) {
	if err != nil {
		trace.Record("lsp", s.name+" exited: "+err.Error(), 0)
	} else {
		trace.Record("lsp", s.name+" exited", 0)
	}
	if s.closing {
		return
	}
	if s.restarted {
		e.message = fmt.Sprintf("language server %s exited again (%v), so it stays off", s.name, err)
		e.down[s.name] = "crashed twice"
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
		s.versions[b] = 0
		s.client.Open(lsp.URI(b.file.path), languageID(e.filetype(b)), b.text.Snapshot())
	}
	e.message = fmt.Sprintf("language server %s exited (%v) and was restarted", s.name, err)
}

// stop detaches every buffer from s and forgets it.
func (e *Session) stop(s *server) {
	e.serversChanged(s.name)
	for _, b := range s.bufs {
		b.servers = slices.DeleteFunc(b.servers, func(o *server) bool { return o == s })
		e.emit("lsp.detached", s.name, b)
	}
	e.syncKeys()
	for k, o := range e.servers {
		if o == s {
			delete(e.servers, k)
		}
	}
}

// notified handles a notification from s. The modules hear the diagnostics
// it publishes for a buffer (B61, B292).
func (e *Session) notified(s *server, method string, params json.RawMessage) {
	switch method {
	case "$/progress":
		s.progressed(params)
		return
	case "initialized":
		e.serversChanged(s.name)
		for _, b := range s.bufs {
			e.emit("lsp.attached", s.name, b)
		}
		return
	}
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		logLine(fmt.Sprintf("diagnostics from %s: %v", s.name, err))
		return
	}
	// A server may write a URI differently, as file:///c%3A/x for
	// file:///C:/x, so the paths are what match.
	path, ok := lsp.Path(p.URI)
	if i := slices.IndexFunc(s.bufs, func(b *buffer) bool { return ok && b.is(path) }); i >= 0 {
		e.published(s, s.bufs[i], params)
	}
}

// published tells every module subscribed to lsp.published of the
// diagnostics s published for b, as the server sent them, each publish an
// event of its own (B292).
func (e *Editor) published(s *server, b *buffer, params json.RawMessage) {
	for _, l := range e.listeners {
		if slices.Contains(l.p.Subscribed(), "lsp.published") {
			l.put(event{name: "lsp.published", detail: s.name, buf: b, extra: api.Context{"server": s.name, "encoding": s.client.Encoding(), "params": string(params)}}, nil)
		}
	}
}

// syncKeys maps the attached keys while the current buffer has a language
// server and removes them otherwise, so gr waits for nothing without one.
func (e *Session) syncKeys() {
	want := len(e.win.buf.servers) > 0
	if want == e.lspKeys {
		return
	}
	e.lspKeys = want
	for _, k := range e.moduleKeys() {
		if k.attached {
			e.mapKey(k, want)
		}
	}
}

// reply is a server's answer to a request, nil when it answered with an
// error.
type reply struct {
	s   *server
	res json.RawMessage
}

// offers reports whether s offers method, such as textDocument/hover. A
// server that has not started yet may.
func (s *server) offers(method string) bool {
	provider := strings.TrimPrefix(method, "textDocument/") + "Provider"
	// The capabilities that are not named after their methods.
	switch method {
	case "workspace/symbol":
		provider = "workspaceSymbolProvider"
	case "textDocument/formatting":
		provider = "documentFormattingProvider"
	case "textDocument/rangeFormatting":
		provider = "documentRangeFormattingProvider"
	case "textDocument/documentColor":
		provider = "colorProvider"
	}
	return s.client.Encoding() == "" || s.client.Supports(provider)
}

// ask sends method to each of servers that offers it, with the params build
// makes for that server in its encoding, and passes answer their replies in
// the servers' order once every one has come. build runs off the editor
// loop. It reports whether any server was asked (B169).
func ask(servers []*server, method string, build func(s *server, enc string) any, answer func([]reply)) bool {
	var to []*server
	for _, s := range servers {
		if s.offers(method) {
			to = append(to, s)
		}
	}
	replies, left := make([]reply, len(to)), len(to)
	for i, s := range to {
		s.client.Request(method, func(enc string) any { return build(s, enc) }, func(res json.RawMessage) {
			replies[i] = reply{s, res}
			if left--; left == 0 {
				answer(replies)
			}
		})
	}
	return len(to) > 0
}

// first returns the first of servers that offers method, or nil.
func first(servers []*server, method string) *server {
	if i := slices.IndexFunc(servers, func(s *server) bool { return s.offers(method) }); i >= 0 {
		return servers[i]
	}
	return nil
}

// workspaceEdit applies a server's workspace edit, each file's edits as one
// transaction, and returns how many files changed and what went wrong. A
// file with no buffer opens in one and is not written. from is the buffer
// that asked, if any, whose edits must apply to t, the text it asked about.
// Another buffer's must apply to the version the server names, when it
// names one.
func (e *Session) workspaceEdit(s *server, from *buffer, t kernel.Snapshot, res json.RawMessage, label string) (int, []string) {
	if err := e.mayEdit(); err != nil {
		return 0, []string{err.Error()}
	}
	files, err := workspaceFiles(res)
	if err != nil {
		return 0, []string{err.Error()}
	}
	enc, changed := s.client.Encoding(), 0
	var problems []string
	for uri, f := range files {
		path, ok := lsp.Path(uri)
		if !ok {
			problems = append(problems, uri+" names no file")
			continue
		}
		if err := e.outside(path); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(path) })
		if i < 0 {
			if _, err := e.add(path); err != nil {
				problems = append(problems, err.Error())
				continue
			}
			i = len(e.bufs) - 1
		}
		b := e.bufs[i]
		base := b.text.Snapshot()
		if b == from {
			base = t
		}
		stale := fmt.Sprintf("%s changed since the server answered, so it was left alone", b.name())
		if f.version != nil && slices.Contains(b.servers, s) && b != from && *f.version != s.versions[b] {
			problems = append(problems, stale)
			continue
		}
		tx := kernel.Transaction{Base: base.Version(), Origin: e.origin, Label: label, Time: e.now(), Edits: kernelEdits(base, f.edits, enc)}
		if err := b.text.Apply(tx); err != nil {
			if errors.Is(err, kernel.ErrStale) {
				err = errors.New(stale)
			}
			problems = append(problems, err.Error())
			continue
		}
		b.head = b.text.Head()
		changed++
	}
	return changed, problems
}

// served answers a request from s. workspace/applyEdit applies its edit, as
// a code action's command asks, and workspace/configuration gets the
// sections of s's settings that the request names (B168). Other requests get an empty
// answer, as fude offers nothing else.
func (e *Session) served(s *server, method string, params json.RawMessage) any {
	if method == "workspace/configuration" {
		var p struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		json.Unmarshal(params, &p)
		out := make([]any, len(p.Items))
		for i, it := range p.Items {
			out[i] = section(e.cfg.LSP[s.name].Settings, it.Section)
		}
		return out
	}
	if method != "workspace/applyEdit" {
		return nil
	}
	var p struct {
		Edit json.RawMessage `json:"edit"`
	}
	json.Unmarshal(params, &p)
	_, problems := e.workspaceEdit(s, nil, kernel.Snapshot{}, p.Edit, "code action")
	if len(problems) > 0 {
		e.message = strings.Join(problems, "  ")
	}
	return map[string]any{"applied": len(problems) == 0}
}

// section returns the part of settings that a dotted name such as
// "gopls.ui" names, all of it for "", or nil when there is none.
func section(settings map[string]any, name string) any {
	if name == "" {
		return settings
	}
	var v any = settings
	for k := range strings.SplitSeq(name, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// textEdit is an edit as the protocol sends it.
type textEdit struct {
	Range   lsp.Range `json:"range"`
	NewText string    `json:"newText"`
}

// fileEdits are a workspace edit's edits of one file, and the version of
// the file they are for when the server names one.
type fileEdits struct {
	edits   []textEdit
	version *int
}

// workspaceFiles reads a workspace edit's changes and document changes, by
// the URI of the file each edits.
func workspaceFiles(raw json.RawMessage) (map[string]fileEdits, error) {
	var w struct {
		Changes         map[string][]textEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version *int   `json:"version"`
			} `json:"textDocument"`
			Edits []textEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("workspace edit: %w", err)
	}
	files := map[string]fileEdits{}
	for uri, edits := range w.Changes {
		files[uri] = fileEdits{edits: edits}
	}
	for _, d := range w.DocumentChanges {
		files[d.TextDocument.URI] = fileEdits{d.Edits, d.TextDocument.Version}
	}
	return files, nil
}

// kernelEdits turns es, counted in encoding enc, into edits of t sorted by
// where they start.
func kernelEdits(t kernel.Snapshot, es []textEdit, enc string) []kernel.Edit {
	out := make([]kernel.Edit, 0, len(es))
	for _, ed := range es {
		out = append(out, kernel.Edit{Start: lsp.Offset(t, ed.Range.Start, enc), End: lsp.Offset(t, ed.Range.End, enc), Text: ed.NewText})
	}
	slices.SortStableFunc(out, func(x, y kernel.Edit) int { return cmp.Compare(x.Start, y.Start) })
	return out
}

// signature asks the server for the signature of the call at the cursor. It
// shows above the cursor's line, with the parameter being typed in bold,
// and under a rule that parameter's documentation and the function's. The
// first server that offers signatures answers. When a typed key asked, it
// gives no message if there is none (B66, B203, N22).
func (e *Session) signature(typed bool) {
	b := e.win.buf
	s := first(b.servers, "textDocument/signatureHelp")
	if s == nil {
		if !typed {
			e.message = "no language server offers signature help"
		}
		return
	}
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	s.client.Request("textDocument/signatureHelp", func(enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc)}
	}, func(res json.RawMessage) {
		// Insert mode may have ended while the server answered.
		if e.vim.Mode() != vim.Insert {
			return
		}
		var help struct {
			Signatures []struct {
				Label         string          `json:"label"`
				Documentation json.RawMessage `json:"documentation"`
				Parameters    []struct {
					Label         json.RawMessage `json:"label"`
					Documentation json.RawMessage `json:"documentation"`
				} `json:"parameters"`
				ActiveParameter *int `json:"activeParameter"`
			} `json:"signatures"`
			ActiveSignature int `json:"activeSignature"`
			ActiveParameter int `json:"activeParameter"`
		}
		json.Unmarshal(res, &help)
		if len(help.Signatures) == 0 {
			e.completion.sig = nil
			if !typed {
				e.message = "no signature help"
			}
			return
		}
		sig := help.Signatures[min(help.ActiveSignature, len(help.Signatures)-1)]
		active := help.ActiveParameter
		if sig.ActiveParameter != nil {
			active = *sig.ActiveParameter
		}
		var spans []syntax.Span
		var docs []string
		if active >= 0 && active < len(sig.Parameters) {
			if d := markupText(sig.Parameters[active].Documentation); d != "" {
				docs = append(docs, d)
			}
			// A parameter's label is its text in the signature, or where it is.
			var name string
			var at [2]int
			if json.Unmarshal(sig.Parameters[active].Label, &name) == nil {
				if i := strings.Index(sig.Label, name); i >= 0 {
					at = [2]int{i, i + len(name)}
				}
			} else {
				json.Unmarshal(sig.Parameters[active].Label, &at)
			}
			if at[0] < at[1] && at[1] <= len(sig.Label) {
				spans = []syntax.Span{{Start: at[0], End: at[1], Name: "markup.strong"}}
			}
		}
		if d := markupText(sig.Documentation); d != "" {
			docs = append(docs, d)
		}
		text := sig.Label
		if len(docs) > 0 {
			md, mdSpans := e.markdown("---\n" + strings.Join(docs, "\n\n"))
			for _, sp := range mdSpans {
				sp.Start, sp.End = sp.Start+len(text)+1, sp.End+len(text)+1
				spans = append(spans, sp)
			}
			text += "\n" + md
		}
		e.completion.sig = &wire.Float{Text: text, Spans: spans, Cursor: -1}
	})
}

// markupText is the text of markup with a value or of a plain string, as
// servers send hover and documentation, or "" for anything else.
func markupText(raw json.RawMessage) string {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var markup struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &markup) != nil {
		return ""
	}
	return markup.Value
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
