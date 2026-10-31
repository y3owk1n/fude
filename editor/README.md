# editor

The editor that fude runs. It is the product, not a library, and it
imports every other package here. It holds:

- its buffers, windows and sessions
- the loop that feeds keys to the vim machine and applies the edits
- the language server host
- the plugin host
- what every module is given

A program imports it to run fude's editor under a client of its own, or to
test a module.

```sh
go get github.com/y3owk1n/fude
```

`go get` takes the newest release. While fude is 0.x, a release that breaks
this API bumps the minor version.

## Running an editor

`editor.Start(args)` starts an editor as the `fude` command does, and
`editor.Open(paths...)` opens files with the default config. Both return a
`Session`, one client's windows and vim machine on the editor. A client
feeds it keys and draws what it reports.

```go
e, err := editor.Open("main.go")
if err != nil {
	return err
}
e.Use(fold.New())  // a module, before Load
e.SetSize(80, 24)
e.Load()           // language servers, plugins and the start screen
e.Feed("j")
for _, v := range e.Windows() {
	fmt.Println(v.Name, v.Cursor)
}
```

- `Feed` takes one key, and `Paste` text. `Wait` says how long to wait for
  the next key of a mapping before `Timeout`.
- `Queue` hands the client the work other goroutines queue for the editor
  loop, such as a module's answer or a server's diagnostics. The client runs
  each func it receives, then draws.
- `Windows`, `Buffers`, `Menu`, `Line`, `Mode` and `Message` are
  what to draw, in the types of [`wire`](../frontend/wire).
- `Use` adds a module over [`api`](../api). `cmd/fude` lists the built-in
  ones, and [`modules`](../modules) holds them.
- `Serve` listens on a socket for clients, `Attach` makes a session for one,
  and `Share` and `ShareRelay` let guests in over TLS.

[`tui`](../tui) is the client fude ships. It runs any
[`client.Session`](../frontend/client), the editor's own or one attached over the
socket.

## Testing on the editor

[`editortest`](editortest) drives a headless editor in tests, for a module
or any program that builds on the editor.

- `Main` gives the tests directories of their own and Go's grammar.
- `Start` and `Open` make a session that quits when the test ends.
- `Feed` types keys.
- `Settle` runs the loop until what the test waits for has happened.

```go
func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

func TestFolds(t *testing.T) {
	e := editortest.Start(t, path)
	e.Use(fold.New())
	editortest.Feed(e, "jzc")
	if e.Windows()[0].Folds[0] != 2 {
		t.Error("zc did not fold the function")
	}
}
```

## What is inside

The package is one Go package on purpose, since splitting it would make
`Session` export an API the size of the package. It is arranged by
feature, one file each, and the spec in [`prd/`](../prd) names what each
file promises. Some parts live elsewhere:

- [`internal`](../internal) holds the parts that need no `Session`: the
  timeline log and the files fude keeps between runs.
- [`frontend/share`](../frontend/share) holds the share's certificates and
  strings, since a guest's client reads them too.

## License

[MIT](../LICENSE)
