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

## D12. Query selection places multicursors

A tree-sitter query places a multicursor at every match, each selecting
its match, then any operator applies at all of them (B42). Why: it gives
structural search and replace without changing the verb-first grammar, and
the multicursors are the ones `Q` places, so nothing is special about
them.

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

## D16. Windows belong to clients

The kernel holds a window's marks and nothing else about it. Why: keeps the
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
happens gives crash recovery and persistent undo from one mechanism. Each
log has a goroutine of its own that writes it in batches, so the editor
loop never waits on a slow or busy disk. A crash loses only the edits made
in the instant before it, which had not yet reached the disk.

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
possible later, as Zed built. D40 reopens one part: fude listens on a
network address when `fude --share` asks it to.

## D25. fude's own client protocol, carrying state

Clients receive buffers, edits, windows, decorations, modes and proposals,
not a cell grid. Why: Neovim's UI protocol shares one screen, size and
cursor between every attached client, and Neovide, goneovim and
vscode-neovim all call far more than the UI protocol (Lua, Vimscript,
autocmds, options). Attaching them would mean emulating Neovim.

## D26. tcell v3 for the terminal, behind fude's own screen interface

`tui/` wraps tcell v3 with `OptAdvancedKeys`, so the rest of fude sees
fude's key and cell types, never tcell's. Startup turns tcell's
negotiation round trip off (`OptNegotiation(false)`) and enables the kitty
keyboard protocol unconditionally, which terminals without it ignore. It
asks once whether the terminal draws cursors of its own, kitty's multiple
cursors protocol, and never waits for the answer. The input takes the
answer out when it comes, and until then multicursors draw as cells
(B401). Why:
tcell v3 is tagged, maintained and terminfo-free. It stores a grapheme
cluster per cell, wraps frames in synchronized output, and reads and
writes OSC 52. Ultraviolet has a better feature set but no tagged release
and a README that says its API may change. vaxis blocks startup on two
round trips with no opt-out, which breaks the 5ms first frame.

Costs accepted: Super and Meta are one modifier, so `<D-x>` cannot differ
from `<M-x>`. Paste arrives as keys between markers, which `tui/`
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
fude writes one `textobjects.scm` per language it links in or builds, and
embeds it. No query comes
from nvim-treesitter or Helix. Why: grammar repos' queries use only the
core predicates the Go binding evaluates, so fude needs no predicate
engine, no regex translation for queries and no third-party query
collection. The tree-sitter CLI (`tree-sitter query`, `tree-sitter
highlight`) checks them against the same grammar. The cost is plainer
highlighting than nvim-treesitter's and one tree-object file per language
to maintain. fude accepts three predicates outside the core ones.
JavaScript's highlights mark built-in names with `#is-not? local`, which
needs the locals a highlighter tracks. fude tracks none and lets those
patterns match, so a local that shadows a built-in name takes the
built-in's colour. Zig's highlights, written for Neovim, use `#lua-match?`
and `#set! "priority"`. A Lua pattern with no `%` item and no `-` outside
a set means the same as a regular expression, so fude reads it as
`#match?`, and leaves out a pattern with any other Lua pattern. A priority orders highlights in Neovim,
and fude ignores it. A language's highlight files combine in the order its
repo's `tree-sitter.json` lists them. Some come from another grammar's
repo, as most of TypeScript's come from JavaScript's.

## D29. Every grammar builds on first use

Every language fude knows, Go included, is a registry entry in `syntax/`:
its repo, a pinned commit, where its sources and queries are, and its
comment syntax. Opening a file of that language fetches the commit with
git, builds it with `tree-sitter build` into
`$XDG_DATA_HOME/fude/grammars/<filetype>/`, and loads the library with
dlopen. The library is named for the commit, so a release that pins a new
one builds again on first use. A grammar built by hand into that
directory as `parser.so` loads too. Wasm grammars are out, because
tree-sitter's wasm store needs wasmtime and cannot run on wazero.

On Windows fude runs llvm-mingw's `gcc` on the grammar's `parser.c` and
`scanner.c` itself, into `parser.dll`, and loads it with LoadLibrary.
`tree-sitter build` there compiles for Microsoft's C runtime, whose
headers llvm-mingw lacks.

Why, as of 2026-10-17: some grammars, markdown's among them, have no Go
binding to link, and linking more would add to the binary for languages a
user may never open. Go was linked in until then, which made it the one
grammar `:grammar` showed differently. The user chose one way for every
language. The cost is that building needs git, the tree-sitter CLI and a
C compiler, and the first file of a language, Go's too, shows without
highlights until its build ends. `auto_install = false` keeps fude from
fetching anything. Tests build Go's grammar from tree-sitter-go's module,
which stays in go.mod for that, through `syntax/syntaxtest`.

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

## D34. Help is a written guide beside the reference built from code

`:h` opens one markdown buffer: an index, the guide's pages, then the
reference (B162). The reference comes from the tables that run fude, so it
cannot fall behind them. The guide is markdown in `editor/guide`, written
by hand and embedded in the binary. Why: a line per command says what it
does, but not how the parts fit, which needs prose and examples no table
can hold. Keeping both in one buffer lets `/` search all of help and keeps
`:h name` a jump to a line.

## D35. A plugin runs what it registers through one export, call

`register` takes an id and a line of help, and fude runs every command,
operator, object and key a plugin registered through its export
`call(id, a, b, c)`. fude runs a plugin's `_initialize` before its `init`.
Before, `register` took an export's name for each, and fude called only
`init`. Why: a
plugin written in Go, built by TinyGo, declares its exports when it
compiles and cannot add one for each function it registers. One dispatcher
lets the SDK register Go functions as they are, in Go's own `init`, which
`_initialize` runs. The help lets `:h` and the key hints say what a
plugin's keys do, as they do for fude's own.

## D36. Plugins are written in Go and built with TinyGo

`sdk/` is a Go package for writing plugins, and `fude plugin new` and
`fude plugin build` write and build one with TinyGo for its
`wasm-unknown` target (B173). Why: the plugin ABI moves text through
pointers into the plugin's memory, which nobody should write by hand.
TinyGo's `wasm-unknown` modules import nothing, so the sandbox stays as the
capabilities draw it. A plugin is about 540KB, as `sdk` builds
`encoding/json`'s encoder in its init for every plugin (D51). Go's own `wasip1`
target would need WASI in the sandbox, builds modules of megabytes, and its
runtime takes longer to start. TinyGo leaves out parts of the standard
library, such as `os` and `net`, which a plugin reaches through fude's
functions anyway.

## D37. The modules are released together, and the program is a module of its own

fude's modules are the root, `api/`, `core/`, `vim/`, `syntax/`,
`protocol/`, `plugin/`, `sdk/` and `cmd/fude/`. They take one version
together, tagged `vX.Y.Z` and `api/vX.Y.Z` through `cmd/fude/vX.Y.Z` on one
commit, which release-please's pull request makes and the release workflow
tags. The root, `plugin/` and `sdk/` point at the others through `replace`
lines. A module's `replace` lines hold only when it is the module being
built, so they never reach a user. `cmd/fude/` has none, as `go install`
refuses a program whose module has them. Every module requires the others
at the release's version, which the release pull request sets, and the
workspace takes them from the checkout. So `go install
github.com/y3owk1n/fude/cmd/fude@vX.Y.Z` works, and the release workflow
builds each system's zip that way. `just standalone` builds every module
but `cmd/fude/` outside the workspace, so a module whose `go.mod` lacks a
requirement fails before a release does. Why: `core/` and `vim/` stay
modules of their own, for programs that embed them without fude's
dependencies (D22, D23). Requiring each other by commit would add a second
commit and push to every change of `core/` or `vim/` the editor depends on.
The program was a package of the root until 2026-10-19. A release then
needed the root without `replace` lines, which development needs, so the
user chose to make it a module of its own.

## D38. A terminal is charmbracelet/x/vt in a pseudo-terminal from x/sys

`:terminal` runs its program in a pseudo-terminal that fude opens itself
with `golang.org/x/sys`, already a dependency through tcell, in about 50
lines for macOS and Linux. The program's output goes through
`github.com/charmbracelet/x/vt`, a terminal emulator, which keeps the
screen, 10,000 lines of scrollback and the alternate screen, and turns
keys and pastes into what the program reads (B205). Why: the standard
library opens no pseudo-terminal. tcell's own emulator, `tcell/v3/vt`,
has no alternate screen, so `less`, `htop` or `vim` in a terminal would
leave its screen garbled, and it keeps its cells to itself, so fude
cannot add one. An emulator of fude's own would be about 1,500 lines of terminal
quirks. The cost is 14 modules more and a v0.1.0 API, pinned by `go.sum`.
x/vt's API brings two of them in as direct dependencies:
`charmbracelet/ultraviolet` for the key names fude sends it, and
`charmbracelet/x/ansi` for the colours it returns.

## D39. An attached client draws the session's windows, sent as frames

