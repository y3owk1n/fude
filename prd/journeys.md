# Journeys

How fude feels from each side. Output shown is illustrative. The promises
behind it are in `prd/behaviours.md`, cited as B-numbers.

## User

### Day one

```
$ fude main.go
```

The first frame shows in under 5ms. `ciw`, `dap`, `3j`, `.` and `u` do what
Neovim does (B10 to B29). There is no config file yet and nothing asks for one.

### Structural edits

With the cursor inside a Go function, `daf` deletes the function and `cia`
changes the argument under the cursor. The same keys work in Rust, Python and
TypeScript (B40, B41).

### Time travel

After twenty minutes of refactoring, `:timeline` opens the branching history.
`[` and `]` scrub it, `d` diffs the node under the cursor against now, and
`<CR>` restores it as a new node, so nothing is lost (B30 to B33).

## Plugin author

```
$ ls ~/.config/fude/plugins
wordcount.wasm
```

```toml
[[plugin]]
path = "plugins/wordcount.wasm"
capabilities = ["buffer.read", "ui"]
```

The plugin subscribes to `buffer.changed` and sets a statusline segment. If
it tries to write the buffer, the host refuses the call and logs it (B50).
If it loops forever, fude stops it and keeps editing (B51).

A plugin may register an operator `gs` (sort lines). It composes with every
target: `gsip`, `gsaf`, `gs3j` (B52).

## Client or agent

An agent attaches over RPC and sends:

```json
{
  "buffer": 3,
  "at": 1180,
  "base": 118,
  "proposal": true,
  "command": {"op": "change", "target": {"name": "inner_quote"}, "text": "hello"}
}
```

The edit lands as a proposal and shows as ghost text. The user's cursor does
not move. Accepting makes one timeline node and rejecting leaves none
(B70 to B72). If the user edits elsewhere first, the proposal follows the
text. If the user's edit overlaps it, the proposal is marked stale (B74).

A tool that writes a file's new text, such as a codemod, proposes its
hunks instead:

```json
{"method": "propose", "params": {"edits": [
  {"buffer": 3, "version": 118, "start": 1180, "end": 1184, "text": "hello"},
  {"path": "/src/app/new.go", "start": 0, "end": 0, "text": "package app\n"}
]}}
```

`:review` lists both as the lines they change. The user accepts the first
with `a` and rejects the second with `r`, and the tool hears what became
of each (B387 to B391).
