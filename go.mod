module github.com/y3owk1n/fude

go 1.26.0

require (
	github.com/gdamore/tcell/v3 v3.6.0
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/tetratelabs/wazero v1.12.0
	github.com/tree-sitter/go-tree-sitter v0.25.0
	github.com/tree-sitter/tree-sitter-go v0.25.0
	github.com/y3owk1n/fude/core v0.0.0
	github.com/y3owk1n/fude/vim v0.0.0
)

require (
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/y3owk1n/fude/core => ./core
	github.com/y3owk1n/fude/vim => ./vim
)
