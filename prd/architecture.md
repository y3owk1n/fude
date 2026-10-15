# Architecture

## Layers

```
clients   terminal UI | GPU UI | web UI | remote fude | agent
            | keys, mouse, Requests       ^ frames, events
modules   statusline | picker | syntax | lsp | plugins (wasm)
            | Requests, via api/          ^ events
editor    the loop | event queues | budgets | files | timeline log
            |    keys --> vim machine --> edits        (vim/, embeddable)
            | Transactions                ^ changes
kernel    buffers | marks | transactions | timeline    (core/, embeddable)
            |
storage   rope (persistent, structural sharing)        (core/)
```

Each layer talks only to the one below it, and listens to events from it.
The vim machine sits beside the editor, not below it. The editor feeds it
keys and text, and applies the edits it returns. It never sees the kernel.

Step 6 puts an RPC boundary between clients and modules. Until then the
terminal client calls the module API in-process, behind the same interface.

## Packages

```
core/            Go module: stdlib only, no cgo
  rope/          persistent rope
  kernel/        buffers, marks, transactions, timeline
vim/             Go module: the vim grammar, no dependency on core/
                 Command, keys, keymap trie, modes, registers, resolution,
                 built-in operators and targets, ex parsing, regex
api/             fude's module API: events, decorations, UI, registration
editor/          the editor loop, config, event queues, budgets, the vim host
syntax/          tree-sitter parsing, highlights, tree targets
plugin/          wazero host, the ABI that mirrors api/
lsp/             language server client
ui/term/         terminal client
cmd/fude/        main, distribution wiring
```

Modules import `api/`, never `core/` or `vim/` directly. Only `editor/`
holds a kernel and a vim machine, and it holds the machine through a small
grammar interface (`Feed`, `Timeout`, `Registry`), so a distribution can
swap in another grammar. The kernel budget in `prd/product.md`
counts `core/`.

## The kernel as a library

`core/` is a Go module of its own, so another program (a TUI text field, a
collaborative server, a code tool) can import it without pulling in
tree-sitter, wazero or a terminal. Its exported API is a public boundary
with semver, and its behaviours are tested through that API.

To stay reusable the kernel holds mechanism, never policy:

- **No goroutines.** The kernel is a data structure. Writes come from one
  goroutine the host chooses. Snapshots are safe to read from any goroutine.
- **No I/O.** It never touches a file, a path or a clock it was not given.
  Loading text, saving files and persisting the timeline go through
  interfaces the host implements.
- **No editor concepts.** It knows text, marks, transactions and history.
  Commands, modes, keys, views, registers, syntax and files are above it.
  A transaction's origin and label are opaque values the host supplies.
- **No dependencies.** Standard library only, enforced in CI.

The kernel's surface:

```go
func New(text string, store Store) *Buffer
func Load(text string, head NodeID, nodes []Node, store Store) (*Buffer, error)

func (b *Buffer) Snapshot() Snapshot
func (b *Buffer) Apply(t Transaction) error // ErrStale unless at its base
func (b *Buffer) BeginGroup()
func (b *Buffer) EndGroup()
func (b *Buffer) Undo() (start int, ok bool)
func (b *Buffer) Redo() (start int, ok bool)
func (b *Buffer) Goto(id NodeID) (start int, ok bool) // any node, any branch
func (b *Buffer) At(id NodeID) (Snapshot, bool)        // the text at a node
func (b *Buffer) Head() NodeID
func (b *Buffer) Nodes() []Node
func (b *Buffer) SetMark(pos int, g Gravity) MarkID
func (b *Buffer) MoveMark(id MarkID, pos int)
func (b *Buffer) DeleteMark(id MarkID) // its id may name a later mark
func (b *Buffer) Listen(l Listener) // hears every change, with the text before it
func (b *Buffer) Mark(id MarkID) int

type Store interface{ Append(Node) }
type Listener interface{ Changed(before Snapshot, edits []Edit) }
```

