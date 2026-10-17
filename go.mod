module github.com/y3owk1n/fude

go 1.26.0

require (
	github.com/charmbracelet/ultraviolet v0.0.0-20260303162955-0b88c25f3fff
	github.com/charmbracelet/x/ansi v0.11.7
	github.com/charmbracelet/x/vt v0.1.0
	github.com/clipperhouse/displaywidth v0.11.0
	github.com/gdamore/tcell/v3 v3.6.0
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/y3owk1n/fude/core v0.0.0
	github.com/y3owk1n/fude/plugin v0.0.0
	github.com/y3owk1n/fude/protocol v0.0.0
	github.com/y3owk1n/fude/syntax v0.0.0
	github.com/y3owk1n/fude/vim v0.0.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/charmbracelet/colorprofile v0.4.2 // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/tree-sitter/go-tree-sitter v0.25.0 // indirect
	github.com/tree-sitter/tree-sitter-go v0.25.0 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/y3owk1n/fude/core => ./core
	github.com/y3owk1n/fude/plugin => ./plugin
	github.com/y3owk1n/fude/protocol => ./protocol
	github.com/y3owk1n/fude/syntax => ./syntax
	github.com/y3owk1n/fude/vim => ./vim
)
