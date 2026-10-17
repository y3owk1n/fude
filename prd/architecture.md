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
api/             Go module: fude's module API, stdlib only
editor/          the editor loop, config, event queues, budgets, the vim host
syntax/          Go module: tree-sitter parsing, highlights, tree targets,
                 the only cgo, no dependency on fude's other packages
plugin/          Go module: the wazero host and its ABI, plugintest for
                 plugin tests, depends only on api/
protocol/        Go module: stdlib only, no dependency on fude's other packages
  lsp/           language server client
  dap/           debug adapter client
  framing/       the Content-Length frames both use
wire/            what fude and its clients send over the socket: replica
                 edits and the frames an attached client draws
ui/term/         terminal client
client/          a client over the socket, with replicas of buffers
internal/        packages only fude's own packages may import
  clipboard/     the system clipboard, through pbcopy, wl-copy or xclip
  pty/           a program in a pseudo-terminal, for :terminal
  relay/         passes a share's connections between its host and guests
  trace/         what fude did, for fude --tail
modules/         Go packages over api/ that cmd/fude adds with editor.Use
  blame/         :blame
cmd/fude/        main, distribution wiring
```

Modules import `api/`, never `core/` or `vim/` directly. `client/` is not
a module: it runs in a client's process and keeps its replicas in kernels
of its own. Only `editor/`
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
func (b *Buffer) BeginGroup(o Origin) // another origin's transaction splits it
func (b *Buffer) EndGroup(o Origin)
func (b *Buffer) Undo() (start int, ok bool)
func (b *Buffer) Redo() (start int, ok bool)
func (b *Buffer) Goto(id NodeID) (start int, ok bool) // any node, any branch
func (b *Buffer) At(id NodeID) (Snapshot, bool)        // the text at a node
func (b *Buffer) Head() NodeID
func (b *Buffer) Nodes() []Node
func (b *Buffer) SetMark(pos int, g Gravity) MarkID
func (b *Buffer) MoveMark(id MarkID, pos int)
func (b *Buffer) DeleteMark(id MarkID) // its id may name a later mark
func (b *Buffer) Map(pos int, from Version) (int, bool)
func (b *Buffer) Since(from Version) ([][]Edit, bool) // the edits after from (B115)
func (b *Buffer) UndoOrigin(o Origin, label string, at time.Time) (int, error)
func (b *Buffer) Listen(l Listener) // hears every change, with the text before it
func (b *Buffer) Mark(id MarkID) int

type Store interface{ Append(Node) }
type Listener interface{ Changed(before Snapshot, edits []Edit) }
```

A transaction carries the host's time, since the kernel reads no clock.
The host restores a state or diffs two. It reads both texts with `At` and
applies or compares them.

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
    Undo() (start, cursor int, err error) // ErrNoStep at the end
    Redo() (start, cursor int, err error)
    Step(n int) (start int, ok bool) // g- and g+, in time order
    SetMark(name rune, pos int)      // marks move with edits
    Mark(name rune) (int, bool)
    Viewport() (top, height, width int)
    SetTop(top int)
    PushJump(pos int) // jumps may cross buffers, so the host keeps the lists
    Jump(n int) bool
    Change(n int) bool
    IndentWidth() int // the buffer's setting, so filetypes can differ
    TabWidth() int    // the columns a tab shows as, the buffer's setting (B237)
    Clipboard() (string, error) // the system clipboard, "+ and "* (B36)
    SetClipboard(text string) error
    Fold(line int) (first, last int) // the closed fold holding line (B223)
}

// Defaults is the part of Host a host may leave out: no history, jumps,
// changes or clipboard, and Unhandled ignored. Embedding it leaves Text,
// Cursor, SetCursor, Apply, SetMark, Mark, Viewport and SetTop to write.
type Defaults struct{}

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

`Object`, `Operator` and `Command` with a nil function remove what they
added under name, as a plugin reload does for a plugin it drops (B191).

`:select` asks the host's selector for regions, and of two that overlap the
earlier stays. The next `d`, `x`, `y` or host operator acts on every region
as one undo step, and any other key keeps only the first region. `c`
deletes every region and types into all of them: each edit made at the
first cursor is repeated at the others, moved by their distance from it. So
`<C-w>` takes the same number of bytes at each cursor, not each cursor's
own word. `<C-o>` and `<Esc>` end the other cursors, and dot repeat makes
the change at one cursor (B42).


`vim/` owns the grammar, modes, registers, dot repeat, macros, search and
the edit-producing ex commands. It keeps its visual anchor and query
selection regions as marks through `Host.SetMark`, so another view's edits
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

Both are an `api.Module`. `cmd/fude` passes each built-in module to
`editor.Use` before `Load`, and the editor runs it as it runs a plugin. A
call reads a `Scope` and returns a `Result`, and its events and callbacks
run off the loop. Built-in modules live in `modules/` and import only
`api/` (D49). `:blame` is the first (B220).

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
the budget. A plugin runs one call at a time, and a key whose plugin is
busy with an event handler says the plugin is busy instead of waiting
(B57).
The editor times a Go module's calls and traces them. It cannot stop one
as it stops a plugin, so a module keeps its calls on the input path short.

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
through the edits since, or dropped. The loop handles a waiting key before
queued work, and draws one frame after all the queued work that is ready.
A key that comes while queued work runs waits only for that func, and the
frame it draws shows both.

## Text and positions

The rope holds UTF-8 bytes. A position is a byte offset. Each rope node
caches its byte count, newline count and UTF-16 code unit count, so line,
column and LSP position conversions are O(log n).

Grapheme clusters and display width are computed where they are needed,
around the cursor and on the visible lines. They are never stored.

## Command

A command names an edit for a client to send over the socket.

```go
type Command struct {
    Count    int    // 0 means no count
    Register string // "" means the unnamed register
    Op       string // "delete", "change", "comment", ..., "" moves the cursor
    Target   Target
    Text     string // typed after a change
}

type Target struct {
    Name string // "word", "find", "inner_quote", "a_function", "lines", ...
    Arg  string // the character for find and till
}
```

The server turns a command into the keys it names and feeds them to the
client's session, so a command does what typing them does (B70). `change`
on `inner_quote` with `Text: "hello"` is `ci"`, then the text pasted as
typed (B8), then `<Esc>`. An operator on `lines` is the operator doubled.
`editor/rpc.go` lists every name with its keys: the built-in operators,
the motions and text objects, and the syntax objects such as
`a_function`. A name in neither can name what a plugin registered, as
`plugin.key`: `kit.gs` is the operator the plugin kit registered as `gs`.
A name with no keys is refused.

### Rules

- **Dot repeat** replays the last command whose operator changes text,
  resolved at the current cursor. Moves and yanks are never repeated. After
  a visual operator, `.` applies to a region of the same size at the cursor,
  as in Neovim.
- **Macros** are key text in a register. `@x` feeds the text through the
  grammar. `:reg x` shows the text and editing it changes the macro (B17).
  A macro can hold anything keys can do, including `:w` and `<C-w>v`.
- **Ex edits** such as `:3,5d` and `:s/a/b/g` run in the machine. Ex
  commands that do not edit text (`:w`, `:q`, `:e`) reach the host's
  `Unhandled`.
- **Insert** keeps the text typed before `<Esc>`, so `.` reinserts it.

### Request

Clients send a command inside a request.

```go
type Request struct {
    Buffer   int     // 0 for the session's current buffer
    At       *int    // a byte offset to act at instead of the cursor
    Base     Version // the version At is in
    Proposal bool    // show as ghost text, apply only on accept (B71)
    Command  Command
}
```

The anchor lives on the request, so it never reaches dot repeat or a
macro. A request with `At` acts there and leaves every cursor where it was
(B75). `At` maps from `Base` through the edits since, and a request whose
position an edit since replaced is refused (B76).

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

A kind of buffer, such as a listing, has a `vim.Keymaps` of its own, which
the editor lays over the machine's with `Machine.UseLocal` before each key.
The machine walks both tries: a local mapping runs as soon as its keys are
typed (N17), and otherwise the longer mapping runs, the local one on a tie,
as Neovim's `<buffer>` mappings win. A buffer's actions, such as a
listing's open and split, are `<Plug>` commands its keymap maps keys to,
so a config table can move or drop each one. `""` in a table unmaps, which
for a local key lets the global mapping through, and `<Nop>` maps a key to
nothing. The finder's keymap is an insert mode one, so the machine runs a
`<Plug>` key in insert mode too. Its open action closes the finder after
the key, as closing feeds the machine `<Esc>` (B142).

A key closes a float that shows and keeps it for that key. When the key
runs `<Plug>(lsp.hover)`, hover moves the cursor into the float it closed
instead of asking the server again, so a remapped hover key enters a float
as `K` does (B142).

