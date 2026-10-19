module github.com/y3owk1n/fude/cmd/fude

go 1.26.0

// The program is a module of its own, with no replace lines, so go install
// takes it at a release (D37). Its requirements are the version the modules
// release together, which the release pull request sets. In the workspace
// they come from the checkout.
require (
	github.com/y3owk1n/fude v0.1.0 // x-release-please-version
	github.com/y3owk1n/fude/api v0.1.0 // x-release-please-version
	github.com/y3owk1n/fude/plugin v0.1.0 // x-release-please-version
)
