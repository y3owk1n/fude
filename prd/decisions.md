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
possible later, as Zed built. D40 reopens one part: fude listens on a
network address when `fude --share` asks it to.

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
capabilities draw it. A plugin that uses only `sdk`'s reads is about 30KB,
and one that asks for anything is about 200KB, with `encoding/json` (D51). Go's own `wasip1`
target would need WASI in the sandbox, builds modules of megabytes, and its
runtime takes longer to start. TinyGo leaves out parts of the standard
library, such as `os` and `net`, which a plugin reaches through fude's
functions anyway.

## D37. The modules are released together, and the first release brings go install

fude's modules are the root, `api/`, `core/`, `vim/`, `syntax/`,
`protocol/`, `plugin/` and `sdk/`. They take one version together, tagged
`vX.Y.Z`, `api/vX.Y.Z`, `core/vX.Y.Z`, `vim/vX.Y.Z`, `syntax/vX.Y.Z`,
`protocol/vX.Y.Z`, `plugin/vX.Y.Z` and `sdk/vX.Y.Z` on one commit. Until the
first release, the root's `go.mod` points at `api/`, `core/`, `vim/`,
`syntax/`, `protocol/` and `plugin/` through `replace` lines, so `go install` refuses it and
fude builds from a clone. A release drops the `replace` lines and requires
the tags, which makes `go install github.com/y3owk1n/fude/cmd/fude@vX.Y.Z`
work. `just standalone` builds every module outside the workspace, so a
module whose `go.mod` lacks a requirement fails before a release does. Why:
`core/` and `vim/` stay modules of their own, for programs that embed them
without fude's dependencies (D22, D23). Requiring each other by commit
before there are releases would add a second commit and push to every
change of `core/` or `vim/` the editor depends on.

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

## D39. An attached client draws the session's views, sent as frames

`fude --attach` is fude's terminal client drawing a session of another
fude (B239). The server sends the session's frame, the views, floats,
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
whose `go.mod` brings the editor's dependencies. Until the first release,
`plugin/` requires `api/` through a `replace` line, which Go ignores in a
dependency. So a plugin's module can require `plugintest` from the first
release on, as with `go install` (D37). A program that
runs fude plugins imports the host without the editor, cgo or a terminal.
The cost is a seventh tag on every release (D37).

## D46. Packages no other program needs are internal

`internal/` holds `clipboard/`, `pty/` and `relay/`. Each is small, and only
fude uses it. Why: a program may import any package outside `internal/`,
and fude would have to keep that package's API for it. These have no
such program.

## D47. One package holds what goes over the socket

`wire/` holds the types fude and its clients send each other: a replica's
edits, pulls and pushes, and the frames an attached client draws. `editor/`,
`client/` and `ui/term/` use it. A guest's hello stays in `editor/`, since
it carries the guest's own keymaps and options. Why: each side had its
own copy of the replica types, and two copies of one protocol can
disagree with nothing to catch it. A client that draws frames needs
them without the editor. `wire/` stays in the root module until a client
outside it needs the types. It takes `Span` from `syntax/`, and a view
carries its `kernel.Snapshot` for a client in the same process, so it
imports both.

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
plugin that asks for anything is about 200KB, and a cold load of one took 112ms before its compiled code
was cached, against 2.4ms after. A plugin now compiles before its init
budget starts. A result of 500 decorations takes 2.7ms in the plugin and
0.9ms for fude to read. Keys a result runs may run any command, so they
need a capability of their own, `keys`. A module's stream job runs the
module's own Go code. A plugin's calls take turns, so its own code cannot
run as a job while it answers calls. A plugin's stream job runs a program
instead, and each line the program prints comes as a callback. Until the first release, the sdk on the module proxy
requires an `api` the proxy does not have, so `fude plugin new` in a build
that is not a release fails, and B173's test skips (D37).

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
2026-10-17. A terminal, a task and a timeline still open below.

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
of a mapping, `<BS>` goes back a key, and `key_timeout` waits. Neovim has
no hints without a plugin, and there `<Esc>` after a mapping's first key
runs that key as typed. Why: fude's keys and mappings already say what
they do for `:h`, so the hints need only a panel. Someone reading the
hints should not race `key_timeout`, and `<Esc>` there means "none of
these".

### N25. The last list of places is the quickfix list

Every list of places a window shows, such as `grr`'s, `:marks`', a
finder's matches sent with `<C-q>`, becomes the last
list, which `]q`, `[q`, `:cnext`, `:cprev`,
`:cfirst`, `:clast` and `:cc` go through after its window closes (B194).
`:copen` shows it again. Neovim keeps one quickfix list and a location list
per window, which commands fill and others list. Why: fude already shows
every list of places in a window, so the newest one is the one to go
through, and a second kind of list would make the user pick which.

### N26. The timeline keeps its newest megabyte

When a file closes at its last save and its timeline log is over 1MB, fude
cuts the log to the newest changes on the way to that save that fit in
1MB, and drops older changes and the branches off that way (B198). The
session that offers a crash's changes with `:recover` does not cut. Neovim
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

fude closes a bracket or quote as it is typed, which `autopairs` turns off
(B202). Neovim closes nothing, and plugins such as nvim-autopairs add it.
Why: most editors people come from close pairs. Pairing only before a
blank or a closing bracket, and never a quote after a letter, avoids the
cases that make it annoying, so it can stay on.

### N29. A terminal opens below, and <Esc> twice leaves it

`:terminal` opens in a window below, a third of the screen tall unless
`terminal_height` says otherwise, in terminal mode, where Neovim's
replaces the current window and starts in normal mode. `<Esc>` twice leaves terminal mode as `<C-\><C-n>` does. A
single `<Esc>` waits 300ms for a second one, then goes to the program,
ahead of any other key, so an Alt chord stays one. In normal mode the
window holds the scrollback and screen as text, as they were when
terminal mode was left, without the program's colours. `i` and `a` go
back to the live screen (B205). Why: a terminal usually runs beside the
file being edited, so it opens below it. `<Esc><Esc>` is quicker to type
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

### N34. Folds come from indent, through zc, zo, za, zR and zM

The fold module folds a buffer by indent, as VS Code does by default
(B224). Neovim's default `foldmethod` is manual, where `zf` makes a fold,
and `foldmethod` offers indent, syntax, markers and expressions. fude has
no `zf`, `foldmethod`, `zj`, `zk`, `zC` or `zO`. Why: indent folds work in every
file, with or without a grammar, and grammar repos ship no fold queries
(D28). The user asked for folding on 2026-10-17.

### N35. <C-n> adds a region

`<C-n>` in normal mode selects the word under the cursor, and in visual
mode adds its next match as another region (B232), as VS Code's `<C-d>`
and vim-visual-multi's `<C-n>` do. Neovim's `<C-n>` in normal mode moves
down a line, as `j` does. Why: `j` does that already, and the user asked
on 2026-10-17 for a way to edit every match of a word at once.

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

## Open

None.
