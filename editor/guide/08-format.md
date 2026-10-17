# Formatting

With `format_on_save = true`, `:w` formats the file before it writes it.
A formatter is a program that reads the text on stdin and writes it
formatted on stdout. When no formatter applies, the file's language
server formats it.

## Formatters

```toml
format_on_save = true
format_timeout = 500

[filetype.go]
format = ["goimports", "gofumpt"]

[filetype.typescript]
format = ["biome", "prettierd"]
format_first = true

[formatter.goimports]
command = ["goimports", "-srcdir", "{path}"]

[formatter.biome]
command = ["biome", "format", "--stdin-file-path", "{path}"]
root = ["biome.json", "biome.jsonc"]
workspace_required = true

[formatter.prettierd]
command = ["prettierd", "{path}"]
```

A filetype's `format` names its formatters, which run in turn, each on the
output of the one before. With `format_first = true`, only the first that
applies runs.

Each `[formatter.<name>]` table is optional. `command` is the program and
its arguments, the name alone when unset, and `{path}` in it is the
file's path. A formatter runs in the folder of the first `root` file
found above the file, or else the file's folder. With
`workspace_required = true`, it runs only when a root file is found.

A formatter applies when its program is on PATH and its root, when
required, is found. With the tables above, a project with a `biome.json`
formats with biome, and any other with prettierd.

## When formatting fails

A formatter that fails, or takes longer than `format_timeout`
milliseconds, leaves the text as it was, and fude writes it anyway and
says why. The write's message names a formatter that is not installed.
The formatting is one change on the timeline, so `u` undoes it.

## Formatting by hand

`:format` formats the file as a write would, through its formatters or
else its language server, and does not write it. It works whether
`format_on_save` is on or off.

`gq` and a motion format those lines through the language server, such as
`gqip` for a paragraph or `gggqG` for the file.

## Next

- `guide-syntax` colours files and selects by syntax.
