package vim

import (
	"strings"
	"unicode/utf8"
)

// Key is one key in Vim notation: a character such as "a", or a named key
// such as "<Esc>" or "<C-r>".
type Key string

var names = map[string]Key{
	"esc":      "<Esc>",
	"cr":       "<CR>",
	"enter":    "<CR>",
	"bs":       "<BS>",
	"left":     "<Left>",
	"right":    "<Right>",
	"up":       "<Up>",
	"down":     "<Down>",
	"home":     "<Home>",
	"end":      "<End>",
	"del":      "<Del>",
	"pageup":   "<PageUp>",
	"pagedown": "<PageDown>",
	"lt":       "<",
	"space":    " ",
	"tab":      "\t",
	"s-tab":    "<S-Tab>",
	"nop":      "<Nop>",
	"insert":   "<Insert>",
	"c-@":      "<C-@>",
	"c-space":  "<C-@>",
	"c-^":      "<C-^>",
	"c-]":      "<C-]>",
	"c-\\":     "<C-\\>",
}

// Keys splits Vim notation such as "ihello<Esc>" into keys. A "<" that does
// not start a known name is the character "<". <Plug>(name) is one key, which
// runs the command the host added as name.
func Keys(s string) []Key {
	var ks []Key
	for s != "" {
		if rest, ok := strings.CutPrefix(s, "<Plug>("); ok {
			if end := strings.IndexByte(rest, ')'); end > 0 {
				ks = append(ks, Key("<Plug>("+rest[:end+1]))
				s = rest[end+1:]
				continue
			}
		}
		if end := strings.IndexByte(s, '>'); s[0] == '<' && end > 0 {
			name := strings.ToLower(s[1:end])
			k, ok := names[name]
			if len(name) == 3 && strings.HasPrefix(name, "c-") && name[2] >= 'a' && name[2] <= 'z' {
				k, ok = Key("<C-"+name[2:]+">"), true
			}
			if ok {
				ks = append(ks, k)
				s = s[end+1:]
				continue
			}
		}
		_, n := utf8.DecodeRuneInString(s)
		ks = append(ks, Key(s[:n]))
		s = s[n:]
	}
	return ks
}

func (k Key) isChar() bool { return k != "" && (len(k) == 1 || k[0] != '<') }