`fude --attach` is fude's terminal client drawing a session of another
fude (B239). The server sends the session's frame, the windows, floats,
command line, hints and status that the terminal draws, whenever it
changes, and the client sends keys, pastes, size and focus. Text goes
once per buffer and then as the edits since, from the kernel's `Since`,
as a replica takes them. The terminal client reads a session through one
interface, which the local session and the attached one both implement.
Why: one renderer draws both, so an attached client looks and behaves as
fude's own terminal does. Frames carry state, not cells (D25), so a
client draws at its own size, and keys need no grammar on the client.
Every key waits a round trip, which is fine on a local socket or a local
network. A client on a slow link attaches through a replica later, as
`client.Editor` already runs the grammar on one (B98). The user chose
this on 2026-10-17.

## D40. Sharing goes to guest sessions, over TLS, with a one-time token

`fude --share` listens on a network address with TLS and prints one
string for the guest: where to reach it, the SHA-256 fingerprint of its
self-signed certificate, and a secret of at least 128 bits. `fude --join`
pins that fingerprint, then proves the secret inside the channel. The
host approves each join, and the secret works once. A session that
joins is a guest, with less power than the host's (B243 to B247). This
reopens D24 and B97, under which fude listened on no network address.
Why: pairing over SSH gives the other person a shell on the host, which
the user ruled out on 2026-10-17. A session attached by any path has
fude's powers, `:!`, `:terminal`, every file the host can read and the
host's clipboard, so the limits belong to the server's session, never to
a client. Formatters, language servers and git still run for a guest as
the host's config says, as they act on the project's files. A guest
cannot reach a `.git`, because fude runs git in the background and git
runs programs its config names, such as `core.fsmonitor`. A guest who
writes other project files, such as `.fude/tasks.toml`,
`.fude/launch.toml` or a tool's config, changes what the host's tools do
when the host next runs them, which comes with sharing a repository. Go's
`crypto/tls` has no pre-shared keys, so the share string carries a
fingerprint to pin instead, which needs only the standard library.

The string names every address of the host, and the guest dials them at
once and keeps the first whose certificate matches. Why: the host cannot
tell which network it shares with the guest. It may be the LAN, a VPN
such as Tailscale or a VM's bridge. An address from another network may
never answer, so trying them in turn would stall the join. A wrong
address costs nothing, since the guest sends the secret only after the
certificate matches. The user chose this on 2026-10-19.

## D41. A relay is fude itself, passing ciphertext between two dial-outs

`fude relay` runs on any machine both sides reach, such as a small server.
`fude --share --relay host` dials it, and the guest's `fude --join` dials
it too, and the relay joins the two connections by a room the share
string names. The TLS of D40 runs end to end through it. Why: two
machines behind NAT cannot reach each other, and the user wants pairing
on a shared network and across the internet alike. The guest pins the
host's fingerprint, so the relay sees only ciphertext and needs no trust,
and anyone can run one. A public relay would mean hosting it, and NAT
traversal or a tunnelling service would bring a large dependency or a
third party's account.

## D42. fude's own debug adapter client

fude speaks the Debug Adapter Protocol to adapters such as Delve and
debugpy through `protocol/dap/`, its own client, built on the standard library
alone. The adapter, the launch settings and the keys are the user's
choice: config.toml names adapters per filetype, as it names language
servers, `.fude/launch.toml` holds a project's launch settings, as
`.fude/tasks.toml` holds its tasks, and leader keys drive it. Why: DAP
frames messages as LSP does, so `protocol/framing/` serves both, and the rest is
requests, responses and events. No Go DAP library is worth a
dependency for that. The user chose the core of nvim-dap and dap-ui on
2026-10-17: breakpoints, launch and attach, stepping, the stack,
variables and evaluation.

## D43. syntax is a Go module that imports no fude package

`syntax/` has its own `go.mod`. It reads text through its own `Text` and
`Edit`, as `vim/` does. Its host passes `New` a trace func that hears each
parse and highlight, since the trace ring belongs to the editor. Why: a
program that wants tree-sitter highlights, tree targets and grammars
built on first use imports it without the editor, wazero or a terminal.
The cost is a fifth tag on every release (D37).

## D44. lsp, dap and framing are one Go module, protocol

`protocol/` has its own `go.mod` and holds `lsp/`, `dap/` and `framing/`.
It imports only the standard library. `lsp/` reads text through its own
`Text` and `Edit`, and both clients take a trace func from their host, as
`syntax/` does (D43). Why: a program that talks to language servers or
debug adapters imports them without the editor, cgo, wazero or a
terminal. The two clients share `framing/`, so one module holds all three
rather than three modules that require each other. The cost is a sixth
tag on every release (D37).

## D45. plugin is a Go module that requires wazero and api

`plugin/` has its own `go.mod` and holds the wazero host and
`plugintest`. It requires wazero and `api/`, whose `Scope` and `Result` it
translates the ABI to (D48). Why: a plugin's tests run its `.wasm` with
`plugintest`, and a plugin's own module can require it without the root,
whose `go.mod` brings the editor's dependencies. `plugin/` requires `api/`
at the version they release together, so a plugin's module can require
`plugintest` (D37). A program that
runs fude plugins imports the host without the editor, cgo or a terminal.
The cost is a seventh tag on every release (D37).

## D46. Packages no other program needs are internal

`internal/` holds packages such as `pty/` and `keep/`. Each is small, and
only fude uses it. Why: a program may import any package outside
`internal/`, and fude would have to keep that package's API for it. These
have no such program. `clipboard/`, `relay/` and `share/` were here until
a client outside fude needed them, and are in `frontend/` now (D64).

## D47. One package holds what goes over the socket

`frontend/wire` holds the types fude and its clients send each other: a
replica's edits, pulls and pushes, and the frames a frontend draws.
`editor/`, `frontend/client` and `tui/` use it. A guest's hello is there too, and the
editor decodes the keymaps and options it carries, so `frontend/client` joins
a share without the editor. Why: each side had its
own copy of the replica types, and two copies of one protocol can
disagree with nothing to catch it. A client that draws frames needs
them without the editor, so it is in the frontend module (D64). It has a
`Span` of its own. The editor
converts a grammar's spans to it, so a client compiles no tree-sitter. A
window carries its `kernel.Snapshot` for a client in the same process, so
it imports `core`.

## D48. api is a Go module that imports only the standard library

`api/` has its own `go.mod`. The trace ring that was in it moved to
`internal/trace/`, since it is the editor's and holds process-wide state.
Why: Go modules and wasm plugins share one API (D21), and `plugin/` is a
module of its own (D45), which cannot import a package of the root.
`Scope`, `Result` and what a `Result` asks for are in `api/`, and so is
`Module`, what fude makes calls into. A `*plugin.Plugin` is a `Module`.
It translates the ABI to those types. The cost is an eighth tag on every
release (D37).

## D49. Built-in features are modules over api, added in cmd/fude

A built-in feature can be a package in `modules/` that imports only
`api/` and `protocol/`, which hold no editor state. It is an `api.Module`,
and `cmd/fude` adds it with `editor.Use`.
`:blame` was the first to move out of `editor/`. Why: a feature that
works through the API a plugin gets proves the API is enough (D21), and
where it does not, the API grows for plugins too, as jobs got stdin for
blame. A distribution leaves a feature out by not adding it. A module is
one for every session, so `:blame` is one switch for the host and its
guests. The editor cannot stop a Go module's call, so it only times it.

## D50. Images are kitty's Unicode placeholders

The terminal client shows an image by kitty's graphics protocol: it sends
the image once with a virtual placement, then draws the image's rows as
cells of U+10EEEE with a diacritic for the row and one for the column, in
a colour that is the image's id (B303). Why: those cells are text to tcell
and to fude, so an image scrolls, splits, clips and has floats drawn over
it with no code of its own, and tmux passes it on. Sixel and iTerm2's
protocol paint over cells the screen then redraws, so each frame would
have to lock the image's cells and paint it again. kitty and Ghostty draw
placeholders, and `charmbracelet/x/ansi`, already a dependency, has the
protocol's pieces. fude reads from the environment which terminal it is,
as asking the terminal would slow the first frame (D26).

WezTerm, which has kitty's protocol but not its placeholders, and iTerm2,
which has its own, get images painted over cells after each frame, with
those cells locked so tcell leaves them (B310). This is second best: an
image shows only when all its rows do and no box covers it, it paints
again whenever it moves, and iTerm2 gets the image each time, so fude
scales it to the size it shows first. tmux redraws over painted images,
so these terminals show none inside it.

## D51. A plugin registers and returns api's own types, as JSON

