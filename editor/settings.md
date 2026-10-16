# Settings

The longer explanations help gives the options and settings, each under the
name `:h` finds it by.

## indent_width

How many columns one indent level is, for `>`, `<`, insert mode's `<C-t>`
and `<C-d>`, and the indent guides. 0 means the width of a tab, 8. With
`expand_tab` on, indents are spaces, and otherwise tabs as far as they go. A
`[filetype.<name>]` table sets it for one filetype.

## key_timeout

When the keys typed start a longer mapping, such as `<Leader>` with
`<Leader>w` mapped, fude waits this many milliseconds for the next key. With
none, it runs the mapping the keys complete, or the keys as typed. While the
key hints show, it waits for the next key instead.

## key_hints

How long a command being typed waits before a panel lists the keys that may
follow it. 0 turns the panel off. Keep it below `key_timeout`, or a
mapping's keys run before its hints show.

## large_file

A file larger than this many megabytes opens with syntax colours and
language servers off, and fude says so when it opens one. The default is
2. A file's syntax tree takes about 30 times the file's size in memory,
60MB for a 2MB file.

## center

Starts with the windows centered, as `:center` and `<Plug>(center)`
toggle them. On a wide screen, the windows then sit in its middle, with
blank margins on either side, so the text is in front of you on a wide
monitor. Each window gets `color_column` columns of text, or 100 without
one, and its gutter, so a split side by side doubles the width. When the
windows need more than the screen, they take all of it. The default is
off.

## autocomplete

In insert mode, the language server's completions show as a word or a
trigger character such as `.` is typed, with none put in the text until
`<C-n>` or `<C-p>`. On the command line, the completion menu shows as the
line is typed. Off, `<C-x><C-o>` and `<Tab>` ask for them.

## highlight_colors

Text that names a colour shows on that colour, in black or white text,
whichever reads better. That is `#rgb`, `#rgba`, `#rrggbb` and
`#rrggbbaa`, the CSS functions `rgb()`, `rgba()`, `hsl()` and `hsla()`,
and the colours a language server reports, such as Tailwind's class
names. The default is on.

## autopairs

An opening bracket or quote typed in insert mode comes with its closing
one, before a blank, a line end or a closing bracket. Typing the closing
one steps over it, `<BS>` in an empty pair deletes both, and `<CR>` in
empty brackets opens an indented line between them. A quote after a
letter stays one, as in `don't`. The default is on.

## completion_sources

Where completions come from, in the menu's order. `lsp` is the language
servers' items. `snippet` is the snippets in `snippets/<filetype>.json`
and `snippets/all.json` next to this file, in VS Code's format, whose
prefix starts with the word typed. `path` is the entries of the directory a path before the
cursor names, once it holds a `/`, relative to the file's directory.
`buffer` is the words of the open buffers that start with the word typed,
nearest the cursor first. The menu names the source of each item that is
not a server's. The default is `["lsp", "snippet", "path", "buffer"]`, and
leaving a name out turns that source off.

## auto_install

When a file needs a grammar that is not built yet, fude builds it in the
background, with git and the tree-sitter CLI, which must be on PATH. Off,
only grammars already built load, and `:grammar install` builds one by hand.

## socket

The path of the unix socket clients attach to. Unset, each fude listens at a
socket of its own in `$XDG_RUNTIME_DIR`. A fixed path lets a client on
another machine reach it through `ssh -L`. A second fude finds the path
taken and listens at a socket of its own.

## format_on_save

`:w` formats the buffer before it writes it, through the filetype's `format`
formatters, or else its first language server that formats. The formatting
is one change on the timeline.

## format_timeout

How long formatting before a write may take, in milliseconds. A formatter or
server that takes longer leaves the text as it was, and fude writes it
anyway and says why.

## leader

What `<Leader>` stands for in a mapping, from config or a plugin, when the
mapping is made. Write it as a mapping writes keys, such as `"<Space>"` or
`","`.

## theme

`dark`, `light`, or the path of a base16 scheme, such as stylix's
`palette.json` or a Tinted Theming scheme, whose sixteen colours fude draws
in. A path may start with `~/`. `:theme` switches the theme until fude
quits.

## number

Shows each line's number on the left. With `relative_number` on too, the
cursor's line shows its number and the others their distance from it.

## relative_number

Numbers each line by its distance from the cursor's, for counts such as
`5j`.

## wrap

A line longer than the window goes on over the rows below. Off, each line
takes one row and the view scrolls sideways to keep the cursor in view.

## scroll_off

How many lines to keep in view above and below the cursor's, at most half
the window. `8` keeps eight lines between the cursor and the window's top
and bottom.

## cursor_line

Shades the line the cursor is on, and shows its number in bold.

## list

Shows a tab as `>` and blanks, a trailing space as `-` and a no-break space
as `+`. The indent guides hide while it is on.

## ignore_case

Searches, `:s` and their highlights ignore case. `\C` in a pattern matches
case anyway.

## smart_case

With `ignore_case` on, a pattern with a capital letter matches case. `\c` in
a pattern ignores case anyway.

## expand_tab

Indents with spaces only. `<Tab>` in insert mode types spaces to the next
indent, and `:retab` turns a file's leading tabs into spaces.

## clipboard

A yank, delete or change without a register also copies to the system
clipboard. A put without a register takes the clipboard when something
outside fude changed it, and else the unnamed register. `"_` still copies
nothing.

## sign_column

Keeps the sign column in every window, so the text does not move when a git
or diagnostic sign comes. Off, the column shows while the buffer has signs.

