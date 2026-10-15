# Glossary

- **Kernel**: the `core/` Go module that owns buffers, marks, transactions
  and the timeline. It knows nothing about modes, keys or files, and any Go
  program can embed it.
- **Host**: the program that embeds the kernel or the vim machine and
  supplies what they need: store, listener and clock for the kernel, text,
  history, clipboard and viewport for the machine. fude's host is
  `editor/`.
- **Machine**: the vim grammar's state machine in `vim/`. Keys go in,
  commands come out and are applied through its host.
- **Module**: a Go package built on the `api/` package. The statusline,
  the file picker, syntax and lsp are modules. The vim grammar is a library
  that `editor/` hosts.
- **Plugin**: a wasm file loaded at runtime. It gets the same API as a
  module, through the ABI, gated by capabilities.
- **Distribution**: the set of modules and default config fude ships. A user
  may build one with different modules.
- **Buffer**: a rope of text plus its marks and timeline. Its path and
  file state belong to `editor/`, not the kernel.
- **Filetype**: a buffer's file extension without the dot, such as `go`. It
  names the `[filetype.<name>]` table in `config.toml` that applies to it.
- **Snapshot**: a buffer's rope root and version at one moment. Free to
  take, safe to read from any goroutine.
- **Version**: a counter on a buffer that moves with every applied edit.
- **Mark**: a position in a buffer that moves with edits. Cursors,
  selections, named marks and decorations are marks.
- **View**: one buffer shown in one window, with its own cursor, selection,
  scroll and jumplist. Owned by a client.
- **Decoration**: a mark range owned by a module, carrying a highlight,
  virtual text or a sign.
- **Transaction**: an atomic, sorted set of edits to one snapshot, with its
  origin, base version and command.
- **Group**: transactions that land as one timeline node, such as
  everything typed in one insert.
- **Origin**: the client a transaction came from. Plugins act within the
  origin of the client that ran them. Undo is scoped by origin.
- **Timeline**: the branching history of a buffer. Each node is a persistent
  snapshot that shares structure with its parent.
- **Command**: a value describing one edit intent: count, register,
  operator, target, text. Keys, macros, plugins and agents all produce
  commands.
- **Request**: a command plus where and how to apply it: buffer, view or
  position, base version, and whether it is a proposal.
- **Operator**: the verb of a command, registered by name, such as delete,
  change, yank or a plugin-defined one.
- **Target**: the noun of a command, registered by name: a motion, a text
  object, a tree query, a line range or the selection.
- **Tree object**: a text object derived from a tree-sitter query, such as
  function, argument or class.
- **Lens**: a view of a buffer that renders the text differently (folded by
  syntax, rendered markdown, hex) and writes edits back to the text.
- **Client**: anything that attaches to the kernel and sends requests. The
  terminal UI is a client.
- **Proposal**: an edit a client offers without applying, shown as ghost
  text. The user accepts it as one node or rejects it.
- **Capability**: a permission a plugin declares, such as reading buffers or
  spawning a process. The host refuses calls outside it.