`register` takes an `api.Registration` and `result` an `api.Result`, each
as JSON. `read` answers with any field of the call's `api.Scope`, and
calls a func field with the arguments the plugin sends. The sdk imports
`api` and sends them with `encoding/json`. The text reads stay host
functions of their own, because keys use them most.
Before, the ABI had a host function for each thing a result could ask for,
about twenty, and `register` took a kind, a name and a help. Why: modules
and plugins share one API (D21). Each field added to `api` needed a host
function of its own, and about 25 never got one, so a plugin could not do
what a module did. With the types themselves, a plugin can set a field as
soon as `api` has it. Tables in `plugin/` name the capability each field
needs, and every field must be in them. It costs size and load time. A
plugin is about 540KB with `encoding/json` (D36), and a cold load of one took 112ms before its compiled code
was cached, against 2.4ms after. A plugin now compiles before its init
budget starts. A result of 500 decorations takes 2.7ms in the plugin and
0.9ms for fude to read. Keys a result runs may run any command, so they
need a capability of their own, `keys`. A module's stream job runs the
module's own Go code. A plugin's calls take turns, so its own code cannot
run as a job while it answers calls. A plugin's stream job runs a program
instead, and each line the program prints comes as a callback. A build of
fude that is not a release takes the newest sdk, which may be older than its
ABI, and B173's test then skips (D37).

## D52. fude's modes are subcommands, parsed by hand

`fude attach`, `tail`, `share`, `join`, `man`, `open`, `keys`, `state`,
`help` and `version` are subcommands, as `relay` and `plugin` were, and
flags only modify them (B350). A word that names a subcommand is never a
file, so `--` or `./` reaches a file with that name. `editor/` has two
tables, one of the subcommands and one of the flags, each flag with the
subcommands that take it. `fude help` prints them, fude(1) renders them,
and the parser reads from them which flags a subcommand takes. Every
subcommand that dials a fude takes `--socket` (B352). Each connection is
a session of its own, so a call must name the session it acts on. A
session's name is not unique, so `:terminal` sets `FUDE_SESSION` to the
session's origin, along with `FUDE_SOCKET`. `fude version` prints the
commit's date and short hash from Go's build info until fude has
releases (B349). Why: the user chose subcommands on 2026-10-19, so modes
read as verbs. Neovim and Helix spell their modes as flags, and both
parse their arguments by hand. The standard `flag` package cannot take
an optional value, as `share [address]` does, and a library would print
help and errors in its own words. This reopens the flag spellings of D39
to D41 and of B163, B174, B239, B248, B251, B255 and B270, and the
socket `fude --tail` and `fude --attach` took as a word.

## D53. Keymap tables go by name, and a module's keys are plugs

`[keymap]` is a table of tables by name: `normal`, `visual`, `insert`,
`groups`, and one for each kind of buffer, fude's or a module's (B142).
Each key a module or plugin adds is a plug named for what it does, with
its default keys beside it, so `<Plug>(lsp.rename)` maps `grn` and any
other key (B327). A config.toml mapping of those keys wins, in any table
and after `:config reload`. `sdk.Key` adds a plug with its keys, and
`sdk.Register` takes every field a module's registration has. Why: the
user asked on 2026-10-19 that every module's keys remap, and that
plugins and modules have the same power. A fixed list of kinds rejected
`[keymap.pins]`, and a module's keys mapped after config.toml's and
replaced them. A misspelled table such as `[keymap.listng]` now loads
without an error, because fude learns a module's kinds only as the
module loads. `:h` lists each kind's keys under its name, so the typo
shows there. Keys that wait for
characters, such as surround's `ds`, stay the vim machine's own.

## D54. On Windows, fude does what Windows programs do

fude runs on Windows 10 and 11, on amd64 and arm64, and CI runs every test
there. Where Windows has its own way, fude takes it rather than a Unix
layer. The shell is `SHELL` when Windows can run it, or else PowerShell,
which runs `:!` without its profile and writes UTF-8 (B156, B205).
`:terminal` runs in a ConPTY pseudo console. The clipboard is Windows' own
calls, with `\r\n` line ends there. Config goes in `%APPDATA%\fude`, and
state, grammars and caches in `%LOCALAPPDATA%\fude`, unless an `XDG_`
variable says otherwise. Paths take `\` and compare without case. A
grammar is a DLL that llvm-mingw's `gcc` compiles (D29), and oku installs
llvm-mingw, as Windows has no C compiler of its own. `:restart` runs the
new fude in the same console, as Windows cannot replace a process.

Why, as of 2026-10-19: the user wanted fude on Windows, chose PowerShell
over cmd.exe as the default shell, and chose Windows' folders over XDG's.
A Unix layer such as Git Bash or MSYS would give fude Unix paths and a
Unix shell, which other Windows programs do not use. The
tests run `sh` and Unix programs from Git for Windows, which a Windows
build needs for grammars anyway. Delve does not run on Windows on arm64,
so oku skips `dlv` there.

## D55. A window's placement is data, which a module proposes and the user changes

Every window has a placement: in the layout's splits, in a float over
them, or hidden (B364). Placement is all that differs, so a float holds
any buffer, a terminal's too, and takes the same keys, search, yanks and
`:w` as a window in a split. A float sits in the middle of the screen, as
the `:lsp` panel does, or at the cursor, as hover does. A hidden window
keeps its buffer, its program, its cursor and the place it had, and shows
there again.

A module's result names where its window opens with `Where`: `above`,
`below`, `left`, `right`, `here` for the current window, `float` or
`cursor` (B367). fude places it by the first of these that says where: a
command modifier such as `:window float help` (B365), `config.toml`'s
`[open]` by module name (B366), the result's `Where`, or else the kind's
own, below for a panel or a terminal and here for a jump. Once a window is
open, `:window float`, `:window below` and the other ways, and `:hide`,
move it. The ways are subcommands of `:window`, as `:lsp stop` is of
`:lsp`, and the window commands from vim, such as `:split`, stay as they
are. A result's `Lock` fixes the placement it asks for, and modifiers,
`[open]` and moves leave that window where it is. A locked window still
closes.

Hover, signature help, help and every panel show in windows in this model
(B368). Menus, the finder and the command line's menu are lists to pick
from, not buffers, and a menu item's doc is part of its menu. They keep
their own code and draw in the same bordered box a float does. `Where`
and `Lock` replace `Panel.Float`, `Jump.Split`, `Scratch.Split` and a
terminal's fixed place below. `Panel.Screen` stays, as it changes how a
window draws, not where. The box a float panel was (B318) goes, with its
cards, and its `Foot` becomes an option of a panel in a float. The wire
sends a float as a window with its rectangle.

Why, as of 2026-10-20: the user asked that a module choose a split or a
float, that the user can always change it while fude runs, and that a
module can lock it. `:terminal` showed the need. It opened below, as a
new shell each time, and could not move. A float was a box of text, so no
option could have floated it. Making the float a window removes the box,
with its own keys and its own drawing. Neovim's `:left` and `:right`
align lines, which fude does not do. This reopens N16 for panels, N29,
B205, B318 and B335.

## D56. The digraphs are Vim's default table

`<C-k>` types the character two keys name (B29), from the same 1366
digraphs Vim has by default. 1297 are RFC 1345's two-key mnemonics as the
RFC gives them. Three take Vim's character instead: `NU` is a line feed,
and `</` and `/>` are the CJK angle brackets. The other 66 are Vim's own
additions, such as `Eu` and `=e` for the euro sign and `` A` `` for À.
`vim/digraphs.go` holds them as a map, written once from Vim's
`digraph.c`.

Why, as of 2026-10-20: the user chose the full table over a smaller set,
so any digraph a Vim user knows works. RFC 1345's distribution is
unlimited. The table is a list of characters by name, which fude holds
as data rather than as Vim's code.

## D57. A command that fails returns why, apart from its message

A command fails with an error its caller takes back, not with a message
alone. The vim machine's host returns one from `Unhandled`, the editor's
commands return theirs, and a module's command fails with its result's
`Error`, which shows as the message does. So `:cdo`, `:g`, `:normal` and a
macro stop at a failed command of any kind (B257, B372). A report of what
a command did, such as `:pwd`'s folder or a task's ending, stays a
`Message`.

Why, as of 2026-10-20: B257 promised that `:cdo` stops where its command
fails, and only the machine's own commands could say so. A message could
not tell a failure from a report, as `:w` sets one too. The user chose to
give every command an error over narrowing B257. `Call`'s own error was
not reused, as it means the module itself broke, and fude unloads a
module that returns one. A plugin needs no capability for `Error`. A
failure is its call's own outcome, as `Log` is, and a plugin without `ui`
could not otherwise fail a command.

## D58. Buffers are numbered as Neovim's, and a view may take no number

Every buffer takes the next number, as in Neovim, and the first file
opened takes the empty buffer fude began with, so it is buffer 1. A
module's panel, scratch buffer or terminal sets `Unnumbered` to take
none, and a float never takes one. Such a buffer gets an id past every
number, which modules still use, and `:b`, `:ls`, `:bn`, `:bp` and the
buffer line pass it (B89, B373). An explorer takes none either.

Why, as of 2026-10-20: the user saw a file opened from the start screen
as buffer 3, and `:b 2` reach the start screen. The user chose Neovim's
numbering, with a way for a module or plugin to keep a view out of it,
over numbering files alone. The views the user reads and leaves take no
number. So does an explorer, which `:b` and the buffer line passed
already, and which `-` opens again. Files, new buffers and shells, which
the user edits or comes back to, keep theirs.

## D59. Modules add options of their own, under their names

