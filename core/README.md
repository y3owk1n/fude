# core

A text buffer with marks, transactions and a branching undo history, for Go programs that edit text. It is fude's kernel. It imports only the standard library, starts no goroutines, does no I/O and reads no clock.

| Package | What it does |
|---|---|
| [`kernel`](kernel) | Buffers: text, marks, transactions, the timeline and its persistence |
| [`rope`](rope) | The persistent rope a buffer keeps its text in |

```sh
go get github.com/y3owk1n/fude/core
```

`go get` takes the newest release. While fude is 0.x, a release that breaks this API bumps the minor version.

## Example

```go
b := kernel.New("hello world", nil)
world := b.SetMark(6, kernel.Left)
t := kernel.Transaction{Base: b.Snapshot().Version(), Origin: "me", Label: "cw", Time: time.Now()}
t.Edits = []kernel.Edit{{Start: 0, End: 5, Text: "goodbye"}}
if err := b.Apply(t); err != nil {
	return err
}
// "goodbye world", and world is at 8
b.Undo()
// "hello world", and world is at 6
```

This example runs as a test in [`kernel/example_test.go`](kernel/example_test.go).

## What a buffer does

- **Transactions.** `Apply` takes sorted edits against a base version, and returns `ErrStale` unless the buffer is still at that version. A transaction carries its origin, a label and the time, which the host supplies.
- **Snapshots.** `Snapshot` returns the text at one version. It never changes, and taking one copies nothing, since it shares the buffer's persistent rope. It is safe to read on any goroutine. It answers `Len`, `Slice`, `Lines`, `LineStart` and `LineOf`, with byte offsets and lines counted from 0.
- **Marks.** `SetMark` places a mark that moves with every edit. Its gravity decides whether text inserted at the mark goes before or after it.
- **A branching timeline.** Every transaction is a node, and an edit after an undo starts a new branch instead of dropping the old one. `Undo` and `Redo` walk one branch. `Goto` moves to any node, and `At` reads the text at a node without moving. `BeginGroup` and `EndGroup` make several transactions one node.
- **Undo by origin.** `UndoOrigin` reverts the newest change of one origin that is still in the text, so each client in a shared buffer undoes only its own edits.
- **Mapping positions.** `Map` moves a position from an older version to now, and `Since` returns the edits made after a version. A background job that read an old snapshot uses them to place its results.
- **Persistence.** The kernel hands every new node to a `Store` you give it. `Load` rebuilds a buffer from the nodes in the order the store received them.
- **Listening.** `Listen` takes a `Listener` that hears every change, with the text from before it, on the writing goroutine.

Write a buffer from one goroutine.

## With the vim grammar

A `Snapshot` is a [`vim`](../vim) `Text` as it is, and `kernel.Edit(e)` converts a `vim.Edit`, so a small host gives a buffer Vim's keys. The root [README](../README.md#build-on-fude) has that host.

## License

[MIT](../LICENSE)
