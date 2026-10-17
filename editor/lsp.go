package editor

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
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
	e.askColours(s, b)
	e.askHints(s, b)
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
		e.forget(s, b)
	}
	b.servers = nil
}

// diagnosed owns the diagnostics a server published for a buffer.
type diagnosed struct {
	s *server
	b *buffer
}

// diagnostics are b's diagnostics from each of its servers.
func (e *Editor) diagnostics(b *buffer) []decoration {
	var out []decoration
	for _, s := range b.servers {
		out = append(out, e.decos[diagnosed{s, b}]...)
	}
	return out
}

// forget drops the diagnostics, colours and inlay hints s sent for b.
func (e *Editor) forget(s *server, b *buffer) {
	for _, owner := range []any{diagnosed{s, b}, coloured{s, b}, hinted{s, b}} {
		for _, d := range e.decos[owner] {
			d.remove()
		}
		delete(e.decos, owner)
	}
	b.signed = len(e.diagnostics(b)) > 0
}

func (e *Session) start(s *server) error {
	cfg := e.cfg.LSP[s.name]
	c, err := lsp.Start(s.name, s.command, s.root, cfg.InitOptions, e.queue, logLine,
		func(method string, params json.RawMessage) { e.notified(s, method, params) },
		func(method string, params json.RawMessage) any { return e.served(s, method, params) },
		func(err error) { e.exited(s, err) })
	if err != nil {
		return err
	}
	api.Trace("lsp", "started "+s.name+" in "+s.root, 0)
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
		api.Trace("lsp", s.name+" exited: "+err.Error(), 0)
	} else {
		api.Trace("lsp", s.name+" exited", 0)
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
	for _, b := range s.bufs {
		b.servers = slices.DeleteFunc(b.servers, func(o *server) bool { return o == s })
		e.forget(s, b)
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
func (e *Session) notified(s *server, method string, params json.RawMessage) {
	switch method {
	case "$/progress":
		s.progressed(params)
		return
	case "initialized":
		for _, b := range s.bufs {
			e.askColours(s, b)
			e.askHints(s, b)
		}
		return
	}
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string            `json:"uri"`
		Diagnostics []json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		logLine(fmt.Sprintf("diagnostics from %s: %v", s.name, err))
		return
	}
	i := slices.IndexFunc(s.bufs, func(b *buffer) bool { return lsp.URI(b.file.path) == p.URI })
	if i < 0 {
		return
	}
	b := s.bufs[i]
	for _, d := range e.decos[diagnosed{s, b}] {
		d.remove()
	}
	t, enc := b.text.Snapshot(), s.client.Encoding()
	var ds []decoration
	for _, raw := range p.Diagnostics {
		var d struct {
			Range    lsp.Range `json:"range"`
			Severity int       `json:"severity"`
			Message  string    `json:"message"`
		}
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		style := severities[d.Severity]
		if style == "" {
			style = severities[1]
		}
		start, end := lsp.Offset(t, d.Range.Start, enc), lsp.Offset(t, d.Range.End, enc)
		ds = append(ds, decoration{0, b, b.text.SetMark(start, kernel.Right), b.text.SetMark(end, kernel.Left), style, d.Message, "", raw})
	}
	e.decos[diagnosed{s, b}] = ds
	b.signed = len(e.diagnostics(b)) > 0
	e.emit("lsp.diagnostics", "", b)
}

// diagnostic moves the cursor to the nth diagnostic after it, or before it
// when n is negative, going round the ends, and shows it in a float.
func (e *Session) diagnostic(n int) {
	b := e.win.buf
	var at []int
	for _, d := range e.diagnostics(b) {
		at = append(at, b.text.Mark(d.start))
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
	e.diagnosticFloat(b, p)
}

// diagnosticFloat shows the diagnostics that start at p in a float at the
// cursor, each message in its severity's colour with the source and code
// its server gives after it (B61).
func (e *Session) diagnosticFloat(b *buffer, p int) {
	var text strings.Builder
	var spans []syntax.Span
	for _, d := range e.diagnostics(b) {
		if b.text.Mark(d.start) != p {
			continue
		}
		var from struct {
			Source string          `json:"source"`
			Code   json.RawMessage `json:"code"`
		}
		json.Unmarshal(d.raw, &from)
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		spans = append(spans, syntax.Span{Start: text.Len(), End: text.Len() + len(d.note), Name: d.style + ".text"})
		text.WriteString(d.note)
		// A code is a number or a string.
		source := from.Source
		if code := strings.Trim(string(from.Code), `"`); code != "" {
			source += "(" + code + ")"
		}
		if source != "" {
			spans = append(spans, syntax.Span{Start: text.Len() + 2, End: text.Len() + 2 + len(source), Name: "comment"})
			text.WriteString("  " + source)
		}
	}
	e.float, e.floatSpans = text.String(), spans
}

// requests are the language server requests that commands send, by name.
var requests = map[string]string{
	"lsp.definition":     "textDocument/definition",
	"lsp.typeDefinition": "textDocument/typeDefinition",
	"lsp.implementation": "textDocument/implementation",
	"lsp.references":     "textDocument/references",
	"lsp.symbols":        "textDocument/documentSymbol",
	"lsp.hover":          "textDocument/hover",
	"lsp.rename":         "textDocument/rename",
	"lsp.codeAction":     "textDocument/codeAction",
}

// attached are the keys that exist only while the current buffer has a
// language server, and the commands they run (N3).
var attached = map[string]string{
	"grr": "lsp.references", "gri": "lsp.implementation", "grt": "lsp.typeDefinition", "gO": "lsp.symbols",
	"grn": "lsp.rename", "gra": "lsp.codeAction",
}

// syncKeys maps the attached keys while the current buffer has a language
// server and removes them otherwise, so gr waits for nothing without one.
func (e *Session) syncKeys() {
	if want := len(e.win.buf.servers) > 0; want != e.lspKeys {
		for keys, name := range attached {
			// Code actions take the selection in visual mode too (B227).
			modes := []vim.Mode{vim.Normal}
			if keys == "gra" {
				modes = append(modes, vim.Visual)
			}
			for _, mode := range modes {
				if want {
					e.vim.Map(mode, keys, "<Plug>("+name+")")
				} else {
					e.vim.Unmap(mode, keys)
				}
			}
		}
		e.lspKeys = want
	}
}

// request sends the request named, such as lsp.definition, about the
// cursor's place in the current buffer, and handles the answer on the editor
// loop when it comes (B62, B66).
func (e *Session) request(name string) {
	if name == "lsp.hover" && e.left != "" {
		// Hover while a float shows moves the cursor into it.
		e.focus(e.left)
		return
	}
	b := e.win.buf
	if len(b.servers) == 0 {
		e.message = "no language server"
		return
	}
	if name == "lsp.codeAction" {
		e.codeActions()
		return
	}
	if name == "lsp.rename" {
		// grn asks for the new name on the command line, starting from the
		// word under the cursor, and :rename sends it.
		t, c := e.Text(), e.Cursor()
		start, end := c, c
		for start > 0 && isWord(t.Slice(start-1, start)[0]) {
			start--
		}
		for end < t.Len() && isWord(t.Slice(end, end+1)[0]) {
			end++
		}
		e.later = append(e.later, vim.Keys(":rename "+t.Slice(start, end))...)
		e.renameAt = c
		return
	}
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	asked := ask(b.servers, requests[name], func(_ *server, enc string) any {
		p := map[string]any{"textDocument": map[string]any{"uri": uri}}
		if name != "lsp.symbols" {
			p["position"] = lsp.Point(t, c, enc)
		}
		if name == "lsp.references" {
			p["context"] = map[string]any{"includeDeclaration": true}
		}
		return p
	}, func(rs []reply) {
		switch name {
		case "lsp.hover":
			e.hover(rs)
		case "lsp.symbols":
			e.symbols(b, rs)
		default:
			e.locate(name, rs)
		}
	})
	if !asked {
		e.message = "no language server offers " + strings.TrimPrefix(name, "lsp.")
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
	if method == "workspace/symbol" {
		provider = "workspaceSymbolProvider"
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

// rename asks the current buffer's server to rename the symbol where grn
// was pressed to name, and applies the edits it answers with, in every file
// they name (B66).
func (e *Session) rename(name string) {
	b := e.win.buf
	s := first(b.servers, requests["lsp.rename"])
	if s == nil {
		e.message = "no language server offers rename"
		return
	}
	if name == "" {
		e.message = "no new name"
		return
	}
	t, c, uri := b.text.Snapshot(), min(e.renameAt, b.text.Snapshot().Len()), lsp.URI(b.file.path)
	s.client.Request(requests["lsp.rename"], func(enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc), "newName": name}
	}, func(res json.RawMessage) {
		if res == nil {
			return
		}
		changed, problems := e.workspaceEdit(s, b, t, res, "rename")
		e.message = fmt.Sprintf("renamed in %d files", changed)
		if len(problems) > 0 {
			e.message = strings.Join(problems, "  ")
		}
	})
}

// workspaceEdit applies a server's workspace edit, each file's edits as one
// transaction, and returns how many files changed and what went wrong. A
// file with no buffer opens in one and is not written. from is the buffer
// that asked, if any, whose edits must apply to t, the text it asked about.
// Another buffer's must apply to the version the server names, when it
// names one.
func (e *Session) workspaceEdit(s *server, from *buffer, t kernel.Snapshot, res json.RawMessage, label string) (int, []string) {
	files, err := workspaceFiles(res)
	if err != nil {
		return 0, []string{err.Error()}
	}
	enc, changed := s.client.Encoding(), 0
	var problems []string
	for uri, f := range files {
		u, err := url.Parse(uri)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(u.Path) })
		if i < 0 {
			if _, err := e.add(u.Path); err != nil {
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

// pick is a code action menu: the actions, the best first, and the one
// selected (B227).
type pick struct {
	b       *buffer
	t       kernel.Snapshot
	actions []action
	sel     int
}

// action is a code action from a server, as the menu shows it.
type action struct {
	reply
	title, kind, disabled string
	preferred             bool
	preview               string // what it changes, as a diff
	spans                 []syntax.Span
	resolving             bool
}

// codeActions asks each server for the code actions at the cursor, or for
// the selection in visual mode, with the diagnostics it published on those
// lines, and shows them to pick from. With several servers, each action
// names its own (B66, B169, B227).
func (e *Session) codeActions() {
	b := e.win.buf
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	start, end := c, c
	if sel, ok := e.Selection(); ok {
		start, end = sel.Start, sel.End
		e.settle(e.vim.Feed("<Esc>"))
	}
	// build runs off the loop, so this gathers the diagnostics first.
	diags := map[*server][]json.RawMessage{}
	for _, s := range b.servers {
		diags[s] = []json.RawMessage{}
		for _, d := range e.decos[diagnosed{s, b}] {
			if l := t.LineOf(b.text.Mark(d.start)); d.raw != nil && l >= t.LineOf(start) && l <= t.LineOf(end) {
				diags[s] = append(diags[s], d.raw)
			}
		}
	}
	asked := ask(b.servers, "textDocument/codeAction", func(s *server, enc string) any {
		return map[string]any{
			"textDocument": map[string]any{"uri": uri}, "range": lsp.Range{Start: lsp.Point(t, start, enc), End: lsp.Point(t, end, enc)},
			"context": map[string]any{"diagnostics": diags[s]},
		}
	}, func(rs []reply) {
		var actions []action
		for _, r := range rs {
			var raws []json.RawMessage
			json.Unmarshal(r.res, &raws)
			for _, raw := range raws {
				var a struct {
					Title       string `json:"title"`
					Kind        string `json:"kind"`
					IsPreferred bool   `json:"isPreferred"`
					Disabled    *struct {
						Reason string `json:"reason"`
					} `json:"disabled"`
				}
				json.Unmarshal(raw, &a)
				// A kind such as source.organizeImports shows as its family, source.
				kind, _, _ := strings.Cut(a.Kind, ".")
				act := action{reply: reply{r.s, raw}, title: a.Title, kind: kind, preferred: a.IsPreferred}
				if a.Disabled != nil {
					act.disabled = cmp.Or(a.Disabled.Reason, "disabled")
				}
				if len(rs) > 1 {
					act.kind = strings.TrimSpace(act.kind + " " + r.s.name)
				}
				actions = append(actions, act)
			}
		}
		if len(actions) == 0 {
			e.message = "no code actions"
			return
		}
		// The preferred ones first, then fixes, refactors and the rest, each
		// in the servers' order.
		rank := func(a action) int {
			switch {
			case a.disabled != "":
				return 4
			case a.preferred:
				return 0
			case strings.HasPrefix(a.kind, "quickfix"):
				return 1
			case strings.HasPrefix(a.kind, "refactor"):
				return 2
			}
			return 3
		}
		slices.SortStableFunc(actions, func(x, y action) int { return cmp.Compare(rank(x), rank(y)) })
		e.pick = &pick{b: b, t: t, actions: actions}
		e.showActions()
	})
	if !asked {
		e.message = "no language server offers code actions"
	}
}

// showActions draws the code action menu at the cursor, each action with
// its number, title and kind, and the selected one's changes beside it.
func (e *Session) showActions() {
	p := e.pick
	var s strings.Builder
	var spans []syntax.Span
	width := 0
	for _, a := range p.actions {
		width = max(width, utf8.RuneCountInString(a.title))
	}
	for i, a := range p.actions {
		if i > 0 {
			s.WriteByte('\n')
		}
		start := s.Len()
		num := " "
		if i < 9 {
			num = strconv.Itoa(i + 1)
		}
		spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(num), Name: "comment"})
		s.WriteString(num + "  " + a.title)
		if a.kind != "" {
			s.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(a.title)+2))
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(a.kind), Name: "comment"})
			s.WriteString(a.kind)
		}
		if a.disabled != "" {
			spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: "comment"})
		}
		if i == p.sel {
			spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: "menu.selected"})
		}
	}
	e.float, e.floatSpans = s.String(), spans
	e.doc, e.docSpans = e.actionPreview(p, p.sel)
}

