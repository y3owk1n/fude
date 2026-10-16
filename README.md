# fude

A modal text editor that works the way Neovim does, without Neovim's weight
and legacy. Written in Go. fude (筆) is a writing brush.

> **Alpha.** fude has no releases and no version numbers yet. Anything may
> change between commits: the config format, the plugin ABI, the RPC
> protocol and the timeline log on disk. Its author uses it daily on macOS,
> but expect rough edges, and keep backups of work you care about.

## What fude is

fude is a terminal editor for people who think in Vim's grammar. Operators,
motions, text objects, counts, registers, macros, dot repeat, visual mode,
ex commands and splits work as they do in Neovim. Where fude does something
differently, it does so on purpose, and `prd/decisions.md` says why.

Around that grammar fude builds in what most Neovim setups add through
plugins: language servers, formatters, tree-sitter syntax, a fuzzy file
finder and live grep, a directory editor, git signs and hunks, key hints,
and a help system. One `config.toml` configures all of it.

## Why

Neovim carries thirty years of Vim compatibility, an in-process Lua runtime
any plugin can stall, and a configuration culture that grows without bound.
Helix and Kakoune are lean, but they change the grammar. fude starts from
how Neovim works and drops the weight.

- **Small kernel.** `core/` holds buffers, marks, transactions and the
  timeline, in under 10k lines of Go with no dependencies. Everything a
  user sees, normal mode included, sits on top of it.
- **One grammar for every edit.** A keystroke, a macro, a plugin's key and
  a remote client's command all become keys the vim machine runs. So dot
  repeat, macros, plugins and clients share one mechanism.
- **Nothing is lost.** Every edit is a node on a branching timeline that
  fude keeps on disk. Undo never discards a branch, any two states can be
  diffed, and a crash loses no edit.
- **Plugins cannot stall the editor.** Plugins are sandboxed WebAssembly
  with declared capabilities and a time budget for every call. A slow or
  crashing plugin is unloaded, and editing goes on.
- **Headless first.** The editor runs without a terminal. The terminal is
  one client of it, other programs attach over a socket, and the tests
  drive it with no terminal at all.

## What works

- **Editing.** The vim grammar with counts on both sides of an operator,
  registers and the system clipboard, macros stored as editable text,
  marks, search with a linear-time Vim pattern engine, `:s` with a live preview, `:g`, `:normal` and ranges.
- **Timeline.** `u` and `<C-r>` move along a branch, `g-` and `g+` across
  every state, `:earlier 10m` and `:later 1f` by time or writes, and
  `:timeline` lists, diffs and restores any state. Unsaved changes survive
  a crash, and `:recover` brings them back.
- **Syntax.** tree-sitter highlighting for Go built in, and for other
  languages through grammars fude builds the first time a file needs one.
  Languages inside other languages, such as code in markdown, get their own
  colours. `daf` deletes a function, `cic` changes a class's body and
  `dia` deletes an argument.
- **Language servers.** Several servers per file with merged diagnostics,
  hover, definitions, references, symbols, code actions and completion,
  per-project roots, per-server settings, and a `:lsp` panel.
- **Formatting.** Formatters by name, run in turn or the first that
  applies, with the language server as the fallback.
- **Files.** `:files` fuzzy finder, `:grep` live grep, and listings that
  edit a folder the way oil.nvim does: rename a line, `:w`, and the file
  is renamed.
- **Git.** Signs in the sign column, `]h` and `[h` between hunks, a hunk
  preview, and `gH` to put a hunk back as git's index has it.
- **Discovery.** Key hints show what may follow a half-typed command, `:h`
  opens a written guide and a full reference, and `fude --man` prints man
  pages.
- **Sessions.** `:restart` starts fude again in place, with config and
  plugins reloaded and every window, cursor and unsaved change kept.
- **Themes.** Dark and light themes in true colour, or any base16 scheme,
  such as stylix's `palette.json`.

## Where it differs from Neovim

fude follows Neovim where the spec is silent, and departs where the
departure is better. A few of the 24 departures:

- No vimscript, no Lua, no Ex mode, no Select mode, no modelines and no
  swap files. The timeline replaces swap files.
- Options have fude's own names, such as `relative_number` and
  `scroll_off`, set with `:set` or in `config.toml`.
