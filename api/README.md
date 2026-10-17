# api

This module is what fude gives the code that runs inside it. A module built into a distribution and a wasm plugin get the same API: a call reads a `Scope` and returns a `Result` of what it asks fude to do. This module imports only the standard library.

```sh
go get github.com/y3owk1n/fude/api
```

fude has no release yet. Until the first one, `go get` takes the latest commit and the API can change.

## A module

A module is a Go type that implements `api.Module`. This one adds `:Upper`, which upper-cases the cursor's line:

```go
type upper struct{}

func (upper) Name() string         { return "upper" }
func (upper) Subscribed() []string { return nil }

func (upper) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "Upper", Help: "upper-cases the cursor's line", ID: 1}}
}

func (upper) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call gets the cursor and the count, as a command's call does.
func (upper) Call(_ context.Context, _ uint32, s api.Scope, args ...int) (api.Result, error) {
	t := s.Text
	line := t.LineOf(args[0])
	start, end := t.LineStart(line), t.Len()
	if line+1 < t.Lines() {
		end = t.LineStart(line+1) - 1
	}
	return api.Result{Edits: []api.Edit{{Start: start, End: end, Text: strings.ToUpper(t.Slice(start, end))}}}, nil
}
```

Its other four methods do nothing here. `Event` and `Callback` return an empty `Result`, `Work` returns `""`, and `Close` returns `nil`. A distribution adds the module before it loads, with `e.Use(upper{})` on the `*editor.Session` it starts. fude's `cmd/fude` lists its own in `modules()`.

[`modules/blame`](../modules/blame) is a fuller one. It hears the cursor move, runs git in the background and shows a note.

## How fude calls a module

- **Registration.** `Registered` lists what the module adds: commands, operators, text objects, keys in normal or insert mode, and plugs that only `<Plug>(owner.name)` reaches, each with the id `Call` runs it by. `Kind`'s comments say which arguments each one's call gets. An operator composes with every motion, and an object with every operator, as fude's own do.
- **Calls.** `Call` runs on the input path, which never waits. While another call into the module runs, `Call` returns `ErrBusy` instead of waiting for it. An object's call sets `Result.Selected` to the range it selects.
- **Events.** `Subscribed` names the events the module hears, such as `buffer.opened`, `buffer.changed`, `buffer.saved`, `buffer.closed`, `mode.changed`, `view.moved`, `syntax.updated`, `lsp.diagnostics`, `editor.focused` when the terminal gets the focus back, `buffer.saving` before a write, which waits for the handler and its callbacks or `format_timeout`, and `owner.event` for one another module emits. `Event` runs off the editor loop, and gets what the event carries after its name, such as the cursor and the view's top for `view.moved`.
- **Callbacks.** A `Timer`, `Job`, `Pick` or language server `Request` carries an id, and `Callback` runs with it later. Its scope's `About` adds what came back: `output` and `error` for a job or request, `server` and `encoding` for a request, and `picked`, `text` and `ok` for a pick.
- **Background work.** A `Job` asks for a program to run, with `Input` on its stdin, or a URL to get. fude runs `Work` off the loop, then the callback.
- **Budgets.** fude stops a plugin's call that runs past its budget, but it can only time a Go module's. Keep calls on the input path short.

## What a call reads and asks for

A `Scope` holds the text of the buffer the call is about, `About` (its `buffer`, `path`, `filetype`, `mode`, `indent` and `tab`, and a command's `args`, `bang`, `first`, `last` and `ranged`), every open buffer by id, and funcs over the syntax: `Language` for the language at a position, `Comment` for how that language comments a line, `Spans` for the highlight captures over a range, and `Node`, which finds syntax nodes. `Option` reads an option, such as `spell`, as the window has it, or a config.toml setting, such as `spell_words`, and `Config` reads any value of config.toml by its dotted path, as JSON. `About` also carries the buffer's `indent` and `tab` widths. A `Text` has `Len`, `Slice`, `Lines`, `LineStart` and `LineOf`, with byte offsets and lines counted from 0.

A `Result` asks for any of these, and fude applies them together once the call returns:

| Field | Asks for |
|---|---|
| `Edits` | edits to the text, applied as one change |
| `Decorate`, `Undecorate` | styles, signs and notes on ranges, which move with edits, by the ids the module gives them |
| `Emit` | events for other modules and plugins |
| `Status` | the module's segment on the statusline |
| `Message` | a line in the message area |
| `Float` | markdown at the cursor |
| `Show` | text in a read-only window of its own, with styles over it |
| `Jump` | where the cursor goes, in this buffer or in a file |
| `Pick` | a list to pick from, or a question |
| `Timers`, `Jobs`, `Requests` | work whose callback runs later |
| `Selected` | the range an object's call selects |

## License

[MIT](../LICENSE)