The terminal sends the arrows, `<Home>`, `<End>`, `<Del>`, `<PageUp>` and
`<PageDown>`. In normal and visual mode the machine reads each as the
command it stands for before anything else sees it (B132). The command
line keeps a cursor, which `Machine.LineCursor` reports, and a history of
`:` lines and one of searches for the session, which no file keeps (B133).
`<C-r>` waits for a register's name or `<C-w>`, as it does in insert mode
(B135).

`Machine.UseClipboard` turns on the `clipboard` option. `store` then writes
the host's clipboard after the default registers, and a put without a
register reads the clipboard and compares it with the unnamed register
(B137).

`Machine.Flash` reports what the last key yanked or put. The editor's
`Undo` and `Redo` take what the kernel's listener last heard changed, as
the text is after it. The session keeps one flash, a decoration over that
text cut to the window's visible lines, since undoing a `:s` can touch the
whole file. A timer through the queue clears it after 300ms, and a change
to its buffer clears it at once (B139).

`<Plug>(window.*)` move as `<C-w>h`, `<C-w>j`, `<C-w>k` and `<C-w>l` do.
When the window stays the same and `$TMUX` is set, they run
`tmux select-pane -t $TMUX_PANE` with `-L`, `-D`, `-U` or `-R`. tmux's own
bindings decide which keys reach fude, so its `vim-tmux-navigator` pattern
has to name `fude` (B140).

tcell reads both 0x08 and 0x7f as backspace. A terminal sends 0x7f for
backspace and 0x08 for `Ctrl+h`, and tmux passes both on as they are, so
the terminal client reads the tty through a wrapper that turns 0x08 into
`Ctrl+h` in the kitty keyboard protocol before tcell parses it (B141).

## Views

A view is one buffer shown in one window. It owns a cursor mark, a top-line
mark and a jumplist. The kernel only holds their marks, so another view's
edits move them (B23). Each buffer keeps its own changelist.

A split shares its space equally until a window in it asks for a size,
with `<C-w>+` or `:resize` and the others (B193). The node that holds it in
the split keeps that size, in rows or columns, while every other node of
the split keeps its least. The rest share what is left, so a terminal that
shrinks shrinks the windows without a size first. `<C-w>=` drops every
size.

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

The kernel applies a transaction only at its base version. `Map(pos, from)`
says where a position in an older version is now, or that an edit since
replaced it (B108). It reads a log of the last 4096 edit lists the kernel
made, so a position older than those maps to nothing. A request made
against an older version resolves at its mapped position, and is refused
when the position no longer exists (B76).

`UndoOrigin(o)` reverts origin `o`'s newest change still in the text. When
that change is the head node, it is a plain `Undo`, so redo keeps working.
Otherwise it reverts the change as one edit, from the text after it back to
the text before it. It moves that edit through the changes others made since
and applies it as a new node of `o`'s. It refuses when one of those changes
overlaps it (B109). The new node's `Reverts` names the change it undid, so
a timeline loaded again passes over both.

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
`buffer.saved`, `buffer.closed`, `view.moved` when a key moves the cursor
or the view, `mode.changed`, `syntax.updated` when a parse lands,
`lsp.diagnostics`, and a plugin's own events, named `plugin.event`, which
it emits with `emit`.

Each plugin that subscribes has its own goroutine and its own list of
events to hear, and emitting only adds to that list, so it never blocks.
An event of a name and buffer already on the list replaces the one there.
The goroutine takes the next event on the editor loop, with the buffer's
text at that moment, so a plugin that falls behind hears the newest state
once, never the stale ones in between (B57). What the handler asks for
applies only if the text has not changed since, and when it has, the
event about the newer text follows.

## Decorations

A decoration is a mark range owned by a plugin, by a buffer for its
diagnostics, or by a session for its proposals, carrying a style.
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
    -> edits when a command completes -> Transaction
    -> kernel applies -> timeline node -> event "buffer.changed"
    -> client renders from the new snapshot and current decorations
    -> modules react off the loop (reparse, lsp didChange, plugins)