// actionPreview returns what action i of p changes: each file's lines that
// differ, with a line around them, or the command it runs, or why it is
// disabled. An action that has neither edit nor command is resolved first,
// when its server can, and shows once it is.
func (e *Session) actionPreview(p *pick, i int) (string, []syntax.Span) {
	a := &p.actions[i]
	if a.disabled != "" {
		return a.disabled, []syntax.Span{{Start: 0, End: len(a.disabled), Name: "comment"}}
	}
	if a.preview != "" {
		return a.preview, a.spans
	}
	var raw struct {
		Edit    json.RawMessage `json:"edit"`
		Command json.RawMessage `json:"command"`
		Data    json.RawMessage `json:"data"`
	}
	json.Unmarshal(a.res, &raw)
	if raw.Edit == nil && raw.Command == nil && raw.Data != nil && resolvable(a.s) {
		if !a.resolving {
			a.resolving = true
			a.s.client.Request("codeAction/resolve", func(string) any { return a.res }, func(res json.RawMessage) {
				if res != nil && e.pick == p {
					a.res = res
					e.showActions()
				}
			})
		}
		return "", nil
	}
	if raw.Edit != nil {
		a.preview, a.spans = e.editPreview(a.s, p, raw.Edit)
	}
	if a.preview == "" && raw.Command != nil {
		// A command is an object, or a bare action's command is its name.
		var c struct {
			Title   string `json:"title"`
			Command string `json:"command"`
		}
		if json.Unmarshal(raw.Command, &c) != nil {
			json.Unmarshal(raw.Command, &c.Command)
		}
		a.preview = "runs " + cmp.Or(c.Title, c.Command)
		a.spans = []syntax.Span{{Start: 0, End: len(a.preview), Name: "comment"}}
	}
	return a.preview, a.spans
}

