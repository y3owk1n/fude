# modules

fude's built-in features, each a module over [`api`](../api) that
`cmd/fude` adds with `editor.Use`. A distribution picks the ones it wants,
and a program that embeds the editor does the same.

| Module | What it does |
|---|---|
| [`blame`](blame) | `:blame` shows who changed the cursor's line |
| [`colours`](colours) | text that names a colour shows on it, and a server's colours |
| [`comment`](comment) | `gc` comments and uncomments |
| [`debug`](debug) | `:debug`, breakpoints, stepping, the panel and evaluation |
| [`editorconfig`](editorconfig) | `.editorconfig` files set a file's indentation |
| [`fold`](fold) | `zc`, `zo`, `za`, `zR` and `zM` fold by indent |
| [`format`](format) | `gq`, `:format` and `format_on_save` |
| [`git`](git) | git signs, hunks, `gh`, `gH`, `:gitdiff`, a listing's git status and the branch on the status line |
| [`grep`](grep) | `:pick grep`, a live search of the files' lines |
| [`inlay`](inlay) | a language server's inlay hints |
| [`links`](links) | `gx` opens the link or file under the cursor, `gf` the file |
| [`lsp`](lsp) | diagnostics, hover, definitions, references, symbols, rename and code actions |
| [`markdown`](markdown) | a markdown file draws as it reads, with `render` on |
| [`shell`](shell) | `:!`, `:{range}!` and `:r !` run a command in the shell |
| [`spell`](spell) | `spell`, `]s`, `[s`, `z=` and `zg` |
| [`task`](task) | `:task` and `:pick tasks` run a project's tasks in a terminal |

A module imports `api`, `protocol` and `internal`, never the editor, and
`just module-imports` checks that. What a module may ask of the editor is
in `api`'s README, and the editor keeps what the modules build on: the
language server lifecycle and document sync, completion, signature help and
the `:lsp` panel.

Each module's tests sit beside it and drive a headless editor through
[`editor/editortest`](../editor/editortest), so `go test ./modules/spell`
proves spell's behaviours on their own.

## License

[MIT](../LICENSE)