```

Highlighting from the previous parse is mapped through the new edits, so
the frame never waits for a reparse (B43).

## Rendering

A line longer than its window wraps onto the rows below at the first
cluster that does not fit, as Neovim's `wrap` does. `vim.Rows` gives where
each row of a line starts. The editor scrolls the window by those rows and
the client draws them, so the two agree. With `wrap` off, which `:set`
changes per window, a line takes one row, and the window scrolls sideways
the least it must to show the cursor's column, as Neovim's `sidescroll` of
1 does. Rows past the end of the text show `~` (B37).

A frame draws only the lines that changed. For each line, the client keeps
what it drew the line from: the window's place and selection, and the
line's text, styles, cursor-line shade, matches, brackets, cursors,
proposals, note and indent. When a line draws from the same as in the
last frame, it keeps its rows. The gutter, the status line and the
cursor's line draw in every frame. The cursor's line draws so the client
knows where the cursor is. While a box,
the command line or a message is over the windows, and in the frame after
it, every line draws (B197).

`:set` changes the options of the current window, and the ones the
session's next windows start with, as Neovim's `:set` sets both the local
and the global value. It takes the names `config.toml` uses (N4): `name`,
`noname` and `name!` for a switch, `name=n` for a number, and `name?` for
either, and shows every option with no arguments. `scroll_off` keeps that
many lines in view above and below the cursor's, at most half the window
(B116). `cursor_line` shades the cursor's line in a dark grey and shows
its number in bold (B117). `list` shows a tab as `>` and blanks, a
trailing space as `-` and a no-break space as `+`, in grey, as Neovim's
default `listchars` does (B118). A buffer with diagnostics shows a sign
column, two columns left of the line numbers, with the letter of each
line's worst diagnostic in its colour, as Neovim's `signcolumn=auto` shows
`vim.diagnostic`'s signs (B119). `ignore_case` and `smart_case` reach the
machine through `Machine.IgnoreCase`, so `/`, `:s` and their highlights
ignore case, and a pattern with an uppercase letter matches case with
`smart_case` (B120). With the cursor on a bracket, or just after one in
insert mode, the bracket and its match show bold and underlined, as
Neovim's matchparen shows them in a terminal. `vim.Pair` looks for the
match only within the window's lines, so a frame never scans the file
(B121). `expand_tab` reaches the machine through `Machine.ExpandTab`, so
indents are spaces only and `<Tab>` types spaces to the next
`indent_width` column (B122, N10).

A file in a git repository shows in the sign column how it differs from
git's index, as gitsigns.nvim does by default: `┃` on added lines in green
and changed ones in blue, `▁` in red under the line before deleted lines,
`▔` over the first line when the first lines went, and `~` on a change
that also deleted lines. fude reads the index's copy with `git show` off
the editor loop when it opens the file and when the terminal gets focus
back. When a frame finds the text changed, fude diffs it with that copy,
also off the loop and one diff at a time. A buffer diffs at most every
100ms, so typing in a long file copies its text a few times a second and
not once a key, and its signs trail the text by up to that. The diff
skips the lines both copies start and end with, so it reads only the
lines between. A diagnostic's sign wins on its
line, as Neovim puts diagnostics over gitsigns' signs. Without git on
the PATH, fude runs nothing and shows no git signs (B123).

A hunk is a run of changed lines from that diff: where it starts in the
text, how many lines it has there, and the index's lines it replaced. The
signs come from the hunks. `]h`, `[h`, `gH` and the preview diff the text
with the base again on the loop, so they act on the text as it is and not
on signs a diff off the loop has yet to update. A deletion's hunk starts
on the line its sign is on (B138).
`number` puts line numbers in a gutter at least 4 columns wide, and
`relative_number` numbers lines from the cursor's. With both, the cursor's
line shows its own number on the left, as in Neovim (B39).

A window's status line starts, in the current window, with the mode in
its colour, then the buffer's name with its directory dim, a dot when it
is modified, the git branch and the counts of errors and warnings. On the
right are the plugins' segments, the keys of a command being typed, the
filetype and the ruler, which the line drops first when it is too narrow.
`View` carries the branch and the counts. The editor keeps `View.Ruler` in
Neovim's form, `line,col`, then `-vcol` when the screen column differs,
then `All`, `Top`, `Bot` or how far down the view is (B38), and the
client shows it as `line:col` with the screen column (B147).

Before the filetype goes the buffer's language server. fude asks servers
for `$/progress` in `initialize`, and keeps each piece of work a server
begins until it ends. `View.Progress` is "starting" until the server
answers `initialize`, then the title, message and percentage of the work
it began first, or "" when it has none. While a frame shows progress, the
client draws a spinner and draws again every 80ms, so an idle editor runs
no timer (B161).

The client draws the command line in a box with a rounded border, a fifth
of the way down the screen and at most 60 columns wide, and the
completion menu under it, so the editor gives the menu's `Float` no place
of its own (B148). The last message is a toast in the top right corner,
drawn while `Message` returns it, so it goes with the next key as before
(B149). With both off the last row, `SetSize` gives the windows the whole
screen.

Every box is a `panel`: a rounded border on the editor's ground, cleared
inside to the same ground. A box-drawing line sits in the middle of its
cell, so a filled inside would leave a seam of the other colour between
the line and the fill, which a terminal cannot avoid. A row a
`menu.selected` span starts takes the raised colour, and the border cell
beside it becomes an accent bar. A `markup.rule` row joins the border with
`├` and `┤` (B150).

Setting a theme also builds its tint: every ground 35% of the way to black
and every colour of text 55% of the way to the tinted ground. A frame with
the command line or the finder open draws the windows in the tint, then
the box in the theme's own colours on the tinted ground, so nothing draws
twice and no seam shows (B151).

A `:s` being typed shows in the current window. `Machine.Preview` reads the
command line as a `:s` and returns the edits it would make on the visible
lines only, from the same code `:s` applies, so `:%s` on a large file costs
one screen. The editor shows them as search matches until a replacement is
typed, then as a struck-out `sub.old` decoration with a `sub.new` ghost
after it. Neovim's `inccommand` changes the text and undoes it, which here
would write every keystroke to the timeline log. The machine keeps up to 16
compiled patterns, as the search highlight and the preview take turns
(B154).

`:cd` makes every buffer's path absolute before it changes the working
directory, so a buffer opened by a relative name keeps naming its file,
and its language server URI stays right. `:e!` and the reload on focus
share `revert`, which puts the file's text in as one `reload` node, so `u`
brings the changes back (B155).

`:sort`, `:join` and `:retab` change only text, so the machine runs them
with its ranges. `:!` and `:r` run programs or read files, so the editor
runs them, and `Machine.LineRange` gives it the range its own commands
start with. A command runs in `$SHELL -c` off the loop, and its output goes
in through `reformat`, so text that changed meanwhile drops it (B156).

Every message reaches a client through `Message`, so `Message` puts each
one into the session's history the first time it returns it. Code that
sets a message does nothing more. `:marks`, `:jumps` and `:changes` open
list windows of places, like the one `grr` opens (B157). The newest one
is the editor's last list, which keeps its places, its buffer and the
place last gone to, so `]q` and the `:c` commands go through it after
its window closes (B194, N25). `<C-q>` in a finder sends its matches to
a list, a grep match as a place at its line and column, a file at its
start (B195). The places of `:pick diagnostics` and `:pick symbols` go
the same way (B196, B214).

The client keeps a cell grid, diffs it against the previous frame, and
writes only the changed cells in one write. It wraps each frame in
synchronized output where the terminal supports it.

## Listings

`:e dir`, `fude dir` and `-` open a directory as a listing: a buffer of
its entries' names in the order `os.ReadDir` gives them, directories with a
trailing `/`, as Neovim's built-in `dir` plugin lists them. The buffer's
name is the directory with a trailing `/`, and it has no file. Its windows
show no line numbers and always keep a sign column, as Neovim's directory
ftplugin sets them. A directory already listed shows its buffer as it was
left, and `<C-l>` reads it again.

`-` is mapped in normal mode to `<Plug>(listing.up)`, in place of the `-`
motion, as Neovim does by default: it lists the directory holding the
current file or listing with the cursor on what it came from, `N-` goes up
N levels, and `1-` lists the working directory. A listing takes oil.nvim's
keys, while the machine is between commands: `<CR>` opens the entry under
the cursor, `<C-s>` and `<C-h>` open it in a split beside or below, and
`_` lists the working directory. `R` stays replace mode, as a listing is
text to edit (B124).

A listing is text to edit, as oil.nvim's buffers are. Reading a listing
puts two marks on each entry. Text inserted at the start mark goes before
it, and text inserted at the end mark goes after it. A line added beside
an entry is then a line of its own. `:w` reads the edits as changes:
- An entry's line is the one its marks are on. A different name there
  renames the entry.
- `dd` makes an entry's marks meet, and the entry is deleted. `cc` makes
  them meet too, but leaves them on one line no other entry has, and that
  line keeps the entry.
- A line no entry has creates a file, or a directory when it ends in `/`.
- A name both deleted and created is a line that moved, which changes
  nothing.
fude asks first, with the number of files in each directory it would
delete. If a new name exists already, it changes nothing. On `y` it
renames each entry to a temporary name and then to its new one, so two
entries can swap names. Then it deletes, with what a directory holds, and
creates. Open buffers of renamed files follow them, and fude reads the
listing again (B125).

A listing in a git repository shows each entry's git status as a sign.
Off the loop, fude runs `git rev-parse --show-prefix` in the directory,
because git names paths from the repository's root. Then it runs `git
--no-optional-locks status --porcelain -z -- .`. That flag stops git from
taking the index lock, so a git command run in a terminal at the same
time does not fail. A change below a directory marks the directory. fude
keeps the signs by name and matches them to the visible lines each frame,
so a sign moves with its line. fude reads the status when it opens a
listing, when it reads the listing again, and when it gains focus (B126).

A listing's windows show a note after some names: where a symlink points,
or a mark for an executable or special file. fude reads a name's note with
`lstat` the first time a window shows it, and forgets the notes when it
reads the listing again. A directory of thousands of entries costs one
`lstat` for each visible line, not one for each entry (B127).

With `icons` on, a listing's icons take two columns at the right of the
gutter, not inline before the name as Neovim's virtual text puts them.
The text then starts at the same column on every row, so the cursor,
wrapping and sideways scrolling need no change. The icons are a table of
names and extensions in `editor/icons.go`, from mini.icons (B128).

## EditorConfig

`add` reads a file's `.editorconfig` files once, in `editorconfig.go`,
from its folder up to the first with `root = true`. Each section's glob
becomes a regular expression, with a group for each `{n1..n2}` checked
after the match. The buffer keeps what they set. `indentWidth` and
`tabWidth` look there before config.toml, and `fit` applies `expand_tab`
over the window's options as it applies a filetype's (B238).

## Formatting on save

With `format_on_save`, `write` formats before `writeOut` writes. A
filetype's formatters run off the loop, each in its root's folder or the
file's. The text goes through each from stdin to stdout. fude leaves out
a formatter whose program is not on PATH or whose required root is
missing. With `format_first`, only the first one left runs (B171). Their output
becomes one edit for each run of changed lines, from the line diff the
timeline uses, so marks on the lines kept stay. When no formatter is left, fude asks the
first language server that formats. `gq`, `:format` and saving
share `formatWith` and `reformat`, which applies the edits as one node and
drops them when the text changed meanwhile. A timer writes the text as it
is after `format_timeout`, so a formatter or server that hangs cannot hold
the write (B136). `:format` and `gq` run the same formatting without the
write, and keep only the edits inside their lines (B65, B234).

## Command line completion

The machine keeps the command line, and `editor/` completes it, since only
the editor knows the commands, files, options and buffers. Keys reach the
completion before the machine while a `:` line is typed. `Machine.SetLine`
puts a completion in the line, and `vim.ExNames` lists the machine's own
commands. The menu offers one full name for each command, so `:w` gained
`:write`. The word being completed is the command's name, after any range
such as `'<,'>`, or its argument after the first space. For `:set` it is
the argument's last word. A path completes in the directory typed so far.
Reading a directory of 10,000 entries takes 10ms and matching them 1.5ms,
so the menu that shows while typing completes a path off the loop. It
shows the result only if the line has not changed by then. `<Tab>`
completes at once. Each command line reads a directory once and keeps its entries. The menu is a `Float` the
editor places above the command line, under the word, at most 10 rows
tall (B131).

## Finder

`:pick files` opens a float whose buffer is the query, in insert mode, so every
insert key edits it. Keys reach the finder before anything else: `<CR>`,
`<C-n>` and `<C-p>` are its own, and the rest go to the machine. When the machine leaves insert mode, by `<Esc>` or a mapping to
it, the finder closes. The editor places the box in the middle of the
screen, at most 80 columns and 20 rows, through `Float`'s `X`, `Y`, `W`
and `H`, and the terminal draws it there.

The files come from `git ls-files -z --cached --others --exclude-standard`
when the working directory is in a repository, and from a walk of it that
skips `.git` and unreadable directories when it is not. fude reads them off
the loop when the finder opens.

Matching uses fzy's algorithm, as `matchfuzzy()` does, so the ranking is
the same. A dynamic program over the query and the path scores each
matched letter by what comes before it:
- 1.0 after the letter matched before it
- 0.9 after `/`, and at the start
- 0.8 after `-`, `_` or a space
- 0.7 for a capital after a lower case letter
- 0.6 after `.`
Each letter skipped costs a little. A tie keeps git's order. Matching runs
off the loop, one match at a time, and a query that grows searches only
the last matches. 4,000 paths take 2ms for one letter. The box shows the
last matches until the new ones come (B129).

The marks come from the same dynamic program kept whole, for the visible
rows only: from the last letter back, each takes the last place that
leads to the best score, or the place a run of letters needs, as fzy's
`match_positions` and `matchfuzzypos()` do. Scoring keeps two rows, and
both share the function that fills one (B152).

On a screen of 60 columns or more, the list takes two fifths of the width
and a preview box the rest, up to 160 columns in all. `Float.DocW` is the
preview box's width. The preview is the start of the selected file, cut to
the box, with tabs as 4 spaces. `syntax.Highlight` colours it by the
file's extension, which takes over a millisecond, so fude reads and
highlights it off the loop, one at a time, when the selection changes. The
old preview shows until the new one comes (B130).

`:pick grep` opens the same finder in grep mode. Each change to the query runs
`rg --fixed-strings --smart-case` off the loop, or `git grep -F` with `-i`
for a query without capitals when `rg` is not installed. One search runs
at a time, as matching does. fude reads at most 1000 lines of its output
and kills it after them. A match is `path:line:column:text`, and the
preview starts a third of its rows above the match's line (B134).

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
indent_width = 0     # columns >, <, <C-t> and <C-d> shift by, 0 for the tab width
tab_width = 8        # columns a tab shows as (B237)
editorconfig = true  # .editorconfig sets a file's indentation, over these (B238)
key_timeout = 1000   # milliseconds a mapping prefix waits (B84)
large_file = 2       # megabytes over which syntax and LSP stay off (B83)
center = false       # start with the windows centered, as :center toggles (B199)
terminal_height = 33 # percent of the screen a terminal or task window opens at (B205)
split_below = true   # false opens :split above (N16)
split_right = true   # false opens :vsplit left (N16)
autocomplete = false # show completions while typing, in insert mode and a command line (B67, B131)
highlight_colors = true # text that names a colour shows on it (B208, B209)
autopairs = true     # a bracket or quote typed comes with its closing one (B202)
completion_sources = ["lsp", "snippet", "path", "buffer"] # where completions come from, in order (B206)
socket = "/run/user/1000/fude.sock" # where clients attach, instead of one path per process (B97)
number = false       # line numbers, which :set number changes per window (B39)
relative_number = false # numbers from the cursor's line (B39)
wrap = true          # long lines go on the rows below, else the view scrolls sideways (B37)
scroll_off = 0       # lines kept in view above and below the cursor's (B116)
cursor_line = false  # shade the cursor's line (B117)
list = false         # show tabs, trailing spaces and no-break spaces (B118)
ignore_case = false  # searches ignore case (B120)
smart_case = false   # with ignore_case, an uppercase letter matches case (B120)
expand_tab = false   # indent with spaces only (B122)
format_on_save = false # format before :w writes (B136)
format_timeout = 1000  # milliseconds a format may take (B136, B234)
key_hints = 300        # milliseconds before the keys that may follow show, 0 for never (B172)
flash_time = 300       # milliseconds a yank, put, undo or redo shows highlighted, 0 for never (B139)
clipboard = false    # yanks, deletes and puts use the system clipboard (B137)
sign_column = false  # keep the sign column when there are no signs (B143)
indent_guides = true # a faint line at each indent level (B153)
color_column = 0     # the column of the text to shade, and how wide :center is (B200)
icons = false        # nerd font icons in listings and diagnostic signs (B128)

leader = "<Space>"   # what <Leader> in a mapping stands for (B144)
theme = "dark"       # dark, light, or a base16 scheme such as "~/.config/stylix/palette.json" (B145, B146)

[keymap.normal]      # and [keymap.visual], [keymap.insert]. Keys map to keys.
"<Leader>w" = ":w<CR>"
"-" = ""             # "" drops a mapping, fude's own too, and "<Nop>" does nothing (B142)

[keymap.groups]      # names of the keys mappings start with, for key hints (B172)
"<Leader>s" = "search"

[keymap.listing]     # and [keymap.list], [keymap.timeline], [keymap.lsp], [keymap.help]: a kind of buffer's own keys (B142, B159, B162)
"<C-h>" = ""
"<C-v>" = "<Plug>(listing.vsplit)"

[filetype.go]        # overrides for buffers of that filetype (B94)
indent_width = 4
expand_tab = false   # and wrap, color_column, while a window shows its files (B94)
format = ["goimports", "gofumpt"]   # formatters, run in turn by format_on_save (B136)
format_first = false # true runs only the first that applies (B171)
format_on_save = true # over the top-level one, for this filetype (B136)
lsp = ["gopls", "golangci_lint_ls"]   # its language servers, by name, in order (B167, B169)

[formatter.prettierd]   # a formatter, all optional (B136, B171)
command = ["prettierd", "{path}"]   # reads stdin, writes stdout; its name when unset
root = [".prettierrc", "package.json"]   # it runs where the first is found
workspace_required = false   # true runs it only under a root file

[lsp.gopls]          # a language server, all optional (B167)
command = ["gopls"]  # its name when unset
files = ["*.go"]     # the files it is for, by name or the end of the path (B169)
workspace_required = false   # true starts it only under a root file (B169)
root = ["go.work", "go.mod", ".git"]   # marks a project's root, most wanted first
init_options = {}    # sent with initialize (B168)

[lsp.gopls.settings.gopls]   # what workspace/configuration answers, by section (B168)
gofumpt = true

[filetype.toml]
files = ["*.toml.tmpl"]   # more files of the filetype, by name (B164)

[[plugin]]           # one table per plugin, loaded after the first frame
path = "plugins/wordcount.wasm"   # relative to $XDG_CONFIG_HOME/fude
capabilities = ["buffer.read", "ui", "process"]
programs = ["git"]   # what process may run (B48)
```

