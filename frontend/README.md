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

A program that attaches to a running fude and composes its windows, as a
frontend does before it paints them:

```go
s, err := client.Attach(os.Getenv("FUDE_SOCKET"), "viewer")
if err != nil {
	return err
}
defer s.Close()
s.SetSize(100, 30)
look := theme.LookOf(theme.Themes["dark"])
var c compose.Composer
grids := map[int]*compose.Grid{}
for frame := 1; !s.Done(); frame++ {
	(<-s.Queue())() // apply what fude sent
	for _, w := range s.Windows() {
		g := grids[w.ID]
		if gw, gh := g.Size(); g == nil || gw != w.W || gh != w.H {
			g = compose.NewGrid(w.W, w.H, look.Base)
			grids[w.ID] = g
		}
		c.Compose(g, w, compose.Options{Look: look, Frame: frame})
		g.Paint(w.X, w.Y, func(x, y int, text string, width int, st theme.Style) {
			// Draw text at x, y in st's colours.
		})
	}
}
```

A frontend that draws its own splits calls `s.Place` with each window's
place, and reads `s.Layout` and `s.Pages` for its splits and tabs. One
with a mouse calls `s.Click`, `s.Drag` and `s.Scroll` with the cell of a
window's text it points at. fude finds the text in that cell.

## License

[MIT](../LICENSE)
