# fude

A modal text editor that works the way Neovim does, without Neovim's weight
and legacy. Written in Go. Its kernel (`core/`) and its vim grammar (`vim/`) are
Go modules other programs can embed.

The spec lives in `prd/`. Start with `prd/product.md`.

## Development

oku installs the toolchain.

```
oku allow
oku sync
just test
```

## License

fude uses the MIT license. Its text is in `LICENSE`.
