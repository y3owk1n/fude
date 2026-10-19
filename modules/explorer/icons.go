package explorer

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// icon is a nerd font glyph and the style it shows in.
type icon struct{ Text, Name string }

// With the icons option, an explorer shows a nerd font icon left of each
// name, in its file kind's colour (B128).
var (
	dirIcon  = icon{Text: "\U000f024b", Name: "icon.azure"}
	fileIcon = icon{Text: "\U000f0214", Name: "icon.grey"}
)

// iconNames are the icons of files known by their whole name.
var iconNames = map[string]icon{
	"go.mod": {Text: "\U000f07d3", Name: "icon.azure"}, "go.sum": {Text: "\U000f07d3", Name: "icon.cyan"}, "go.work": {Text: "\U000f07d3", Name: "icon.purple"},
	"justfile": {Text: "\U000f05b7", Name: "icon.orange"}, "Makefile": {Text: "\U000f1064", Name: "icon.grey"},
	"Dockerfile": {Text: "\U000f0868", Name: "icon.blue"}, ".gitignore": {Text: "\U000f02a2", Name: "icon.purple"},
}

// iconExts are the icons of files by extension.
var iconExts = map[string]icon{
	"go": {Text: "\U000f07d3", Name: "icon.azure"}, "lua": {Text: "\U000f08b1", Name: "icon.azure"}, "md": {Text: "\U000f0354", Name: "icon.grey"},
	"toml": {Text: "", Name: "icon.orange"}, "json": {Text: "\U000f0626", Name: "icon.yellow"}, "yaml": {Text: "", Name: "icon.purple"},
	"yml": {Text: "", Name: "icon.purple"}, "xml": {Text: "\U000f05c0", Name: "icon.orange"}, "csv": {Text: "", Name: "icon.green"},
	"txt": {Text: "\U000f09aa", Name: "icon.yellow"}, "js": {Text: "\U000f031e", Name: "icon.yellow"}, "jsx": {Text: "", Name: "icon.azure"},
	"ts": {Text: "\U000f06e6", Name: "icon.azure"}, "tsx": {Text: "", Name: "icon.blue"}, "html": {Text: "\U000f031d", Name: "icon.orange"},
	"css": {Text: "\U000f031c", Name: "icon.azure"}, "py": {Text: "\U000f0320", Name: "icon.yellow"}, "rs": {Text: "\U000f1617", Name: "icon.orange"},
	"c": {Text: "\U000f0671", Name: "icon.blue"}, "h": {Text: "\U000f0af5", Name: "icon.purple"}, "cpp": {Text: "\U000f0672", Name: "icon.azure"},
	"zig": {Text: "", Name: "icon.orange"}, "java": {Text: "\U000f0b37", Name: "icon.orange"}, "rb": {Text: "\U000f0d2d", Name: "icon.red"},
	"swift": {Text: "\U000f06e5", Name: "icon.orange"}, "kt": {Text: "\U000f1219", Name: "icon.blue"}, "php": {Text: "\U000f031f", Name: "icon.purple"},
	"hs": {Text: "\U000f0c92", Name: "icon.purple"}, "ex": {Text: "", Name: "icon.purple"}, "nix": {Text: "\U000f1105", Name: "icon.azure"},
	"sh": {Text: "", Name: "icon.grey"}, "bash": {Text: "", Name: "icon.green"}, "zsh": {Text: "", Name: "icon.green"},
	"fish": {Text: "", Name: "icon.grey"}, "vim": {Text: "", Name: "icon.green"}, "sql": {Text: "\U000f01bc", Name: "icon.grey"},
	"scm": {Text: "\U000f0405", Name: "icon.green"}, "wat": {Text: "", Name: "icon.purple"}, "diff": {Text: "\U000f0993", Name: "icon.red"},
	"patch": {Text: "\U000f0993", Name: "icon.red"}, "svg": {Text: "\U000f0721", Name: "icon.yellow"}, "pdf": {Text: "\U000f0226", Name: "icon.red"},
	"png": {Text: "\U000f0e2d", Name: "icon.purple"}, "jpg": {Text: "\U000f0225", Name: "icon.orange"}, "jpeg": {Text: "\U000f0225", Name: "icon.orange"},
	"gif": {Text: "\U000f0d78", Name: "icon.azure"}, "zip": {Text: "\U000f05c4", Name: "icon.azure"}, "gz": {Text: "\U000f05c4", Name: "icon.grey"},
	"tar": {Text: "\U000f05c4", Name: "icon.cyan"},
}

// iconOf returns the icon of an explorer's line name.
func iconOf(name string) icon {
	if strings.HasSuffix(name, "/") {
		return dirIcon
	}
	if s, ok := iconNames[name]; ok {
		return s
	}
	if s, ok := iconExts[strings.TrimPrefix(filepath.Ext(name), ".")]; ok {
		return s
	}
	return fileIcon
}

// note is what an explorer shows after the entry at path: where a symlink
// points, * for an executable, or a mark for a pipe, socket or device
// (B127).
func note(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	m := info.Mode()
	switch {
	case m&fs.ModeSymlink != 0:
		to, err := os.Readlink(path)
		if err != nil {
			to = "?"
		}
		return " -> " + to
	case m&fs.ModeNamedPipe != 0:
		return "|"
	case m&fs.ModeSocket != 0:
		return "="
	case m&fs.ModeCharDevice != 0:
		return "%"
	case m&fs.ModeDevice != 0:
		return "#"
	case m.IsRegular() && m&0o111 != 0:
		return "*"
	}
	return ""
}

// Event decorates an explorer's lines with their icons and notes. It
// decorates every line when the explorer reads its directory, icons turns
// on or off, or the config is read again. After an edit it decorates only
// the lines the edit changed (B127, B128, B337).
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, buf := s.About["explorer"], s.About["buffer"]
	if dir == "" || name == "buffer.closed" {
		delete(m.notes, buf)
		return api.Result{}, nil
	}
	t := s.Text
	first, last := 0, t.Lines()-1
	switch event, detail, _ := strings.Cut(name, " "); event {
	case "explorer.read":
		// The names' notes are read again.
		delete(m.notes, buf)
	case "option.set":
		if detail != "icons" {
			return api.Result{}, nil
		}
	case "buffer.changed":
		from, to, _ := strings.Cut(detail, " ")
		start, _ := strconv.Atoi(from)
		end, _ := strconv.Atoi(to)
		first, last = t.LineOf(min(start, t.Len())), t.LineOf(min(end, t.Len()))
	}
	if m.notes[buf] == nil {
		m.notes[buf] = map[string]string{}
	}
	var res api.Result
	for _, d := range s.Decorations() {
		if line := t.LineOf(min(d.Start, t.Len())); line >= first && line <= last {
			res.Undecorate = append(res.Undecorate, d.ID)
		}
	}
	icons, _ := s.Option("icons")
	for line := first; line <= last; line++ {
		start := t.LineStart(line)
		end := t.Len()
		if line+1 < t.Lines() {
			end = t.LineStart(line+1) - 1
		}
		entry := t.Slice(start, end)
		if entry == "" {
			continue
		}
		if icons == "true" {
			ic := iconOf(entry)
			m.ids++
			res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: start, End: start, Sign: ic.Text, Style: ic.Name, Icon: true})
		}
		n, ok := m.notes[buf][entry]
		if !ok {
			n = note(filepath.Join(dir, entry))
			m.notes[buf][entry] = n
		}
		if n != "" {
			m.ids++
			res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: start, End: start, Note: n})
		}
	}
	return res, nil
}
