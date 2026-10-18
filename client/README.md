# client

A Go client of a running fude, over its socket. A `Replica` keeps its own
copy of a buffer, so its edits show at once, and rebases them on the
server's order when others edit in between. Two clients editing one buffer
converge to the same text.

```go
c, err := client.Dial(socket)
if err != nil {
	return err
}
defer c.Close()
r, err := c.Open(buffer)
if err != nil {
	return err
}
r.Begin()
r.Edit(kernel.Edit{Start: 0, End: 0, Text: "// "})
r.End()
edits, err := r.Sync() // pushes the edit, and pulls what others made
```

`Changed` tells when the server has new edits to pull. `NewEditor` puts
the vim machine over a replica, so a program edits a buffer with Vim's
grammar from another process, as an agent does.

The socket's messages are in [`wire`](../wire), and `:h guide-remote` has
the protocol.

## License

[MIT](../LICENSE)
