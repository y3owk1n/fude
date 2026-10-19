# syntax

tree-sitter highlighting and syntax-aware text objects for Go programs, with grammars that build on first use. It is the syntax package fude runs, and it imports nothing else from fude.

```sh
go get github.com/y3owk1n/fude/syntax
```

`go get` takes the newest release. While fude is 0.x, a release that breaks this API bumps the minor version.

The package uses cgo, through tree-sitter's official Go binding, so it needs a C compiler. `Build` also needs `git` and the [tree-sitter CLI](https://tree-sitter.github.io/tree-sitter/cli/).

## Grammars

The package knows each grammar it supports by its repo and a pinned commit. `Build` clones the repo at that commit and builds the grammar with the queries the repo ships. The package fetches nothing until you call `Build`.

```go
syntax.Use("grammars", nil) // where grammars are built and loaded from
if err := syntax.Build("go"); err != nil {
	return err
}
spans, err := syntax.Highlight("go", "", "package main\n\nfunc main() {}\n")
// [{0 7 keyword} {14 18 keyword} {19 23 variable}]
```

`Grammars` lists the grammars the package knows and whether each is built. `Buildable` says whether it can build one.

The second argument to `Use` turns the language an injection names, such as `py` in a Markdown code fence, into a filetype. A nil func takes the name as it is.

## Highlighting a buffer as it changes

`Highlight` parses on your goroutine, which suits short text such as a hover. For a buffer that the user is editing, `New` returns a `Buffer` that parses on a goroutine of its own:

```go
queue := make(chan func(), 16)
h, ok, err := syntax.New("go", queriesDir, queue,
	func() {},                                // a new parse landed
	func(filetype string, err error) {},      // an injected language has no grammar
	func(text string, took time.Duration) {}) // each parse and highlight, for tracing
```

`ok` is false when no grammar for the filetype is built.

- `Changed` takes each edit, with the text from before it.
- `Show` returns the spans for the part of the text a view shows. Until the new spans come, it returns the old ones moved through the edits since.
- `Object` and `Select` find text objects and query captures in the last parse.
- `Context` returns where each enclosing function and class starts, and `LanguageAt` the injected language at a position.

Results come back as funcs on `queue`. Run them on the goroutine that calls the buffer's methods. A document is any `syntax.Text`, and a `kernel.Snapshot` from [`core`](../core) is one as it is.

A query file in `queriesDir/<filetype>/` replaces the shipped query of the same name.

## License

[MIT](../LICENSE)