A module, built in or a plugin, registers an option as it registers a
command, with a default, its help and whether each window has its own.
It is named after the module, as `autopairs.enabled`, so no two modules
share a name. `:set`, `:h` and a module's scope treat it as they treat
fude's options. A plugin's starts at its `options` in `[[plugin]]`, and
a built-in module's at its table `[module.<name>]` (B377). A module
that acts on its own, such as `start`, `session`, `hints` or `spell`, has
an `enabled` switch. One that acts only on a key or a command has none,
and dropping its keys turns it off. `git` and `lsp` do both, so they keep
the narrower `git.signs` and `lsp.diagnostic_notes`.

Why, as of 2026-10-20: the user wanted plugins to add settings a user
turns on and off with `:set`, and fude's modules to declare theirs the
way a plugin does, instead of as fields of the editor's config. The user
chose a table for each module over flat names at the top of config.toml,
though it moves keys such as `autopairs`. A flat name would need a check
across modules for each one, and would hide which module reads it. A
plugin keeps its starting values in its own table, as `options`, since
its name is known only once it loads, in the background. The user chose
that over keeping a plugin's read-only settings beside its options, two
ways to do one job, and named the table `options` after what it starts.

## D60. Sticky context and indent guides are modules

`modules/sticky` decides which lines pin over a window's top, and
`modules/guides` the display columns of each line's indent guides. The
editor draws the pins, and the client the guides, on blank cells after
tabs are expanded. Their options, `sticky.enabled`, `sticky.lines` and
`guides.enabled`, are shared by every window (B153, B217, B379, B380).

Why, as of 2026-10-20: the user wanted sticky context to be a module, so
a distribution may leave it out. The editor keeps the drawing, since a
pinned line needs its number, highlights, replacements and ground, which
no decoration gives. Its options are shared because a module reads only
the window its call is about. The pins now come a step after a scroll,
as spell's and markdown's decorations do. The user then wanted indent
guides to be a module too, as fast as it could be. A decoration marks
text, and a replaced tab draws at its replacement's width, so guides as
replacements would shrink every tab. The module gives display columns
instead, which the client draws as it did, so a guide may still fall
inside a tab. It sends a window's height of guides above the window and
below it, so a short scroll draws with the guides it has, and the editor
passes them to the frame as they are, with no decoration to look up.

## D61. A client's change is proposals a user reviews

A client of fude's socket may edit a buffer, with `push`, or propose its
change, with `propose`, as it chooses. A proposed change is a list of
edits, each a range of a buffer's or a file's text and its new text, and
each is a proposal of its own, so the user takes some and drops others
(B387, B388). The client hears what became of each (B389). `:review`
lists them as the lines they change, and is a module over the proposals
a module reads (B390, B391).

Why, as of 2026-10-21: a proposal was one edit made by running a command
on a copy of the text. That covers a client that thinks in vim commands,
but not a tool that writes a file's new text, such as a refactoring
script, a codemod or a linter's fixes. Each hunk is a proposal, not a
part of one, so `:accept`, `:reject`, staleness and the ids a client
reads stay as they were. A separate diff view would be a second way to
review a change. The review window is a module, as the list of places
is, so a distribution may leave it out. The user first wanted this for
AI agent integrations, then chose not to support agent workflows, and
kept proposals for any client.

## D62. A server's needs are config data or a plugin, never editor code

fude has no code for any one language server. What a server needs to
start and work is either data in its `[lsp.<name>]` table, such as
placeholders for its root and a cache folder, root patterns, variables,
a language id, fixed answers to its requests, root groups and
conditions, command candidates, capabilities and a socket to connect to
(B527 to B538), or a plugin over the messages and client commands fude
does not handle itself (B532, B539). Vue's server, whose TypeScript
requests another server must answer, is such a plugin. The pieces come
from what nvim-lspconfig's 421 configs do with code, read on 2026-10-22.
Each piece is what one or more of them needs. A plugin handles a root
that only a command finds, such as `cargo metadata`'s, and arguments
worked out from a program's output. utf-32 positions, which ccls and lean3 ask for, are
out, since they would change how fude counts text.

Why, as of 2026-10-22: a table of default servers, and code to pass
Vue's requests on, were built and dropped. The defaults would put
dozens of servers' commands and root files in fude to keep up with, and
start whatever is on PATH in any project. The Vue code would be the
first of many pieces of code for one server. The user chose general
pieces any server's config or a plugin builds on, and no defaults.

## D63. The editor serves frontends, and the terminal is one

`editor/` holds no frontend. `tui` is the terminal frontend fude
ships, and it stays as it is. fude may add a native frontend that uses
the system's own tabs, splits and mouse. Every frontend draws a session
through `client.Session` (D39), so it runs a local session or an
attached one alike.

The editor's layout is the default place of every window. It holds the
split tree, the rows of text or columns each split asks for, and the
cells it gives each window from one screen size. A window's place is its
text, gutter included. The terminal frontend draws a status line under
each window, a column between windows side by side and the buffer line
on top, in the room the layout leaves for them. A frontend that draws its
own splits reads the layout from the frame. It gives each window's place
in cells of its own grid, and the editor uses that place instead (B541,
B542). The editor gives floats, the finder and the key hints a place in
cells too, as the default. A float and a menu each say where they go,
such as at the cursor, in the middle or across the bottom, so a frontend
may place them its own way (B543). A frontend with a mouse gives the cell
of a window it points at. The editor finds the text there, since it
knows how each line wraps and what draws between lines (B552). A click on
a menu gives the line of the menu's text, and the editor finds the item
(B556).

Why, as of 2026-10-23: a spike drew fude in a system webview from the
terminal client's cells. A key took 9ms at p50 from press to paint, so
the editor needs no rewrite to run in a window. The user chose native
tabs, splits and mouse for that window, so a native split resizes freely
instead of in the editor's cells. The layout stays in the editor as the
default, because headless sessions and attached clients have no frontend
to place their windows, and moving it out would mean two copies of it.
fude has no users yet, so the plugin API and the wire format changed
with it.

## D64. Frontends build on one module with no cgo

`frontend/` is a Go module of `wire/`, `client/`, `theme/`, `compose/`,
`share/`, `relay/` and `clipboard/`. Of fude's packages it imports only
`core/` and `vim/`, and it builds with no C compiler, which
`just standalone` checks. The editor and `tui/` import it. A frontend
written outside fude, such as a native window, a client in another
process or an agent, needs nothing else. It is one module, not one for
each package, since they import one another. `protocol/` is one module
for the same reason (D44).

Why, as of 2026-10-23: the user wants fude's libraries to be what other
people build on. Before this, a frontend outside fude could not import
`theme` or `compose`, which were internal. Importing `client` compiled
tree-sitter and put the editor's dependencies in its `go.mod`. The
packages' API is now a promise, as D46 says of any package outside
`internal/`.

## D65. A native window on macOS, through AppKit with purego

`--gui` shows a session in a native macOS window, for each command that
shows one (B547). `gui/` is a frontend of `client.Session`, as `tui/` is.
It calls AppKit through purego's Objective-C runtime, so it needs no cgo,
and cgo stays in `syntax/`. A custom view draws each window's text from
the cells `frontend/compose` gives. The command line, the finder and
messages show in boxes over the text, as a palette, a panel and toasts.
Questions show in the system's sheets (B548). Each window of the layout
shows in a pane, with a status bar under its text. The panes sit in the
system's split views, and each pane places its window (B542, B549). The
title bar holds the buffers as tabs and the tab pages at its right. Floats
and menus show in boxes over the panes, placed by their anchors (B543,
B550). The root view is an input method's client, and the menus' Edit
actions type Vim's keys for them (B551). The mouse goes to the session by
cell (B552). The window takes the theme's light or dark look, and AppKit
shows the current file's icon and unsaved changes as it does for any
document window (B558). The strip, the status bars and the boxes take the
system's materials, colours, font and symbols, as the chrome of other Mac
applications does. The text keeps the theme (B559). A menu carries its
items as data beside its text, which the terminal client draws, so the
window lays them out as lists in the system's font with icons (B560). A
right click opens a menu of the language server's actions and the
clipboard's, and the scroll bars, a trackpad's scroll and the blinking
cursor behave as they do in other Mac applications (B561). The mouse
resting on a word shows its hover there (B562), and with Command held a
word is a link to its definition (B563). A task that ends while the window
is not in front bounces the Dock's icon, and from an app bundle shows a
notification, since the system's notifications need a bundle (B564).
Terminal windows draw from their cells. Images draw from the PNGs the
session gives, with no terminal protocol (B553). The editor stays the
source of each, and keys still go through Vim's grammar, so a palette
holds no text field of its own. On other systems `--gui` says it runs only
on macOS for now. `just app` puts the same binary in an app bundle,
`Fude.app`, behind a launcher script that takes the login shell's PATH,
since Finder gives an app only the system's (B565). `gui/app/bundle.sh`
makes it for `just app`, a release's macOS zips and both oku manifests, so
all of them ship the same bundle. `fude` stays one binary, since the
window adds about 0.1MB to it.

