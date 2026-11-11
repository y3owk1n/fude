module github.com/y3owk1n/fude/frontend

go 1.27

require (
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/clipperhouse/displaywidth v0.11.0
	github.com/y3owk1n/fude/core v0.4.2 // x-release-please-version
	github.com/y3owk1n/fude/vim v0.4.2 // x-release-please-version
	golang.org/x/sys v0.49.0
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
)

replace (
	github.com/y3owk1n/fude/core => ../core
	github.com/y3owk1n/fude/vim => ../vim
)
