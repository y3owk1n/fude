# Configuring fude

fude reads its settings from `config.toml` in `$XDG_CONFIG_HOME/fude`, or
`~/.config/fude` when that variable is unset. Every setting is optional,
and fude starts without the file. A mistake shows with its line when fude
starts, and that setting keeps its default. `:restart` loads the file
again.

## Settings and options

A setting is a key at the top of the file. Options are settings too, but
each window has its own copy, and `:set` changes it for the current
window.

```toml
theme = "~/.config/stylix/palette.json"
leader = "<Space>"
key_timeout = 500
format_on_save = true

number = true
relative_number = true
scroll_off = 8
```

`:set number` turns an option on, `:set nonumber` off, `:set number!`
the other way, and `:set number?` shows it. `:set scroll_off=4` sets a
number. `:set` alone shows every option.

## Filetypes

A `[filetype.<name>]` table applies to the files of one filetype, and
its settings override the ones at the top. A file's filetype is its
language, from its extension or its whole name. `files` adds more names
by pattern.

```toml
[filetype.go]
indent_width = 8
lsp = ["gopls"]
format = ["goimports", "gofumpt"]

[filetype.toml]
files = ["*.toml.tmpl"]
```

`lsp` names the filetype's language servers, and `format` its
formatters. Each server has an `[lsp.<name>]` table, and each formatter
a `[formatter.<name>]` table. `lsp.command` and `formatter.command`
describe them.

## Keymaps

A `[keymap.<mode>]` table maps keys to the keys they type, written as
`<C-x>`, `<CR>` and `<Leader>` are. `<Leader>` stands for the `leader`
setting.

```toml
[keymap.normal]
"<Leader>w" = ":w<CR>"
"<Leader>sg" = ":grep<CR>"
"<Leader>gd" = "<Plug>(git.preview)"

[keymap.groups]
"<Leader>s" = "search"
"<Leader>g" = "git"
```

`[keymap.normal]`, `[keymap.visual]` and `[keymap.insert]` map keys in
those modes. A kind of buffer, such as a listing, has a table of its own
whose keys override the normal mode ones in that buffer. A key mapped to `""` loses its mapping,
and `<Plug>(name)` runs one of fude's commands, which `:h` lists.
`[keymap.groups]` names the keys mappings start with, for the key hints.

## Next

- `settings` lists every setting with its default.
- `options` lists the options.
- `mappings-in-normal-mode` lists the mappings in use.