- Search patterns use Vim's syntax on a linear-time engine, so a pattern
  that needs backtracking is refused rather than run.
- No mouse.
- Splits open below and right, and the leader is space.
- `:restart` keeps unsaved changes instead of refusing.

`prd/decisions.md` lists every one, each with its reason.

## Install

fude builds from source. It runs on macOS and Linux, and is developed on
macOS.

You need:

- Go 1.26 or later.
- A C compiler, as fude's tree-sitter binding uses cgo.
- A terminal with true colour.

fude also uses these when they are on PATH:

- `git` and the `tree-sitter` CLI, to build grammars for languages other
  than Go.
- `rg`, for `:grep`, which falls back to `git grep`.
- `pbcopy` on macOS, or `wl-copy` or `xclip` on Linux, for the clipboard.
- TinyGo, to build plugins.
- A nerd font, for icons, which are off by default.

Then:

```sh
git clone https://github.com/y3owk1n/fude
cd fude
go build -o bin/fude ./cmd/fude
```

Put `bin/fude` on your PATH. `go install` does not work yet, because the
repository's modules point at each other through local paths.

## Getting started

```sh
fude main.go          # open a file
fude +42 main.go      # open it at line 42
fude src/             # list a folder
```

Inside fude, `:h` opens help on an index of the guide and the reference,
and `:h guide-start` is the place to begin. Type the start of a command
and wait, and a panel shows every key that may follow.

`fude --man`, `fude --man config` and `fude --man guide` print the
fude(1), fude-config(5) and fude-guide(7) man pages.

## Configuration

fude reads `$XDG_CONFIG_HOME/fude/config.toml`, or
`~/.config/fude/config.toml`. Every setting is optional, and fude never
writes the file.

```toml
theme = "dark"
number = true
relative_number = true
scroll_off = 8
format_on_save = true

[keymap.normal]
"<Leader>w" = ":w<CR>"
"<Leader><Leader>" = ":files<CR>"
"<Leader>sg" = ":grep<CR>"

[keymap.groups]
"<Leader>s" = "search"

[filetype.go]
lsp = ["gopls"]
format = ["goimports", "gofumpt"]

[lsp.gopls]
root = ["go.work", "go.mod", ".git"]

[lsp.gopls.settings.gopls]
gofumpt = true
```

`:h settings` explains every setting, and `:h guide-config`,
`:h guide-lsp` and `:h guide-format` walk through config, language servers
and formatters. `:restart` loads a changed config.

## Plugins

A plugin is a WebAssembly module and a table in `config.toml` that names
what it may do. Plugins are written in Go with fude's sdk and built with
TinyGo.

```sh
fude plugin new wordcount
cd wordcount
fude plugin build
```

```go
package main

import (
	"strings"

	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Operator("zu", "upper-cases the text a motion covers", func(r sdk.Range) {
		text, err := sdk.Text(r.Start, r.End)
		if err != nil {
			sdk.Log(err.Error())
			return
		}
		sdk.Edit(r.Start, r.End, strings.ToUpper(text))
	})
}

func main() {}
```

```toml
[[plugin]]
path = "plugins/wordcount.wasm"
capabilities = ["buffer.read", "buffer.write"]
```

A plugin's operators, objects, commands and keys work like fude's own,
with counts, in macros and with `.`. The capabilities are `buffer.read`,
`buffer.write`, `ui`, `fs.read`, `process` and `net`, the last two limited
to the programs and hosts the table lists. `plugin/testdata` holds small
example plugins, and `:h guide-plugins` covers the rest.

## Clients

fude listens on a unix socket, and other programs attach to it as clients.
A client sees the buffers and sends keys or edits on the same timeline as
the terminal, and each client's `u` undoes only its own changes. A client
may send an edit as a proposal instead, which shows as ghost text until
`:accept` applies it or `:reject` drops it. Go programs attach through the
`client` package, and `:h guide-remote` has the details.

## Performance

`just check` fails when one of these regresses, measured on an Apple
M-series Mac:

| Promise | Limit | Measured |
|---|---|---|
| Cold start to the first frame | 5ms | about 3ms |
| A key read to its frame written, p99, on a 10MB file | 1ms | about 0.8ms |
| A 1GB file's first frame | 1s | about 0.6s |

## How it is built

