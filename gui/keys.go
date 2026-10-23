package gui

import (
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/vim"
)

// The modifiers held with a key, as each system's window gives them.
const (
	shiftKey = 1 << iota
	controlKey
	altKey   // Option on macOS
	superKey // Command on macOS, Super on Linux and the Windows key
)

// chord returns a key down in Vim notation. named is the key's name when
// it types no character, such as <Esc> or a tab, mods the modifiers held,
// chars what it types and plain what it types without modifiers. An Alt
// chord is <Esc> and the key, as the terminal client reads one (D26). A
// Super chord is the system's, so it gives none.
func chord(named vim.Key, mods int, chars, plain string) []vim.Key {
	switch {
	case mods&superKey != 0:
		return nil
	case named == "\t" && mods&shiftKey != 0:
		return []vim.Key{"<S-Tab>"}
	case named == "\t":
		return []vim.Key{"\t"}
	}
	if named != "" {
		if mods&controlKey != 0 && strings.Contains("<Left><Right><Up><Down><Home><End>", string(named)) {
			return []vim.Key{"<C-" + named[1:]}
		}
		if mods&shiftKey != 0 && strings.Contains("<Left><Right><Up><Down><Home><End>", string(named)) {
			return []vim.Key{"<S-" + named[1:]}
		}
		return []vim.Key{named}
	}
	if mods&controlKey != 0 {
		switch p := strings.ToLower(plain); {
		case p == "6" || p == "^":
			return []vim.Key{"<C-^>"}
		case p == " " || p == "@" || p == "2":
			return []vim.Key{"<C-@>"}
		case utf8.RuneCountInString(p) == 1:
			return []vim.Key{vim.Key("<C-" + p + ">")}
		}
		return nil
	}
	if mods&altKey != 0 && plain != "" {
		return []vim.Key{"<Esc>", vim.Key(plain)}
	}
	var out []vim.Key
	for _, r := range chars {
		out = append(out, vim.Key(string(r)))
	}
	return out
}
