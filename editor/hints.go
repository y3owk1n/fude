package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// keyGroups name the keys that fude's own keys start with (B172).
var keyGroups = map[string]string{
	"g": "more motions", "gr": "language server", "[": "previous", "]": "next",
	"i": "inside", "a": "around", "z": "scroll", "Z": "write and quit", "<C-w>": "window",
}

// groupName returns the name of the keys that longer ones start with:
// [keymap.groups]'s, else fude's, or "".
func (e *Session) groupName(keys string) string {
	for from, name := range e.cfg.Keymap["groups"] {
		if vim.Notation(withLeader(from, e.cfg.Leader)) == keys {
			return name
		}
	}
	return keyGroups[keys]
}

// keyHints are the keys that may follow the command being typed, with what
// each does and the name of each group (B326).
func (e *Session) keyHints() []api.Hint {
	var out []api.Hint
	for _, h := range e.vim.Hints(slices.Concat(hostBindings(), e.pluginBindings)) {
		hint := api.Hint{Key: h.Key, Help: h.Help, More: h.More}
		if h.To != "" {
			if hint.Help = e.mappingText(h.To); strings.HasPrefix(hint.Help, "types ") {
				hint.Help, hint.Typed = h.To, true
			}
		}
		if h.More > 0 && hint.Help == "" {
			hint.Group = e.groupName(h.Keys)
		}
		out = append(out, hint)
	}
	return out
}

// typing tells the modules the command being typed changed, and closes
// the key hints once none is (B326).
func (e *Session) typing() {
	shown := e.vim.Shown()
	if shown == "" {
		e.keyFloat = nil
	}
	if shown != e.pendingKeys {
		e.pendingKeys, e.keyPage = shown, 0
		e.emit("keys.pending", vim.Notation(shown), e.win.buf)
	}
}

// keysMenu is the page of the key hints that shows, across the bottom of
// the windows, which :center may narrow, above the status line.
func (e *Session) keysMenu() wire.Menu {
	x, w, _ := e.area()
	pages := strings.Split(e.keyFloat.Text, "\f")
	n := min(e.keyPage, len(pages)-1)
	start := 0
	for _, p := range pages[:n] {
		start += len(p) + 1
	}
	text := pages[n]
	var spans []api.Span
	for _, s := range e.keyFloat.Spans {
		if s.Start >= start && s.End <= start+len(text) {
			spans = append(spans, api.Span{Start: s.Start - start, End: s.End - start, Name: s.Name})
		}
	}
	title := vim.Notation(e.vim.Shown())
	if len(pages) > 1 {
		title += fmt.Sprintf("  %d/%d", n+1, len(pages))
	}
	h := strings.Count(text, "\n") + 3
	return wire.Menu{Text: text, Spans: styled(spans), Cursor: -1, Title: title, Lit: true, X: x, Y: max(e.height-1-h, 0), W: w, H: h}
}

// turnPage turns the key hints a page on for <C-d> and back for <C-u>,
// unless the command being typed goes on with that key (B172).
func (e *Session) turnPage(k vim.Key) bool {
	turn := map[vim.Key]int{"<C-d>": 1, "<C-u>": -1}[k]
	if turn == 0 || slices.ContainsFunc(e.keyHints(), func(h api.Hint) bool { return h.Key == string(k) }) {
		return false
	}
	e.keyPage = min(max(e.keyPage+turn, 0), strings.Count(e.keyFloat.Text, "\f"))
	return true
}

// hinted gives a module that hears keys.pending the command being typed,
// its key hints and the room they have, read on the loop (B326).
func (e *Session) hinted(p api.Module, scope *api.Scope) {
	shown := e.vim.Shown()
	if !slices.Contains(p.Subscribed(), "keys.pending") || shown == "" {
		scope.Hints = func() []api.Hint { return nil }
		return
	}
	hints := e.keyHints()
	_, w, _ := e.area()
	scope.Hints = func() []api.Hint { return hints }
	scope.About["pending"] = vim.Notation(shown)
	scope.About["area"] = strconv.Itoa(w)
	scope.About["lines"] = strconv.Itoa(e.height)
}
