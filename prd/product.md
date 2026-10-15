# fude

A modal text editor that works the way Neovim does, without Neovim's weight
and legacy. Built to be as fast and as extendable as an editor can be. fude (筆)
is a writing brush. Language: Go.

## Vision

A small kernel holds buffers, marks, transactions and events. Everything a
user sees, including normal mode, is a module on top. What ships is a
distribution of modules, the way a Linux distro sits on a kernel.

Every command is a value. A keystroke, a macro, a plugin and an AI agent all
produce the same `Command`, so dot repeat, macros, remote clients and agent
proposals share one mechanism.

The syntax tree is a first-class noun. `daf` deletes the enclosing function in
every language fude has a grammar for, from one small query file per
language.

Every edit is a snapshot on a branching timeline. Undo is scrubbing, and any
two moments can be diffed.

The document lives in a headless core. Terminal, GPU and web UIs, a second
machine and an agent are all clients of it.

## The problem

Neovim carries thirty years of Vim compatibility, an in-process Lua runtime
any plugin can stall, and a configuration culture that grows without bound.
Helix and Kakoune are lean but change the grammar. fude starts from how
Neovim works and drops the weight.

## Promises

- **Works like Neovim**: where the spec is silent, a command does what it
  does in Neovim. fude departs where the departure is better, and each one
  is a decision with its reason (D20).
- **Fast**: CI fails when any of these regress, measured on an M-series Mac.
  - Cold start to first frame on an empty buffer under 5ms.
  - Key read to frame written under 1ms at p99, on a 10MB file.
  - A 1GB file shows its first frame in under 1s and scrolls at frame rate.
- **Extendable**: every built-in operator, target, ex command, keymap,
  decoration and UI element uses the API a plugin gets. Anything the
  distribution does, a plugin can do (D21).
- **Reusable kernel and grammar**: `core/` and `vim/` are Go modules any Go
  program can embed. Neither starts goroutines or does I/O. `core/` uses
  only the standard library, and `vim/` works over any text through an
  interface (D22, D23).
- **Small kernel**: `core/` stays under 10k lines of Go, excluding tests.
  CI fails when it grows past that.
- **Plugins cannot stall the editor**: plugins run as sandboxed wasm with
  declared capabilities. A plugin call on the input path is cut off at its
  budget, and a slow or crashing plugin slows or kills only itself.
- **Commands are data**: every edit is a serializable `Command`, inspectable,
  replayable and sendable over the wire.
- **Nothing is lost**: every transaction lands on the timeline and on disk.
  Undo and redo never discard a branch, and a crash loses no edit.
- **Headless first**: every behaviour is reachable without a terminal, which
  is also how fude is tested.

## Boundaries

Permanent edges, not deferrals.

- No vimscript, no Lua runtime, no Vim option compatibility (`:set` names are
  fude's own).
- No in-process plugins. Extension is Go modules in a distribution, wasm
  plugins, or RPC clients.
- No bundled wasm plugins. The default distribution is the vim grammar
  and the syntax, lsp, statusline and file picker modules.
- No package manager for plugins. A plugin is a `.wasm` file and a line of
  config.
- No legacy modes or files: Ex mode, Select mode, modelines and swap files
  are out.
- Text is the source of truth. Lenses render it, they never replace it.

## Known hard parts

- Motion rules: Vim's are full of special cases (exclusive motions that
  become inclusive, `cw` acting like `ce`, linewise `j` on the last line).
  Each one is kept or dropped on purpose, never by accident.
- Regex: Neovim's pattern syntax is not Go's.
- Unicode: grapheme clusters, display width, emoji, combining marks, tabs.
- Terminal quirks: key encoding across terminals, the kitty keyboard
  protocol and its fallback, bracketed paste, truecolor detection.
- Large files: a 1GB file must open and scroll without loading the whole
  syntax tree.
- LSP edge cases: position encodings (UTF-16 vs UTF-8), incremental sync,
  servers that ignore capabilities.
- Ecosystem: Neovim's value is its plugins. fude leans on standards (LSP,
  DAP, tree-sitter queries) to start with something.

## Build order

The order is dependency order, not priority. Behaviours in
`prd/behaviours.md` are tagged with these numbers.

1. Kernel and terminal client: rope, buffers, transactions, render loop,
   normal and insert mode, open and save files, the timeline log, and the
   benchmarks behind the speed promises.
2. Grammar: operators, motions, text objects, counts, registers, dot repeat,
   macros, visual mode, search, ex commands, timeline undo, buffers, splits.
3. Syntax: tree-sitter highlighting, tree text objects, query selection,
   commenting.
4. Plugins: wazero host, capability ABI, event subscriptions, decorations,
   plugin operators and targets.
5. LSP: diagnostics, go to definition, hover, completion, formatting,
   rename, code actions.
6. Server split: RPC protocol, remote and agent clients, proposals,
   multi-client editing.

Not scheduled: lenses, folding, DAP, GPU and web clients.