A transaction carries the host's time, since the kernel reads no clock.
The host restores a state or diffs two. It reads both texts with `At` and
applies or compares them. `Rebase`, `Map` and the change listener
come with the behaviours that need them (B76).

The kernel calls its store synchronously on the writing goroutine. fude's
store appends one line to the timeline log per node and reports its own
errors, since the edit is already applied.

## The grammar as a library

`vim/` is a second Go module. A program that wants Neovim's editing model
over its own text (a TUI text field, a REPL, another editor) imports it
without `core/`. fude is one host of it.

The machine is a state machine with no goroutines, no I/O and no clock. The
host feeds it keys, and it reads and acts through interfaces the host
implements:

```go
type Text interface { // read-only, core's Snapshot satisfies it as is
    Len() int
    Slice(start, end int) string
    Lines() int
    LineStart(line int) int // lines count from 0
    LineOf(pos int) int
}

type Host interface {
    Text() Text
    Cursor() int
    SetCursor(pos int)
    Apply(edits []Edit, label string) error
    BeginGroup()
    EndGroup()
    Unhandled(keys string) // ex lines and <C-w> keys vim does not own
    Undo() (start, cursor int, ok bool)
    Redo() (start, cursor int, ok bool)
    Step(n int) (start int, ok bool) // g- and g+, in time order
    SetMark(name rune, pos int)      // marks move with edits
    Mark(name rune) (int, bool)
    Viewport() (top, height int)
    SetTop(top int)
    PushJump(pos int) // jumps may cross buffers, so the host keeps the lists
    Jump(n int) bool
    Change(n int) bool
    IndentWidth() int // the buffer's setting, so filetypes can differ
}

func New(h Host) *Machine
func (m *Machine) Feed(key Key) error
func (m *Machine) Paste(text string) error
func (m *Machine) Map(mode Mode, from, to string)
func (m *Machine) Idle() bool // normal mode, between commands
func (m *Machine) Waiting() bool
func (m *Machine) Timeout() error
func (m *Machine) Object(name string, find func(t Text, start, end int) (int, int, bool))
func (m *Machine) Operator(name string, apply func(t Text, start, end int, lines bool) []Edit)
func (m *Machine) Selector(find func(t Text, query string, start, end int) ([][2]int, error))
func (m *Machine) Command(name string, run func(t Text, pos, count int) []Edit) // the key <Plug>(name)
func (m *Machine) Cursors() []int // the other cursors while c types into several regions
```

`:select` asks the host's selector for regions, and of two that overlap the
earlier stays. The next `d`, `x`, `y` or host operator acts on every region
as one undo step, and any other key keeps only the first region. `c`
deletes every region and types into all of them: each edit made at the
first cursor is repeated at the others, moved by their distance from it. So
`<C-w>` takes the same number of bytes at each cursor, not each cursor's
own word. `<C-o>` and `<Esc>` end the other cursors, and dot repeat makes
the change at one cursor (B42).

A later behaviour adds a clipboard for `"+`.


`vim/` owns the grammar, modes, registers, dot repeat, macros, search and
the edit-producing ex commands. It keeps its visual anchor and query
selection regions as marks through `Host.Marks()`, so another view's edits
move them (B23). The host owns files, windows, history
storage, the clipboard and everything shown on screen. `:w` and `<C-w>v`
reach the host through `Unhandled`.

Tree targets are not part of `vim/`. `editor/` adds `af`, `if` and the rest
from `syntax/` with `Machine.Object`, and `gc` with `Machine.Operator`, the
way a plugin will.

`vim/` may use small pure-Go dependencies for Unicode segmentation and
display width. It never imports `core/`, and `core/` never imports it.

## Extension

A module is a Go package compiled into a distribution. A plugin is a wasm
file loaded at runtime. Both get the same API: what a built-in module can
do, a plugin can do through the ABI, gated by capability.