Why, as of 2026-10-23: the user wants a first-class Vim GUI with native
controls, not a terminal in a window. A spike drew fude's text in a native
view through purego, with p50 5.7ms from key to paint against 9ms in a
webview. mygo was considered, but its windows show web content, so its
tabs and splits would be HTML. purego is a new dependency because the
standard library cannot call the Objective-C runtime, and cgo there would
break the rule that cgo lives only in `syntax/`. AppKit is macOS's own, so
Windows and Linux need windows of their own later. `--gui` is a flag on
each command that shows a session, as the user asked, since each of them
can show its session in a window.

## D66. Native windows on Linux through GTK4, and on Windows through Win32

`--gui` shows the window on Linux through GTK4 and on Windows through
Win32, each the platform's own, as D65 uses AppKit. On Linux purego opens
GTK4, cairo and Pango, as it opens AppKit, so no cgo is added outside
`syntax/`. On Windows `syscall` calls Win32, and DirectWrite and Direct2D
through their COM interfaces, with the tab strip drawn as the window's
title bar. `gui/` keeps what all three share, the painter and the frames
it hands the main thread, in plain files, and each system has the same
unexported functions in its own `_darwin`, `_linux` and `_windows` files,
with no interface type between them.

Why, as of 2026-10-23: the user chose GTK4 for Linux and Win32 with
DirectWrite for Windows. A spike showed purego calls GTK 4.18 on
linux/arm64 with cgo on, with callbacks for signals and drawing,
`g_idle_add` from another goroutine, and Pango's cell size. GTK4 takes the
desktop's theme, fonts and menus, which raw Wayland and X11 would leave to
fude. WinUI 3 needs the Windows App SDK's runtime, and GDI blurs text at
high DPI, so Windows takes Win32 with DirectWrite. Linux comes first, as
Docker with a virtual display runs and shows its window here, and
Windows followed, its tests run in the user's VM. purego passes the
floats that Direct2D's and DirectWrite's methods take, as `syscall` does
not on arm64.

## D68. The window is made of the system's own controls

D65 drew the window's chrome itself on the system's materials. Where a
system has a control for a part, the window now uses it. The title bar
shows the title and the file's icon itself, the status bars are the
system's labels, and lists are the system's tables. Floats, such as a
hover, stay fude's panels inside the window, where the session places
them. A system popover places and sizes itself and may leave the window.
fude still takes every key, so the controls only show the
session and take the mouse. They never take the keyboard's focus, and
the row typed into stays fude's own, as D65 says. Tabs stay fude's own
on every system, drawn to the system's measures. macOS and Windows have
no public tab bar of this kind. GNOME's tab bar is libadwaita's, which
fude does not load. GTK's notebook would add a row of tabs under the
header bar, a style GNOME's own applications no longer use. Windows'
status bars stay fude's own too. Win32 has no label that shows text in
several colours, and its status bar control draws the classic grey bar
with no dark look. A part that becomes a system control loses the
accessibility D67 gave it by hand, as the control has its own.

Why, as of 2026-10-24: the user found the drawn chrome did not look
native and chose the system's controls over drawing closer to them. The
first part, the title bar and the status bars on macOS, also removes the
seam between the grey title bar and the theme's ground.

## D67. Assistive apps read the window through each system's accessibility

The window's views draw their own text, so each system's assistive apps
find nothing in them on their own. `gui/` gives each window's text to
the system's accessibility as a text area holding the buffer's whole
text, with the cursor or the visual selection as its selection and the
lines in view as its visible part (B581). The loop hands the main thread
the buffer's snapshot with each frame. The main thread turns the text
into the system's units, UTF-16 on macOS, when an assistive app first
asks, and again only after the snapshot changes. Each status bar is
static text and the tab strip a tab group of radio buttons, each with a
close button. The palette and the finder are each a group of a text
field and a list, a menu is a list and a toast is static text. The
assistive app speaks a list's selected item and each message, since the
focus stays in the text while a menu shows. On macOS the views answer
NSAccessibility's methods and post its notifications. On Linux GTK gives
AT-SPI each widget. fude's text areas are FudeText, a subclass of
GtkDrawingArea that purego registers with GtkAccessibleText, which
counts in characters. The area with the focus takes GTK's focus, since
AT-SPI reports that focus. The window's key controller still takes every
key first. Once an assistive app has read a text, fude tells it of each
change as a removal and an insertion, so Orca speaks only what changed.
Without an assistive app fude compares no texts. GTK shows no widget for
a tab or a list's row, so on Linux the tab strip is named by its tabs
and the assistive app speaks a list's selected item. On Windows the
window answers WM_GETOBJECT with a UI Automation provider written in Go.
Each element is a node, a COM object whose methods are syscall
callbacks, and its text ranges count in UTF-16 units. UI Automation
calls them on the window's thread. Two of their methods take floats, which amd64 and
arm64 pass in different registers, so each CPU's file says where the
result goes. Where a range does not start at the cursor, its frame is
the whole view, since cells do not say where other text is.

Why, as of 2026-10-24: the user found the macOS window empty to the
accessibility tree, and asked for Linux and Windows too. The whole text,
not the rows in view, is what a native text view gives, so a screen
reader reads past the window's edge as it does in other editors.

## D69. Windows' common controls come from a checked-in resource

Windows draws a list view, a button or a status bar in the Windows 95
style unless the program asks for version 6 of the common controls. The
program asks in its manifest, a resource linked into the binary. `gui/`
holds that resource as a `.syso` file for each Windows architecture,
which Go links into every Windows binary that imports `gui/`, its tests
too. `just winres` writes them with go-winres, and they are checked in,
as the test plugins' `.wasm` files are, so `go build` and `go install`
need no step of their own. The standard library cannot write a
resource, and activating a manifest at run time needs a file on disk and
more Win32 code.

Why, as of 2026-10-24: the user chose the system's list view for
Windows' lists (D68) and an embedded manifest over activating one at
run time.

## D70. The finder keeps its own matcher, not fzf's or fff's

`internal/fuzzy` ranks the finder's matches with fzy's algorithm in Go
(B129). On Chromium's 509,413 files, fzf's Go matcher found the same
matches and was slower on every query, on one thread or ten. The fzf
program prints no match positions, so B152's marks would come from a
different ranker. fff is Rust, which fude could call only through cgo,
and fude keeps cgo to `syntax/`. fff allows typos, so its first results
for `httpserver` are Python scripts under `third_party`, where fude
lists `net/server/http_server.h` first. B129's ranking does not allow
that. fff is faster when a query matches nearly every file, 13.6ms
against fude's 29.1ms for `net`, because it ranks only the top 50. fude
is faster on specific queries, 3.9ms against 26.5ms for
`renderframehostimpl`.

Why, as of 2026-10-24: the user asked whether fzf or fff would make the
picker better, and chose to bring their methods into fude's matcher.

## D71. A plugin's grammar is the user's grant

A plugin may name a grammar for its filetype, and fude builds it only
with `grammar` in the plugin's capabilities (B590). Why: a built grammar
is C that fude loads into its own process, outside the wasm sandbox, so
D8's promise that a plugin cannot crash the editor does not hold for it.
The grant lets the user make that choice, as `process` and `net` do. The
grammar builds as fude's own do (D9), not as wasm, because tree-sitter
runs wasm grammars on wasmtime, and fude would carry a second wasm
runtime beside wazero.

Why, as of 2026-10-25: the user wants org mode as a plugin, and chose a
grant over a grammar any plugin may build, and over one the user writes
into `config.toml` in full.

## D72. Plugins reach the folders the user lists, and write with fs.write

`paths` in a plugin's table lists folders outside the project it may
reach, and `fs.write` is a capability of its own (B585, B586, B587).
Why: org keeps notes in one folder, such as `~/org`, whatever the project
is, and its agenda, capture and refile read and write files there. Going
through buffers would open windows the user did not ask for, and a buffer
cannot list a folder. fude refuses to write a file whose buffer has
unwritten changes, so a plugin never drops the user's edits.

Why, as of 2026-10-25: the user chose a grant of folders over doing
everything through buffers.

## D73. A plugin's key may belong to a filetype and give the key back

A key may name a filetype (B588), and one whose call returns nothing
does what the key does without it (B589). Why: org's keys depend on the
text under the cursor, so `<Tab>` cycles a heading and `<C-a>` steps a
date, and elsewhere they must stay Vim's. A call cannot run fude's own key
without running its own mapping again. B323 already gives insert mode
this rule, so normal mode follows it. Only a key that names a filetype
gives the key back, so the keys modules ship today still do nothing when
they find nothing. The key names a filetype rather than using `In`,
because `In` names the kinds of buffer fude makes, such as explorer.

## D74. Folds come from the grammar when it has a folds query

The fold module folds by a grammar's `@fold` captures when it has a
folds query, and by indent otherwise (B591). Why: org and markdown
sections are not indented, so indent folds miss them. It is a query, not
an interface for modules, because grammars already ship folds queries in
this form, and a plugin's grammar brings its own (D71). Cycling is in the
fold module (B592), because the fold module owns the folds (B502) and
markdown cycles them too. In the middle step for the whole buffer, a
section that holds others still shows its own text, where org hides it.
A fold holds all of a section's lines, and the section's own text has no
fold.