fude shows an error with its line, and the settings after it keep their
defaults (B93). `editor/` keeps the settings and gives the machine the
indent width through `Host.IndentWidth`.

## Plugin ABI

Core wasm on wazero (D8). A plugin exports `memory`, `alloc(n) -> ptr`,
`call(id, a, b, c) -> i64`, which runs what it registered, `on_event(ptr,
len)` when it subscribes to events, `callback(id)` when it asks for a
timer, and `_initialize`, `init` or both.
fude runs `_initialize`, where a compiler such as TinyGo puts the program's
setup, then `init`, together under the init budget, and either may register
and subscribe (D35). `on_event` gets the event's name, and for some events
a space and a detail. For `mode.changed` the detail is the new mode. For
`view.moved` it is the cursor and the view's top, as `cursor top` (B182).
For `buffer.changed` it is where the change starts and ends in the text
after it, as `start end`. Changes the plugin hears of as one come as one
range that covers them all, with an older one moved through the edits
after it (B183). `on_event` reads the text of the buffer the event is
about. It runs off the input path
under a budget of 100ms. A plugin imports host functions from the module `fude`, gated by capability:

| Capability | Grants |
|---|---|
| `buffer.read` | read the text, where its lines start, its syntax nodes, and every open buffer's text |
| `buffer.write` | edit the text |
| `ui` | decorations, signs, notes after a line, statusline segments, floating windows, read-only windows, lists to pick from and questions, moving the cursor, opening project files |
| `fs.read` | read files under the project root |
| `process` | spawn a declared program |
| `net` | get http and https URLs on declared hosts |
| `lsp` | send requests to the buffer's language servers |

Each call reads a `plugin.Scope` that the editor loop builds before the
call. It holds the text of the buffer the call is about, its `context`,
and a snapshot of every open buffer. A call off the loop reads text that
does not change under it. When the buffer has syntax, the scope also has a
copy of its last tree, moved through the edits since, for `node`. Each
call that takes a scope frees the copy when it ends, since the original
may be freed off the loop once a newer parse lands (B189).
The plugin passes text as a pointer and length into its memory. The host returns text by calling the
plugin's `alloc` and writing there, and returns the `i64` `ptr<<32|len`. A
negative result is an error: -1 when the plugin did not declare the
capability, -2 for a range outside the text, -3 when its memory could not
take the result, -4 for a file outside the project or one that cannot be
read, -5 for a program the plugin may not run or one that failed, -6 for a
host it may not reach or a request that failed.

