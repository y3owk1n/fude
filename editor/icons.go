package editor

import (
	"path/filepath"
	"strings"

	"github.com/y3owk1n/fude/wire"
)

// With the icons option, a listing shows a nerd font icon left of each
// name, in its file kind's colour. Diagnostic signs show icons in place of
// letters (B128).
var (
	dirIcon  = wire.Sign{Text: "\U000f024b", Name: "icon.azure"}
	fileIcon = wire.Sign{Text: "\U000f0214", Name: "icon.grey"}
)

// iconNames are the icons of files known by their whole name.
var iconNames = map[string]wire.Sign{
	"go.mod": {Text: "\U000f07d3", Name: "icon.azure"}, "go.sum": {Text: "\U000f07d3", Name: "icon.cyan"}, "go.work": {Text: "\U000f07d3", Name: "icon.purple"},
	"justfile": {Text: "\U000f05b7", Name: "icon.orange"}, "Makefile": {Text: "\U000f1064", Name: "icon.grey"},
	"Dockerfile": {Text: "\U000f0868", Name: "icon.blue"}, ".gitignore": {Text: "\U000f02a2", Name: "icon.purple"},
}

// iconExts are the icons of files by extension.
var iconExts = map[string]wire.Sign{
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

// diagnosticIcons replace the diagnostic letters with the icons option.
var diagnosticIcons = map[string]string{
	"diagnostic.error": "", "diagnostic.warn": "", "diagnostic.info": "", "diagnostic.hint": "",
}

// iconOf returns the icon of a listing's line name.
func iconOf(name string) wire.Sign {
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