| Extension point | Adds | Runs |
|---|---|---|
| Operator | a verb, composes with every target | input path, budgeted |
| Target | a motion or text object, composes with every operator | input path, budgeted |
| Ex command | `:name args` | input path, budgeted |
| Keymap binding | keys to a command, an ex line, or a plugin function | input path, budgeted |
| Event handler | reacts to an event | off the input path |
| Decoration | highlight, virtual text, sign on a range | rendered by clients |
| UI element | statusline segment, floating window, picker source | rendered by clients |
| Completion source | items for insert-mode completion | off the input path |
| Client | anything over RPC (step 6) | its own process |

A call on the input path that runs over budget cancels the command, stops
the plugin, and tells the user (B51). The keystroke never waits longer than
the budget.

A user who wants a different grammar builds a distribution without
`vim/`. A plugin cannot replace the grammar, because the input path
must stay inside the latency budget.

## Concurrency

The editor loop in `editor/` is the one goroutine that writes to the
kernel. It reads input, runs the grammar, resolves commands and applies
transactions. Everything else works on
snapshots. A snapshot is a rope root and a version, and a persistent rope
makes it free.

Background work (parsing, LSP, plugins, persistence, rendering) takes a
snapshot, works off the loop, and posts its result back to the loop's queue
tagged with the version it read. A result for an old version is mapped
through the edits since, or dropped.

## Text and positions

The rope holds UTF-8 bytes. A position is a byte offset. Each rope node
caches its byte count, newline count and UTF-16 code unit count, so line,
column and LSP position conversions are O(log n).

Grapheme clusters and display width are computed where they are needed,
around the cursor and on the visible lines. They are never stored.

## Command

```go
type Command struct {
    Count    int    // 0 means no count was typed
    Register rune   // 0 means the unnamed register
    Op       string // registered operator, "" means move the cursor
    Target   Target
    Text     string // inserted text, or an ex operator's argument
}

type Target struct {
    Name string // registered target: "word", "find", "inner_quote", "tree", "lines", "selection"
    Arg  string // its argument: the char for f, the pattern for /, the query, the line range
}
```

Operators and targets are registered by name, never stored as functions, so
a command serializes to JSON and back unchanged. Plugin names are prefixed
with the plugin name.

Counts multiply as in Neovim. `2d3w` is `Count: 6`.

### Resolution

```go
type Range struct {
    Anchor, Head int  // byte offsets, Head is where a move puts the cursor
    Kind         Kind // charwise, linewise, blockwise
}

type TargetFunc   func(t Text, at, count int, arg string) ([]Range, error)
type OperatorFunc func(t Text, rs []Range, reg *Register, text string) ([]Edit, error)
```

The machine resolves a command against the host's text and cursor. It
calls the target and gets ranges, several for blockwise or a query
selection (B42). With no operator, the cursor moves to the last range's
head. Otherwise the operator turns the ranges into edits, which the host applies
as one transaction. Inclusive, exclusive and linewise rules belong
to the target, as Vim's motion types do.

### Rules

- **Dot repeat** replays the last command whose operator changes text,
  resolved at the current cursor. Moves and yanks are never repeated. After
  a visual operator, `.` applies to a region of the same size at the cursor,
  as in Neovim.
- **Macros** are key text in a register. `@x` feeds the text through the
  grammar, which produces commands. `:reg x` shows the text and editing it
  changes the macro (B17). A macro can hold anything keys can do, including
  `:w` and `<C-w>v`.
- **Ex edits** are commands. `:3,5d` is `delete` on `lines` with
  `Arg: "3,5"`. `:s/a/b/g` is `substitute` on `lines` with the pattern in
  `Text`. Ex commands that do not edit text (`:w`, `:q`, `:e`) are module
  calls.
- **Insert** is `change` on an empty target at the insert position (`i`,
  `a`, `o` differ only in the target). The text typed before `<Esc>` is
  `Text`, so `.` reinserts it.

### Request

Clients and plugins send a command inside a request.

```go
type Request struct {
    Buffer   BufferID
    View     ViewID    // whose cursor to use and move, zero for none
    At       *Position // resolve here through a temporary mark instead
    Base     Version   // the snapshot the sender saw
    Proposal bool      // show as ghost text, apply only on accept (B71)
    Command  Command
}
```

