<div align="center">

# fude

**Vim's grammar, with everything built in.**

A modal editor for the terminal and a native window, built from Go libraries you can use on their own.

[![License](https://img.shields.io/github/license/y3owk1n/fude?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/status-alpha-orange?style=flat-square)](#status)
[![Sponsor](https://img.shields.io/badge/sponsor-%E2%9D%A4-30363D?style=flat-square)](https://github.com/sponsors/y3owk1n)

macOS | Linux | Windows

[Install](#install) | [Tour](#first-steps) | [Built in](#built-in) | [Recover](#nothing-is-lost) | [Window](#in-a-window) | [Together](#edit-together) | [Plugins](#plugins) | [Libraries](#build-on-fude) | [Speed](#speed)

</div>

---

![fude on its own source: the finder, key hints, live grep, hover, deleting and restoring a function, completion, editing every match and the timeline](demos/fude.gif)

fude (筆) is a writing brush. It is an editor for people who already think in Vim, and a set of Go modules for people who build editors.

## Why fude

- **Your Vim keys, exactly.** `ciw`, `dap`, `.`, macros, registers and `:s` do what they do in Neovim.
- **Everything built in.** Language servers, tree-sitter, a finder, git, a debugger and live sharing, with no plugins to wire up.
- **One TOML file.** Settings and keymaps are data. Nothing in your config runs.
- **Plugins can't freeze you.** They run as sandboxed WebAssembly with a time limit on every call.
- **Nothing is lost.** Every edit lands on a branching timeline on disk, so a crash loses nothing and undo never drops a branch.
- **Fast, and tested to stay fast.** A key draws in under 1ms at p99 on 10MB of Go, and a test fails when it doesn't.
- **In a terminal or a window.** The same editor runs in any true colour terminal, or in the system's own window on macOS, Linux and Windows.
- **A library too.** The kernel, the Vim grammar, the syntax, the LSP and DAP clients, and the session a frontend draws are Go modules you can import.

## Install

```sh
oku add github:y3owk1n/fude     # with oku, man pages included
go install github.com/y3owk1n/fude/cmd/fude@latest
```

Each [release](https://github.com/y3owk1n/fude/releases) also has zips for macOS, Linux and Windows on arm64 and amd64. On macOS and Linux, fude builds grammars with the `tree-sitter` CLI.

`fude --gui` opens fude in a window of the system's own:

- **macOS.** A native window. oku also puts `Fude.app` in `~/Applications`, and the macOS zips hold `Fude.app` too. `Fude.app` is unsigned for now, so a copy downloaded in a browser opens only after `xattr -dr com.apple.quarantine Fude.app`.
- **Linux.** A GTK 4 window, so it needs GTK 4, as `libgtk-4-1` on Debian and Ubuntu. oku also adds Fude to the desktop's launcher.
- **Windows.** A Win32 window. oku also adds Fude to the Start menu.

<details>
<summary>Building from source, and what fude uses when it is on PATH</summary>

Building needs Go 1.27 or later and a C compiler, since tree-sitter uses cgo. On Windows that is llvm-mingw's `gcc` and Git for Windows.

```sh
git clone https://github.com/y3owk1n/fude && cd fude
go build -o bin/fude ./cmd/fude
```

| Tool | For |
| :--- | :-- |
| `git` and `tree-sitter`, or `git` and `gcc` on Windows | Building grammars |
| `rg` | `:pick grep`, else `git grep` |
| `pbcopy`, `wl-copy` or `xclip` | The clipboard |
| TinyGo | Building plugins |
| A nerd font | Icons, off by default |

fude needs a terminal with true colour.

</details>

## First steps

```sh
fude main.go
```

- **Edit.** `daf` deletes the function you are in and `cia` changes an argument, in every language with a grammar.
- **Find.** `:pick files` and `:pick grep` search with a live preview.
- **Rewind.** `:timeline` lists every state of the file, diffs any of them and restores one.
- **Ask.** Type part of a command and pause, and key hints show what can follow. `:h guide-start` opens the guide.

## Built in

| Area | What you get |
| :--- | :----------- |
| **Editing** | Jump by label with `s`, edit every match with `<C-n>`, folds, surround, macros, abbreviations, `:s` with a live preview |
| **Timeline** | Branching undo, `:earlier 10m`, `:timeline` to diff and restore, `:recover` after a crash |
| **Syntax** | tree-sitter with 49 grammars built on first use, `daf` and `cia`, the enclosing function pinned on top |
| **Language servers** | Several per file, hover, references, code actions with a preview, inlay hints, completion, semantic colours, code lenses, call hierarchies |
| **Debugging** | DAP adapters, breakpoints, stepping and a panel of frames and variables |
| **Finder** | Files, grep, buffers, symbols, diagnostics and help, and `:pin` for files a key away |
| **Splits and tabs** | Splits, floats, tab pages, diff mode with `do` and `dp`, windows that scroll together |
| **Git** | Signs, hunk preview, stage with `gh`, `:gitdiff`, `:blame` |
| **Files** | Edit a folder as text, then `:w` to rename, create and delete |
| **Terminal and tasks** | `:terminal`, and `:task` for just recipes, make targets and npm scripts |
| **Look** | Dark and light themes, any base16 scheme, markdown drawn as it reads, images in kitty, Ghostty, WezTerm and iTerm2 |

## Nothing is lost

![two edits, fude killed before a save, the file on disk unchanged, and :recover bringing both edits back](demos/recover.gif)

fude writes each edit to the file's timeline on disk as you make it, before any save. After a crash, opening the file again offers back every edit since the last save, and `:recover` restores them. `:timeline` lists each one, and edits you do not recover stay on the timeline as a branch.

## Where it drifts from Neovim

Neovim is the reference, and fude departs only where that is better. [`prd/decisions.md`](prd/decisions.md) gives each reason.

- **Gone.** fude has no vimscript, Lua, Ex mode, Select mode, modelines or swap files. The timeline does what swap and undo files did.
- **Config.** It is one TOML file, with fude's own option names such as `relative_number`. Plugins are sandboxed wasm.
- **Search.** fude runs Vim's patterns on a linear-time engine and refuses backreferences, so no pattern can hang the editor.
- **Defaults.** Space is the leader, splits open below and right, brackets close themselves, and indent guides and key hints are on.
- **Keys.** `s` jumps by label, `S` in visual mode surrounds, `<C-n>` adds a region, `=` formats as `gq` does, and `q` closes fude's own windows.
- **Built in.** `:pick` replaces `:grep` and picker plugins, folds come from the grammar or from indent, and language server keys exist only while a server is attached.
- **Restart.** `:restart` keeps unsaved changes, since the timeline holds them.

## In a window

![fude in a macOS window: hover where the mouse rests, Command-click to a definition in a new tab, a dragged selection and the right-click menu](demos/window.gif)

<p align="center">
<img src="demos/window-macos.png" alt="the finder in fude's macOS window" width="32%">
<img src="demos/window-linux.png" alt="the finder in fude's GTK 4 window on Linux" width="32%">
<img src="demos/window-windows.png" alt="the finder in fude's Win32 window on Windows" width="32%">
</p>

`fude --gui` opens fude in the system's own window: AppKit on macOS, GTK 4 on Linux and Win32 with DirectWrite on Windows. `fude share`, `attach` and `join` take `--gui` too.

- **Native chrome.** Buffers are tabs in the title bar, and splits are the system's split views. The lists of the palette, the finder and menus are the system's tables, and on macOS and Linux the status bars are the system's labels.
- **The mouse.** Click, drag, double and triple click, drag a selection to move it, rest on a word for its hover, and hold Command or Control to go to a definition.
- **Input methods and the clipboard.** Composed text shows at the cursor, and the system's copy and paste work.
- **Screen readers.** VoiceOver, Orca and Narrator read each window's text with its cursor and selection, and the status bars, the palette, menus and tabs. They speak each message as it shows.

`[gui]` in the config sets the window's font. The text takes the first family in `font` that the system has, so one config works on every system.

```toml
[gui]
font = ["JetBrains Mono", "Menlo", "Cascadia Mono"]
font_size = 13
```

## Edit together

![ana shares a file and lets ben in, they edit at once, ben's u undoes only his edit, and ben follows ana into a tab page](demos/together.gif)

```sh
fude attach            # another session of the fude running here
fude share             # let someone in over TLS
fude join <string>     # on their machine
```

- Everyone has their own cursor, colour, windows and undo.
- `:follow ana` keeps your view on what ana sees.
- A guest edits only the project, and `fude join --read-only` watches without editing.
- Agents attach the same way and propose edits you `:accept` or `:reject`.

## Configure

fude reads `~/.config/fude/config.toml`, and every key in it is optional.

```toml
theme = "dark"
relative_number = true

[keymap.normal]
"<Leader><Leader>" = ":pick files<CR>"

[filetype.go]
lsp = ["gopls"]
format = ["goimports", "gofumpt"]

[module.autopairs]
enabled = false
```

`:h settings` lists every key, and `:set` changes one while fude runs.

## Plugins

A plugin is Go built into WebAssembly with TinyGo. It runs in a sandbox and gets only the capabilities its table grants.

Install one from its repo:

```sh
fude plugin add github.com/y3owk1n/fude-org
```

It shows what the plugin asks for, and once you answer y, adds its table to `config.toml`:

```toml
[[plugin]]
repo = "github.com/y3owk1n/fude-org"
capabilities = ["buffer.read", "buffer.write", "ui"]
```

- fude installs a plugin from the repo's release, and `plugins.lock` beside `config.toml` pins the commit.
- `fude plugin install` installs what the lock pins on a new machine, and `fude plugin update` updates them.
- `:plugin` lists the plugins, `:plugin update` updates them, and `:plugin disable {name}` turns one off.
- `path` in place of `repo` loads a `.wasm` file, or a folder of a plugin's source that fude builds.

Write one:

```go
func init() {
	sdk.Operator("zu", "upper-cases what a motion covers", func(r sdk.Range) {
		text, err := sdk.Text(r.Start, r.End)
		if err != nil {
			sdk.Log(err.Error())
			return
		}
		sdk.Edit(r.Start, r.End, strings.ToUpper(text))
	})
}
```

- `zu` now works with every motion, count, macro and `.`.
- `fude plugin new upper` starts one, with its manifest and a release workflow, and `fude plugin build` builds it.
- `:h guide-plugins` covers the rest.

Plugins others have written carry the [`fude-plugin`](https://github.com/topics/fude-plugin) topic on GitHub, such as [fude-org](https://github.com/y3owk1n/fude-org) for org mode and [fude-supermaven](https://github.com/y3owk1n/fude-supermaven) for Supermaven completion. Give yours the topic so others find it.

## Build on fude

Every layer is a Go module with a small API, and the editor uses each one the way your program would.

| Module | What it is | Needs |
| :----- | :--------- | :---- |
| [`core`](core) | A text buffer with marks and a branching timeline, on a persistent rope | The standard library |
| [`vim`](vim) | Vim's grammar as a state machine over your own text and host | Two Unicode packages |
| [`syntax`](syntax) | tree-sitter highlighting and syntax text objects | cgo |
| [`protocol`](protocol) | LSP and DAP clients that never block your loop | The standard library |
| [`api`](api), [`plugin`](plugin), [`sdk`](sdk) | The module API, the wasm plugin host and its Go SDK | wazero |
| [`frontend`](frontend) | A session to draw, in this process or another fude, the wire format, themes and windows composed into cells | `core` and `vim`, no cgo |
| [`editor`](editor), [`modules`](modules) | The whole editor headless, and its 49 built-in modules | fude's root module |
| [`tui`](tui), [`gui`](gui) | The terminal client and the native window, both frontends of `client.Session` | fude's root module, plus tcell for the terminal and purego for the window |

```sh
go get github.com/y3owk1n/fude/core github.com/y3owk1n/fude/vim
```

- A distribution of your own is a list of modules handed to `editor.Use`.
- A frontend of your own gets its `client.Session` from the editor in the same process, or from `client.Attach` to a running fude.

Each package's README shows how to start.

## Speed

Each line is a test, measured on an Apple M-series Mac, and `just check` fails when one regresses.

| Promise | Limit | Measured |
| :------ | :---: | :------: |
| Cold start to first frame | 5ms | 3ms |
| Key to frame, p99, 10MB of Go with syntax | 1ms | 0.6ms |
| The same on a 50MB line, wrapped or not | 1ms | 0.6ms |
| Key to frame, p99, with every module, git, a language server and a plugin | 2ms | 0.9ms |
| A scroll with everything the modules draw for it, p99 | 5ms | 2ms |
| Undo and redo on a 10,000-step timeline, p99 | 1ms | 0.2ms |
| A key after a burst of 10,000 edits, every module on, p99 | 1ms | 0.4ms |
| `:%s` over 10MB of Go, median | 50ms | 7.5ms |
| A search key that misses in 10MB of Go, median | 2ms | 1.7ms |
| Through `fude attach`, p99 | 5ms | 0.7ms |
| First frame of a 1GB file | 1s | 0.53s |

Memory stays flat too. A hundred files opened, edited and closed, or sixty terminals run and ended, leave nothing behind.

## Status

fude is alpha. Releases are 0.x, and the config, plugin ABI, protocol and library APIs may change between them. Keep backups of work you care about.

## Documentation

| | |
| :- | :- |
| `:h guide-start` | The guide, from your first file on |
| `:h settings` | Every setting |
| `fude help` | Every command-line option |
| `fude man` | The man pages |
| [`prd/`](prd) | The spec: what fude promises, how it works and why |

## Contributing

```sh
oku sync && oku allow    # the toolchain from oku.toml
just check               # format, vet, tests with -race, and the promises
```

A change starts from the spec: a behaviour in [`prd/behaviours.md`](prd/behaviours.md), a test named after it, then the code. [`AGENTS.md`](AGENTS.md) holds the conventions.

## Credits

fude follows [Neovim](https://neovim.io) and [Vim](https://www.vim.org) for how an editor behaves.

<details>
<summary>Built on, and following</summary>

fude is built on:

- [tcell](https://github.com/gdamore/tcell)
- [purego](https://github.com/ebitengine/purego)
- [wazero](https://github.com/tetratelabs/wazero)
- [tree-sitter](https://tree-sitter.github.io) and its [Go binding](https://github.com/tree-sitter/go-tree-sitter) with every grammar's authors
- [go-toml](https://github.com/pelletier/go-toml)
- [uax29](https://github.com/clipperhouse/uax29)
- [displaywidth](https://github.com/clipperhouse/displaywidth)
- [charmbracelet/x](https://github.com/charmbracelet/x) and [ultraviolet](https://github.com/charmbracelet/ultraviolet)
- [go-colorful](https://github.com/lucasb-eyer/go-colorful)
- [go-pointer](https://github.com/mattn/go-pointer)
- Go's [x](https://pkg.go.dev/golang.org/x) packages
- [TinyGo](https://tinygo.org)
- [Binaryen](https://github.com/WebAssembly/binaryen)

Much of it follows a project that did it first:

- [oil.nvim](https://github.com/stevearc/oil.nvim)
- [fff.nvim](https://github.com/dmtrKovalenko/fff)
- [which-key.nvim](https://github.com/folke/which-key.nvim)
- [gitsigns.nvim](https://github.com/lewis6991/gitsigns.nvim)
- [mini.diff](https://github.com/nvim-mini/mini.diff)
- [mini.icons](https://github.com/nvim-mini/mini.icons)
- [conform.nvim](https://github.com/stevearc/conform.nvim)
- [undo-glow.nvim](https://github.com/y3owk1n/undo-glow.nvim)
- [nvim-tmux-navigation](https://github.com/alexghergh/nvim-tmux-navigation)
- [fzy](https://github.com/jhawthorn/fzy)
- [CodeMirror's collab module](https://codemirror.net/examples/collab/)
- [base16](https://github.com/chriskempson/base16)
- [Tinted Theming](https://github.com/tinted-theming)
- [stylix](https://github.com/nix-community/stylix)
- [Helix](https://helix-editor.com)
- [Zed](https://zed.dev)

`:pick grep` runs [ripgrep](https://github.com/BurntSushi/ripgrep), and [just](https://github.com/casey/just) and [oku](https://github.com/y3owk1n/oku) run fude's development.

</details>

## Support

One person builds fude in their spare time. If you use it, you can [sponsor it](https://github.com/sponsors/y3owk1n). Also by the same author: [neru](https://github.com/y3owk1n/neru) and [mimi](https://github.com/y3owk1n/mimi).

## License

MIT. See [LICENSE](LICENSE).
