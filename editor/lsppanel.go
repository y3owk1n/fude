package editor

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/wire"
)

// disabled is why a server :lsp disable turned off is down. It alone keeps
// the server from starting when a file it serves opens (B160).
const disabled = "disabled"

// notInstalled is why a server whose program is not on PATH is down. It
// keeps the server from starting until :lsp restart or enable.
const notInstalled = "not installed"

// instances are the running servers named name, by root.
func (e *Editor) instances(name string) []*server {
	var out []*server
	for _, s := range e.servers {
		if s.name == name {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *server) int { return strings.Compare(a.root, b.root) })
	return out
}

// lspCommands are what :lsp takes after it.
var lspCommands = []string{"disable", "enable", "restart", "stop"}

// lspCommand runs :lsp sub on the language server name: restart, stop,
// disable to keep it off for every file, or enable. Restart and enable
// attach every buffer it serves that has no server, which also starts one
// that stayed off after crashing (B159, B160).
func (e *Session) lspCommand(sub, name string) {
	switch {
	case name == "":
		e.message = "no language server is set for this file"
		return
	case !slices.Contains(lspCommands, sub):
		e.message = "not an lsp command: " + sub
		return
	}
	if sub != "enable" {
		for _, s := range e.instances(name) {
			s.closing = true
			s.client.Close()
			e.stop(s)
		}
	}
	switch sub {
	case "stop":
		if e.down[name] != disabled {
			e.down[name] = "stopped"
		}
		e.message = "stopped " + name
		return
	case "disable":
		e.down[name] = disabled
	case "enable":
		delete(e.down, name)
	}
	if e.down[name] == notInstalled {
		delete(e.down, name)
	}
	for _, b := range e.bufs {
		if slices.Contains(e.serverNames(b), name) {
			if err := e.attachTo(b, name); err != nil {
				e.message = err.Error()
			}
		}
	}
	e.syncKeys()
	switch {
	case e.down[name] == disabled:
		e.message = name + " is off until :lsp enable"
	case len(e.instances(name)) > 0:
		e.message = name + " is running"
	}
}

// lspKeys are the keys of the panel of language servers, which [keymap.lsp]
// changes. Each acts on the server under the cursor (B159).
var lspKeys = map[string]string{
	"r": "<Plug>(lsp.restart)", "s": "<Plug>(lsp.stop)", "d": "<Plug>(lsp.disable)", "e": "<Plug>(lsp.enable)",
	"j": "<Plug>(lsp.next)", "k": "<Plug>(lsp.prev)", "<Down>": "<Plug>(lsp.next)", "<Up>": "<Plug>(lsp.prev)",
	"q": "<Plug>(float.close)", "<Esc>": "<Plug>(float.close)",
}

// lspCard is a card of the :lsp panel: the server it is about and the line
// of its first row.
type lspCard struct {
	name string
	line int
}

// lspPanel opens the panel of the language servers config.toml sets (B159).
func (e *Session) lspPanel() {
	if len(e.configured()) == 0 {
		e.message = "no language servers are set in config.toml"
		return
	}
	e.focus("")
	e.win.buf.title = "[lsp]"
}

// configured returns the filetypes of each language server config.toml
// sets, by its name.
func (e *Editor) configured() map[string][]string {
	out := map[string][]string{}
	for ft, c := range e.cfg.Filetype {
		for _, name := range c.LSP {
			out[name] = append(out[name], ft)
		}
	}
	return out
}

// lspAt returns the card with the cursor: the last that starts at or above
// its line.
func (e *Session) lspAt() int {
	line := e.win.buf.text.Snapshot().LineOf(e.Cursor())
	at := 0
	for i, c := range e.lspCards {
		if c.line <= line {
			at = i
		}
	}
	return at
}

// lspCardName names the server whose card has the cursor.
func (e *Session) lspCardName() string {
	if len(e.lspCards) == 0 {
		return ""
	}
	return e.lspCards[e.lspAt()].name
}

// lspMove moves the cursor n cards down, or up for a negative n.
func (e *Session) lspMove(n int) {
	if len(e.lspCards) == 0 {
		return
	}
	i := min(max(e.lspAt()+n, 0), len(e.lspCards)-1)
	e.setCursor(e.win.buf.text.Snapshot().LineStart(e.lspCards[i].line))
}

