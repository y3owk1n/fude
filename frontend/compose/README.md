# compose

Draws a window of a session into a grid of styled cells: its text and
highlights, the gutter with signs and numbers, ghost text, notes, the
rows a decoration adds, pinned lines, indent guides and the cursors. A
`Grid` keeps a window's cells between frames, and `Paint` gives only the
cells that changed since the last, so a frontend redraws only those.

```go
g := compose.NewGrid(w.W, w.H, look.Base)
out := c.Compose(g, w, compose.Options{Look: look, Frame: frame})
g.Paint(w.X, w.Y, put) // put draws one cell
```

`Compose` returns where the cursor is, the multicursors, the rows of
images and where a float's lines start, for the frontend to draw those
its own way. Cells are as wide as a terminal shows them, so a frontend
draws them in a monospace font.

## License

[MIT](../../LICENSE)