| Function | Capability | Does |
|---|---|---|
| `log(ptr, len)` | none | adds a line to fude's log |
| `after(ms, id) -> i32` | none | runs `callback(id)` once, `ms` milliseconds after the call returns, under the event budget, about the buffer the call was about (B180) |
| `start(ptr, len, in_ptr, in_len, id) -> i32` | `process` | runs the program with its arguments, NUL-separated, in the background with the text at `in_ptr` on its stdin, then `callback(id)`. The program must be in the plugin's `programs` (B181) |
| `request(ptr, len, pos, id) -> i32` | `lsp` | sends the method, then a NUL and its params as a JSON object or nothing, to the language servers of the buffer the call is about, then `callback(id)` with the first answer that is not null as `output`. fude adds `textDocument`, and `position` at `pos` in each server's encoding unless `pos` is negative. A method outside `textDocument/` goes to every server, and one inside it to those that offer it (B190) |
| `fetch(ptr, len, id) -> i32` | `net` | gets the http or https URL in the background, as `http_get` does, then `callback(id)` (B181) |
| `setting(ptr, len) -> i64` | none | the value of the plugin's setting by its name, as text, or -2 for a name it has none of (B178) |
| `context(ptr, len) -> i64` | none | the value of what the call is about by its name, or -2 for a name it has none of. Every call has `buffer`, `path` and `filetype`, of the buffer it is about, and `mode` (B176) |
| `text_len() -> i32` | `buffer.read` | the length of the text |
| `text(start, end) -> i64` | `buffer.read` | the text in `[start, end)` |
| `lines() -> i32` | `buffer.read` | how many lines the text has |
| `node(pos, up) -> i64` | `buffer.read` | the kind, start and end, NUL-separated, of the named syntax node that holds `pos`, or of its `up`'th named parent, or -2 for none (B189) |
| `language(pos) -> i64` | `buffer.read` | the filetype of the text at `pos`, the buffer's or that of an injected region holding it (B273) |
| `spans(start, end) -> i64` | `buffer.read` | the start, end and name of each highlight capture over `[start, end)`, each followed by a NUL (B273) |
| `comment(pos) -> i64` | `buffer.read` | how the language at `pos` comments a line, such as `// %s`, or -2 for none (B275) |
| `option(ptr, len) -> i64` | none | the option or config.toml setting called the name at `ptr`, as text, as the window has it, or -2 for none (B274) |
| `buffers() -> i64` | `buffer.read` | each open buffer's id, path and filetype, each followed by a NUL (B188) |
| `len_of(id) -> i32` | `buffer.read` | the length of the text of the buffer `id` |
| `text_of(id, start, end) -> i64` | `buffer.read` | the text of the buffer `id` in `[start, end)` |
| `line_of(pos) -> i32` | `buffer.read` | the line that holds `pos`, from 0 |
| `line_start(line) -> i32` | `buffer.read` | where `line` starts |
| `edit(start, end, ptr, len) -> i32` | `buffer.write` | asks to replace `[start, end)` |
| `register(kind, name, nlen, help, hlen, id) -> i32` | none, in init only | adds a command, operator, object or key that `call` runs by id, with the help `:h` and the key hints show |
| `subscribe(ptr, len) -> i32` | none, in `init` only | hears the event named through `on_event` |
| `emit(ptr, len) -> i32` | none | emits the event `plugin.name` about the buffer the call read |
| `decorate(start, end, ptr, len) -> i32` | `ui` | puts a style, such as `keyword`, on `[start, end)`, and returns its id |
| `undecorate(id) -> i32` | `ui` | removes a decoration, sign or note |
| `sign(pos, ptr, len) -> i32` | `ui` | shows the text, then a NUL and its style, in the sign column on `pos`'s line, and returns its id (B184) |
| `note(pos, ptr, len) -> i32` | `ui` | shows the text dim after the end of `pos`'s line, and returns its id (B184) |
| `read_file(ptr, len) -> i64` | `fs.read` | the file at the path, relative to the project, the directory fude started in. fude refuses a path that leads outside it, through `..` or a link (B47) |
| `run(ptr, len) -> i64` | `process` | what the program prints, run with its arguments, NUL-separated, in the project. It must be in the plugin's `programs`, and the call's budget stops it (B48) |
| `http_get(ptr, len) -> i64` | `net` | the body of the http or https URL, up to 16MB. Its host, and any host a redirect leads to, must be in the plugin's `hosts`, and the call's budget stops the request (B49) |
| `show(ptr, len) -> i32` | `ui` | shows the title, filetype and text, NUL-separated, in a read-only window, or in place of what the window of that title shows (B186) |
| `pick(ptr, len, id) -> i32` | `ui` | shows a prompt and the items after it, NUL-separated, in the finder. With no items it asks the prompt as a question. Then it runs `callback(id)`, whose `context` has `picked`, the item's index or -1, `text`, what was typed, and `ok`, false when cancelled (B185) |
| `jump(pos) -> i32` | `ui` | moves the cursor to `pos` once the call and its edits are done, when the current window shows the buffer the call was about (B187) |
| `open(pos, ptr, len) -> i32` | `ui` | opens the file at the path, relative to the project, in the current window with the cursor at `pos`. It refuses a path that leads outside the project, as `read_file` does (B187) |
| `float(ptr, len) -> i32` | `ui` | shows the markdown in a float at the cursor, or closes it for "" (B59) |
| `status(ptr, len) -> i32` | `ui` | shows the text on the statusline of the buffer the call read, or none for "" (B58) |
| `message(ptr, len) -> i32` | `ui` | shows the text in the message area once the call returns (B272) |

`register` gives what it adds an id, and fude runs it through
`call(id, a, b, c)`. Kind 0 is an ex command and kind 3 a key in normal
mode, both called with `(cursor, count)`. Kind 4 is a key in insert mode,
called with `(cursor, 0)`. The cursor then goes after the text of its
first edit, as typing that text would leave it (B177, B179). An ex command's `context` has
`args`, the text typed after its name, `bang`, `true` when `!` followed the
name, `first` and `last`, where its range's first and last lines start,
and `ranged`, `true` when it was given a range. Without one, the range is
the cursor's line (B175). Kind 1 is an operator, called
with `(start, end, linewise)`. Kind 2 is a text object, called with
`(start, end)` and returning `start<<32|end`, or a negative number for
none. fude adds them to the machine as its own: operators and objects with
`Machine.Operator` and `Machine.Object`, and a key by mapping it to
`<Plug>(plugin.keys)` and adding that with `Machine.Command`. So they compose with every built-in,
and macros and dot repeat replay them (B52, B54). A call that fails unloads
the plugin, and using what it added then says the plugin is unloaded.

A plugin declares its capabilities in `config.toml`, for `process` the
programs it may run as `programs = ["git"]`, and for `net` the hosts it may
reach as `hosts = ["api.github.com"]`. A call outside the
declared capabilities returns -1, and fude logs it to
`$XDG_STATE_HOME/fude/log` (B50). Its table's `settings`, such as
`settings = { depth = 3 }`, are what it reads through `setting`.

The edits a call asks for apply as one transaction once the call returns,
after its decorations, so its edits move them. A call that traps or runs
past its budget leaves no edit and no decoration (B53), and starts no
timer and no job.

Timers and jobs come back through the plugin's listener, the goroutine
that hands it events, so one call into it runs at a time. A timer's
callback runs once, after its delay. A job is a program or a get with no
budget, which `Plugin.Work` runs through the same checks as `run` and
`http_get`, at most 8 at once for a plugin. Its callback reads `output`,
and `error` when it failed, from its `context`. Each callback runs under
the event budget, about the buffer the call that asked was about, and not
at all once that buffer closes. Unloading the plugin cancels its
listener's context, which stops its jobs (B180, B181).

Each plugin gets its own wazero runtime, so each has its own memory limit,
64MB (`WithMemoryLimitPages` is per runtime). Each runtime's compilation
cache uses the same folder, `$XDG_CACHE_HOME/fude/wasm`, so a plugin
compiles once across runs. Plugins compile and run
`init` on their own goroutines, started after the first frame (B56).

Calls into one plugin take turns. Every call carries a context with its
budget: 200ms for `init`, 100ms for an event, and 16ms, one frame, for an
operator, target or command on the input path. With
`WithCloseOnContextDone`, wazero stops the plugin at its next loop back-edge
after the deadline. Compiled code is never preempted mid-loop, so host
functions never block. A stopped or trapped instance is closed and its state
is lost. fude unloads it and tells the user which plugin (B51, B53). It can
be instantiated again from the compiled module without recompiling.

`:plugin reload` reads the `[[plugin]]` tables of config.toml again,
closes every plugin and takes out what it added: its commands, keys,
decorations, segments, help and key hints. Then it loads the plugins as at
startup, each from its file, so a rebuilt `.wasm` runs at once.
`Editor.setup` holds first what each session's machine gets before the
plugins. A reload keeps that part, and the plugins add theirs again as
they register (B191).

Plugins are written in Go with `sdk/`, a module of its own that imports
nothing of fude's (D36). The package registers Go functions from the
plugin's `init` functions, which TinyGo runs in `_initialize`. It keeps
them in a table, and its `call` export runs them by id. It also exports
`on_event`, and `alloc`, which keeps the buffer it hands fude in a package
variable until the plugin has read it. `sdk/fude.json` is the TinyGo target
plugins build for. It is `wasm-unknown`, which imports no WASI, with the
conservative collector and a 64KB stack, as the target's own collector
never frees memory. `fude plugin
new` writes a plugin that requires the sdk of fude's release, or the newest
for a build that is not one, so a release tags `sdk/vX.Y.Z` beside
`vX.Y.Z`. `fude plugin build` builds a plugin with that target and without debug
info, which would make it twenty times its size, finding it through `go list -m` in the plugin's
module (B173).