// resolvable reports whether s resolves code actions.
func resolvable(s *server) bool {
	var p struct {
		ResolveProvider bool `json:"resolveProvider"`
	}
	json.Unmarshal(s.client.Capability("codeActionProvider"), &p)
	return p.ResolveProvider
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

// editPreview returns the lines a workspace edit changes, file by file as
// a diff, under each file's name when it changes another file than p's.
func (e *Session) editPreview(s *server, p *pick, edit json.RawMessage) (string, []syntax.Span) {
	files, err := workspaceFiles(edit)
	if err != nil {
		return err.Error(), nil
	}
	var out strings.Builder
	var spans []syntax.Span
	for _, uri := range slices.Sorted(maps.Keys(files)) {
		u, err := url.Parse(uri)
		if err != nil {
			continue
		}
		var before kernel.Snapshot
		switch i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(u.Path) }); {
		case p.b.is(u.Path):
			before = p.t
		case i >= 0:
			before = e.bufs[i].text.Snapshot()
		default:
			data, err := os.ReadFile(u.Path)
			if err != nil {
				continue
			}
			before = kernel.New(strings.TrimSuffix(string(data), "\n"), nil).Snapshot()
		}
		edits := kernelEdits(before, files[uri].edits, s.client.Encoding())
		after := before.Slice(0, before.Len())
		for _, ed := range slices.Backward(edits) {
			after = after[:ed.Start] + ed.Text + after[ed.End:]
		}
		if !p.b.is(u.Path) || len(files) > 1 {
			name := filepath.Base(u.Path)
			spans = append(spans, syntax.Span{Start: out.Len(), End: out.Len() + len(name), Name: "markup.heading"})
			out.WriteString(name + "\n")
		}
		// A run of changes after the first shows after ⋯, and none says
		// where it is.
		for i, l := range strings.Split(unified(lineDiff(strings.Split(before.Slice(0, before.Len()), "\n"), strings.Split(after, "\n")), 1), "\n") {
			switch {
			case l == "" || strings.HasPrefix(l, "@@") && i == 0:
				continue
			case strings.HasPrefix(l, "@@"):
				l = "⋯"
			}
			if style := map[byte]string{'-': "diff.minus", '+': "diff.plus"}[l[0]]; style != "" {
				spans = append(spans, syntax.Span{Start: out.Len(), End: out.Len() + len(l), Name: style})
			}
			out.WriteString(l + "\n")
		}
	}
	return strings.TrimSuffix(out.String(), "\n"), spans
}

