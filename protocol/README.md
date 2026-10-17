# protocol

Go clients for the Language Server Protocol and the Debug Adapter Protocol, built on the standard library alone. They are the clients fude uses, and they import nothing else from fude.

| Package | What it does |
|---|---|
| [`lsp`](lsp) | Runs a language server over stdio, keeps its documents in sync and sends requests |
| [`dap`](dap) | Runs a debug adapter over stdio or a TCP port and sends requests |
| [`framing`](framing) | Reads and writes Content-Length frames, which both protocols use |

```sh
go get github.com/y3owk1n/fude/protocol
```

fude has no release yet. Until the first one, `go get` takes the latest commit and the API can change.

## How the clients run

Each client reads and writes on goroutines of its own and never blocks the caller. Everything that comes back runs on your goroutine instead: answers, notifications, the server's own requests and the end of the process. The client sends each one as a func to a `queue` channel you pass in, and your loop runs them. Call a client's methods from that same loop.

## lsp

A document is any `lsp.Text`: `Len`, `Slice`, `Lines`, `LineStart` and `LineOf`, with byte offsets and lines counted from 0. A `kernel.Snapshot` from [`core`](../core) is one as it is.

```go
queue := make(chan func(), 16)
c, err := lsp.Start("gopls", []string{"gopls"}, root, nil, queue,
	func(line string) { log.Print(line) },                          // the server's stderr
	func(text string, took time.Duration) {},                       // each message, for tracing
	func(method string, params json.RawMessage) {},                 // notifications
	func(method string, params json.RawMessage) any { return nil }, // the server's requests
	func(err error) { log.Print("gopls exited: ", err) })
if err != nil {
	return err
}
defer c.Close()

c.Open(uri, "go", text)
done := false
c.Request("textDocument/hover", func(enc string) any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": lsp.Point(text, at, enc)}
}, func(result json.RawMessage) {
	fmt.Println(string(result))
	done = true
})
for !done {
	(<-queue)()
}
```

- **`Start` initializes the server.** It sends `initialize` with the options you pass. It offers `utf-8`, then `utf-16`, as the position encoding. Messages you send before the server answers wait in order.
- **Positions are byte offsets.** `Point` turns one into a protocol position in the encoding the server chose, and `Offset` turns one back. A request builds its params in a func that takes that encoding, so the params are right even when you send before the server has answered.
- **Changes go as edits.** `Change` takes the text before the edits and the edits themselves. It sends ranges, or the whole new text to a server that asked for that.
- **Capabilities.** `Supports` and `Capability` read what the server offered in its answer to `initialize`.
- **Errors.** An error answer goes to the log func, and the answer func gets `nil`, as it does when the server exits before it answers.

## dap

```go
c, err := dap.Start("dlv", []string{"dlv", "dap", "--listen", "127.0.0.1:{port}"}, dir, queue,
	func(line string) { log.Print(line) }, // the adapter's stderr
	func(text string) {},                  // each message sent, for tracing
	func(event string, body json.RawMessage) { fmt.Println(event) },
	func(err error) { log.Print("dlv exited: ", err) })
if err != nil {
	return err
}
c.Request("initialize", map[string]any{"adapterID": "go"}, func(body json.RawMessage, err error) {
	// the adapter's capabilities, or why it refused
})
```

A command with `{port}` in it gets a free port, and `Start` dials it until the adapter listens, its process ends or five seconds pass. A command without one talks over stdio. The client refuses requests from the adapter, such as `runInTerminal`.

## framing

`framing.Read` reads one message body after its headers, and `framing.Write` writes a body with its `Content-Length` header. `framing.Lines` is an `io.Writer` for a server's stderr that logs a line at a time.

## License

[MIT](../LICENSE)