## indent_guides

Draws a faint line at each indent level in a line's leading blanks, and a
blank line shows the guides of the next line with text.

## color_column

Shades this column of the text, counting from 1, on each line. It is also
how many columns of text `:center` leaves, and centered windows end there,
so they shade none. The default is 0, for none.

## icons

Shows nerd font icons left of a listing's names, and in place of the letters
of diagnostic signs. Needs a terminal font with nerd font glyphs.

## keymap.normal

Maps keys in normal mode to the keys they run.

```toml
[keymap.normal]
"<Leader>w" = ":w<CR>"
"-" = ""
```

`<Plug>(name)` runs one of fude's commands. A key mapped to `""` loses its
mapping, fude's own too, and `<Nop>` does nothing.

## keymap.visual

Maps keys in visual mode, as `[keymap.normal]` does in normal mode.

## keymap.insert

Maps keys in insert mode, such as `jk = "<Esc>"`. A typed key that starts a
mapping waits `key_timeout` before it shows.

## keymap.listing

Keys in a listing of a folder, run at once over the normal mode ones. fude's
own are `<CR>`, `<C-s>`, `<C-h>`, `_` and `<C-l>`.

## keymap.list

Keys in a list of places, such as `grr`'s references, where `<CR>` goes to
the place on its line and `q` closes the list.

## keymap.timeline

Keys in the `:timeline` window, where `[` and `]` move the text, `d` diffs,
`<CR>` restores and `q` closes it.

## keymap.finder

Keys in the finders of `:pick`, which are insert mode keys, such
as `<C-n>` and `<C-p>`.

## keymap.float

Keys while the cursor is in a float, such as hover's, where `q` closes it.

## keymap.lsp

Keys in the `:lsp` panel. `r` restarts the server under the cursor, `s`
stops it, `d` keeps it off and `e` lets it start.

## keymap.help

Keys in help, where `<CR>` goes to the name under the cursor and `q` closes
it.

## keymap.scratch

Keys in a window fude writes to show something, such as `:messages`, `:grammar`
or a diff, where `q` closes it.

## keymap.groups

Names for the keys mappings start with, which the key hints show beside the
count of keys that follow.

```toml
[keymap.groups]
"<Leader>s" = "search"
```

## filetype.files

Patterns of file names that have this filetype, matched before the names and
extensions fude knows, such as `["*.toml.tmpl"]` in `[filetype.toml]`.

## filetype.indent_width

`indent_width` for files of this filetype, over the one at the top of
config.toml.

## filetype.lsp

The language servers of this filetype, by the names of their `[lsp.<name>]`
tables, in order. Every one starts for the filetype's files. The first that
formats, renames or gives signature help does it.

```toml
[filetype.go]
lsp = ["gopls", "golangci_lint_ls"]
```

## filetype.format

The formatters of this filetype, by the names of their `[formatter.<name>]`
tables, run in turn when `format_on_save` is on.

```toml
[filetype.go]
format = ["goimports", "gofumpt"]
```

## filetype.format_first

Runs only the first formatter of `format` whose program is on PATH and whose
required root is found, such as biome where a project has its config and
prettierd elsewhere.

## lsp.command

The server's program and arguments, found on PATH. Unset, it is the table's
name alone. `~/` at the start of a word is the home folder.

```toml
[lsp.bashls]
command = ["bash-language-server", "start"]
```

## lsp.root

The files that mark a project's root, most wanted first. fude looks for each
in turn from the file's folder up, and starts one server for each root. With
none found, the root is the file's folder.

```toml
[lsp.gopls]
root = ["go.work", "go.mod", ".git"]
```

## lsp.settings

The server's configuration. fude sends it once the server starts, and
answers the server's requests for a section of it, such as `gopls.ui`.

```toml
[lsp.gopls.settings.gopls]
gofumpt = true
```

## lsp.init_options

What the server gets when it starts, as its initialization options. Some
servers fail to start without it, even empty, as `init_options = {}`.

## lsp.files

Keeps the server to the files that match one of its patterns: a name, such
as `*.go`, or with a slash the end of a path, such as
`.github/workflows/*.yml`.

## lsp.workspace_required

Starts the server only for a file under one of its `root` files, such as a
linter that needs its config.

## formatter.command

The formatter's program and arguments, found on PATH. It reads the text on
stdin and writes it formatted on stdout. `{path}` in it is the file's path.
Unset, it is the table's name alone.

```toml
[formatter.biome]
command = ["biome", "format", "--stdin-file-path", "{path}"]
```

## formatter.root

The files that mark the folder the formatter runs in, most wanted first,
looked for from the file's folder up. With none found, it runs in the file's
folder.

## formatter.workspace_required

Runs the formatter only for a file under one of its `root` files, such as
biome where a project has a `biome.json`.

## plugin.path

The plugin's WebAssembly file, from the folder of config.toml, such as
`plugins/wordcount.wasm`.

## plugin.capabilities

What the plugin may call: `buffer.read`, `buffer.write`, `ui`, `fs.read`,
`process`, `net` and `lsp`. A call outside them gets an error and changes nothing.

## plugin.programs

The programs a plugin with `process` may run, such as `["git"]`.

## plugin.hosts

The hosts a plugin with `net` may reach over http and https, a redirect too,
such as `["api.github.com"]`.

## plugin.settings

A table of the plugin's own settings, such as `{ depth = 3, style = "box" }`,
which it reads by name. Each is a number, a string or a boolean.