The anchor lives on the request, so it never reaches dot repeat or a
macro. A request with `At` leaves the user's cursor where it was.

## Keys

Keys are written in Vim notation: `d3w`, `<C-w>v`, `<Esc>`, `<CR>`,
`<Space>`. One parser reads them in config keymaps, macro text, `:reg`
output and the RPC `keys` call.

The machine keeps a keymap trie for each of normal, visual and insert mode.
A mapping runs keys, so an ex line is keys ending in `<CR>`. Plugin
functions come with plugins. The machine never maps the keys a mapping
runs, and an operator waiting for its target takes keys unmapped. While typed keys
start a longer mapping, `Feed` holds them and `Waiting` reports true. The
machine reads no clock, so the host waits its key timeout and calls
`Timeout` if no key comes first. A macro replays its keys through the
keymap, and dot repeat replays the keys the mapping ran.

## Views

A view is one buffer shown in one window. It owns a cursor mark, a top-line
mark and a jumplist. The kernel only holds their marks, so another view's
edits move them (B23). Each buffer keeps its own changelist.

The window layout (splits) is outside the kernel. Until step 6 it lives in
`editor/`, and the terminal client draws what `Editor.Views` returns. With
remote clients, each client gets its own views (D16).
After an undo, the view that undid puts its cursor at the start of the
changed text, as Neovim does. Other views only see their marks move.

## Registers

Registers belong to the vim machine and are shared by every view.
Unnamed, `"a` to `"z` with uppercase append, `"0` to `"9`, `"-`, `".`,
`":`, `"/`, `"%`, `"_`, `"+` and `"*` behave as in Neovim. `"+` and `"*`
go through the host's clipboard. fude's uses the OS clipboard command when
there is one, and OSC 52 in a terminal without one, such as over SSH.

## Transactions and the timeline

A transaction is a sorted list of non-overlapping edits, each a range and
new text relative to its base version. It also carries its id, which is its
origin and a sequence number counted per origin, and a label. The origin
is the client the edit came from, such as the terminal client or an agent.
A plugin's edit carries the origin of the client whose action or buffer ran
it, so a plugin's `gsip` undoes with plain `u`. The label names the author
(keys, a plugin, an agent) and the command's key text, which `:timeline`
shows (B31). The ids and the base
version are what step 6 and any later CRDT build on, so they exist from
step 1 even with one client.

The kernel applies a transaction only at its base version. For an older
base it offers `Rebase`, which maps the edits through every transaction
since and fails if one overlaps them. Whether to rebase, re-resolve the
command or give up is the host's choice. `Map(pos, bias, from, to)` maps any
position between two versions, which is how late async results, marks and
proposal ranges follow the text.

The kernel keeps a transaction log beside the timeline. A command such as
`dw` resolves to different edits at a different version, so the log stores
the resolved edits and the label, never only the command.

The vim machine may open a group through its host. Edits in an open group
apply and render immediately, and closing the group makes one timeline
node. Insert mode is one group (B7). `<Left>`, `<Right>`, `<C-w>`, `<C-u>`
and `<C-g>u` in insert mode close it and open a new one, as in Neovim.

A timeline node holds its parent, time, transaction id, label, edits, the
inverse edits and a rope root. The host supplies the time. Roots share
structure, so every node costs only the rope nodes its edits touched.

Undo only reverts the undoer's own transactions. When the undoer's last
transaction is the head, undo moves the head to its parent, and the next
edit starts a new branch (B30). When another origin has edited since, undo
appends a new node with the inverse edits, mapped through the later ones
(B77). With one client the first case is the only one, so this is plain
Neovim undo.

### Persistence

The kernel hands each new node to its `Store`. fude's store keeps an
append-only log per file under the timeline directory, one JSON record per
line. A `base` record holds a hash of the text the log starts from. Each node
gets a `node` record. Every write adds a `save` record with the head and a
text hash, and a clean close adds a `close` record. On open:

