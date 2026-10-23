# frontend

What a frontend of fude builds on, as a Go module with no cgo and no
dependency on the editor. A native window, a client in another process
or an agent needs this module, which brings `core` and `vim` with it.

- [`client`](client) is the session a frontend draws and types into, one
  in the same process or one of another fude over its socket or a share,
  and a replica of a buffer that edits with Vim's grammar.
- [`wire`](wire) is what goes over the socket: frames, edits and a
  guest's hello.
- [`theme`](theme) is the colours fude draws in and the style of each
  highlight's name.
- [`compose`](compose) draws a window into a grid of styled cells and
  says which cells changed, for a frontend to paint.
- [`share`](share), [`relay`](relay) and [`clipboard`](clipboard) join a
  share, pass it through a relay and reach the system clipboard.

## License

[MIT](../LICENSE)
