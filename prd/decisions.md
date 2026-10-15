# Decisions

Each entry: the choice, then why. Reopen one only with new information.

## D1. Go

Why: fast compiles, goroutines fit the client, module and plugin loops, a GC
makes persistent structures natural, and wazero is a pure-Go wasm runtime.
The cost is cgo for tree-sitter and fewer ready-made libraries (rope, CRDT).

## D2. Vim grammar, verb first

Operator then target, as in Vim. Visual mode is the selection-first escape
hatch. Why: the user's muscle memory is vim, and tree objects fit vim's noun
model directly.

## D3. Commands are data

Every edit is a `Command` value before it is a transaction. Why: dot repeat,
plugin-defined verbs and nouns that compose with every existing one, RPC
and agent proposals all fall out of one type. Macros are keys that produce
commands (D15).

## D4. A kernel with modules on top

The kernel knows buffers, marks, transactions, the timeline and events. Modes
and keymaps live in the vim grammar module. Why: it keeps the kernel small
enough to budget (product.md promises), and every module can be replaced.

## D5. Headless kernel, behaviours tested without a terminal

Why: a behaviour is a key sequence in and a buffer state out. Tests drive the
kernel and the grammar module directly, so they are fast and do not depend on
terminal emulation.

## D6. Hand-written persistent rope

Why: no mature Go rope fits. A persistent rope gives O(log n) edits and makes
every timeline node a cheap snapshot through structural sharing.

## D7. Branching timeline for undo

Why: Vim's undo tree is right but hard to navigate. With persistent snapshots
every branch is kept, scrubbing is cheap, and any two nodes can be diffed.

## D8. Plugins are core wasm on wazero with a capability ABI

Not the wasm component model, which wazero does not run. Why: pure Go, no
cgo, sandboxed, any source language. A plugin cannot stall or crash the
editor. wazero stops a plugin cooperatively at loop back-edges, so host
functions must never block, and a stopped plugin loses its state.

## D9. tree-sitter through cgo

Why: grammars are C and there is no complete pure-Go runtime. The per-call
overhead is small because parsing is incremental.

## D10. Reuse standards before inventing

LSP, DAP, tree-sitter grammars and the queries they ship. Why:
the ecosystem is the moat, and standards give fude a usable one on day one.

## D11. Client and server split designed now, built last

The terminal client talks to modules through the same interface the RPC
boundary will expose. Why: retrofitting a split is painful, building it first
delays a usable editor.

## D12. Query selection lives in visual mode

A tree-sitter query selects every match as visual regions, then any operator
applies to all of them. Why: it gives structural search and replace without
changing the verb-first grammar.

## D13. Operators and targets are registered names

A command holds names, never functions. Why: a command must serialize to
JSON and back unchanged for macros, RPC and plugins, and a plugin's
operator must compose with a built-in target through the same lookup.

## D14. The anchor lives on the request, not the command

A command has no position. A request carries the buffer, view, optional
anchor and base version. Why: dot repeat must replay at the current cursor,
and an agent must be able to edit line 40 without moving the user's cursor.

## D15. Macros are key text

A register holds keys, and `@x` replays them through the grammar. Why: Vim
and Neovim work this way, the text is trivially editable (B17), and a macro
can hold `:w` or `<C-w>v`, which are not commands. The cost is that a
remapped key changes how an old macro replays, as in Neovim.

## D16. Views belong to clients

The kernel holds a view's marks and nothing else about it. Why: keeps the
kernel small, and in step 6 each client has its own windows and cursors on
shared buffers.

## D17. One goroutine owns mutation

Input, grammar, resolution and applying transactions run on one editor
loop. Everything else reads snapshots and posts results back. Why: no locks
on the hot path, deterministic tests, and the persistent rope makes
snapshots free.

## D18. Transactions carry an id and a base version from step 1

The id is the origin (the client an edit came from) and a per-origin
sequence number. Plugins act within the origin of the client that ran
them. Edits are stored
relative to the base version, and the log keeps resolved edits, not only
commands. Why: the kernel can refuse or rebase a stale write, per-origin
undo works with several clients, and a CRDT built into the rope later needs
no data migration.

## D19. The timeline log replaces swap files

Why: the timeline already records every edit. Appending it to disk as it
happens gives crash recovery and persistent undo from one mechanism.

## D20. Neovim is the reference, not a contract

