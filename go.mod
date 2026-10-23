module github.com/y3owk1n/fude

go 1.27.0

require (
	github.com/charmbracelet/ultraviolet v0.0.0-20261008173134-6b8d4baf91b4
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/charmbracelet/x/vt v0.1.0
	github.com/clipperhouse/displaywidth v0.11.0
	github.com/gdamore/tcell/v3 v3.6.0
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/y3owk1n/fude/api v0.2.1 // x-release-please-version
	github.com/y3owk1n/fude/core v0.2.1 // x-release-please-version
	github.com/y3owk1n/fude/frontend v0.2.1 // x-release-please-version
	github.com/y3owk1n/fude/plugin v0.2.1 // x-release-please-version
	github.com/y3owk1n/fude/protocol v0.2.1 // x-release-please-version
	github.com/y3owk1n/fude/syntax v0.2.1 // x-release-please-version
	github.com/y3owk1n/fude/vim v0.2.1 // x-release-please-version
	golang.org/x/sys v0.49.0
)

require (
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/tree-sitter/go-tree-sitter v0.25.0 // indirect
	github.com/tree-sitter/tree-sitter-go v0.25.0 // indirect
	github.com/xo/terminfo v1.2.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/term v0.47.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace (
	github.com/y3owk1n/fude/api => ./api
	github.com/y3owk1n/fude/core => ./core
	github.com/y3owk1n/fude/frontend => ./frontend
	github.com/y3owk1n/fude/plugin => ./plugin
	github.com/y3owk1n/fude/protocol => ./protocol
	github.com/y3owk1n/fude/syntax => ./syntax
	github.com/y3owk1n/fude/vim => ./vim
)