Indent folds leave out a block's closing line, such as Go's `}` or Lua's
`end`. fude ships folds queries for those languages, and for TOML, whose
tables have no indent to fold by. Python and YAML keep indent's folds. Their grammars start a
body on the line after its colon, so a query could only fold a whole `if`
with its `elif` and `else`. A fold stops before a last line that goes on
past its capture, as `} else {` does, so a query folds blocks and the
`else` stays in view. Lua's `if` folds whole, to its `end`, with an
`elseif` or `else` folding inside it.

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

Ex mode and Select mode are gone, and `gQ` puts back multicursors (B397),
as in Neovim 0.13. Modelines are never read. The timeline log
replaces swap files (D19). Why: legacy that fude exists to drop, and
modelines are a long-running source of security bugs.

### N3. LSP keys exist only while a server is attached

Neovim's default LSP keys (B66) are bound when a server attaches to the
buffer and removed when it detaches. Why: in Neovim they
exist from startup, show an error without a server, and make the built-in
`gr{char}` wait `timeoutlen` in every buffer.

### N4. Options have fude's own names

`:set` takes fude's option names, not Vim's, with Neovim's defaults where
the option exists in both, and their values as `config.toml` writes them:
`:set wrap false`, not `:set nowrap`. Why: there is no Vim option
compatibility (product.md boundaries), and one form for an option in both
places is less to learn. The user asked for it on 2026-10-18.

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

### N8. Autocomplete and <C-x><C-o> list every source

With `autocomplete` on, and on `<C-x><C-o>`, the menu lists the items of
every source `completion_sources` names: a language server's, snippets,
paths and the open buffers' words (B206). Neovim's `'autocomplete'` lists
what `'complete'` names, which by default is the buffers' words and no
server's items, and its `<C-x><C-o>` asks `'omnifunc'` alone. Why: one
menu of a server's names, the buffers' words and paths works with a
server and without one, and `completion_sources` picks and orders what
it holds. The keys that ask for one kind, `<C-n>`, `<C-p>`, `<C-x><C-n>`,
`<C-x><C-p>`, `<C-x><C-l>` and `<C-x><C-f>`, work as Neovim's do (B440
to B443).

### N9. No mouse in the terminal

fude's terminal frontend takes no mouse input, where Neovim's `mouse`
defaults to `nvi`. The terminal keeps its own selection, scrolling and
copy. A native frontend takes the mouse as its platform does (D63). Why:
the user chose keys only in the terminal on 2026-10-15, and the mouse
for a native frontend on 2026-10-23.

### N10. Tab with expand_tab goes to the next indent column

With `expand_tab`, `<Tab>` in insert mode types spaces to the next
`indent_width` column, as Neovim's `softtabstop=-1` does. Neovim's default
`softtabstop` of 0 types spaces to the next 8-column tabstop instead. Why:
fude has one indent setting, and a `<Tab>` that indents by it is what most
Neovim configs set.

### N11. :pick finds things

`:pick files`, `:pick grep`, `:pick buffers` and `:pick recent` open the
finder on files, their text, the open buffers and the recent files (B129,
B134, B213). Neovim has no finder, and its `:files` is another name for
`:ls`. Why: one command opens every finder, so each works the same way and
`<Tab>` lists every source. The user chose it on 2026-10-17, in place
of `:files` and `:grep`.

### N12. autocomplete covers the command line

With `autocomplete` on, a `:` command line shows its completions as it is
typed (B131). Neovim's `'autocomplete'` is for insert mode only, and a
command line needs `wildtrigger()` in a `CmdlineChanged` autocommand to do
the same. Why: one setting for completion while typing, and fude has no
autocommands.

### N13. No :grep, and :pick grep is a live grep

fude has no `:grep`. `:pick grep` opens a finder that searches as the query
is typed (B134), where Neovim's `:grep` runs `'grepprg'` once and fills the
quickfix list. Why: the user searches with fff.nvim's live grep. Its
matches reach the last list of places, fude's quickfix list, only when sent
there (N25).

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
user sets both, as many configs do, and asked for it on 2026-10-16.
`split_below` and `split_right` turn each off, which the user asked for on
2026-10-17. `modules/window` and `modules/explorer` read them and ask for
above or left, and every other window opens where it asks (D55), so a
terminal, a task and a timeline still open below.

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

fude draws indent guides unless `guides.enabled` is off (B153), where
Neovim draws none without a plugin. Why: the redesign the user approved on
2026-10-16 shows them, and `:set guides.enabled false` turns them off.

### N20. `:lsp` alone shows the servers, and there is no `:checkhealth`

`:lsp` opens a panel of the language servers `config.toml` sets, with
each one's state and why it failed (B159). Neovim's `:lsp`
needs a subcommand, and its servers' state is in `:checkhealth`. Why:
fude has no providers or plugin checks for `:checkhealth` to cover.
Config errors already show at startup (B93), and server failures show as
messages that `:messages` keeps (B157). The servers' state had nowhere
to show, and the panel shows it.

### N21. A server that is not installed stops trying

When a language server's program is not on PATH, fude says so once and
does not start it for files opened later. `:lsp` shows it as not
installed, and `:lsp restart` or `:lsp enable` tries again (B167).
Neovim tries again for each file it opens and shows the error each time.
Why: fude's PATH does not change while it runs, so a missing program
stays missing. A server installed for some projects only would otherwise
show the same error on every file.

### N22. One server formats, renames and gives signatures

With several servers on a file, formatting, rename and signature help go
to the first server in the filetype's list that offers them (B169).
Neovim formats with every server in turn, renames with every server in
turn, and shows every server's signatures, which `<C-s>` cycles. Why: two
servers formatting the same text undo each other's layout, and two
renames edit the same names twice. The user already orders the servers in
the filetype's list, so that order decides.

### N23. `:restart` keeps unsaved changes

`:restart` saves each buffer's timeline node with the session, and the new
process puts the text back at it, so it never refuses and loses nothing
(B170). Neovim's `:restart` quits with `:qall`, which refuses while a
buffer has unsaved changes, takes `+qall!` to drop them, and takes `!` to
skip the session. Why: fude's timeline already keeps every unsaved change
on disk, so the new process can put them back. Refusing would only make
the user write or drop changes to reload config.

### N24. Key hints show without a plugin

fude lists the keys that may follow a command being typed, as which-key.nvim
does for Neovim (B172). While the hints show, `<Esc>` drops the held keys
of a mapping, `<BS>` goes back a key, `<C-d>` and `<C-u>` turn their pages,
and `key_timeout` waits. Neovim has
no hints without a plugin, and there `<Esc>` after a mapping's first key
runs that key as typed. Why: fude's keys and mappings already say what
they do for `:h`, so the hints need only a panel. Someone reading the
hints should not race `key_timeout`, and `<Esc>` there means "none of
these". The panel pages instead of cutting the list, because `g` or an
operator has more keys than a small screen holds, and a cut list hid
whichever keys sorted last.

### N25. The last list of places is the quickfix list

Every list of places a window shows, such as `grr`'s, `:marks`', a
finder's matches sent with `<C-q>`, becomes the last
list, which `]q`, `[q`, `:cnext`, `:cprev`,
`:cfirst`, `:clast` and `:cc` go through after its window closes (B194).
`:copen` shows it again. fude keeps the last ten lists, as Neovim keeps
ten quickfix lists, and `:colder` and `:cnewer` go back and forth between
them (B501). Neovim also keeps a location list per window, which fude
does not. Where `:colder 5` goes past the oldest list, fude goes as far
as it can and says which list it is, where Neovim goes as far and says it
is at the bottom. Why: fude already shows every list of places in a
window, so the newest one is the one to go through, and a second kind of
list would make the user pick which.

### N26. The timeline keeps its newest megabyte

When a file closes at its last save and its timeline log is over 1MB, fude
cuts the log to the newest changes on the way to that save that fit in
1MB, and drops older changes and the branches off that way (B198). A
session that ended without closing the file, as a crash or a killed
terminal ends one, cut nothing, so the next open cuts the log the same way.
The session that offers a crash's changes with `:recover` does not cut. Neovim
keeps the last 1000 changes of the undo tree, by `undolevels`, whatever
their size. Why: opening a file reads its whole log, at about 7ms a
megabyte, and one `:%s` over a 10MB file adds 315KB. A cap on bytes bounds
the time to open a file, whatever made the changes.

### N27. The windows can sit in the middle of a wide screen

`:center` lays fude's windows out in a centered part of a wider screen,
each as wide as the colour column, and leaves the margins blank (B199). Neovim
has no such command. Plugins get the effect with empty windows or floats
beside the real one, which `<C-w>` keys, `:q` and window counts then see.
Why: on a wide monitor, text at the left edge turns the reader's head. In
fude the margins are no windows at all, only less room for the layout.

### N28. Brackets and quotes close themselves

fude closes a bracket or quote as it is typed, which `autopairs.enabled` turns
off (B202). Neovim closes nothing, and plugins such as nvim-autopairs add it.
Why: most editors people come from close pairs. Pairing only before a
blank or a closing bracket, and never a quote after a letter, avoids the
cases that make it annoying, so it can stay on.

