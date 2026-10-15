package editor

import (
	"path/filepath"
	"strings"
)

// With the icons option, a listing shows a nerd font icon left of each
// name, in mini.icons' glyphs and colours. Diagnostic signs show icons in
// place of letters (B128).
var (
	dirIcon  = Sign{"\U000f024b", "icon.azure"}
	fileIcon = Sign{"\U000f0214", "icon.grey"}
)

// iconNames are the icons of files known by their whole name.
var iconNames = map[string]Sign{
	"go.mod": {"\U000f07d3", "icon.azure"}, "go.sum": {"\U000f07d3", "icon.cyan"}, "go.work": {"\U000f07d3", "icon.purple"},
	"justfile": {"\U000f05b7", "icon.orange"}, "Makefile": {"\U000f1064", "icon.grey"},
	"Dockerfile": {"\U000f0868", "icon.blue"}, ".gitignore": {"\U000f02a2", "icon.purple"},
}

// iconExts are the icons of files by extension.
var iconExts = map[string]Sign{
	"go": {"\U000f07d3", "icon.azure"}, "lua": {"\U000f08b1", "icon.azure"}, "md": {"\U000f0354", "icon.grey"},
	"toml": {"", "icon.orange"}, "json": {"\U000f0626", "icon.yellow"}, "yaml": {"", "icon.purple"},
	"yml": {"", "icon.purple"}, "xml": {"\U000f05c0", "icon.orange"}, "csv": {"", "icon.green"},
	"txt": {"\U000f09aa", "icon.yellow"}, "js": {"\U000f031e", "icon.yellow"}, "jsx": {"", "icon.azure"},
	"ts": {"\U000f06e6", "icon.azure"}, "tsx": {"", "icon.blue"}, "html": {"\U000f031d", "icon.orange"},
	"css": {"\U000f031c", "icon.azure"}, "py": {"\U000f0320", "icon.yellow"}, "rs": {"\U000f1617", "icon.orange"},
	"c": {"\U000f0671", "icon.blue"}, "h": {"\U000f0af5", "icon.purple"}, "cpp": {"\U000f0672", "icon.azure"},
	"zig": {"", "icon.orange"}, "java": {"\U000f0b37", "icon.orange"}, "rb": {"\U000f0d2d", "icon.red"},
	"swift": {"\U000f06e5", "icon.orange"}, "kt": {"\U000f1219", "icon.blue"}, "php": {"\U000f031f", "icon.purple"},
	"hs": {"\U000f0c92", "icon.purple"}, "ex": {"", "icon.purple"}, "nix": {"\U000f1105", "icon.azure"},
	"sh": {"", "icon.grey"}, "bash": {"", "icon.green"}, "zsh": {"", "icon.green"},
	"fish": {"", "icon.grey"}, "vim": {"", "icon.green"}, "sql": {"\U000f01bc", "icon.grey"},
	"scm": {"\U000f0405", "icon.green"}, "wat": {"", "icon.purple"}, "diff": {"\U000f0993", "icon.red"},
	"patch": {"\U000f0993", "icon.red"}, "svg": {"\U000f0721", "icon.yellow"}, "pdf": {"\U000f0226", "icon.red"},
	"png": {"\U000f0e2d", "icon.purple"}, "jpg": {"\U000f0225", "icon.orange"}, "jpeg": {"\U000f0225", "icon.orange"},
	"gif": {"\U000f0d78", "icon.azure"}, "zip": {"\U000f05c4", "icon.azure"}, "gz": {"\U000f05c4", "icon.grey"},
	"tar": {"\U000f05c4", "icon.cyan"},
}

// diagnosticIcons replace the diagnostic letters with the icons option.
var diagnosticIcons = map[string]string{
	"diagnostic.error": "", "diagnostic.warn": "", "diagnostic.info": "", "diagnostic.hint": "",
}

// iconOf returns the icon of a listing's line name.
func iconOf(name string) Sign {
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
