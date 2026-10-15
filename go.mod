module github.com/y3owk1n/fude

go 1.26.0

require (
	github.com/gdamore/tcell/v3 v3.6.0
	github.com/y3owk1n/fude/core v0.0.0
	github.com/y3owk1n/fude/vim v0.0.0
)

require (
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/y3owk1n/fude/core => ./core
	github.com/y3owk1n/fude/vim => ./vim
)
