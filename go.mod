module github.com/y3owk1n/fude

go 1.26

require (
	github.com/y3owk1n/fude/core v0.0.0
	github.com/y3owk1n/fude/vim v0.0.0
)

require github.com/clipperhouse/uax29/v2 v2.7.0 // indirect

replace (
	github.com/y3owk1n/fude/core => ./core
	github.com/y3owk1n/fude/vim => ./vim
)
