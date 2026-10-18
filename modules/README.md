# modules

fude's built-in features, each a module over [`api`](../api) that
`cmd/fude` adds with `editor.Use`. A distribution picks the ones it wants,
and a program that embeds the editor does the same.

| Module | What it does |
|---|---|
| [`autopairs`](autopairs) | a bracket or quote typed comes with its closing one |
| [`blame`](blame) | `:blame` shows who changed the cursor's line |
| [`bufferline`](bufferline) | what the top row lists, the open buffers with files |
| [`buffers`](buffers) | `:buffer`, `:bnext`, `:bprevious`, `:bdelete` and `:enew` |
| [`colours`](colours) | text that names a colour shows on it, and a server's colours |
| [`comment`](comment) | `gc` comments and uncomments |
| [`completion`](completion) | buffer words, paths and user snippets complete in insert mode |
| [`debug`](debug) | `:debug`, breakpoints, stepping, the panel and evaluation |
| [`editorconfig`](editorconfig) | `.editorconfig` files set a file's indentation |
| [`files`](files) | `:pick files` and `:pick buffers` |
| [`flash`](flash) | text just yanked, put, undone or redone shows for a moment |
| [`fold`](fold) | `zc`, `zo`, `za`, `zR` and `zM` fold by indent |
| [`format`](format) | `gq`, `:format` and `format_on_save` |
| [`git`](git) | git signs, hunks, `gh`, `gH`, `:gitdiff`, a listing's git status and the branch on the status line |
| [`grammars`](grammars) | `:grammar` lists, installs and updates the grammars |
| [`grep`](grep) | `:pick grep`, a live search of the files' lines |
| [`help`](help) | `:help` shows help at an entry, and `<CR>` follows a name |
| [`hints`](hints) | the keys that may follow the command being typed, after `key_hints` |
| [`inlay`](inlay) | a language server's inlay hints |
| [`jump`](jump) | `s` jumps to text in view by its label |
| [`links`](links) | `gx` opens the link or file under the cursor, `gf` the file |
| [`listing`](listing) | `-`, a listing's keys, its notes and icons, and `:w` renaming, deleting and creating its entries |
| [`lists`](lists) | `:buffers`, `:marks`, `:jumps`, `:changes`, `:registers` and `:messages` |
| [`lsp`](lsp) | diagnostics, hover, definitions, references, symbols, rename, code actions and the `:lsp` panel |
| [`markdown`](markdown) | a markdown file draws as it reads, with `render` on |
| [`pick`](pick) | `:pick help`, `:pick keymaps` and `:pick commands` |
| [`recent`](recent) | keeps the files windows show, for `:pick recent` and the start screen |
| [`session`](session) | keeps the working directory's session as fude quits, and restores it |
| [`sessions`](sessions) | `:who`, `:kick` and `:follow` |
| [`shell`](shell) | `:!`, `:{range}!` and `:r !` run a command in the shell |
| [`spell`](spell) | `spell`, `]s`, `[s`, `z=` and `zg` |
| [`start`](start) | the start screen of fude started with no file |
| [`surround`](surround) | `ys`, `yss`, `ds`, `cs` and `S` in visual mode surround text |
| [`window`](window) | `<Plug>(window.*)` move to the window that way, or to tmux's pane |
| [`terminal`](terminal) | `:terminal` runs a command, or the shell, in a window below |
| [`timeline`](timeline) | `:timeline`, `:earlier`, `:later`, `:restore`, `:diff` and `:recover` |
| [`task`](task) | `:task` and `:pick tasks` run a project's tasks in a terminal |

A module imports `api`, `protocol` and `internal`, never the editor, and
`just module-imports` checks that. What a module may ask of the editor is
in `api`'s README, and the editor keeps what the modules build on: the
language server lifecycle and document sync, completion and signature
help.

Each module's tests sit beside it and drive a headless editor through
[`editor/editortest`](../editor/editortest), so `go test ./modules/spell`
proves spell's behaviours on their own.

## License

[MIT](../LICENSE)