// lspFloat builds the panel of language servers from their state each
// frame. Each server config.toml sets has a card with its state, version,
// roots and files. The card with the cursor is raised, and the keys go
// below the cards. The cursor stays on a card's first row.
func (e *Session) lspFloat() wire.Float {
	w := min(64, max(e.width-4, 24))
	inner := w - 4
	var s strings.Builder
	var spans []syntax.Span
	add := func(text, style string) {
		if style != "" {
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(text), Name: style})
		}
		s.WriteString(text)
	}
	// A field's value wraps under itself.
	room := inner - 11
	field := func(label, value string) {
		r := []rune(value)
		for i := 0; i == 0 || i < len(r); i += room {
			s.WriteString("\n    ")
			add(fmt.Sprintf("%-7s", label), "comment")
			s.WriteString(string(r[i:min(i+room, len(r))]))
			label = ""
		}
	}
	b := e.win.buf
	t := b.text.Snapshot()
	home := os.Getenv("HOME")
	configured := e.configured()
	e.lspCards = e.lspCards[:0]
	var heads []int // where each card's first row starts
	for i, name := range slices.Sorted(maps.Keys(configured)) {
		if i > 0 {
			s.WriteString("\n\n")
		}
		e.lspCards = append(e.lspCards, lspCard{name, strings.Count(s.String(), "\n")})
		heads = append(heads, s.Len())
		running := e.instances(name)
		state, style := "no files", "comment" // no file it serves is open
		switch {
		case len(running) > 0 && running[0].client.Encoding() == "":
			state, style = "starting", "lsp.starting"
		case len(running) > 0:
			state, style = "running", "lsp.running"
		case e.down[name] == disabled || e.down[name] == "stopped" || e.down[name] == notInstalled:
			state = e.down[name]
		case e.down[name] != "":
			state, style = "failed", "lsp.error"
		}
		dot := "○ "
		if len(running) > 0 {
			dot = "● "
		}
		add(dot, style)
		add(name, "markup.strong")
		title := 2 + utf8.RuneCountInString(name)
		if len(running) > 0 {
			if v := []rune(running[0].client.Version()); len(v) > 0 {
				// The state needs room, so a long version, such as clangd's, ends in an ellipsis.
				if fit := inner - title - len(state) - 2; len(v) > fit {
					v = append(v[:max(fit-1, 0)], '…')
				}
				add(" "+string(v), "comment")
				title += 1 + len(v)
			}
		}
		s.WriteString(strings.Repeat(" ", max(inner-title-len(state), 1)))
		add(state, style)
		fts := slices.Sorted(slices.Values(configured[name]))
		field("for", strings.Join(fts, ", ")+" files")
		if state == "failed" {
			field("why", e.down[name])
		}
		for _, sv := range running {
			root := sv.root
			if rest, ok := strings.CutPrefix(root, home+"/"); home != "" && ok {
				root = "~/" + rest
			}
			// A long root keeps its end, where it names its folder.
			if r := []rune(root); len(r) > room {
				root = "…" + string(r[len(r)-room+1:])
			}
			field("root", root)
			var files []string
			for k, o := range sv.bufs {
				f := filepath.Base(o.file.path)
				if more := fmt.Sprintf("  +%d", len(sv.bufs)-k); utf8.RuneCountInString(strings.Join(append(files, f), "  "))+len(more) > room {
					files = append(files, strings.TrimSpace(more))
					break
				}
				files = append(files, f)
			}
			field("files", strings.Join(files, "  "))
		}
	}
	// The keys go under a rule.
	s.WriteString("\n\n")
	spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len(), Name: "markup.rule"})
	s.WriteString("\n")
	for i, k := range []string{"r", "restart", "s", "stop", "d", "disable", "e", "enable", "q", "close"} {
		if i%2 == 0 {
			add(k, "markup.strong")
			s.WriteString(" ")
		} else {
			add(k+"  ", "comment")
		}
	}
	text := strings.TrimRight(s.String(), " ")
	at := e.lspAt()
	if text != t.Slice(0, t.Len()) {
		if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Edits: []kernel.Edit{{End: t.Len(), Text: text}}}); err != nil {
			e.message = err.Error()
		}
	}
	if head := b.text.Snapshot().LineStart(e.lspCards[at].line); e.Cursor() != head {
		e.setCursor(head)
	}
	end := heads[at] + strings.IndexByte(text[heads[at]:], '\n')
	spans = append(spans, syntax.Span{Start: heads[at], End: end + 1, Name: "menu.selected"})
	// The panel sits a fifth of the way down, or higher when it is too tall
	// for that.
	h := min(strings.Count(text, "\n")+3, e.height-2)
	y := max(min(e.height/5, e.height-1-h), 0)
	return wire.Float{Text: text, Spans: spans, Cursor: e.Cursor(), Title: "language servers", X: (e.width - w) / 2, Y: y, W: w, H: h}
}