- If the disk text matches the last save, or the base, the timeline loads
  (B33).
- If nodes follow that save and no close does, fude offers them back with
  `:recover` (B34). This replaces swap files.
- If the disk text matches neither, fude renames the log with a timestamp
  and starts a new one.

## Events

The kernel reports only changes, through its listener. `editor/` turns
that into `buffer.changed` and emits the rest by name: `buffer.opened`,
`buffer.changed`, `buffer.saved`, `buffer.closed`, `view.moved`,
`mode.changed`, `syntax.updated`, `lsp.diagnostics`, plus any a plugin
declares under its own prefix.

Each subscriber has its own queue, and emitting never blocks. An event
carries the snapshot it describes. A subscriber that falls behind gets one
`buffer.changed` covering every edit since the last one it handled, so it
never processes stale states one by one.

## Decorations

A decoration is a mark range owned by a plugin or module, carrying a style.
It moves with edits like any mark, and goes when its owner unloads (B55).
Clients render them and know nothing about where they came from. Virtual
text and signs come with the behaviours that need them.

Marks live in a slice, which the kernel walks on every edit, and a deleted
mark frees its slot for the next one. Syntax highlights are not marks. The
syntax goroutine sends spans, and the frame moves them through the edits
since (B43). If decorations ever number in the tens of thousands, marks move
to a tree ordered by position that shifts lazily, as Neovim's marktree
does.

## One keystroke

```
key -> client -> grammar (keymap trie, mode state)
    -> Command when complete -> resolve against snapshot -> Transaction
    -> kernel applies -> timeline node -> event "buffer.changed"
    -> client renders from the new snapshot and current decorations
    -> modules react off the loop (reparse, lsp didChange, plugins)
```

Highlighting from the previous parse is mapped through the new edits, so
the frame never waits for a reparse (B43).

## Rendering

The client keeps a cell grid, diffs it against the previous frame, and
writes only the changed cells in one write. It wraps each frame in
synchronized output where the terminal supports it.

## Paths

```
$XDG_CONFIG_HOME/fude/config.toml   settings, keymaps, plugin list
$XDG_CONFIG_HOME/fude/plugins/      .wasm files
$XDG_CONFIG_HOME/fude/queries/      tree-sitter queries that override shipped ones
$XDG_STATE_HOME/fude/timeline/      timeline logs, keyed by file path
$XDG_DATA_HOME/fude/grammars/       grammars and their queries, loaded at runtime
```

## Config

`editor.Start` reads `config.toml` once (D27, D31). It holds:

```toml
indent_width = 8     # columns >, <, <C-t> and <C-d> shift by, 0 for the tab width
key_timeout = 1000   # milliseconds a mapping prefix waits (B84)
large_file = 50      # megabytes over which syntax and LSP stay off (B83)

[keymap.normal]      # and [keymap.visual], [keymap.insert]. Keys map to keys.
"<Space>w" = ":w<CR>"

[filetype.go]        # overrides for buffers of that filetype (B94)
indent_width = 4

[[plugin]]           # one table per plugin, loaded after the first frame
path = "plugins/wordcount.wasm"   # relative to $XDG_CONFIG_HOME/fude
capabilities = ["buffer.read", "ui"]
```

fude shows an error with its line, and the settings after it keep their
defaults (B93). `editor/` keeps the settings and gives the machine the
indent width through `Host.IndentWidth`.

## Plugin ABI

Core wasm on wazero (D8). A plugin exports `memory`, `alloc(n) -> ptr` and
`init()`, and the operators, targets and commands it registers. Events
(`on_event`) come with the first behaviour that needs them. It
imports host functions from the module `fude`, gated by capability:

| Capability | Grants |
|---|---|
| `buffer.read` | read text, marks, syntax nodes |
| `buffer.write` | submit requests |
| `ui` | decorations, statusline segments, floating windows |
| `fs.read` | read files under the project root |
| `process` | spawn a declared program |
| `net` | open connections to declared hosts |

