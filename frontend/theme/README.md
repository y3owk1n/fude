# theme

The colours fude draws in, for any frontend. `Themes` holds dark and
light, `FromBase16` makes a theme from a base16 scheme, and `With` puts
the colours config.toml's `[colors]` sets over one. `LookOf` gives what
drawing takes: the style of each highlight's name, the colours
diagnostics underline in, and those of signs and icons. `Tint` darkens a
theme for what shows under a modal box.

```go
look := theme.LookOf(theme.Themes["dark"].With(colours))
style, ok := look.Name("function.builtin") // falls back to function
```

A session's `Theme` says which theme to draw in, and its `Styles` the
styles modules defined, which `Look.Restyle` puts over the theme's.

## License

[MIT](../../LICENSE)