```
cmd/fude      the binary
ui/term       the terminal client, on tcell
editor        the editor loop, config, windows, help, LSP and plugin glue
vim           the vim grammar: a state machine over any text
core/kernel   buffers, marks, transactions and the timeline
core/rope     a persistent, balanced rope
syntax        tree-sitter parsing and highlights, the only cgo
lsp           language server clients over stdio
plugin        the wasm plugin host, on wazero
sdk           the Go package plugins are written with
api           the types modules share
client        a Go client for fude's socket
```

`core/` imports only the standard library, starts no goroutines and does no
I/O. `vim/` never imports `core/`, and `core/` never imports `vim/`. Nothing
on the input path waits for a language server, a grammar or a plugin.

The spec lives in `prd/`. `prd/product.md` says what fude is for,
`prd/architecture.md` how it works, `prd/behaviours.md` lists what it
promises, each line a numbered behaviour with a test, and
`prd/decisions.md` says why it is shaped this way.

## Embedding the kernel and the grammar

`core/` and `vim/` are Go modules of their own, for any Go program that
wants an editing buffer with a timeline, or a vim grammar over its own
text. The kernel has no dependencies, and the vim machine reads and edits
any text through a small interface.

## Development

[oku](https://github.com/y3owk1n/oku) installs the toolchain from
`oku.toml`, and `just` runs the tasks.

```sh
oku allow
oku sync
just check    # formatting, vet, the tests with -race, the size and speed limits
just run      # build and open fude
just wasm     # build the test plugins
just man      # write the man pages to share/man
```

Changes start from the spec: a behaviour in `prd/behaviours.md`, a test
named after it, then the code. `AGENTS.md` holds the conventions.

## Credits

fude follows [Neovim](https://neovim.io) and [Vim](https://www.vim.org) as
its reference for how an editor behaves, and owes its grammar to them.

It is built on:

- [tcell](https://github.com/gdamore/tcell), for the terminal.
- [wazero](https://github.com/tetratelabs/wazero), which runs plugins
  with no cgo.
- [tree-sitter](https://tree-sitter.github.io) and its
  [Go binding](https://github.com/tree-sitter/go-tree-sitter), and the authors of every grammar and query fude builds.
- [go-toml](https://github.com/pelletier/go-toml), for `config.toml`.
- [uax29](https://github.com/clipperhouse/uax29) and
  [displaywidth](https://github.com/clipperhouse/displaywidth), for
  graphemes and their width.
- [go-colorful](https://github.com/lucasb-eyer/go-colorful),
  [go-pointer](https://github.com/mattn/go-pointer) and Go's
  [x](https://pkg.go.dev/golang.org/x) packages.
- [TinyGo](https://tinygo.org) and
  [Binaryen](https://github.com/WebAssembly/binaryen), which build
  plugins.

Much of what fude builds in follows a plugin or project that did it first:
[oil.nvim](https://github.com/stevearc/oil.nvim) for editing folders,
[fff.nvim](https://github.com/dmtrKovalenko/fff) for the finder and
grep, [which-key.nvim](https://github.com/folke/which-key.nvim) for key
hints, [gitsigns.nvim](https://github.com/lewis6991/gitsigns.nvim) and
[mini.diff](https://github.com/nvim-mini/mini.diff) for git signs and
hunks, [mini.icons](https://github.com/nvim-mini/mini.icons) for icons,
[conform.nvim](https://github.com/stevearc/conform.nvim) for formatting,
[undo-glow.nvim](https://github.com/y3owk1n/undo-glow.nvim) for the flash
of a yank or undo,
[nvim-tmux-navigation](https://github.com/alexghergh/nvim-tmux-navigation)
for moving into tmux panes, [fzy](https://github.com/jhawthorn/fzy) for
fuzzy ranking, [base16](https://github.com/chriskempson/base16) and
[Tinted Theming](https://github.com/tinted-theming) for schemes,
[stylix](https://github.com/nix-community/stylix) for palettes, and
[Helix](https://helix-editor.com) and [Zed](https://zed.dev) for the look.

`:grep` runs [ripgrep](https://github.com/BurntSushi/ripgrep) when it is
installed, and [just](https://github.com/casey/just) and
[oku](https://github.com/y3owk1n/oku) run fude's development.

## License

fude uses the MIT license. Its text is in `LICENSE`.