// picking takes a key while code actions show. <C-n>, <C-p>, j, k and the
// arrows move the selection, <CR> takes the selected action and a digit the
// one it numbers, and any other key closes the menu.
func (e *Session) picking(k vim.Key) {
	p := e.pick
	switch k {
	case "<C-n>", "<Down>", "j", "\t":
		p.sel = (p.sel + 1) % len(p.actions)
	case "<C-p>", "<Up>", "k", "<S-Tab>":
		p.sel = (p.sel + len(p.actions) - 1) % len(p.actions)
	case "<CR>", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := p.sel
		if k != "<CR>" {
			i = int(k[0] - '1')
		}
		e.pick, e.float, e.doc, e.docSpans = nil, "", "", nil
		switch {
		case i >= len(p.actions):
		case p.actions[i].disabled != "":
			e.message = p.actions[i].title + ": " + p.actions[i].disabled
		default:
			e.act(p, p.actions[i].reply)
		}
		return
	default:
		e.pick, e.float, e.doc, e.docSpans = nil, "", "", nil
		return
	}
	e.showActions()
}

// act takes a code action from its server: it applies its edit and runs its
// command. An action with neither but data is resolved first, when the
// server can.
func (e *Session) act(p *pick, a reply) {
	s, raw := a.s, a.res
	var action struct {
		Edit      json.RawMessage `json:"edit"`
		Command   json.RawMessage `json:"command"`
		Arguments json.RawMessage `json:"arguments"`
		Data      json.RawMessage `json:"data"`
	}
	json.Unmarshal(raw, &action)
	if action.Edit == nil && action.Command == nil && action.Data != nil && resolvable(s) {
		s.client.Request("codeAction/resolve", func(string) any { return raw }, func(res json.RawMessage) {
			if res != nil {
				e.act(p, reply{s, res})
			}
		})
		return
	}
	if action.Edit != nil {
		if _, problems := e.workspaceEdit(s, p.b, p.t, action.Edit, "code action"); len(problems) > 0 {
			e.message = strings.Join(problems, "  ")
			return
		}
	}
	var command json.RawMessage
	switch {
	case action.Command == nil:
		return
	case action.Command[0] == '{':
		// A code action's command is an object of its own.
		command = action.Command
	default:
		// A bare command names itself with a string.
		command = raw
	}
	s.client.Request("workspace/executeCommand", func(string) any { return command }, func(json.RawMessage) {})
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
			e.sig = nil
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
		e.sig = &Float{Text: text, Spans: spans, Cursor: -1}
	})
}

