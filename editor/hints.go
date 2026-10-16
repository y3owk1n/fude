package editor

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// keyGroups name the keys that fude's own keys start with (B172).
var keyGroups = map[string]string{
	"g": "more motions", "gr": "language server", "[": "previous", "]": "next",
	"i": "inside", "a": "around", "z": "scroll", "Z": "write and quit", "<C-w>": "window",
}

// groupName returns the name of the keys that longer ones start with:
// [keymap.groups]'s, else fude's, or "".
func (e *Session) groupName(keys string) string {
	for from, name := range e.cfg.Keymap.Groups {
		if vim.Notation(withLeader(from, e.cfg.Leader)) == keys {
			return name
		}
	}
	return keyGroups[keys]
}

// hintWidth is the fewest columns a key and what it does take in the hints
// panel. Cells widen to fill its width.
const hintWidth = 34

// HintsIn returns how long the command being typed waits before its hints
// show, or false when none are due (B172).
func (e *Session) HintsIn() (time.Duration, bool) {
	return time.Duration(e.cfg.KeyHints) * time.Millisecond, e.cfg.KeyHints > 0 && e.hints == nil && e.vim.Shown() != ""
}

// ShowHints shows the keys that may follow the command being typed, which
// stay while it goes on. A mapping's held keys then wait for the next key
// instead of key_timeout (B172).
func (e *Session) ShowHints() {
	if e.hints = e.vim.Hints(hostBindings()); len(e.hints) == 0 {
		e.hints = nil
	}
}

// piece is text of one style in a hints cell.
type piece struct{ text, style string }

// hintPieces are what a hint's cell shows after its key: a group's name and
// count, a key's help, or the keys a mapping types (B172).
func (e *Session) hintPieces(h vim.Hint) []piece {
	help := h.Help
	if h.To != "" {
		if help = mappingText(h.To); strings.HasPrefix(help, "types ") {
			return []piece{{h.To, "hints.typed"}}
		}
	}
	switch {
	case h.More > 0 && help == "":
		name := cmp.Or(e.groupName(h.Keys), "keys")
		return []piece{{"+" + name, "hints.group"}, {fmt.Sprintf(" %d", h.More), "hints.muted"}}
	case h.More > 0:
		return []piece{{help, ""}, {fmt.Sprintf(" +%d", h.More), "hints.muted"}}
	}
	return []piece{{help, ""}}
}

// hintsFloat is the panel across the bottom that lists the hints in
// columns, groups first, above the status line.
func (e *Session) hintsFloat() Float {
	inner := max(e.width-4, hintWidth)
	cols := max((inner+2)/(hintWidth+2), 1)
	cellW := (inner+2)/cols - 2
	hints := slices.Clone(e.hints)
	group := func(h vim.Hint) bool { return h.More > 0 && h.Help == "" && h.To == "" }
	slices.SortStableFunc(hints, func(a, b vim.Hint) int {
		switch {
		case group(a) && !group(b):
			return -1
		case group(b) && !group(a):
			return 1
		}
		return 0
	})
	// Rows past a third of the screen are cut, and the last cell counts the
	// rest.
	rows := (len(hints) + cols - 1) / cols
	if limit := max(e.height/3, 2); rows > limit {
		rows = limit
		more := len(hints) - (rows*cols - 1)
		hints = append(hints[:rows*cols-1], vim.Hint{Key: "…", Help: fmt.Sprintf("%d more", more)})
	}
	keyW := 1
	for _, h := range hints {
		keyW = max(keyW, utf8.RuneCountInString(h.Key))
	}
	keyW = min(keyW, cellW/3)
	var s strings.Builder
	var spans []syntax.Span
	for r := range rows {
		if r > 0 {
			s.WriteString("\n")
		}
		for c := range cols {
			i := c*rows + r
			if i >= len(hints) {
				break
			}
			if c > 0 {
				s.WriteString("  ")
			}
			h := hints[i]
			pieces := append([]piece{{fmt.Sprintf("%-*s", keyW, h.Key), "hints.key"}, {"  ", ""}}, e.hintPieces(h)...)
			// A cell longer than its width ends in an ellipsis.
			room := cellW
			for _, p := range pieces {
				text := []rune(p.text)
				if len(text) > room {
					text = append(text[:max(room-1, 0)], '…')
				}
				if len(text) == 0 {
					break
				}
				start := s.Len()
				s.WriteString(string(text))
				if p.style != "" {
					spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: p.style})
				}
				if room -= len(text); room <= 0 {
					break
				}
			}
			if c < cols-1 {
				s.WriteString(strings.Repeat(" ", max(room, 0)))
			}
		}
	}
	text := strings.TrimRight(s.String(), " ")
	h := rows + 2
	return Float{Text: text, Spans: spans, Cursor: -1, Title: vim.Notation(e.vim.Shown()), X: 0, Y: max(e.height-1-h, 0), W: e.width, H: h}
}
