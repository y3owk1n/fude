# vim

Vim's editing grammar as a state machine, for Go programs that keep their own text: a text field in a TUI, a REPL, another editor. It is the grammar fude runs. It starts no goroutines, does no I/O and reads no clock, and it does not import fude's kernel.

```sh
go get github.com/y3owk1n/fude/vim
```

`go get` takes the newest release. While fude is 0.x, a release that breaks this API bumps the minor version.

## How it works

Your program is the host. It feeds the machine keys, and the machine reads and edits the text through the `Host` interface you write.

The machine owns modes, counts, operators, motions, text objects, registers, dot repeat, macros, search and the ex commands that edit text. The host owns the text, its undo history, files, windows and what shows on screen. An ex line the machine does not own, such as `:w`, reaches the host through `Unhandled`, and an error it returns stops what ran the line, such as a macro or `:g`.

Feed keys from one goroutine. When `Waiting` reports keys that wait for more, as `d` waits for a motion, call `Timeout` after your own delay.

## Example

A host for a one-line text field. It embeds `vim.Defaults` for what it has no use for, such as undo, jump lists and the clipboard, and writes the rest:

```go
type field struct {
	vim.Defaults
	text   string
	cursor int
	marks  map[rune]int
}

func (f *field) Text() vim.Text             { return line(f.text) }
func (f *field) Cursor() int                { return f.cursor }
func (f *field) SetCursor(pos int)          { f.cursor = pos }
func (f *field) Viewport() (int, int, int)  { return 0, 1, 80 }
func (f *field) SetTop(int)                 {}
func (f *field) SetMark(name rune, pos int) { f.marks[name] = pos }
func (f *field) DeleteMark(name rune)       { delete(f.marks, name) }

func (f *field) Mark(name rune) (int, bool) {
	pos, ok := f.marks[name]
	return pos, ok
}

// Apply makes the edits last to first, so each edit's offsets are still
// right when it is made. A full host also moves its marks here.
func (f *field) Apply(edits []vim.Edit, _ string) error {
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		f.text = f.text[:e.Start] + e.Text + f.text[e.End:]
	}
	return nil
}
```

```go
f := &field{text: "hello big world", marks: map[rune]int{}}
m := vim.New(f)
for _, k := range vim.Keys("wdw") {
	if err := m.Feed(k); err != nil {
		return err
	}
}
// f.text is "hello world"
```

`line` is a string type that implements `vim.Text`: `Len`, `Slice`, `Lines`, `LineStart` and `LineOf`, with byte offsets and lines counted from 0. The whole example, with `line` and marks that move, runs as a test in [`example_test.go`](example_test.go).

## Drawing it yourself

The machine draws nothing. How text looks is up to the host, and the machine tells it what to draw:

- `Rows` and `WordRows` return where each screen row of a line starts when it wraps, by character or after a space, and `Hang` how many columns the rows after the first start in, for an indent kept or a mark.
- `Column` returns the display column a byte draws at, and `ColumnStart` the byte at a column. They count graphemes, wide characters and tab stops.
- `Selection` returns the visual selection's bytes, and for a block its display columns `Left` and `Right`.
- `Mode`, `Shown`, `Line`, `LineCursor` and `Message` give the mode, the keys of the command being typed, the command line with its cursor, and what the last key has to say.

Measure columns with `Column`, passing nil inlines and the width your `TabWidth` returns, rather than with code of your own. The machine measures block selections and `gj` the same way, so the highlight then covers what `d` deletes.

## Adding to the grammar

- `Map` maps keys to keys in one mode, as a mapping in a config does.
- `Operator` adds a verb that composes with every motion and text object.
- `Object` adds a text object that composes with every operator.
- `Command` adds a command a key reaches as `<Plug>(name)`.
- `Selector` gives `:select` a way to find regions, which the next operator acts on together.

`Object`, `Operator` and `Command` with a nil func remove what they added under that name.

## With fude's kernel

[`core`](../core) holds text with marks, transactions and a branching history. Its `Snapshot` is a `Text` as it is, and `kernel.Edit(e)` converts an `Edit`. The root [README](../README.md#build-on-fude) has a host over the kernel.

## License

[MIT](../LICENSE)
