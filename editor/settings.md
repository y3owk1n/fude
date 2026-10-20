# Settings

The longer explanations help gives the options and settings, each under the
name `:h` finds it by. `:set name value` changes any of them in the running
fude, as config.toml would, and never writes config.toml. The tables, such
as `keymap` and `filetype`, change in config.toml, and `:config reload`
reads it again.

## indent_width

How many columns one indent level is, for `>`, `<`, insert mode's `<C-t>`
and `<C-d>`, and the indent guides. 0, the default, means `tab_width`. With
`expand_tab` on, indents are spaces, and otherwise tabs as far as they go. A
`[filetype.<name>]` table sets it for one filetype.

## tab_width

How many columns a tab shows as, 8 unless set. Indents count tabs at this
width, so with `tab_width = 4` and `indent_width = 2`, two `>>` make one
tab. A `[filetype.<name>]` table sets it for one filetype.

```toml
[filetype.go]
tab_width = 4
```

## editorconfig

Opening a file reads the `.editorconfig` files in its folder and each one
above it, up to the first that sets `root = true`. Their `indent_style`,
`indent_size` and `tab_width` set the file's `expand_tab`, `indent_width`
and `tab_width`, over config.toml's. `~/.editorconfig` applies to every
file under the home folder, unless a project's `.editorconfig` above the
file sets `root = true`. On unless set.

```toml
editorconfig = false
```

## key_timeout

When the keys typed start a longer mapping, such as `<Leader>` with
`<Leader>w` mapped, fude waits this many milliseconds for the next key. With
none, it runs the mapping the keys complete, or the keys as typed. While the
key hints show, it waits for the next key instead.

## key_hints

How long a command being typed waits before a panel lists the keys that may
follow it. 0 turns the panel off. Keep it below `key_timeout`, or a
mapping's keys run before its hints show.

## flash_time

How long text just yanked, put, undone or redone shows highlighted, in
milliseconds. A change to the buffer clears it sooner. 0 turns it off.

## large_file

A file larger than this many megabytes opens with syntax colours and
language servers off, and fude says so when it opens one. The default is
2. A file's syntax tree takes about 30 times the file's size in memory,
60MB for a 2MB file.

## center

Starts with the windows centered, as `:center` and `<Plug>(theme.center)`
toggle them. On a wide screen, the windows then sit in its middle, with
blank margins on either side, so the text is in front of you on a wide
monitor. Each window gets `color_column` columns of text, or 100 without
one, and its gutter, so a split side by side doubles the width. When the
windows need more than the screen, they take all of it. The default is
off.

## terminal_height

How tall a window from `:terminal` or a task opens, in percent of the
screen's rows, 33 unless set. It is never under 3 rows, and `:resize`
changes it after.

## open

Where each module's windows open, by the module's name: `above`, `below`,
`left` or `right` of the current window, `here` in it, or `float` in the
middle of the screen or at the `cursor`. It wins over where the module
asks, and `:window` typed before a command, such as `:window float help`,
wins over it. A window its module locks opens where the module asks.

```toml
[open]
help = "float"
terminal = "right"
timeline = "left"
```

## split_below

`:split`, `:new`, `<C-w>s` and an explorer's `<C-h>` open the new window
below the current one, or above it when off. A terminal, a task and a
timeline open below either way. On unless set.

## split_right

`:vsplit`, `:vnew`, `<C-w>v` and an explorer's `<C-s>` open the new window
right of the current one, or left of it when off. On unless set.

## autocomplete

In insert mode, the language server's completions show as a word or a
trigger character such as `.` is typed, with none put in the text until
`<C-n>` or `<C-p>`. On the command line, the completion menu shows as the
line is typed. Off, `<C-x><C-o>` and `<Tab>` ask for them.

## git_signs

A file in a git repository shows in the sign column which lines differ
from git's index. The default is on. When it is off, the sign column shows no git signs,
and `]h`, `[h`, `gh` and `gH` still act on the hunks.

## remote_images

