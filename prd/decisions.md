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

## D3. Every edit goes through the grammar

Typed keys, a macro, a plugin key and a client's `Command` all reach the
vim machine as keys, and the machine's edits become transactions. Why:
with one path, dot repeat, plugin verbs and nouns, RPC and agent proposals
all behave as typed keys do. A `Command`
is how a client names an edit instead of typing it (D13). Macros are key
text (D15).

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

The machine registers operators, targets and commands by name, and a
client's command names them. The server turns a command into the keys it
names and feeds them to the client's session. Why: a plugin's operator
must compose with a built-in target through the same lookup, a command
must cross the socket as JSON, and as keys a client's edit, a macro and
typed keys all go through one grammar.

## D14. The anchor lives on the request, not the command

A command has no position. A request carries the buffer, an optional byte
offset and the version the offset is in. Why: dot repeat must replay at
the current cursor, and an agent must be able to edit line 40 without
moving the user's cursor. A byte offset maps through edits with the
kernel's `Map`, which a line and column cannot without the old text.

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

The server holds every buffer and applies each client's keys and requests
in the order they arrive. Each client is a session with its own windows,
vim machine and origin, and undo reverts only the undoer's own
transactions. A thin client sends keys and requests and draws the state it
gets back. A replica keeps a copy of a buffer in a kernel of its own,
applies its edits there at once, and rebases them on the server's order,
as CodeMirror's collab module does (B79). A replica may run the grammar
on its copy, so keys cost no round trip, while undo and the commands the
grammar leaves to its host still run on the server, which keeps the one
timeline (B98). Why: fude has a server by
design, so the main payoff of a CRDT (offline merge without a server) is
not needed. Yrs, Loro and Automerge each own the document, which would
demote the rope to a mirror and add cgo. The replica reuses the kernel's
`Undo`, `Map` and `Since`, so it is a small library over two calls, and
the server stays the only history. The user asked for it on 2026-10-15,
ahead of a client on another machine. D18 keeps a CRDT in fude's own tree
possible later, as Zed built.

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

## D33. The terminal client draws in its own true colour theme

The terminal client paints every cell from fude's theme, dark or light in
24-bit colour, where it took the terminal's 16 colours before. Why: the
user wants a modern look like Zed's and Helix's, and 16 colours
give too few shades for layered grounds, a cursor line and muted text. A
terminal without true colour gets the nearest colours tcell can send.

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

### N8. Autocomplete asks the language server only

With `autocomplete` on, the menu lists the server's items and never the
buffer's words, which Neovim's `'autocomplete'` takes from `'complete'`.
Why: fude has no keyword completion, and a server's items already cover a
buffer's own names.

### N9. No mouse

fude takes no mouse input, where Neovim's `mouse` defaults to `nvi`. The
terminal keeps its own selection, scrolling and copy. Why: the user chose
keys only on 2026-10-15.

### N10. Tab with expand_tab goes to the next indent column

With `expand_tab`, `<Tab>` in insert mode types spaces to the next
`indent_width` column, as Neovim's `softtabstop=-1` does. Neovim's default
`softtabstop` of 0 types spaces to the next 8-column tabstop instead. Why:
fude has one indent setting, and a `<Tab>` that indents by it is what most
Neovim configs set.

### N11. :files finds files

`:files` opens a fuzzy file finder (B129), where Neovim's `:files` is
another name for `:ls`. Why: the user chose the name on 2026-10-16, and
`:ls` and `:buffers` still list buffers.

### N12. autocomplete covers the command line

With `autocomplete` on, a `:` command line shows its completions as it is
typed (B131). Neovim's `'autocomplete'` is for insert mode only, and a
command line needs `wildtrigger()` in a `CmdlineChanged` autocommand to do
the same. Why: one setting for completion while typing, and fude has no
autocommands.

### N13. :grep is a live grep

`:grep` opens a finder that searches as the query is typed (B134), where
Neovim's `:grep` runs `'grepprg'` once and fills the quickfix list. Why:
the user searches with fff.nvim's live grep, and fude has no quickfix
list.

### N14. clipboard keeps a copy's shape

With `clipboard` on, a put without a register takes the unnamed register
while the system clipboard holds its text, and the clipboard once it
holds other text (B137). Neovim's `clipboard=unnamedplus` always puts from
the clipboard, so a block copied in Neovim puts as plain text. Why: the
clipboard holds only text, and the user chose a better way over
Neovim's on 2026-10-16.

### N15. Yanks and undos flash

Text just yanked, put, undone or redone shows highlighted for a moment
(B139). Neovim shows nothing unless a config calls `vim.hl.on_yank()` or a
plugin does the rest. Why: it shows what a command touched, and the user
runs undo-glow.nvim for it.

### N16. Splits open below and right

A new window goes below the current one for `:split` and right of it for
`:vsplit`, and takes the cursor (B23), as Neovim does with `splitbelow`
and `splitright` set. Neovim's defaults put it above and left. Why: the
user sets both, as many configs do, and asked for it on 2026-10-16. An
option to choose waits for someone who wants the other way.

### N17. A buffer's keys do not wait

A mapping in a kind of buffer's keymap runs as soon as its keys are typed,
even when a longer mapping of `[keymap.normal]` starts with them (B142).
Neovim's `<buffer>` mappings wait `timeoutlen` for the longer one unless
marked `<nowait>`. Why: a timeline's `[` would wait for `[h` before it
moved, and a buffer's own keys are the ones meant there.

### N18. The leader is space

`<Leader>` stands for `<Space>` unless `leader` sets it (B144), where
Neovim's `mapleader` is `\`. Why: the user asked for it, and most Neovim
configs set space.

### N19. Indent guides are on

fude draws indent guides unless `indent_guides` is off (B153), where
Neovim draws none without a plugin. Why: the redesign the user approved on
2026-10-16 shows them, and `indent_guides = false` turns them off.

### N20. `:lsp` alone shows the servers, and there is no `:checkhealth`

`:lsp` lists the running language servers, their roots and files, and
says when the current file's server is not running (B159). Neovim's `:lsp`
needs a subcommand, and its servers' state is in `:checkhealth`. Why:
fude has no providers or plugin checks for `:checkhealth` to cover.
Config errors already show at startup (B93), and server failures show as
messages that `:messages` keeps (B157). Which servers run had nowhere
to show, and `:lsp` shows it.

## Open

None.