Where the spec is silent, fude does what Neovim does. A departure is allowed
when it is better, and is recorded below under Departures with its reason.
Why: Neovim's behaviour is the one the user prefers, but matching it
exactly would bring back the legacy fude exists to drop.

## D21. Two extension tiers, one API

Go modules compiled into a distribution, and wasm plugins at runtime. Both
use `api/`. Why: built-in features are proof the API is enough, and nothing
the distribution does is out of reach for a plugin. The grammar itself is
replaceable only at build time, because the input path must stay inside its
budget.

## D22. The kernel is a separate Go module with no policy

`core/` has its own `go.mod`, imports only the standard library, starts no
goroutines and does no I/O. Persistence and change notification are
interfaces the host implements. Why: other projects can embed the kernel
without fude's cgo, wasm and terminal dependencies, and keeping policy out
is what keeps the kernel small (D4). The cost is a versioned public API
that fude itself must respect.

## D23. The vim grammar is a separate Go module over interfaces

`vim/` has its own `go.mod`, does not import `core/`, starts no goroutines
and does no I/O. It reads text through a `Text` interface and acts through a
`Host` interface, and the host drives its timeouts. Why: a program with its
own text storage can embed fude's editing model, tests drive it with a
tiny in-memory host, and the grammar cannot reach into the
kernel. `core/`'s snapshot satisfies `vim.Text` as is, which constrains both
APIs.

## D24. One authoritative server for multi-client editing

The server orders transactions. Clients apply their own edits at once and
rebase unconfirmed ones on the server's order. Undo reverts only the
undoer's own transactions. Why: fude has a server by design, so the main
payoff of a CRDT (offline merge without a server) is not needed. Yrs, Loro
and Automerge each own the document, which would demote the rope to a
mirror and add cgo. CodeMirror's collab module shows the rebase approach is
small. D18 keeps a CRDT in fude's own tree possible later, as Zed built.

## D25. fude's own client protocol, carrying state

Clients receive buffers, edits, views, decorations, modes and proposals,
not a cell grid. Why: Neovim's UI protocol shares one screen, size and
cursor between every attached client, and Neovide, goneovim and
vscode-neovim all call far more than the UI protocol (Lua, Vimscript,
autocmds, options). Attaching them would mean emulating Neovim.

## D26. tcell v3 for the terminal, behind fude's own screen interface

`ui/term/` wraps tcell v3 with `OptAdvancedKeys`, so the rest of fude sees
fude's key and cell types, never tcell's. Startup turns tcell's
negotiation round trip off (`OptNegotiation(false)`) and enables the kitty
keyboard protocol unconditionally, which terminals without it ignore. Why:
tcell v3 is tagged, maintained and terminfo-free. It stores a grapheme
cluster per cell, wraps frames in synchronized output, and reads and
writes OSC 52. Ultraviolet has a better feature set but no tagged release
and a README that says its API may change. vaxis blocks startup on two
round trips with no opt-out, which breaks the 5ms first frame.

Costs accepted: Super and Meta are one modifier, so `<D-x>` cannot differ
from `<M-x>`. Paste arrives as keys between markers, which `ui/term/`
collects into one paste (B8). Grapheme mode 2027 is never enabled. Without
kitty or modifyOtherKeys, `<C-i>` and `<Tab>` are the same key, as in every
terminal library. With the kitty protocol forced, tcell's
parser still decodes legacy bytes. A terminal without kitty sends an Alt
chord as `<Esc>` and the key, which vim reads as those two keys.
Ultraviolet is the fallback if these costs bite.

## D27. Config is TOML data, never code that runs later

`config.toml` holds settings, keymaps, plugins, and per-filetype and
per-project tables. Keymaps map keys to keys, ex lines or plugin functions.
Logic that runs while editing lives in a plugin, under its budget. If TOML
proves too weak, a language evaluated once at startup may be added, as long
as it outputs the same data TOML does and registers no callbacks. Starlark
(`go.starlark.net`) is the first candidate, because it is hermetic, always
terminates and is pure Go. Why: a config that registers callbacks is a
plugin without a sandbox or a budget, which is the Neovim problem fude
exists to avoid. A Lua runtime would also be a second extension system
beside wasm, and Neovim configs would not run anyway, since they call
`vim.api`.

## D28. Queries come from each grammar's own repo