An image at an http or https address, such as one a markdown file shows,
loads from the network, in at most 10 seconds and 16MB. The default is
on. `:set remote_images false` stops new ones loading, so opening a file
contacts no server it names.

## highlight_colors

Text that names a colour shows on that colour, in black or white text,
whichever reads better. That is `#rgb`, `#rgba`, `#rrggbb` and
`#rrggbbaa`, the CSS functions `rgb()`, `rgba()`, `hsl()` and `hsla()`,
and the colours a language server reports, such as Tailwind's class
names. The default is on.

## inlay_hints

A language server's inlay hints, such as the names of a call's
parameters or a variable's inferred type, show among the text in a dim
colour, at the place the server gives. They are not part of the text, so
the cursor and edits pass over them. The default is on. Some servers send
hints only when their settings ask for them, such as gopls with
`[lsp.gopls.settings.gopls.hints]`.

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

How long formatting may take, in milliseconds. A formatter or server that
takes longer leaves the text as it was, and fude says why. Before a write,
fude writes the text anyway.

## leader

What `<Leader>` stands for in a mapping, from config or a plugin, when the
mapping is made. Write it as a mapping writes keys, such as `"<Space>"` or
`","`.

## buffer_line

Once two or more files are open, the top row lists them by number and
name, the current one lit and a modified one marked ●. `:b 3` shows
buffer 3. With it off, the windows take the top row and only `:ls` and
`:pick buffers` list the buffers.

## spell_words

The word list spell checks against, one word a line, and
`/usr/share/dict/words` when unset, which macOS ships and Linux has with
the `words` package. Words `zg` adds go in `words.txt` in fude's config
directory, which spell reads too.

## theme

`dark`, `light`, or the path of a base16 scheme, such as stylix's
`palette.json` or a Tinted Theming scheme, whose sixteen colours fude draws
in. A path may start with `~/`. `:theme` switches the theme until fude
quits.

## colors

Replaces the theme's colour for a role, such as `bg = "#101418"` under
`[colors]`. Every style drawn in that role's colour changes with it.
`:set colors.bg #101418` sets one in the running fude. The roles:

- `bg`, `surface` for floats and the bars, `raised` for the selected item,
  and `border` are the grounds.
- `text`, `muted` and `faint` are the text, from the most to the least
  prominent.
- `cursor_line` and `selection` shade those lines, and `search` is the
  ground of search matches. The match a search being typed goes to is on
  `number`.
- `normal`, `insert`, `visual` and `command` are the modes' colours.
- `error`, `warn`, `info` and `hint` are the diagnostics' colours.
- `keyword`, `string`, `function`, `type`, `number`, `comment` and
  `punctuation` are the syntax colours.

## spell

Underlines the words the word list lacks: all the text of Markdown, text
and git commit files but their code and links, and the comments and
strings of other files. `]s` and `[s` go to them, `z=` lists words for
the one under the cursor, and `zg` adds it to your words. A filetype
table may turn it on, as `[filetype.markdown] spell = true`.

## number

Shows each line's number on the left. With `relative_number` on too, the
cursor's line shows its number and the others their distance from it.

## relative_number

Numbers each line by its distance from the cursor's, for counts such as
`5j`.

## wrap

A line longer than the window goes on over the rows below. Off, each line
takes one row and the window scrolls sideways to keep the cursor in view.

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

## inc_search

While a search is typed, the window shows the match it goes to, lit apart
from the other matches, and `<Esc>` scrolls the window back. It is on unless
set off.

## sticky_context

When the top of a window is inside a function or a class whose first line
has scrolled out of view, that first line stays pinned over the window's
top row, on the surface colour, as nvim-treesitter-context does. Nested
ones pin too, the outermost first, at most `sticky_context_lines`, and
never over the cursor's line. It needs the file's grammar. The default is
on, and `:set sticky_context false` turns it off in the window.

## sticky_context_lines

Sticky context pins at most this many lines over a window's top. The
default is 3. When the top is inside more functions and classes than that,
the innermost ones pin.

## diagnostic_notes

After a line with a diagnostic, its message shows in the severity's colour.
The default is on, and `:set diagnostic_notes false` turns the messages off
in every window of the buffer. The signs stay, and `]d` and `<C-w>d` still
show each one.