### N29. `:terminal` shows one shell, and <Esc> twice leaves it

`:terminal` shows the session's shell, starting it the first time, and
hides it when its window is current. `:terminal n` does the same for
shell n, as `:b n` names a buffer. It opens below in terminal mode, a
third of the screen tall unless `terminal_height` says otherwise, and a
modifier or `[open]` may place it elsewhere (D55). `:terminal cmd`
opens a new window each time. Neovim's replaces the current window,
starts a new shell each time and starts in normal mode. `<Esc>` twice leaves terminal mode as `<C-\><C-n>` does. A
single `<Esc>` waits 300ms for a second one, then goes to the program,
ahead of any other key, so an Alt chord stays one. In normal mode the
window holds the scrollback and screen as text, as they were when
terminal mode was left, without the program's colours. `i` and `a` go
back to the live screen (B205). Why: a terminal usually runs beside the
file being edited, so it opens below it. One shell that hides and shows
again keeps its history and directory. A new shell each time loses them. `<Esc><Esc>` is quicker to type
than `<C-\><C-n>`. Text that stays as it was keeps the cursor and marks in
place in normal mode while the program goes on writing.

### N30. Snippets come from VS Code's snippet files

fude reads snippets from `snippets/<filetype>.json` and `snippets/all.json`
in its config directory, in VS Code's format, and lists them as
completions, where Neovim has `vim.snippet` to expand them and no
snippets of its own (B207). Why: friendly-snippets, the collection most
Neovim snippet plugins load, ships in VS Code's format, so it works as it
is.

### N31. `q` closes fude's own windows

In a list of places, a timeline, help, the `lsp` panel and the windows fude
writes, such as `:messages` and a diff, `q` closes the window (B212).
Neovim records a macro there. Why: these windows are read-only, so a macro
in them has nothing to edit, and configs such as LazyVim map `q` to close
them. An explorer keeps `q`, as it is a directory you edit.

### N32. s jumps

`s` in normal and visual mode starts a labelled jump (B218), as flash.nvim
does, where Neovim's `s` deletes the characters under the cursor and
starts insert mode. Why: `cl` and `c` do what `s` did, and a jump reaches
any text in view in three or four keys. The user asked for jump labels on 2026-10-17. `s` does not
start a jump after an operator, as `ds`, `cs` and `ys` are surround's
(B219).

### N33. Surround is built in, and S in visual mode surrounds

`ys`, `ds`, `cs` and `S` in visual mode add, delete and change the brackets
and quotes around text (B219), as vim-surround does. Neovim has none of
them, and its `S` in visual mode changes the selected lines. Why: the user
asked for surround on 2026-10-17, and `R` in visual mode still changes the
selected lines. `ys`, `ds` and `cs` have no meaning in Neovim, so they take
nothing away. `modules/surround` offers it, through the vim machine's
operators and keyed commands that wait for characters (B322).

### N34. Folds come from indent, and zf adds one by hand

The fold module folds a buffer by indent, as VS Code does by default
(B224), and `zf` closes a fold over the lines a motion covers. Neovim's
default `foldmethod` is manual, where only `zf` makes a fold, and
`foldmethod` offers indent, syntax, markers and expressions. A fold by
indent starts on the line before the lines indented more, where Neovim's
starts on the first of them. fude keeps every closed fold, nested ones
too, and `zo`, `zO`, `zc`, `zC`, `za` and `zA` open and close them a
level or every level at a time as Neovim's do (B502). Closing a fold puts
the cursor on its first line, where Neovim leaves it on its own line
inside the fold, so a count on `zo` opens more than one level only where
folds start on the same line. fude has no `foldmethod` and no
`foldlevel` or `foldenable` options. `zm` and `zr` take the level from
the folds closed now, and `zi` keeps the folds it opens to close them
again. Why: indent folds work in every
file, with or without a grammar, and grammar repos ship no fold queries
(D28). The user asked for folding on 2026-10-17.

### N35. <C-n> adds a multicursor

`<C-n>` in normal mode selects the word under the cursor, and in visual
mode places a multicursor that keeps the selection and selects its next
match (B232), as VS Code's `<C-d>` and vim-visual-multi's `<C-n>` do.
Neovim's `<C-n>` in normal mode moves down a line, as `j` does. Why: `j`
does that already, and the user asked on 2026-10-17 for a way to edit
every match of a word at once. `zq*` does it too, but places every multicursor at once.

### N36. Visual > and < keep the selection

In visual mode, `>` and `<` shift the selected lines and keep them
selected (B235). In Neovim they end visual mode, so a second `>` shifts
only the cursor's line. LazyVim maps `>` to `>gv` and `<` to `<gv` to
fix that. Why: indenting a block by several levels takes one `>` per
level, and the user asked on 2026-10-17 for `V>>>` to keep shifting the
block. The other operators still end visual mode.

### N37. ]e and [e move lines

`]e` and `[e` move the line, or the selected lines, down and up (B236),
as vim-unimpaired's do. Neovim has no default keys for this, and LazyVim
maps `<A-j>` and `<A-k>` to `:m`. Why: the user asked on 2026-10-17 for
a way to move a line or a block. fude reads an Alt chord as `<Esc>` and
then the key, which is what a terminal without the kitty protocol sends.
So fude has no Alt keys to map. `]` and `[` already start fude's other pairs, such as
`]h` and `]q`.

### N38. Replacements show live, not by a conceal level

A module's decoration may draw other text in place of its range (B295),
and the cursor's line and a selection's always show their text. Neovim
hides text with `conceal` and decides how much shows with `conceallevel`
and `concealcursor`. Why: what you edit is always what you see, as
Obsidian's live preview does, and one rule replaces four levels and a
mode list. A window option to turn rendering off comes with the module
that renders.

### N39. Pins are a short list of files for each working directory

`:pin` keeps up to 9 files for the working directory, in the order the
user chooses, and `<Leader>1` to `<Leader>9` go to them (B356, B357), as
harpoon does. Neovim has no such list, and its file marks `A` to `Z` keep
a place, not a file, in one set for every directory. fude departs from
harpoon in five ways. A pin follows its file through an explorer's rename
(B360), where harpoon loses it. The list's window writes with `:w`
(B358), as an explorer does, where harpoon writes as the window closes.
The status line shows a file's pin as `2/4` (B359), where harpoon's
users add a tabline. fude maps the keys, so the hints show them, where
harpoon maps none. Each session has its own pins (B361). Why: the user
asked on 2026-10-19 for a list like harpoon's and chose each of these.
`q` closes the list's window though its lines are edited, as harpoon's
does, where an explorer keeps `q` (N31).

### N40. `=` formats as `gq` does

`=` puts the lines it covers through the filetype's formatters, or else
the language server, as `gq` does (B234). Neovim reindents them with
`indentexpr` or C indenting. Why: fude has no indent engine, and the user
chose on 2026-10-20 that `=` format rather than reindent by bracket depth
or by tree-sitter queries.

### N41. A timeline log goes 30 days after its last change

fude deletes a file's timeline log when the file changed outside fude, and
at start the logs no session has written for 30 days (B382). Closing a
file writes to its log, so a file opened in that time keeps its timeline.
Neovim's `undodir` keeps every undo file until the user deletes it. Why:
fude kept one log for every file ever opened, and one more each time a file
changed outside fude, and nothing removed them. The user chose on 2026-10-20 to
drop the logs nothing reads and those of files left alone for 30 days.

### N42. A lone Esc waits 10ms

A terminal without the kitty keyboard protocol sends Esc as one byte, which
also starts every escape sequence, so fude waits for more before it takes
the byte as Esc. It waits 10ms, and on Windows tcell's own 125ms, as
Windows' terminal input comes in delayed batches. Neovim waits 50ms, its
`ttimeoutlen`. Why: a terminal writes a sequence in one go, so its bytes
come together, and 50ms on every Esc is the longest wait a key has. With
the kitty protocol, which fude asks for, Esc is a sequence of its own and
waits for nothing. `TCELL_ESCDELAY`, in milliseconds, sets a longer wait
for a link that splits sequences.

### N43. Follow is not an option, and multicursors end with their buffer

`q=` turns follow on and off for the session, where Neovim keeps it in
the buffer-local `'follow'` option. Multicursors belong to one buffer, and
showing another in the window removes them, where Neovim keeps each
buffer's multicursors until it is current again. Why: one vim machine
runs every window of a session, and a set of cursors for each buffer is
more state for little use.

### N44. Tab pages, beside the buffer line

fude has Neovim's tab pages, each with its own layout of splits and
floats, and the session module keeps them all as fude quits (B544, B546).
Why: a native frontend's tabs need a page to stand for, and a second
layout no longer needs a second session through `fude attach`. Until 2026-10-23 fude
had none, since splits, floats, pins and the buffer line gave each place
its own view. The user chose them that day, for the terminal too.

### N45. No tags