The plugin passes text as a pointer and length into its memory. The host returns text by calling the
plugin's `alloc` and writing there, and returns the `i64` `ptr<<32|len`. A
negative result is an error: -1 when the plugin did not declare the
capability, -2 for a range outside the text, -3 when its memory could not
take the result.

| Function | Capability | Does |
|---|---|---|
| `log(ptr, len)` | none | adds a line to fude's log |
| `text_len() -> i32` | `buffer.read` | the length of the text |
| `text(start, end) -> i64` | `buffer.read` | the text in `[start, end)` |
| `edit(start, end, ptr, len) -> i32` | `buffer.write` | asks to replace `[start, end)` |
| `register(kind, name, nlen, export, elen) -> i32` | none, in `init` only | adds a command, operator, object or key |
| `decorate(start, end, ptr, len) -> i32` | `ui` | puts a style, such as `keyword`, on `[start, end)`, and returns its id |
| `undecorate(id) -> i32` | `ui` | removes a decoration |

`register` names the export that runs what it adds. Kind 0 is an ex
command and kind 3 a key in normal mode, both exported as `(cursor, count)`.
Kind 1 is an operator, exported as `(start, end, linewise)`. Kind 2 is a
text object, exported as `(start, end) -> i64` and returning
`start<<32|end`, or a negative number for none. fude adds them to the
machine as its own: operators and objects with `Machine.Operator` and
`Machine.Object`, and a key by mapping it to `<Plug>(plugin.export)` and
adding that with `Machine.Command`. So they compose with every built-in,
and macros and dot repeat replay them (B52, B54). A call that fails unloads
the plugin, and using what it added then says the plugin is unloaded.

A plugin declares its capabilities in `config.toml`. `fs.read`, `process`
and `net` are accepted there but grant nothing yet. A call outside the
declared capabilities returns -1, and fude logs it to
`$XDG_STATE_HOME/fude/log` (B50).

The edits a call asks for apply as one transaction once the call returns,
after its decorations, so its edits move them. A call that traps or runs
past its budget leaves no edit and no decoration (B53).

Each plugin gets its own wazero runtime, so each has its own memory limit,
64MB (`WithMemoryLimitPages` is per runtime). All runtimes share one
compilation cache, in `$XDG_CACHE_HOME/fude/wasm`. Plugins compile and run
`init` on their own goroutines, started after the first frame (B56).

Calls into one plugin take turns. Every call carries a context with its
budget: 200ms for `init`, 100ms for an event, and 16ms, one frame, for an
operator, target or command on the input path. With
`WithCloseOnContextDone`, wazero stops the plugin at its next loop back-edge
after the deadline. Compiled code is never preempted mid-loop, so host
functions never block. A stopped or trapped instance is closed and its state
is lost. fude unloads it and tells the user which plugin (B51, B53). It can
be instantiated again from the compiled module without recompiling.

Test plugins are `.wat` files in `plugin/testdata`, with the `.wasm` that
`just wasm` builds from them beside them, so tests never need
`wasm-tools`. Go and other WASI plugins wait for a behaviour that needs
them.

## Syntax

tree-sitter through cgo, with the official `go-tree-sitter` binding (D32).
`syntax/` reads text through `api.Text` and never imports `core/`. Until
modules are wired through `api/` in step 4, `editor/` imports it directly.

Each highlighted buffer has one goroutine that owns its parser, its tree
and a copy of its text. The kernel's listener hands every change to the
buffer's `syntax.Buffer` on the editor loop, which queues it for the goroutine
as a tree edit and a text edit. Each frame asks for the captures of the
window plus a margin. The goroutine takes the newest request and the edits
up to it, parses incrementally and sends the spans back through the
editor's queue. Until they arrive, the frame moves the old spans through the
edits made since, so a keystroke never waits for a parse (B43). The
goroutine abandons a parse for an old request through the progress callback
of `ParseWithOptions`. The binding has no finalizers, so the goroutine closes
its parser, tree, query and cursor when the buffer closes.

