module github.com/y3owk1n/fude/frontend

go 1.27

require (
	github.com/clipperhouse/displaywidth v0.11.0
	github.com/y3owk1n/fude/core v0.2.1 // x-release-please-version
	github.com/y3owk1n/fude/vim v0.2.1 // x-release-please-version
	golang.org/x/sys v0.49.0
)

require github.com/clipperhouse/uax29/v2 v2.7.0 // indirect

replace (
	github.com/y3owk1n/fude/core => ../core
	github.com/y3owk1n/fude/vim => ../vim
)
