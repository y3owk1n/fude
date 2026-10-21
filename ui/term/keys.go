package term

import (
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/y3owk1n/fude/vim"
)

// keys returns ev in Vim notation, or nothing for a key fude does not use
// yet. An Alt chord is <Esc> and the key, as a terminal without kitty sends
// it (D26).
func keys(ev *tcell.EventKey) []vim.Key {
	if ev.Key() == tcell.KeyBacktab || ev.Key() == tcell.KeyTab && ev.Modifiers()&tcell.ModShift != 0 {
		return []vim.Key{"<S-Tab>"}
	}
	if ev.Modifiers()&^tcell.ModShift == tcell.ModAlt {
		if k := key(ev.Key(), 0, ev.Str()); k != "" {
			return []vim.Key{"<Esc>", k}
		}
	}
	if k := key(ev.Key(), ev.Modifiers()&^tcell.ModShift, ev.Str()); k != "" {
		return []vim.Key{modified(k, ev.Modifiers())}
	}
	return nil
}

// modified names an arrow, <Home> or <End> held with Ctrl or Shift, as
// <C-Left> or <S-End>.
func modified(k vim.Key, mods tcell.ModMask) vim.Key {
	switch k {
	case "<Left>", "<Right>", "<Up>", "<Down>", "<Home>", "<End>":
	default:
		return k
	}
	switch {
	case mods&tcell.ModCtrl != 0:
		return "<C-" + k[1:]
	case mods&tcell.ModShift != 0:
		return "<S-" + k[1:]
	}
	return k
}

func key(k tcell.Key, mods tcell.ModMask, str string) vim.Key {
	switch k {
	case tcell.KeyRune:
		switch {
		case mods == 0:
			return vim.Key(str)
		case mods == tcell.ModCtrl && (str == "6" || str == "^"):
			return "<C-^>"
		case mods == tcell.ModCtrl && (str == " " || str == "@" || str == "2"):
			return "<C-@>"
		case mods == tcell.ModCtrl && len(str) == 1:
			return vim.Key("<C-" + strings.ToLower(str) + ">")
		}
	case tcell.KeyEnter:
		return "<CR>"
	case tcell.KeyEsc:
		return "<Esc>"
	case tcell.KeyBackspace:
		return "<BS>"
	case tcell.KeyLeft:
		return "<Left>"
	case tcell.KeyRight:
		return "<Right>"
	case tcell.KeyUp:
		return "<Up>"
	case tcell.KeyDown:
		return "<Down>"
	case tcell.KeyHome:
		return "<Home>"
	case tcell.KeyEnd:
		return "<End>"
	case tcell.KeyDelete:
		return "<Del>"
	case tcell.KeyInsert:
		return "<Insert>"
	case tcell.KeyPgUp:
		return "<PageUp>"
	case tcell.KeyPgDn:
		return "<PageDown>"
	case tcell.KeyTab:
		return "\t"
	}
	return ""
}

// text returns what a key inside a paste stands for.
func text(ev *tcell.EventKey) string {
	switch ev.Key() {
	case tcell.KeyEnter:
		return "\n"
	case tcell.KeyTab:
		return "\t"
	}
	return ev.Str()
}