`plugin/plugintest` runs a plugin in `go test` without fude (B192). It
loads the `.wasm` with `plugin.Load`, as fude does, and calls it with a
scope over a string, the only open buffer. A test gets the call's
`plugin.Result`, and `Apply` makes the edits it asks for. Timers, jobs,
requests and picks stay in the result, and the test runs their callback
itself with `Callback`. `plugin/` is a Go module that requires wazero
and `api/` (D45), so a plugin's own module can require it from fude's
first release on (D37).

Test plugins are written with the sdk, each a folder in `plugin/testdata`.
`just wasm` builds each with `fude plugin build`, and the `.wasm` it builds
sits beside the source, so tests never need TinyGo.

## Syntax

tree-sitter through cgo, with the official `go-tree-sitter` binding (D32).
`syntax/` is a Go module of its own (D43). It reads text through its own
`Text` and `Edit`, which core's snapshot and edits satisfy, and imports no
fude package. `editor/` imports it directly.

Each highlighted buffer has one goroutine that owns its parser, its tree
and a copy of its text. The kernel's listener hands every change to the
buffer's `syntax.Buffer` on the editor loop, which queues it for the goroutine
as a tree edit and a text edit. Each frame asks for the captures of each
window on the buffer, plus a margin. One request holds every window's
range, keyed by the window, so two windows on far apart parts of a file
do not take turns replacing each other's request. The goroutine merges
ranges that overlap, then takes the newest request and the edits
up to it, parses incrementally and sends the spans back through the
editor's queue. Until they arrive, the frame moves the old spans through the
edits made since, so a keystroke never waits for a parse (B43). A parse
runs to the end even when a newer request comes, since a stopped parse
starts over. On a large file, keys that come faster than the first parse
would stop it each time, and the buffer would never get colours. Only
closing the buffer stops a parse, through the progress callback of
`ParseWithOptions`. A parser stopped as it balances its tree cannot parse
again, because `ts_parser_reset` leaves it set to resume a tree it freed.
The binding has no finalizers, so the goroutine closes its parser, tree,
query and cursor when the buffer closes.

The binding routes each of tree-sitter's allocations through a call into
Go. Those calls cost a fifth of a parse. `syntax/` sets tree-sitter's
allocator back to C's malloc at init. Both use the same C heap, so memory
allocated before the switch frees safely.

Captures paint as tree-sitter's own highlighter picks them (B46). A capture
on a node inside another wins, and of the captures on one node only the last
pattern's counts. The terminal client colours them from fude's theme, in
`ui/term/theme.go`: a set of named colours for each of dark and light,
from which every style derives, starting with the text's colours on the
ground (B145, D33). A base16 scheme fills the same set: base00 to base03
are the grounds and faint text, base04 and base05 muted and plain text, and
base08 to base0E errors, numbers, warnings, strings, hints, functions and
keywords. `editor/` reads the scheme, as config, with one pattern that
takes both stylix's JSON and Tinted Theming's YAML, so no YAML parser comes
in (B146). `:theme` sets the editor's theme for every client. Each client
compares the theme with the one it draws in before each frame and builds
the new styles when they differ (B158).

Help is markdown in one buffer: an index, the guide, then the reference
(D34). The guide's pages are files in `editor/guide`, embedded in the
binary, whose names order them and give their tags. fude builds the
reference from the tables that run it, so it cannot fall behind them.
`editor/settings.md` explains each option and setting at length, under a
heading of its name, and help puts each explanation under its entry. Each ex command, key, `<Plug>` command and text object is an entry
in a table that holds its handler and a line of help: `vim.Commands` and
`vim.Bindings` for the machine's, `commands`, `plugs`, `hostOperators`
and `syntax.Objects` for the editor's. Options and settings carry their
help in a `help` tag beside their `toml` tag. The mappings come from the
keymaps in use, so the user's own show too. The keys `normal` reads
itself, such as `"{reg}` and `m{a-z}`, are the one list written beside
the code instead of in a table (B162). `fude --man` renders help as
three man pages: the reference in fude(1), the options and settings in
fude-config(5), and the guide in fude-guide(7). It renders them from an
editor with the default config, so they do not depend on whoever builds
them. A guide page's headings, paragraphs, lists and code become roff
(B163).

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
`syntax/` gives for the language at the cursor, such as `// %s` for Go
(B44).

A language's `injections.scm` marks text of another language inside it, as
markdown marks a fenced block's code and HTML a `<script>`. After each
parse the worker runs it over the requested range and parses each region
it marks with a parser of its own, never the buffer's, which keeps its
tree for the next incremental parse. A region is the content node's range
less its named children's, unless `injection.include-children` says to
keep them. Anonymous children stay. Markdown's block grammar makes its `*`
and backticks children of the inline text, and its inline grammar needs
them.
`injection.combined` parses one pattern's regions of a language as one
document. `SetIncludedRanges` keeps the region's bytes where they
are in the whole text, so its spans need no moving. They paint over the
buffer's own, and the region's own injections run in turn, three levels
deep. A language name such as `py` becomes a filetype as a file's
extension would. The worker hands the loop the regions with the spans, so
`gc` finds the language at the cursor. An injected language with no
grammar fude could build starts a build, and every buffer asks for its
spans again once it is built. Highlights and injections queries are lenient: a
pattern with a predicate fude does not evaluate, such as JavaScript's
`#offset!` or Objective-C's `#has-ancestor?`, drops out, and the rest of
the query stays (B46, B166). A query `:select` takes is strict.

Queries use only the predicates the binding evaluates: the `eq?`,
`match?` and `any-of?` families. A query that uses any other fails to load
with its file and line, with three exceptions. `#is-not? local` always holds,
because fude keeps no locals. A plain `#lua-match?` becomes `#match?` in
the source before it compiles, and `#set! "priority"` is ignored (D28). A
file at
`$XDG_CONFIG_HOME/fude/queries/<lang>/<name>.scm` replaces the shipped
query of that name (B41). Indentation follows brackets (B211) and folds
follow indent (B224), so there are no indent or fold queries.

Every grammar, Go's too, comes from a registry in
`syntax/grammar.go`, each a repo, a pinned commit, the directories of its
sources and queries, and a comment syntax. The first file of a language
whose grammar is not built starts a build off the loop: git fetches the
pinned commit alone into a temporary directory, the highlights query is
copied out, and `tree-sitter build` writes the library to
`$XDG_DATA_HOME/fude/grammars/<filetype>/<commit>.so`, last and through a
rename, so a library there means a whole build. `lookup` loads each
library once with dlopen, and fude never closes it. It takes a lock,
because the finder's preview highlights off the loop. The text objects of a built
grammar are fude's, embedded beside Go's. Every buffer of the language
gets its syntax when the build ends, and a failed build is not tried
again that session except by `:grammar install` (D29, B165).

## LSP

`[filetype.<name>] lsp` names a filetype's servers, and `[lsp.<name>]`
sets one's command and root markers. fude starts one server per name and
project root, after the first frame, for the buffers open then and any
opened later. A buffer has each server its filetype names whose `files`
match it. A request goes to every one of them that declared the provider
for it, and fude waits for all their answers before it shows them. When a
server exits before it answers, fude counts its answer as empty (B169). The root is the nearest
folder above the file holding the
first marker found, or else the file's folder. fude sends it as the
server's `rootUri` and its only workspace folder (B167). It talks to it over stdio with JSON-RPC
from the standard library, through `protocol/lsp/`. It reads text through
its own `Text` and `Edit`, and `protocol/` is a Go module of its own (D44).

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
crash it stays off, and fude says so (B63). fude answers
`workspace/configuration` with the sections of the server's settings that
the request names. It answers `workspace/applyEdit` by applying the edit.
Every other request from a server gets an empty answer. fude also sends a
server's settings in `workspace/didChangeConfiguration` once the server
has started (B168).

Diagnostics are decorations the buffer owns, so they move with edits until
the server publishes new ones. They show inline the way Neovim shows them
by default: an underline in the colour of their severity, over the text's
other colours, with no virtual text. `]d` and `[d` jump to the next and
previous diagnostic, going round the ends, add the jump to the jumplist and
show its message (B61).

A request carries an id the loop keeps with the code that handles the
answer, and the answer comes back through the editor's queue, so a key that
asks a server returns at once. `<C-]>` jumps to the first location the
definition answer names, opening its file when no buffer has it. `K` shows
the hover text in a float at the cursor, rendered from markdown.
The next key closes the float and then acts as usual. `K` while it shows
moves the cursor into the float, a read-only window outside the layout,
where keys move and scroll as in any window. `q`, `:q`, `:close` and any
`<C-w>` key leave it, and another ex command leaves it and runs in the
window under it. Both keys exist in
every buffer, as in Neovim, and say so when no server is attached (B62).

