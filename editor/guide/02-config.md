# Configuring fude

fude reads its settings from `config.toml` in `$XDG_CONFIG_HOME/fude`, or
`~/.config/fude` when that variable is unset. Every setting is optional,
and fude starts without the file. A mistake shows with its line when fude
starts, and that setting keeps its default. `:restart` loads the file
again. `fude --clean` starts with the defaults and reads nothing in that
folder. Use it to tell whether a problem comes from your config or from
fude.

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

`:set number true` sets an option as config.toml does, and `:set
scroll_off 4` sets a number. `:set number` shows an option, and `:set`
alone lists them all. `:set` changes the running fude, never config.toml.

## Filetypes

A `[filetype.<name>]` table applies to the files of one filetype, and
its settings override the ones at the top. A file's filetype is its
language, from its extension or its whole name. `files` adds more names
by pattern.

```toml
[filetype.go]
tab_width = 4
lsp = ["gopls"]
format = ["goimports", "gofumpt"]

[filetype.toml]
files = ["*.toml.tmpl"]

[filetype.markdown]
wrap = true
expand_tab = true
```

`expand_tab`, `wrap` and `color_column` hold in a window while it shows a
file of the filetype, and the window gets its own values back when it
shows another file.

`lsp` names the filetype's language servers, and `format` its
formatters. Each server has an `[lsp.<name>]` table, and each formatter
a `[formatter.<name>]` table. `lsp.command` and `formatter.command`
describe them.

## EditorConfig

A project's `.editorconfig` sets the indentation of its files. Its
`indent_style`, `indent_size` and `tab_width` win over config.toml's, and
`~/.editorconfig` works for every file under your home folder.
`editorconfig = false` turns this off.

## Keymaps

A `[keymap.<mode>]` table maps keys to the keys they type, written as
`<C-x>`, `<CR>` and `<Leader>` are. `<Leader>` stands for the `leader`
setting.

```toml
[keymap.normal]
"<Leader>w" = ":w<CR>"
"<Leader>sg" = ":pick grep<CR>"
"<Leader>gd" = "<Plug>(git.preview)"

[keymap.groups]
"<Leader>s" = "search"
"<Leader>g" = "git"
```

`[keymap.normal]`, `[keymap.visual]` and `[keymap.insert]` map keys in
those modes. A kind of buffer, such as a listing, has a table of its own
whose keys override the normal mode ones in that buffer. A key mapped to
`""` loses its mapping, and `<Plug>(name)` runs one of fude's commands.
`[keymap.groups]` names the keys mappings start with, for the key hints.

## Next

- `guide-keys`, `guide-lsp` and `guide-format` go further with keymaps,
  language servers and formatters.
- `settings` lists every setting with its default.
- `options` lists the options.
- `mappings-in-normal-mode` lists the mappings in use.