fude reads no tags file, so there is no `:tag`, `<C-t>` or tag stack, and
`<C-]>` goes to a definition only through a language server (N3). Why: a
tags file has to be generated and kept up to date, and a language server
answers the same question from the code as it is.

### N46. Files open as buffers, not as an argument list

`fude a b` opens each file as a buffer (B80), and there is no `:args`,
`:next` or `:argdo`. `:bufdo` runs a command in every buffer. Why: the
buffer list and the buffer line already hold the files given, and a second
list of the same files is a second thing to keep in step.

### N47. Typing keeps the line's indent, with no indent rules for a language

A new line takes the indent of the line above, and one level more after an
opening bracket, with the bracket's closing one moved to a line of its own
(B211). fude has no `indentexpr`, `cindent` or indent queries. Why:
grammar repos ship no indent queries (D28), fude has no indent engine
(N40), and a formatter, which `format_on_save` runs, puts each line where
the language wants it.

### N48. Some keys do fude's own thing

`-` in normal mode opens the explorer on the file's directory (B124),
where Neovim moves up a line, which `k` does. `<C-w>z` zooms the window
(B370), where Neovim closes a preview window, which fude does not have.
`gh` and `gH` stage hunks in git (B221), where Neovim starts Select mode,
which fude does not have (N2). Why: each key's Neovim meaning is one
fude has no use for, or one another key already gives.

### N49. Keymaps live in config.toml, not in commands

fude has no `:map`, `:noremap`, `:autocmd`, `:command`, `:let` or
`:source`. Keymaps are tables in `config.toml` (D27), and logic that runs
on events lives in a module or plugin. Why: commands that change mappings
and register callbacks at run time are the config that runs later, which
D27 rules out, and `:config reload` already applies a changed
`config.toml`.

### N50. A word is letters, digits and `_` in every language

`w`, `iw`, `*` and abbreviations take a word as a run of letters, digits
and `_`. There is no `iskeyword` to add `-` for CSS or Lisp. Why: one rule
keeps every word key the same in every file, and the syntax node objects
(B45) already select a language's own units, such as a CSS property or a
Lisp symbol.

### N51. No :make, errorformat or :vimgrep

fude has no `:make`, `makeprg`, `errorformat` or `:vimgrep`. A task runs
a build or the tests in a terminal window (B228), language servers show
a build's errors as diagnostics where they are (B61), and `:pick grep`
searches the project as the query is typed (N13), sending its matches to
the quickfix list with `<C-q>` (N25). Why: `errorformat` is a pattern
language to learn for each compiler, which diagnostics make needless in
a language with a server, and `:vimgrep` is `:pick grep` run once.

### N52. One working directory per session

`:cd` sets the session's working directory (B155), and there is no
`:lcd` or `:tcd`. Why: `:tcd` belongs to tab pages, which fude does not
have (N44), and a directory for each window would make one relative path
name a different file in each, while the finders, the explorer and the
tasks all work from the one directory.

### N53. fude watches no files

fude tells language servers of the files it changes itself: what it
writes, and what the explorer creates, renames and deletes. It also
tells them of an open file it finds changed on disk. It runs no watcher
over the project. Why: the standard library has no file watcher, and one
means a dependency with code for each system, or polling every directory
of the project while fude runs. The changes that matter while editing are
the ones fude makes.

### N54. The rest of Neovim's options stay fixed or absent

Beside the options fude has and those N44 to N53 leave out, the options
below stay as fude has them: absent, or fixed at one value, most often
Neovim's default (N4). Windows in diff mode scroll together without
`scrollbind`, `cursorbind` or `scrollopt` (B523).

`laststatus`, `cmdheight`, `cmdwinheight`, `ruler`, `showcmd`,
`showcmdloc`, `showmode`, `statusline`, `rulerformat`, `winbar`,
`showtabline`, `numberwidth`, `display`, `cursorlineopt`, `guicursor`,
`titlestring`, `titlelen`, `titleold`, `icon` and `iconstring` stay
fixed. Why: every window has a status line and the command line floats
(B147, B148), the modules add their segments to it, and a format string
for the bars or the title is config that runs, which D27 rules out.

`winborder`, `winhighlight`, `winblend`, `pumblend`, `pumborder`,
`pumwidth`, `pummaxwidth`, `completeitemalign`, `winheight`, `winwidth`,
`winminheight`, `winminwidth`, `splitkeep`, `equalalways`, `eadirection`,
`winfixbuf`, `winfixheight`, `winfixwidth`, `winpinned` and `switchbuf`
stay fixed. Why: the layout sizes itself and `<C-w>=` evens it (B193), a
module's window stays where it opened (D55), and floats share one look
that the theme colours.

`scroll`, `scrolljump`, `scrolloffpad`, `smoothscroll`, `sidescroll`,
`startofline`, `whichwrap`, `jumpoptions`, `backspace`, `selection`,
`nrformats`, `joinspaces`, `matchpairs`, `tildeop`, `cedit`, `digraph`,
`langmap`, `keymap` and `timeout` stay fixed. Why: they change what a key
does, and every key goes through one grammar that macros, plugins,
remote clients and agents share, so a key must do the same thing in
every fude.

`formatoptions`, `formatlistpat`, `wrapmargin`, `autoindent`,
`smartindent`, `copyindent`, `preserveindent`, `shiftround`,
`vartabstop`, `infercase`, `showmatch`, `matchtime`, `comments` and
`commentstring` stay fixed. Why: `text_width` keeps a comment's marker
and a list item's indent (B450), typing keeps the line's indent (N47),
`gc` takes a language's comments from its grammar, and a formatter does
the rest.

`complete`, `completeopt`, `autocompletedelay`, `autocompletetimeout`,
`completetimeout`, `dictionary`, `thesaurus`, `wildmenu`, `wildmode`,
`wildoptions`, `wildchar`, `wildcharm`, `wildignore`, `inccommand`,
`history` and `messagesopt` stay fixed. Why: `completion_sources` picks
where completions come from (B206), the finders skip what git ignores
(B210), `:s` shows its changes as they are typed (B154), and the
histories and `:messages` keep a fixed number of entries (B157).

`autoread`, `autowrite`, `autowriteall`, `confirm`, `hidden`,
`fileformat`, `fileformats`, `readonly`, `modified`, `modifiable` and
`exrc` stay fixed. Why: a file unchanged in fude reloads when it changes
on disk (B82), a buffer may stay unsaved in the background, fude writes
only when asked and refuses to quit with unsaved changes (B5), line
endings stay as found (B3), fude alone says what may change (B302), and
a project's own config would be config that runs (D27).

`sessionoptions`, `shada`, `shadafile`, `viewdir`, `viewoptions`,
`autochdir`, `cdpath`, `cdhome`, `path`, `suffixesadd`, `keywordprg`,
`shell` and its kin, and `scrollback` stay fixed. Why: a session keeps
its windows and files (B226) and what fude held comes back in the next
run (B454), the working directory changes only with `:cd` (N52), the
finders and language servers find files and documentation, terminals and
tasks run `$SHELL`, and a terminal keeps a fixed scrollback.

`foldlevel`, `foldlevelstart`, `foldopen`, `foldclose`, `syntax`,
`synmaxcol`, `spelllang`, `spellfile`, `spellcapcheck`, `spelloptions`,
`spellsuggest` and `mkspellmem` stay fixed, and `signcolumn` and
`colorcolumn` stay one switch and one column as `sign_column` and
`color_column`. Why: folds start open and `zm` and `zr` set their level
(N34), `large_file` turns syntax off for a big file, spelling reads one
word list, `spell.words`, and one sign column and one shaded column
cover what a window needs.

### N55. `:diff` stays the timeline's

In Neovim `:diff` is short for `:diffupdate`. In fude `:diff` compares two
states of a buffer's timeline (B32), and diff mode's `:diffupdate` answers
to its full name and `:diffu`. Why: fude's diff mode compares again after
every edit, so `:diffupdate` is rarely typed, while comparing two moments
of the timeline is fude's own and was `:diff` first. Giving the name to
diff mode would take it from the command fude's users type more.

### N56. A server's features are on when it offers them

Linked editing ranges, formatting as text is typed and inline completion
work whenever a server offers them (B472, B473, B474), where Neovim
leaves them off until a `vim.lsp.*.enable()` call. Semantic tokens and
pulled diagnostics are on in both (B470, B471). Why: fude has no Lua to
make that call, and naming a server for a filetype in `config.toml` is
already the choice to use what it offers, as inlay hints show unless set
off (B216). An option for each would be five switches to find before a
server works.

### N57. A file opens at a line as `path:N`

`fude a.go:12:5` opens a.go at line 12, byte 5 (B80). Compilers, `grep -n`
and stack traces print a place in this form, and Neovim cannot open it.
fude runs each `+cmd` as a `:` command once the files and modules load, so
`+N`, `+` and `+/pat` are `:N`, `:$` and `:/pat`. There is no `-c`,
`--cmd`, `-t` or `-q`. Why: a place pasted from a tool's output should
open as printed, and `:` commands already cover every other start-up need.
A colon before anything but a number stays in the name, as in `C:\a.go`.
A name that is already a file opens as that file.

## Open

None.