A float is text and the spans that style it, which the editor holds and the
client draws over the windows, in a bordered box at the cursor: below its
line, or above when there is more room there, at most 80 columns and 20
lines. A line wraps after the last space that fits in the box, or at the
box's edge when one word is wider, as Neovim's `'linebreak'` wraps.

fude renders the markdown language servers write, not all of markdown.
Fenced code takes its language's highlights through `syntax.Highlight`.
Inline code, bold, italic, headings and links lose their markers and keep
their styles as spans, and a line of `---` draws as a rule across the
float. A full markdown grammar comes only if servers start sending more.

Completion works as Neovim's does with the same items. In insert mode,
`<C-x><C-o>` asks the server what may go at the cursor. When the answer
comes, fude puts the first item in place of the word before the cursor and
shows every item's label in a float. `<C-n>` and `<C-p>` step through the
items and the typed word, putting each in the text. `<C-y>` keeps the item
and `<C-e>` puts the typed word back. Any other key keeps the item and goes
on to type. An item puts in its `textEdit`, else its `insertText`, else its
label, and fude offers no snippets. Each step is an edit inside the insert's
undo group, so the whole insert stays one undo step (B64).

A second box beside the menu shows the selected item's `detail` as code of
the buffer's filetype, then its `documentation` rendered from markdown as
hover is. Neovim's `'completeopt'` `popup` shows the same. The box goes
right of the menu, or left when there is more room there. When an item
came without either, fude sends it back once with `completionItem/resolve`
if the server offers that, and shows the answer if the item is still
selected (B68).

With `autocomplete` on, as with Neovim's `'autocomplete'`, typing a word
character or one of the server's trigger characters asks for completions
too. That menu puts nothing in the text until `<C-n>` or `<C-p>`, and the
next typed key closes it and asks again. fude drops an answer for text that
changed after the request. The items come from the language
server only, not from the buffer's words (N8).

`gq` is an operator `editor/` adds. With no formatter that applies, it
asks for `rangeFormatting` of the target's lines when the server offers it. Otherwise it asks for
`formatting` of the document, as gopls offers only that, and keeps the
edits that fall inside the lines. The answer applies as one transaction on
the text the request saw, so it is one timeline node. fude drops an answer
for text that has changed since, and says so (B65).

`grr`, `gri`, `grt` and `gO` are mapped while the current buffer has a
server and removed when it has none (N3). `Machine.Unmap` also removes the
prefixes only they used, so `gr` waits for nothing in a buffer without one
(B114). While a server is attached, `gr` waits `key_timeout` for the next
key, as in Neovim. When an answer names one place, fude jumps there, as
`<C-]>` does. When it names several, and always for references, fude opens
a list window of `path:line:col: text` lines, with paths relative to the
working directory.
`gO` lists the document's symbols, nested ones indented. `<CR>` on a line
jumps to its place in another window and keeps the list (B66).

`grn` opens the command line as `:rename <word>`, with the word under the
cursor, the way Neovim prompts with it. `:rename new` asks the server to
rename the symbol where `grn` was pressed. Its workspace edit applies one
transaction per file, opening a buffer for a file that has none and
writing nothing, as Neovim does. Edits to the buffer that asked must fit
the text it asked about, and edits to another buffer the version the
server names, or fude leaves that file alone and says so. A command runs
inside the machine's handling of the key that ran it, so keys it wants
typed, such as `:rename`, wait in a queue until that key is done.

`gra` asks for the code actions at the cursor, sending the diagnostics on
its line, and shows them numbered in a float. A number and `<CR>` take one,
as Neovim's prompt does, and any other key cancels. An action's edit
applies as rename's does, and its command goes to the server with
`workspace/executeCommand`. An action with neither but `data` is resolved
first when the server can. A server request such as `workspace/applyEdit`,
which gopls sends to finish a command, reaches the editor loop, which
answers it. While a server is attached, insert `<C-s>` shows the signature
being typed in a float, with its active parameter bold. The next key closes
it (B66).

## Terminal

A terminal is a buffer whose `term` holds a program running in a
pseudo-terminal (D38). Three goroutines serve it. One reads the program's
output and hands each chunk to the editor loop, which writes it into the
emulator, so the emulator and the screen it keeps are only ever touched
on the loop. One copies what the emulator answers and encodes, such as
keys, to the program. One waits for the program to end and tells the
loop. In terminal mode, `Feed` sends keys to the emulator before
anything else sees them, and the view carries the screen's cells, which
the client draws as they are, every frame, outside B197's line keys.
Leaving terminal mode replaces the buffer's kernel text with the
scrollback and screen as text, marks the windows on it afresh, and makes
it read-only. The layout tells each terminal its window's size, and
closing the buffer ends the program (B205).

## Server split (step 6)

One server holds the kernel and its modules and applies every client's
keys and requests in arrival order on the editor loop. A thin client keeps
no text of its own, and a replica keeps a copy it rebases on the server's
order (D24). Either way the server's buffer is the one history, so two
clients converge (B73).

`editor.Editor` is the shared state: buffers, plugins, language servers and
decorations. `editor.Session` is one client: its windows, vim machine,
message and float. The terminal is the first session, and `Attach` adds
another. Every session's machine gets the built-in operators, the config
keymaps and the plugin keys, including a session that attaches later. A
session's edits carry its origin, so a kernel group never mixes two
clients' edits and `u` reverts only the session's own changes with
`UndoOrigin` (B77).

Clients attach over a unix socket: the `socket` setting, or else
`fude.<pid>.sock` in `XDG_RUNTIME_DIR` or the temporary directory. fude
listens on no network address unless `fude --share` asks it to (D40). A client on another machine forwards the
socket over SSH, as `ssh -L /tmp/fude.sock:/run/user/1000/fude.sock host`,
and dials the local end, so SSH encrypts and authenticates it (B97). fude
leaves alone a socket that another fude answers and listens at the socket
named by its process instead, as without the setting, so a second fude in
another pane says nothing. It replaces a socket that no one answers, which
a crashed fude left. Each call is a line of JSON,
`{"id", "method", "params"}`, and each reply is a line with the `id` and a
`result` or an `error`. `keys` types keys as one complete input, so keys
held for a longer mapping run at once. `request` runs a request, and
`state` changes nothing. `pull` and `push` are a replica's, below, and
return results of their own. Every other result is the session's state: its buffer,
the buffer's name, version and text, the cursor, the mode, the message and
the session's proposals on the buffer.
Between replies the server sends
`{"method": "changed", "params": {"buffer", "version"}}` when the text of
the buffer a client shows changes, whoever changed it, so a client knows to
read `state` and redraw (B78). A newer notice replaces one the client has
yet to read, so it hears the newest version and not every step.

The protocol is fude's own and carries state, not cells: buffer text and
edits, views, decorations, modes and proposals. A terminal client renders
its own cells from that state, and an agent reads text and positions
straight from it. Neovim's UI protocol does not fit, because it shares one
screen and one cursor between every client, and real Neovim GUIs also call
much of Neovim's Lua and Vimscript API (D25).

A request with `Proposal` runs its keys on a copy of the buffer's text and
keeps the one edit they make as a decoration the proposing session owns:
marks around the text it replaces, and its new text. A view draws the new
text as ghost text after the range, which it shows struck out, and the
buffer's text does not change (B71). The copy shares the buffer's syntax,
so `a_function` and the other syntax objects work in a proposal. `format`
cannot be proposed, as its edit waits for the language server. The marks move with edits, and an
edit inside the range marks the proposal stale (B74). `:accept [n]` applies
it as one transaction of the proposer's origin, which makes one node
(B72), and refuses a stale one. `:reject [n]` drops it, and nothing enters the
timeline. Both default to the newest proposal on the current buffer. A
client accepts or rejects with `keys`, and its state lists its proposals on
its buffer with their ids. A proposal outlives the client that made it,
and goes when its buffer closes.

### Trace

`fude --tail` shows what a running fude does, as it happens (B174). Code
in any package of the root module records an event with
`trace.Record(src, text, took)`, from `internal/trace/`. The
sources are a key and its mode, a command line, a message the user saw,
a frame, a parse or highlight, a language server's messages and answers,
a plugin call or event, a file read or written, a program fude ran, a
client's call, and a line of fude's log. `trace.Record` is safe on any
goroutine. It takes a mutex, stamps the time and a sequence number, and
keeps the event in a ring of the last 4096. With no tail watching that is
all it does. It does no I/O, and it adds about 40µs to key latency at p99.
A library in a module of its own, such as `syntax/` or `protocol/`, takes a trace func
from its host instead.

A client's `tail` call gets an empty reply, then the kept events and each
new one as `{"method": "event", "params": {"seq", "at", "src", "text",
"took"}}`. The server writes them off the editor loop until the client
leaves. A tail that falls 4096 events behind loses events instead of
slowing fude, and the gap in `seq` says how many. `fude --tail [--json] [socket]` dials the
socket, or else the `socket` setting, or else the one fude that answers in
`XDG_RUNTIME_DIR` or the temporary directory. It prints each event as the
time, the source, what happened and how long it took, or as JSON.

