package client

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Part is a part of a window's status line: its text, and its role, which
// picks how a frontend draws it (B147). On the left, the mode's role is
// mode.normal, mode.insert, mode.replace, mode.command, mode.visual or
// mode.terminal, and the others are recording, dir, name, modified,
// segment, errors and warnings. On the right the roles are sharing,
// zoomed, other, muted, busy for a spinner, and ruler. Colour is another
// session's colour (B253).
type Part struct {
	Text   string
	Role   string
	Colour int
}

// StatusParts are what v's status line shows on its left and on its right.
// They hold v's name when named is set.
func StatusParts(s Session, v wire.Window, named bool) (left, right []Part) {
	if v.Current {
		label, kind := ModeName(s)
		left = append(left, Part{Text: label, Role: "mode." + kind})
		if r := s.Recording(); r != 0 {
			left = append(left, Part{Text: "@" + string(r), Role: "recording"})
		}
	}
	if named {
		dir, name := SplitName(v.Name)
		left = append(left, Part{Text: dir, Role: "dir"}, Part{Text: name, Role: "name"})
	}
	if v.Modified {
		left = append(left, Part{Role: "modified"})
	}
	for _, seg := range v.LeftSegments {
		left = append(left, Part{Text: seg, Role: "segment"})
	}
	if v.Errors > 0 {
		left = append(left, Part{Text: strconv.Itoa(v.Errors), Role: "errors"})
	}
	if v.Warnings > 0 {
		left = append(left, Part{Text: strconv.Itoa(v.Warnings), Role: "warnings"})
	}
	if v.Sharing != "" {
		right = append(right, Part{Text: v.Sharing, Role: "sharing"})
	}
	// A zoomed window says so, as the others are hidden behind it (B370).
	if v.Zoomed {
		right = append(right, Part{Text: "zoomed", Role: "zoomed"})
	}
	for _, o := range v.Others {
		right = append(right, Part{Text: o.Name, Role: "other", Colour: o.Colour})
	}
	for _, seg := range v.Segments {
		right = append(right, Part{Text: seg, Role: "muted"})
	}
	if v.Current {
		if shown := s.Shown(); shown != "" {
			right = append(right, Part{Text: shown, Role: "muted"})
		}
		// Tasks that run turn a spinner before their names (B231).
		if len(v.Tasks) > 0 {
			right = append(right, Part{Role: "busy"}, Part{Text: strings.Join(v.Tasks, ", "), Role: "muted"})
		}
	}
	// The buffer's language server goes before the filetype. While it
	// works, a spinner and what it is doing go with it (B161).
	if v.Progress != "" {
		doing := v.Progress
		if r := []rune(doing); len(r) > 32 {
			doing = string(r[:31]) + "…"
		}
		right = append(right, Part{Role: "busy"}, Part{Text: v.Server + " " + doing, Role: "muted"})
	} else if v.Server != "" {
		right = append(right, Part{Text: v.Server, Role: "muted"})
	}
	if v.Filetype != "" {
		right = append(right, Part{Text: v.Filetype, Role: "muted"})
	}
	right = append(right, Part{Text: Ruler(v.Ruler), Role: "ruler"})
	return left, right
}

// ModeName is the current window's mode as a status line names it, and its
// kind: normal, insert, replace, command, visual or terminal.
func ModeName(s Session) (string, string) {
	if s.Terminal() {
		return "TERMINAL", "terminal"
	}
	switch s.Mode() {
	case vim.Insert:
		return "INSERT", "insert"
	case vim.Replace:
		return "REPLACE", "replace"
	case vim.Cmdline:
		return "COMMAND", "command"
	case vim.Visual:
		sel, _ := s.Selection()
		return map[vim.Key]string{"v": "VISUAL", "V": "V-LINE", "<C-v>": "V-BLOCK"}[sel.Kind], "visual"
	}
	return "NORMAL", "normal"
}

// SplitName splits a buffer's name into its directory, with a trailing
// slash and the home directory as ~, and the rest. An explorer's name keeps
// its own trailing slash.
func SplitName(name string) (string, string) {
	sep := string(filepath.Separator)
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(name, home+sep); ok {
			name = "~" + sep + rest
		}
	}
	i := strings.LastIndex(strings.TrimSuffix(name, "/"), sep)
	return name[:i+1], name[i+1:]
}

// Ruler shows a Vim ruler, "21,12-13  48%", as the status line does:
// "21:13  48%", with the screen column when it differs.
func Ruler(r string) string {
	f := strings.Fields(r)
	if len(f) < 2 {
		return r
	}
	line, col, _ := strings.Cut(f[0], ",")
	if _, screen, ok := strings.Cut(col, "-"); ok {
		col = screen
	}
	return line + ":" + col + "  " + f[1]
}
