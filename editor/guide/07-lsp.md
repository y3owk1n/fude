# Language servers

A language server is a program that understands one language. fude asks
it for definitions, errors and completions, and starts the servers a
filetype names when one of its files opens.

## Setting up a server

```toml
[filetype.go]
lsp = ["gopls", "golangci_lint_ls"]

[lsp.gopls]
root = ["go.work", "go.mod", ".git"]

[lsp.gopls.settings.gopls]
gofumpt = true

[lsp.golangci_lint_ls]
command = ["golangci-lint-langserver"]
root = [".golangci.yml", "go.mod", ".git"]
```

`lsp` names the filetype's servers, in order. Each `[lsp.<name>]` table
is optional.

- `command` is the program and its arguments, found on PATH. It is the
  name when unset, and `~/` at the start of a word is the home folder.
- `root` names the files that mark a project's root, most wanted first.
  fude looks for each in turn from the file's folder up, and starts one
  server for each root.
- `settings` are what the server reads as its configuration, and
  `init_options` what it gets when it starts.
- `files` keeps the server to some files, by name or the end of the path,
  such as `.github/workflows/*.yml`.
- `workspace_required = true` starts the server only when a root file is
  found, such as a linter's config.

A server whose program is not on PATH says so once, and fude does not try
it again until `:lsp restart`.

## Using a server

- `K` shows what the server says of the name under the cursor.
- `<C-]>` goes to its definition, and `<C-o>` comes back.
- `grr` lists references, `gri` implementations, `grt` the type
  definition and `gO` the file's symbols. `<CR>` on a line of the list
  goes there.
- `grn` renames the name everywhere, and `gra` lists code actions.
- `]d` and `[d` go to the next and previous diagnostic.
- `<C-x><C-o>` in insert mode lists completions, and with `autocomplete`
  on they show as you type. `<C-n>` and `<C-p>` select one and `<C-y>`
  takes it. `<C-s>` shows the signature of the call being typed.

With several servers on a file, every one's diagnostics show, and
hover, places, symbols, code actions and completions come from each server
that offers them. Rename, signature help and formatting go to the first
server in the filetype's list that offers them.

## Watching servers

The status line shows a file's servers, and a spinner with what a server
is doing while it starts or works. `:lsp` opens a panel with every server
and its state, roots and files. In the panel, `r` restarts the server
under the cursor, `s` stops it, `d` keeps it off and `e` lets it start
again. `:lsp restart` restarts the current file's servers.

## Next

- `guide-format` formats files when they are written.