Captures paint as tree-sitter's own highlighter picks them (B46). A capture
on a node inside another wins, and of the captures on one node only the last
pattern's counts. The terminal client colours them from the palette, so the
terminal's theme applies.

Each language is a grammar and its queries, from the grammar's own repo
(D28):

- `highlights.scm`, `injections.scm` and `locals.scm` as the repo ships
  them, with tree-sitter's standard capture names.
- `textobjects.scm`, which fude writes, with `@function.outer`,
  `@function.inner`, `@parameter.inner`, `@class.outer` and `@class.inner`.
  `aa` is a parameter with the comma and blanks after it, or the comma
  before it when it is last.

A text object reads a tree on the editor loop, so it never waits for a
parse. The worker hands the loop a clone of each tree it parses, and an
object queries that clone moved through the edits made since. It picks the
smallest capture that holds the cursor, without the blanks a grammar puts
at a node's end (B40). `an`, `in`, `]n` and `[n` are objects too, run on
the visual selection, and pick nodes as Neovim's tree selection does (B45).

`gc` toggles comments on whole lines as Neovim's does, in the comment syntax
`syntax/` gives for the buffer's language, such as `// %s` for Go (B44).
Picking the language at the cursor matters once a grammar with injections
ships. Go has none.

Queries use only the predicates the binding evaluates: the `eq?`,
`match?` and `any-of?` families. A query that uses any other fails to load
with its file and line. A file at
`$XDG_CONFIG_HOME/fude/queries/<lang>/<name>.scm` replaces the shipped
query of that name (B41). Indentation is the previous line's (B29), so there
are no indent or fold queries.

A distribution links its grammars through each grammar repo's Go binding.
Any other grammar is built with `tree-sitter build` into
`$XDG_DATA_HOME/fude/grammars/<lang>/`, with its queries beside it, and
loaded at runtime (D29).

## LSP

`[filetype.<name>] lsp` names a filetype's server command. fude starts one
server per command and directory, after the first frame, for the buffers
open then and any opened later. It talks to it over stdio with JSON-RPC
from the standard library, through `lsp/`, which reads text through
`api.Text`.

fude offers the server `utf-8`, then `utf-16`, in `positionEncodings`.
rust-analyzer and clangd take `utf-8`. gopls and most others answer
`utf-16`. A position converts by scanning its line from the rope's line
start and counting 2 for each rune above U+FFFF, which costs the line's
length. Changes go out as incremental `didChange` notifications, in order.
The kernel's listener gives a change's edits sorted, against the text before
it, so they go out last first and each range stays a range of that text
(B60).

Editing never waits on a server. A writer goroutine owns the server's
stdin and sends from a queue that never blocks the loop. It holds
everything until `initialize` returns and the encoding is known. A reader goroutine
takes its stdout, another drains its stderr into fude's log, and another
waits for it to exit and tells the loop. A server that exits without being
asked is restarted once and its documents opened again. After a second
crash it stays off, and fude says so (B63).

## Server split (step 6)

One server holds the kernel and its modules and orders every transaction.
Clients send requests and keys. Each client applies its own edits at once,
and on receiving the server's order rebases its unconfirmed ones over it,
as CodeMirror's collab module does. Two clients converge because the
server's order is the only history (B73).

The protocol is fude's own and carries state, not cells: buffer text and
edits, views, decorations, modes and proposals. A terminal client renders
its own cells from that state, and an agent reads text and positions
straight from it. Neovim's UI protocol does not fit, because it shares one
screen and one cursor between every client, and real Neovim GUIs also call
much of Neovim's Lua and Vimscript API (D25).

A proposal is a branch off the current node, with its own origin. Clients
show the branch's edits as ghost text decorations, anchored by `Map`. On
accept, the server rebases the branch's edits onto the head and applies
them as one transaction (B72). If an edit since overlaps them, the proposal
is stale and cannot be accepted until the sender refreshes it (B74). Reject
drops the branch, and nothing enters the timeline.
