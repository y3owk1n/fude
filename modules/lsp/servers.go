package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
)

// panelTitle is the title of the panel of language servers.
const panelTitle = "[language servers]"

// serverActions are what :lsp takes after it, and what the panel's keys do.
var serverActions = []string{"disable", "enable", "restart", "stop"}

// panelKeys are the keys of the panel of language servers, which
// [keymap.lsp] changes. Each acts on the server under the cursor (B159).
var panelKeys = map[string]string{
	"r": "<Plug>(lsp.restart)", "s": "<Plug>(lsp.stop)", "d": "<Plug>(lsp.disable)", "e": "<Plug>(lsp.enable)",
	"j": "<Plug>(lsp.next)", "k": "<Plug>(lsp.prev)", "<Down>": "<Plug>(lsp.next)", "<Up>": "<Plug>(lsp.prev)",
	"q": "<Plug>(float.close)", "<Esc>": "<Plug>(float.close)",
}

// serverCall runs :lsp, which opens the panel or acts on the current
// file's servers, and the panel's keys (B159, B160).
func (m *Module) serverCall(res *api.Result, s api.Scope, id uint32, cursor, count int) {
	switch id {
	case lspCmd:
		if arg, ok := s.About["complete"]; ok && !strings.Contains(arg, " ") {
			res.Complete = serverActions
			return
		}
		do := strings.TrimSpace(s.About["args"])
		if do == "" {
			p, _ := panel(s)
			if len(p.Cards) == 0 {
				res.Message = "no language servers are set in config.toml"
				return
			}
			res.Show = p
			return
		}
		for _, sv := range s.Servers() {
			if slices.Contains(sv.Filetypes, s.About["filetype"]) {
				res.Servers = append(res.Servers, api.ServerAction{Name: sv.Name, Do: do})
			}
		}
		if len(res.Servers) == 0 {
			res.Message = "no language server is set for this file"
		}
	case nextCard, prevCard:
		p, _ := panel(s)
		if len(p.Cards) == 0 {
			return
		}
		n := max(count, 1)
		if id == prevCard {
			n = -n
		}
		at := min(max(card(p, s.Text.LineOf(cursor))+n, 0), len(p.Cards)-1)
		res.Jump = &api.Jump{Pos: s.Text.LineStart(p.Cards[at])}
	default:
		p, names := panel(s)
		if len(names) > 0 {
			res.Servers = []api.ServerAction{{Name: names[card(p, s.Text.LineOf(cursor))], Do: map[uint32]string{restartKey: "restart", stopKey: "stop", disableKey: "disable", enableKey: "enable"}[id]}}
		}
	}
}

// card is the index of the card with line: the last that starts at or
// above it.
func card(p *api.Panel, line int) int {
	at := 0
	for i, c := range p.Cards {
		if c <= line {
			at = i
		}
	}
	return at
}

// panel is the panel of the language servers config.toml sets, with a card
// for each: its state, version, filetypes, roots and files, as wide as the
// screen allows up to 64 columns. It returns the servers' names, by card.
func panel(s api.Scope) (*api.Panel, []string) {
	columns, _ := strconv.Atoi(s.About["columns"])
	inner := min(64, max(columns-4, 24)) - 4
	var b strings.Builder
	var spans []api.Span
	add := func(text, style string) {
		if style != "" {
			spans = append(spans, api.Span{Start: b.Len(), End: b.Len() + len(text), Name: style})
		}
		b.WriteString(text)
	}
	// A field's value wraps under itself.
	room := inner - 11
	field := func(label, value string) {
		r := []rune(value)
		for i := 0; i == 0 || i < len(r); i += room {
			b.WriteString("\n    ")
			add(fmt.Sprintf("%-7s", label), "comment")
			b.WriteString(string(r[i:min(i+room, len(r))]))
			label = ""
		}
	}
	home := os.Getenv("HOME")
	p := &api.Panel{Title: panelTitle, Float: true, Keys: panelKeys}
	var names []string
	for i, sv := range s.Servers() {
		if i > 0 {
			b.WriteString("\n\n")
		}
		p.Cards = append(p.Cards, strings.Count(b.String(), "\n"))
		names = append(names, sv.Name)
		style := map[string]string{"starting": "lsp.starting", "running": "lsp.running", "failed": "lsp.error"}[sv.State]
		if style == "" {
			style = "comment"
		}
		dot := "○ "
		if len(sv.Roots) > 0 {
			dot = "● "
		}
		add(dot, style)
		add(sv.Name, "markup.strong")
		title := 2 + utf8.RuneCountInString(sv.Name)
		if v := []rune(sv.Version); len(v) > 0 {
			// The state needs room, so a long version, such as clangd's, ends in an ellipsis.
			if fit := inner - title - len(sv.State) - 2; len(v) > fit {
				v = append(v[:max(fit-1, 0)], '…')
			}
			add(" "+string(v), "comment")
			title += 1 + len(v)
		}
		b.WriteString(strings.Repeat(" ", max(inner-title-len(sv.State), 1)))
		add(sv.State, style)
		field("for", strings.Join(sv.Filetypes, ", ")+" files")
		if sv.Why != "" {
			field("why", sv.Why)
		}
		for _, r := range sv.Roots {
			root := r.Path
			if rest, ok := strings.CutPrefix(root, home+"/"); home != "" && ok {
				root = "~/" + rest
			}
			// A long root keeps its end, where it names its folder.
			if r := []rune(root); len(r) > room {
				root = "…" + string(r[len(r)-room+1:])
			}
			field("root", root)
			var files []string
			for k, path := range r.Files {
				f := filepath.Base(path)
				if more := fmt.Sprintf("  +%d", len(r.Files)-k); utf8.RuneCountInString(strings.Join(append(files, f), "  "))+len(more) > room {
					files = append(files, strings.TrimSpace(more))
					break
				}
				files = append(files, f)
			}
			field("files", strings.Join(files, "  "))
		}
	}
	p.Text, p.Spans = b.String(), spans
	// The keys stay at the foot of the panel as the cards scroll.
	var foot strings.Builder
	for i, k := range []string{"r", "restart", "s", "stop", "d", "disable", "e", "enable", "q", "close"} {
		style, word := "markup.strong", k+" "
		if i%2 == 1 {
			style, word = "comment", k+"  "
		}
		p.FootSpans = append(p.FootSpans, api.Span{Start: foot.Len(), End: foot.Len() + len(strings.TrimSpace(word)), Name: style})
		foot.WriteString(word)
	}
	p.Foot = strings.TrimRight(foot.String(), " ")
	return p, names
}
