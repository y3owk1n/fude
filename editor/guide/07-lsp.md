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
- `env` sets variables the server runs with.
- `answers` answer requests the server sends that fude does not answer
  itself, by method.

- `requires` and `excludes` start the server only when one of some files,
  and none of others, is in a folder between the file's and its root.
- `fallback` is the root when no root file is found: `cwd` or `home`.
- `capabilities` change what fude tells the server it can do.
- `connect` connects to a server that is already running, at a host and
  port.

Placeholders fill in a table's strings: `{root}`, `{root_uri}` and
`{root_name}` for the server's root, `{cache}` for a folder fude keeps
for the server and the project, `{env:NAME:-default}`, `{find:path}`
for the nearest path from the root up, and `{pid}`. A `command` may be
a list of commands, and fude runs the first one it finds. A root file
may be a pattern, a group that ranks the same, or a table that asks the
file to hold some text. A filetype's `language_id` changes what its
servers hear the files are written in.

```toml
[lsp.eslint]
command = [
  ["{root}/node_modules/.bin/vscode-eslint-language-server", "--stdio"],
  ["vscode-eslint-language-server", "--stdio"],
]
root = [["package-lock.json", "yarn.lock", "pnpm-lock.yaml"], ".git"]
requires = ["eslint.config.js", { name = "package.json", contains = ["eslintConfig"] }]
fallback = "cwd"

[lsp.eslint.settings]
workspaceFolder = { uri = "{root_uri}", name = "{root_name}" }

[lsp.eslint.answers]
"eslint/confirmESLintExecution" = 4
```

A server that needs more than its table, such as Vue's, whose TypeScript
requests another server answers, takes a plugin. A plugin with `lsp`
hears the messages a server sends that fude does not handle, answers its
requests, sends it notifications, and runs the client commands its code
actions and lenses name, such as `editor.action.showReferences`.

fude watches no files. A server hears of the files fude writes, of an
open file fude finds changed when it comes back into focus, and of what
the explorer renames, creates and deletes, and may edit files for a
rename before it happens, as it does to update imports.

A server whose program is not on PATH says so once, and fude does not try
it again until `:lsp restart`.

## Using a server

- `K` shows what the server says of the name under the cursor.
- `<C-]>` goes to its definition, and `<C-o>` comes back.
- `grr` lists references, `gri` implementations, `grt` the type
  definition and `gO` the file's symbols. `<CR>` on a line of the list
  goes there.
- `grn` renames the name everywhere, and `gra` lists code actions.
- `<Plug>(lsp.declaration)` goes to the declaration,
  `<Plug>(lsp.incoming_calls)` and `<Plug>(lsp.outgoing_calls)` list the
  calls of a function and those it makes, and `<Plug>(lsp.supertypes)` and
  `<Plug>(lsp.subtypes)` the types around a type. They have no keys, so
  map them in `[keymap.normal]`.
- `]d` and `[d` go to the next and previous diagnostic.
- Resting the cursor on a name lights up its other places in the file.
- A server's code lenses, such as "run test", show above their lines, and
  `grx` runs the one on the cursor's line.
- With the cursor in text a server links to other text, such as an HTML
  tag's name, the linked text shows shaded, and what you type in insert
  mode goes in each.
- A server's inline completion, such as Copilot's, shows dim after the
  cursor in insert mode, and `<Tab>` takes it.
- A server's semantic tokens colour names by what they are, such as a
  function or a type, over the syntax's colours.
- `<C-x><C-o>` in insert mode lists completions, and with `autocomplete`
  on they show as you type. `<C-n>` and `<C-p>` select one and `<C-y>`
  takes it. With no menu, `<C-n>` and `<C-p>` complete the word from the
  open buffers' words. `<C-s>` shows the signature of the call being
  typed. A server that formats as you type, such as after `}`, does so
  as part of the insert's undo step.

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

## Debugging

fude debugs a program through a debug adapter, the way it reads code
through a language server. Name the adapter in `config.toml`, and what to debug in
`.fude/launch.toml` in the project.

```toml
# config.toml
[debug.delve]
command = ["dlv", "dap", "--listen", "127.0.0.1:{port}"]

[filetype.go]
debug = ["delve"]
```

```toml
# .fude/launch.toml
[launch.tests]
args = { mode = "test", program = "{dir}" }
```

`<Leader>db` puts a breakpoint on the line, and `<Leader>dc` starts the
session, or lets it go on once it stops. `<Leader>dn`, `<Leader>di` and
`<Leader>do` step over, into and out, and `<Leader>dq` ends it. Each is
a `:debug` command too, such as `:debug next`.

`<Leader>dp` opens a panel of the stopped thread's frames, the chosen
frame's variables and the program's output. `<CR>` on a frame chooses it,
and on a variable shows or hides its members.

`<Leader>de` shows the value of the name under the cursor, or of the
selection, in a float, and `:debug eval expr` shows that of expr.

## Next

- `guide-format` formats files when they are written.
