package editor

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/lsp"
)

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