// spot is a place in a file that a server named.
type spot struct {
	path string
	pos  lsp.Position
	enc  string // the encoding pos counts in
}

// locate jumps to the one place the replies name, or lists them in a
// window when they name several, as references always are.
func (e *Session) locate(name string, rs []reply) {
	type location struct {
		URI       string    `json:"uri"`
		Range     lsp.Range `json:"range"`
		TargetURI string    `json:"targetUri"`
		Target    lsp.Range `json:"targetSelectionRange"`
	}
	var spots []spot
	for _, r := range rs {
		var locs []location
		if json.Unmarshal(r.res, &locs) != nil {
			var one location
			if json.Unmarshal(r.res, &one) == nil && (one.URI != "" || one.TargetURI != "") {
				locs = []location{one}
			}
		}
		for _, l := range locs {
			if l.TargetURI != "" {
				l.URI, l.Range = l.TargetURI, l.Target
			}
			if u, err := url.Parse(l.URI); err == nil && u.Path != "" {
				spots = append(spots, spot{u.Path, l.Range.Start, r.s.client.Encoding()})
			}
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
			if rel, ok := under(wd, path); ok {
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

// symbols lists the symbols of b that the documentSymbol replies name, in a
// window where <CR> jumps to one. Nested symbols are indented under theirs.
func (e *Session) symbols(b *buffer, rs []reply) {
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
	var add func(s *server, raw []json.RawMessage, depth int)
	add = func(s *server, raw []json.RawMessage, depth int) {
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
			add(s, sym.Children, depth+1)
		}
	}
	for _, r := range rs {
		var raw []json.RawMessage
		json.Unmarshal(r.res, &raw)
		add(r.s, raw, 0)
	}
	if len(lines) == 0 {
		e.message = "no symbols found"
		return
	}
	e.showList("[symbols]", lines, spots)
}

// lineAt returns the text of the line sp is on, from its buffer, or from
// the file when no buffer has it.
func (e *Session) lineAt(sp spot) string {
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
func (e *Session) showList(title string, lines []string, spots []spot) {
	e.scratch(title, strings.Join(lines, "\n"), nil, nil)
	e.win.buf.spots = spots
	e.lastList = &places{e.win.buf, title, lines, spots, -1}
}

// places is the last list of places a window showed, which ]q, :cnext and
// the others go through after its window closes (B194, N25).
type places struct {
	buf   *buffer
	title string
	lines []string
	spots []spot
	at    int // the place last gone to, -1 before the first
}

// stepPlace goes to the place of the last list that to gives, from the
// place last gone to and the count of places, and says which it is.
func (e *Session) stepPlace(to func(at, n int) int) {
	l := e.lastList
	if l == nil {
		e.message = "no list of places"
		return
	}
	at := to(l.at, len(l.spots))
	if at < 0 || at >= len(l.spots) {
		e.message = "no more places"
		return
	}
	l.at = at
	e.leaveList()
	e.visit(l.spots[at])
	e.message = fmt.Sprintf("(%d of %d) %s", at+1, len(l.spots), strings.TrimSpace(l.lines[at]))
}

// leaveList moves to a window that does not show a list of places, when
// there is one, so going to a place keeps the list in view.
func (e *Session) leaveList() {
	if e.win.buf.spots == nil {
		return
	}
	for _, w := range e.root.windows() {
		if w.buf.spots == nil {
			e.win = w
			return
		}
	}
}

// openList shows the last list of places in a window of its own, the one
// that shows it already or a new one, with the cursor on the place last
// gone to (B194).
func (e *Session) openList() {
	l := e.lastList
	if l == nil {
		e.message = "no list of places"
		return
	}
	if i := slices.IndexFunc(e.root.windows(), func(w *window) bool { return w.buf == l.buf }); i >= 0 {
		e.win = e.root.windows()[i]
	} else {
		at := l.at
		e.showList(l.title, l.lines, l.spots)
		e.lastList.at = at
	}
	t := e.Text()
	e.setCursor(t.LineStart(max(e.lastList.at, 0)))
}

// closeList closes the windows that show the last list of places.
func (e *Session) closeList() {
	for _, w := range e.root.windows() {
		if e.lastList != nil && w.buf == e.lastList.buf {
			if err := e.closeWindow(w); err != nil {
				e.message = err.Error()
				return
			}
		}
	}
}

// listKeys are a list window's own keys, which [keymap.list] changes
// (B142).
var listKeys = map[string]string{"<CR>": "<Plug>(list.open)", "q": ":close<CR>"}

// openSpot jumps to the place on the cursor's line of a list window, such as
// grr's, in another window, keeping the list.
func (e *Session) openSpot() {
	b := e.win.buf
	if b.spots == nil {
		e.message = "not in a list"
		return
	}
	line := e.Text().LineOf(e.Cursor())
	if e.lastList != nil && e.lastList.buf == b {
		e.lastList.at = line
	}
	sp := b.spots[line]
	e.leaveList()
	e.visit(sp)
}

// visit jumps to sp, opening its file when no buffer has it.
func (e *Session) visit(sp spot) {
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

// hover shows the hover replies' text in a float at the cursor, rendered
// from markdown.
func (e *Session) hover(rs []reply) {
	var texts []string
	for _, r := range rs {
		var h struct {
			Contents json.RawMessage `json:"contents"`
		}
		json.Unmarshal(r.res, &h)
		// Contents is markup with a value, a string, or a list of either.
		var parts []json.RawMessage
		if json.Unmarshal(h.Contents, &parts) != nil {
			parts = []json.RawMessage{h.Contents}
		}
		for _, p := range parts {
			if text := markupText(p); text != "" {
				texts = append(texts, text)
			}
		}
	}
	if len(texts) == 0 {
		e.message = "no hover information"
		return
	}
	e.float, e.floatSpans = e.markdown(strings.TrimSpace(strings.Join(texts, "\n\n")))
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