Highlights, injections and locals are the queries the grammar repo ships.
fude writes one `textobjects.scm` per language it ships. No query comes
from nvim-treesitter or Helix. Why: grammar repos' queries use only the
core predicates the Go binding evaluates, so fude needs no predicate
engine, no regex translation for queries and no third-party query
collection. The tree-sitter CLI (`tree-sitter query`, `tree-sitter
highlight`) checks them against the same grammar. The cost is plainer
highlighting than nvim-treesitter's and one tree-object file per language
to maintain.

## D29. Grammars are linked in, or built with the tree-sitter CLI

A distribution links its grammars through each grammar repo's Go binding,
so the shipped set needs no files beside the binary. Any other grammar is
built with `tree-sitter build` and loaded at runtime with dlopen. Wasm
grammars are out, because tree-sitter's wasm store needs wasmtime and
cannot run on wazero.

## D30. Grapheme clusters and widths come from clipperhouse

`vim/` segments text with `github.com/clipperhouse/uax29/v2/graphemes` and
measures display width with `github.com/clipperhouse/displaywidth`. Why:
the standard library has neither, and tcell v3 (D26) already depends on
both. The cursor and the renderer then agree on
cluster boundaries, and the binary contains one segmentation table.

## D31. config.toml is read with pelletier/go-toml

`editor/` decodes `config.toml` with `github.com/pelletier/go-toml/v2` and
rejects unknown keys. Why: the standard library has no TOML parser. B93
needs the line of an error, and go-toml gives it for a bad value, an unknown
key and a syntax error. A decode that fails keeps what it read before the
error. BurntSushi/toml gives no line for a bad value and decodes the whole
file or nothing. go-toml has no dependencies.

## D32. tree-sitter comes through its official Go binding

`syntax/` uses `github.com/tree-sitter/go-tree-sitter` and links each
grammar through the Go binding in the grammar's own repo, starting with
`github.com/tree-sitter/tree-sitter-go`. Why: there is no tree-sitter in
the standard library, and the official binding tracks the C library. Its
one dependency, `github.com/mattn/go-pointer`, passes Go callbacks to C.

## Departures from Neovim

Each entry: what differs, then why.

### N1. Vim pattern syntax on a linear-time engine

`/`, `?`, `:s` and `:g` take Vim's syntax: magic modes (`\v \m \M \V`),
`\< \>`, `\zs \ze`, `\c \C`, smartcase, and the replacement forms `&`,
`~`, `\1`, `\u \U \l \L \e \E` and `\r`. fude translates them onto Go's
`regexp`. Patterns that need backtracking (backreferences in the pattern,
lookaround, `\&`) are refused with a message naming the atom. `\=`
expressions do not exist, since there is no expression language. Why: Go's
engine runs in linear time, so no pattern can hang the editor on a large
file, and the refused atoms are rare in interactive use.

### N2. No Ex mode, Select mode, modelines or swap files

`gQ` and Select mode are gone. Modelines are never read. The timeline log
replaces swap files (D19). Why: legacy that fude exists to drop, and
modelines are a long-running source of security bugs.

### N3. LSP keys exist only while a server is attached

Neovim's default LSP keys (B66) are bound when a server attaches to the
buffer and removed when it detaches. Why: in Neovim they
exist from startup, show an error without a server, and make the built-in
`gr{char}` wait `timeoutlen` in every buffer.

### N4. Options have fude's own names

`:set` takes fude's option names, not Vim's, with Neovim's defaults where
the option exists in both. Why: there is no Vim option compatibility
(product.md boundaries).

### N5. A missing final newline stays missing

fude writes a file back with or without its final newline, as it read it.
Neovim's `fixendofline` adds one. Why: a save should change only what the
user edited.

### N6. Macros are stored as key notation

A recorded macro is text such as `A!<Esc>j`, and fude stores a typed `<` as
`<lt>`. Running a register or a `:normal` line parses that notation, so
`<Esc>` in either is the Escape key. Neovim stores raw key codes, which show as `A!^[j`. Why:
a macro you can put, read and edit as plain text and yank back is the point
of D15.

### N7. Marks follow their character

A mark stays on the character it was set on when text before it on the same
line changes. When its text is deleted, it moves to where the deletion was.
Neovim marks track the line and keep their column, and drop a mark whose
line is deleted. Why: a mark should point at the text you marked, which is
what kernel marks already do.

## Open

None.
