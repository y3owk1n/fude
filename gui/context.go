package gui

import (
	"slices"

	"github.com/y3owk1n/fude/vim"
)

// contextItems are what a right click on text offers, the language
// server's items for the place clicked, then the clipboard's. Each has its
// title, the keys it types and the language server feature it needs, and
// "" is a separator (B561).
var contextItems = []struct{ title, keys, needs string }{
	{"Go to Definition", "<C-]>", "definition"},
	{"Go to Type Definition", "grt", "typeDefinition"},
	{"Find References", "grr", "references"},
	{"Find Implementations", "gri", "implementation"},
	{"Rename Symbol", "grn", "rename"},
	{"Code Actions", "gra", "codeAction"},
	{"Show Hover", "K", "hover"},
	{},
	{"Cut", "", ""},
	{"Copy", "", ""},
	{"Paste", "", ""},
	{},
	{"Select All", "", ""},
}

// contextState says which of contextItems a right click shows and which it
// enables. The language server's items show when the window's servers
// offer them, and the separator after them goes with them. Cut and Copy
// take a selection, and Paste text on the clipboard.
func contextState(offers []string, visual, pasted bool) (shown, enabled []bool) {
	shown, enabled = make([]bool, len(contextItems)), make([]bool, len(contextItems))
	offered := false
	for i, it := range contextItems {
		shown[i], enabled[i] = true, true
		switch {
		case it.needs != "":
			shown[i] = slices.Contains(offers, it.needs)
			offered = offered || shown[i]
		case it.title == "Cut" || it.title == "Copy":
			enabled[i] = visual
		case it.title == "Paste":
			enabled[i] = pasted
		case it.title == "" && contextItems[i-1].needs != "":
			shown[i] = offered
		}
	}
	return shown, enabled
}

// act types keys in the mode the window is in: normal in normal mode,
// insert in insert mode and visual in visual mode. Other modes take none.
func (w *window) act(normal, insert, visual string) {
	keys := map[string]string{"normal": normal, "insert": insert, "visual": visual}[w.tabs.mode]
	for _, k := range vim.Keys(keys) {
		w.keys <- k
	}
}

// contextAct types the keys of a language server's item of contextItems,
// in normal mode or after <Esc>. Code Actions take a selection too.
func (w *window) contextAct(keys string) {
	visual := "<Esc>" + keys
	if keys == "gra" {
		visual = keys
	}
	w.act(keys, "<Esc>"+keys, visual)
}

// edits are the keys the system's editing commands type, by title, in
// normal, insert and visual mode. Cut and Copy go through the system's
// clipboard, the + register.
var edits = map[string][3]string{
	"Undo":       {"u", "<C-o>u", "<Esc>u"},
	"Redo":       {"<C-r>", "<C-o><C-r>", "<Esc><C-r>"},
	"Cut":        {"", "", `"+d`},
	"Copy":       {"", "", `"+y`},
	"Select All": {"ggVG", "<Esc>ggVG", "<Esc>ggVG"},
}

// edit types what the system's editing command of the title types.
func (w *window) edit(title string) {
	e := edits[title]
	w.act(e[0], e[1], e[2])
}