### Replicas

`client.Replica` keeps a copy of one buffer in a `kernel.Buffer` of its
own. `Edit` applies edits to the copy at once, so they show without a
round trip, and keeps them as pending. `Begin` and `End` group `Edit`
calls into one node, as an insert is. `Sync` pulls the server's edits
since the version the copy last heard, then pushes the pending nodes one
at a time. Inside a group it does nothing, as the group is not a node
yet.

- `pull {buffer, version}` returns `{buffer, version, edits}`, the edit
  lists from `Since`, or `{buffer, version, text}` when the version is
  left out or the server no longer has the edits. Both carry `indent`,
  the buffer's indent width from the server's `config.toml`. It shows the buffer in
  the client's window, so the client hears `changed` for it.
- `push {buffer, version, changes}` applies the changes, each a list of
  edits, one after another as one node of the client's origin when
  `version` is the buffer's, and returns `{applied: true, version}`. Otherwise it returns `{applied: false,
  version}` and changes nothing, and the client pulls and pushes again.

To take the server's edits, the replica first undoes its pending edits.
That leaves the server's text as the replica last heard it. It applies the
server's edits, then applies each pending edit again where `Map` moves it
from the version it was made at. The log from that version holds the edit
itself and its undo, which cancel, and the server's edits. Each pending
edit keeps the text it replaced. When another client changed that text
first, the replica drops the edit, and `Sync` returns it. A replica so far behind that the server
no longer has the edits takes the server's whole text, which drops every
pending edit inside it.

`client.Editor` runs the vim grammar on a replica, with its cursor and
marks as marks in the copy, so they move when `Sync` rebases. Keys edit the
copy with no round trip, and an insert is one group. `u`, `<C-r>` and the
keys the grammar leaves to its host, such as `:w`, sync and then run in
the client's session on the server, so undo reverts the client's own
changes from the one timeline (B77, B98). The jumplist and changelist are
the client's own, as marks in the copy, and `g-` and `g+` run on the
server, as the timeline is there (B99). `"+` and `"*` are the clipboard of
the client's machine (B36), and `>` and `<` shift by the server's indent
width for the buffer (B94). The grammar's cases in
`vim/testdata/grammar.txt` run through it too (B110).

## Sharing (step 7)

Step 7 lets another person edit in a running fude. It comes in phases,
each usable on its own: attach (D39), guests and `--share` (D40), the
relay (D41), then presence, a guest's own keymaps and options, read-only
and follow, and attaching through a replica.

### Attach

`fude --attach` dials the socket and calls `attach`. The server makes a
session for the connection with `Attach`, and the connection then carries
a stream each way. The client sends `keys`, `paste`, `size` and `focus`
notices, the keys as a list in key notation. One goroutine per connection
makes a frame on the loop after each notice, and every 50ms to catch other
sessions' edits, new parses and servers' answers. It sends the frame as a
`frame` notice when it differs from the last one sent. Each frame counts
the notices fude has handled, so a frame follows every notice and the
client knows which of its keys it shows. Because one goroutine sends,
each frame's edits arrive in order. Closing the connection makes the
session leave (B240).

A frame holds what `ui/term` reads of a session: the views without their
text, the float, the command line and its cursor, the mode, message,
selection, other cursors, theme, key hints, signature help, `:s` preview,
the macro being recorded and the text to put on the client's clipboard.
Each view names its buffer and version. A buffer's text comes once, then
as the edits since the version the client holds, the lists `Since` gives
and `pull` returns. A client that falls too far behind gets the whole
text again.

`ui/term` reads a session through one interface, the methods it calls
today, such as `Views`, `Float`, `Feed` and `SetSize`. `*editor.Session`
implements it, and so does the attached session in `ui/term`, which
applies frames as they come and sends input as it is typed. The speed
harness measures the attached path on the local socket as it measures
the local one.

A session other than the first keeps its clipboard to itself: a copy
sets the text it puts and hands it to its client, which writes it with
its machine's clipboard program or OSC 52, and a put reads that text
(B241). Only the first session reads and writes the system clipboard.

### Guests

Whether a session is a guest, and whether it is read-only, is fixed when
the session is made. The server checks each limit in one place:

- Commands: `ex`, the `<Plug>` dispatch and fude's own operators refuse,
  for a guest, the commands B244 names, so a key mapped to one is refused
  too. A code
  action that runs a command is refused where it is taken.
- Paths: one check, under the project after `EvalSymlinks` with `under`,
  runs where a buffer opens, where a file is written, read or listed, and
  where a listing applies its changes (B245). A guest cannot show a
  buffer whose path fails that check, even one already open.
- Plugins: a plugin's key, command, operator or object checks the
  plugin's grants before it runs for a guest (B246).
- Read-only: each place a session changes a shared buffer's text calls
  `mayEdit` first. Those places are `host.Apply`, `:accept`, `:e!`,
  formatting, a language server's edits, `:restore` and requests. Keys,
  completion, snippets and plugins' commands go through `host.Apply`.
  Replica pushes will call it once a guest can push (B247).

### Share and join

`fude --share [address]` makes a self-signed certificate for the share
and listens with TLS 1.3. Its first message is the share string, and it
puts `fude --join` and the string on the clipboard. The string is the
address, the certificate's SHA-256 fingerprint and a 128-bit secret,
joined by `/`, the last two in unpadded base64url.
`fude --join string` dials it with `InsecureSkipVerify` and a
`VerifyConnection` that compares the fingerprint, then sends the secret
and `$USER` as its first line. The host's fude asks whether to let the
guest in, and on yes the connection goes on as an attach does, to a
guest session. The secret then no longer works, and `:share` makes a new
one. One join waits for the host's answer at a time. `:who` lists the
guests and `:kick name` ends one. Quitting fude ends the share and every
guest session, and `:restart` starts fude again without `--share`.

### Relay

`fude relay [address]` takes connections whose first line names a room
and whether they share or join. It answers each with one line, `{}` or
an error. A share holds the room's one place, and a later share for the
room takes the place from it. A join to a held place takes it, the relay
sends the share a second `{}`, and the relay copies bytes between the
two until either closes. It never terminates the TLS, so it cannot read
what it copies. The host holds a new place each time a guest takes one.
A share string names the relay and a random room instead of an address,
as `relay/room/fingerprint/secret`, when `--share --relay host` made it.

### Presence

Each session has a name and a colour, a number fixed when it begins.
`Views` gives each view the other sessions whose current window shows
its buffer, with their names, colours, cursors and selections. `ui/term`
maps the number to one of six of the theme's accents, so each client
draws the others in its own theme. The attach call and a guest's first
line carry the client's name.

### A guest's own config

`fude --join` reads `config.toml` on its own machine. Its first line
carries the file's keymaps, leader and window options, which the guest's
session takes in place of the host's (B254). The client keeps the theme
and draws in it. It reads a base16 file on its own machine, so nothing
of the theme reaches the host.

### Read-only joins and following

`fude --join --read-only` sets `read_only` in its first line, the host's
question names it, and the session is `ReadOnly` (B247, B255). `:follow
name` stores the session it follows. `Views` first rebuilds the
session's window tree as a copy of the other's. It keeps each window
that copied one of the other's last time, and makes the rest anew. Each
window takes its counterpart's buffer, top, left column and cursor. Then
it scrolls as its own rows need to show the cursor, so each frame and
draw shows what the other shows. `Feed` and `Paste` end following, since keys
typed at the other's cursor would edit there (B256).

### Later phases

- Attaching through a replica, so keys need no round trip on a slow link
  (D24, B98).

## Debugging (step 8)

`protocol/dap/` is the client, shaped as `protocol/lsp/` is: an outbox a goroutine writes,
a reader that queues each response and event on the loop, and a pending
map only the loop touches. Both read and write frames through
`protocol/framing/`. A command with `{port}` listens on that port, which fude
dials until the process ends or five seconds pass. Others speak on
stdio.

A session sends `initialize`, then `launch` or `attach`. It answers the
`initialized` event, which Delve sends before it answers `launch`, with
`setBreakpoints` for each file, `setExceptionBreakpoints` with no
filters and `configurationDone`, so the launch's answer is never waited
on. `stopped` names the thread that later commands act on, or `threads`
gives the first. `runInTerminal` and `startDebugging` from the adapter
get a refusal, so launch settings use the adapter's own console. A
session ends on `terminated`, `exited` or the adapter's process ending,
and each clears the stopped line, the thread and the panel. `:debug stop`
sends `disconnect` and ends the process if it still runs a second later.

Breakpoints are kernel marks in their buffers. The stopped line is a
decoration with its own owner, and a sign. The panel is a scratch buffer
of the kind `debug`, whose `<CR>` acts on the frame or variable of its
line. `:debug` is refused for a guest (B244).

