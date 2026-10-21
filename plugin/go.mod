module github.com/y3owk1n/fude/plugin

go 1.27

require (
	github.com/tetratelabs/wazero v1.12.0
	github.com/y3owk1n/fude/api v0.2.0 // x-release-please-version
)

require golang.org/x/sys v0.49.0 // indirect

replace github.com/y3owk1n/fude/api => ../api