## render

A markdown file draws as it reads. A heading's `#`s go and its line is on
its level's colour, a code block is on the surface colour without its
fences, bullets, checkboxes, quotes and rules show as marks, and a
table's columns line up between box lines. Emphasis loses its marks, a
link shows its text alone, and an image file shows below its line where
the terminal shows images. The cursor's line, and
a selection's, show their text as it is. The default is on, and
`:set render false` shows the text as it is everywhere.

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

Shows nerd font icons left of an explorer's names, and in place of the letters
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

## keymap.explorer

Keys in an explorer of a folder, run at once over the normal mode ones. fude's
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

## keymap.debug

Keys in the debug panel. `<CR>` chooses the frame under the cursor, or
shows or hides the members of its variable, and `q` closes the panel.

## keymap.help

Keys in help, where `<CR>` goes to the name under the cursor and `q` closes
it.

## keymap.start

Keys on the start screen that `fude` with no file shows, where `f`, `g`,
`r`, `e`, `c` and `q` do what their lines say, a digit opens that recent
file and `<CR>` runs the line under the cursor.

## keymap.terminal

Keys in a terminal or task window in normal mode, where `q` closes the
window once its program has ended.

## keymap.scratch

Keys in a window fude writes to show something, such as `:messages`, `:grammar`
or a diff, where `q` closes it.

## keymap.<kind>

Keys in a kind of buffer a module or plugin adds, such as `pins` for
`:pin list`, as `[keymap.explorer]` is for an explorer. `:h` lists each
kind's keys under its name.

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

## filetype.tab_width

`tab_width` for files of this filetype, over the one at the top of
config.toml.

## filetype.expand_tab

`expand_tab` for files of this filetype. While a window shows one of its
files, the window indents with spaces only, or with tabs when `false`. It
gets its own `expand_tab` back when it shows another file.

```toml
[filetype.yaml]
expand_tab = true
```

## filetype.wrap

`wrap` for files of this filetype, which a window takes while it shows one
of them, as `expand_tab` does.

## filetype.color_column

`color_column` for files of this filetype, which a window takes while it
shows one of them, as `expand_tab` does.

```toml
[filetype.gitcommit]
color_column = 73
```

## filetype.spell

Turns `spell` on or off in a file of the type, over the setting outside
the table. `[filetype.markdown] spell = true` checks the prose you write
and leaves code alone.

## filetype.debug

The debug adapters of files of the type, by the names of their
`[debug.{name}]` tables. `:debug start` uses the first, unless the
launch settings name another.

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

## filetype.format_on_save

`format_on_save` for files of this filetype, over the one at the top of
config.toml. With it on at the top, `false` here writes these files as
they are.

```toml
format_on_save = true

[filetype.markdown]
format_on_save = false
```

## debug.command

The debug adapter's program and arguments, found on PATH. One that
listens on a port, as Delve does, takes `{port}` where the port goes, and
fude picks a free one and dials it. Any other talks on stdin and stdout.

```toml
[debug.delve]
command = ["dlv", "dap", "--listen", "127.0.0.1:{port}"]

[debug.debugpy]
command = ["python3", "-m", "debugpy.adapter"]

[filetype.go]
debug = ["delve"]
```

`.fude/launch.toml` in the project holds what to debug, a table each:

```toml
[launch.tests]
args = { mode = "test", program = "{dir}" }
```

`adapter` names one other than the filetype's first, and `request =
"attach"` attaches to a running program. `{file}`, `{dir}` and
`{project}` in `args` become the current file, its folder and the
project.

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

## task.command

The shell command a task runs, which `:task name` and `:pick tasks` run in
a terminal window below. `{file}`, `{dir}`, `{line}` and `{word}` in it
stand for the current buffer's file, its folder, the cursor's line and the
word under the cursor.

```toml
[task.test-file]
command = "go test {dir}"
```

A project's own tasks go in `.fude/tasks.toml` in its folder, as the same
`[task.<name>]` tables and nothing else.

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
