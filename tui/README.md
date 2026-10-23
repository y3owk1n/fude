# tui

The terminal client fude ships. It draws a session on a tcell screen in
fude's own true colour theme and feeds it the keys and pastes it reads,
with the latency promises of [`prd/product.md`](../prd/product.md) tested
in `perf_test.go`.

```go
e, err := editor.Start(os.Args[1:])
if err != nil {
	return err
}
return tui.Run(e)
```

`Run` takes any [`client.Session`](../client), the editor's own or an
`Attached` one from `client.Attach` or `client.Join`. An `Attached`
session holds the frames another fude sends over its socket or a share,
and `Run` draws them. The interface is what a frontend needs of an
editor: keys in, and the windows, floats, command line, mode and message
out.

Everything the client draws comes as [`wire`](../wire) types, so another
client, for a GPU or the web, draws the same frames.

## License

[MIT](../LICENSE)
