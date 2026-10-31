# Keymaps and key hints

A mapping makes typed keys run other keys. Mappings are tables in
`config.toml`, one table for each mode or kind of buffer.

## Writing a mapping

```toml
[keymap.normal]
"<Leader>w" = ":w<CR>"
"<Leader>sw" = ":pick grep <C-r><C-w><CR>"
"<C-h>" = "<Plug>(window.left)"
"-" = ""

[keymap.insert]
jk = "<Esc>"
```

The left side is the keys typed and the right side the keys they run.

- Keys are written with names such as `<CR>`, `<Esc>`, `<Space>`,
  `<C-x>` for Ctrl+x and `<Leader>`.
- `<Leader>` stands for the `leader` setting, `<Space>` unless set.
- `<Plug>(name)` runs one of fude's commands, which `commands-for-keys`
  lists, and `:plug name` runs one with no key.
- Every key a module or plugin adds runs a plug named for what it does,
  such as `<Plug>(lsp.rename)` for `grn`.
  `"<Leader>r" = "<Plug>(lsp.rename)"` gives rename another key, and a
  mapping of `grn` itself wins over the module's.
- A key mapped to `""` loses its mapping, fude's own too.
- `<Nop>` makes a key do nothing.
- The keys a mapping runs are never mapped again.

## Kinds of buffer

Some buffers have keys of their own, each kind in its own table:

- `[keymap.explorer]` is for explorers.
- `[keymap.list]` is for lists of places.
- `[keymap.timeline]` is for the `:timeline` window.
- `[keymap.finder]` is for the finders of `:pick`.
- `[keymap.float]` is for floats.
- `[keymap.lsp]` is for the language server panel.
- `[keymap.help]` is for help.
- `[keymap.scratch]` is for the windows fude writes, such as `:messages`
  and a diff.
- A module's kind takes a table by its name, as `[keymap.pins]` does for
  `:pin list`.

A key mapped there runs in that kind of buffer at once, before the
normal mode mappings. In each of them but an explorer, `q` closes the
window.

## Waiting for a longer mapping

When typed keys start a longer mapping, fude waits `key_timeout`
milliseconds for the next key. If none comes, the keys run as they are.

## Key hints

Type the start of a command and wait `hints.delay` milliseconds. A panel
above the status line lists every key that may come next and what it
does.

- A key that starts longer ones is a group, listed first with how many
  keys it leads to.
- Name a group of your own mappings with `[keymap.groups]`.

```toml
[keymap.groups]
"<Leader>s" = "search"
```

While the hints show:

- The keys typed wait for the next one.
- `<BS>` goes back a key, and `<Esc>` drops the command.
- When the keys do not fit, the panel shows them a page at a time, with
  the page in its title, as `g  1/3`. `<C-d>` and `<C-u>` turn the pages.

`:set hints.enabled false` turns the hints off.

## Next

- `mappings-in-normal-mode` lists the mappings in use.
- `commands-for-keys` lists the `<Plug>` commands.
