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

- **Registration.** `Registered` lists what the module adds: commands, operators, text objects, keys in normal or insert mode, plugs that only `<Plug>(owner.name)` reaches, and sources of `:pick`, each with the id `Call` runs it by. A key with `Visual` set is mapped in visual mode too, and its call gets the selection. One with `Attached` set is mapped only while a language server is attached to the buffer, and one with `Enters` set moves the cursor into a float that shows instead of running. An operator or key with `Chars` set waits for that many characters typed after it, which its call gets as `chars` in `About`, and dot repeats them. Such a key is read by the vim machine as its own, after an operator's key, as `ds` is, or alone in visual mode with `Visual`, as `S` is. A plug with `Keys` is mapped to them, in normal mode or with `In` among a kind of buffer's own keys, and config.toml's mappings win. A `Write` writes buffers of the kind it names, as `:w` does, with `Entries` in its scope, and its result's `Confirm` asks first. A `Typed` key runs before a key typed in insert mode does its own work. Its call's edits take the key's place, and none lets the key do what it does. A command's `Short` are names it answers to too, as `ls` is `buffers`. A command with `Complete` set also completes its argument. `Call` then runs with the word typed as `complete` in `About`, and the menu shows the `Complete` of its result or of a later callback's. `Kind`'s comments say which arguments each one's call gets. An operator composes with every motion, and an object with every operator, as fude's own do.
- **Calls.** `Call` runs on the input path, which never waits. While another call into the module runs, `Call` returns `ErrBusy` instead of waiting for it. An object's call sets `Result.Selected` to the range it selects.
- **Events.** `Subscribed` names the events the module hears, such as `buffer.opened`, `buffer.changed`, `buffer.saved`, `buffer.closed`, `mode.changed`, `view.moved`, `syntax.updated`, `lsp.diagnostics`, `lsp.attached` once a server is ready for a buffer, `editor.focused` when the terminal gets the focus back, `editor.started` when fude starts with no file to open, `keys.pending` with the command being typed when it changes, `editor.quitting` as fude quits, which runs on the loop before the events stop, `terminal.exited` when a program its `Terminal` ran ends, `lsp.published` with a server's diagnostics for a buffer as sent, in `params` with the server's `encoding`, `lsp.detached` when a server leaves a buffer, `lsp.changed` when a server starts, is ready, stops, fails or is disabled or enabled, `buffer.saving` before a write, which waits for the handler and its callbacks or `format_timeout`, and `owner.event` for one another module emits. `Event` runs off the editor loop, and gets what the event carries after its name, such as the cursor and the view's top for `view.moved`. A module hears `terminal.exited` without subscribing. Its scope is the terminal's buffer, with `status` and `took` in `About`.
- **Callbacks.** A `Timer`, `Job`, `Pick` or language server `Request` carries an id, and `Callback` runs with it later. Its scope's `About` adds what came back: `output` and `error` for a job or request, `server` and `encoding` for a request, and `picked`, `text` and `ok` for a pick. When the text a callback read has changed since, its message, window, file jump and decorations on other buffers still apply. Its edits, and its decorations and jump on that text, wait for a newer event.
- **Background work.** A `Job` asks for a program to run, with `Input` on its stdin, or a URL to get. fude runs `Work` off the loop, then the callback.
- **Budgets.** fude stops a plugin's call that runs past its budget, but it can only time a Go module's. Keep calls on the input path short.

## What a call reads and asks for

A `Scope` holds the text of the buffer the call is about, `About` (its `buffer`, `path`, `filetype`, `mode`, `power`, `servers`, the language servers attached to it, `indent` and `tab`, and a command's `args`, `bang`, `first`, `last` and `ranged`), every open buffer by id, and funcs over the syntax: `Language` for the language at a position, `Comment` for how that language comments a line, `Spans` for the highlight captures over a range, and `Node`, which finds syntax nodes. `Object` finds the range one of the vim machine's text objects, such as `a(` or `2i"`, selects at a place. `Decorations` lists the module's own decorations on every open buffer, each with its `Buffer`, where they were when the call began. A buffer with a title, such as a panel, has `title` in `About`. A call about the current window has `top`, where its view starts, and `bottom`, the end of the line one window height below. A call that captured the keys has the key typed in `key`. `columns` in `About` is the screen's width. `Keys`, `Commands` and `HelpTags` list every key, ex command and help tag fude knows, in a call alone. `States` and `StateText` read a buffer's states and their text, in a call alone, and so do `Marks`, `Jumps`, `Changes`, `Messages` and `Register`. Each buffer in `Buffers` has its `Name` and whether it is `Modified`. `Window` says whether a window is next to the current one that way, in a call alone. `Session` is the windows on files as JSON, in a call and in `editor.quitting`. `Hints` lists the keys that may follow the command being typed, for a module that hears `keys.pending`, with `pending`, `area` and `lines` in `About`. `Servers` lists the language servers config.toml sets, each with its state, version, filetypes, roots and files. `Option` reads an option, such as `spell`, as the window has it, or a config.toml setting, such as `spell_words`, and `Config` reads any value of config.toml by its dotted path, as JSON. `About` also carries the buffer's `indent` and `tab` widths. A `Text` has `Len`, `Slice`, `Lines`, `LineStart` and `LineOf`, with byte offsets and lines counted from 0.

A `Result` asks for any of these, and fude applies them together once the call returns:

| Field | Asks for |
|---|---|
| `Edits` | edits to the text, applied as one change |
| `Decorate`, `Undecorate` | styles, signs and notes on ranges, which move with edits, by the ids the module gives them, on this buffer or on another by its `Buffer` id. On one line the sign at the highest `Layer` shows: git's are at -1, most at 0, a diagnostic's at 1 and a breakpoint at 2. The style `inlay` draws the note among the text at the start, and `colour #rrggbb` draws the range on that colour. A decoration with `Fold` set closes a fold over its lines, and one with `Cover` draws its note over its range |
| `Emit` | events for other modules and plugins |
| `Status` | the module's segment on the statusline |
| `Capture` | the keys typed next, each sent to the same call with `key` in `About`, until a result without it |
| `Message` | a line in the message area |
| `Float` | text at the cursor, markdown, or as it is with its own spans. With `Keys` it is the key hints across the bottom of the windows |
| `Show` | text in a read-only window of its own, with styles over it and keys of its own. Showing it again keeps the cursor's line and the focus. With `Float` it shows in a box in the middle of the screen. With `Cards` the cursor moves by card, and `Foot` stays at the box's bottom as the text scrolls. With `Screen` it shows in the current window in place of the buffer, without line numbers or guides, and `Cursor` is where the cursor starts. With `Update` it only replaces a panel that is open |
| `Servers` | restarts, stops, disables or enables language servers, which a guest may not |
| `Keys` | keys to run once the call is done, as typed |
| `Restore` | a session from `Session`, whose files open and whose windows build again |
| `Confirm` | a `Write`'s yes or no question, with the paths it would change |
| `Moved` | paths a write renamed, whose open buffers follow them |
| `Travel` | a buffer's text moves to one of its states, or restores one as a new state |
| `Copy` | text to put on the clipboard |
| `Window` | the window next to the current one that way becomes current |
| `Jump` | where the cursor goes, in this buffer, or at a line and column of a file, in a window that shows a file, or with `Split` in a new one |
| `Pick` | a list to pick from, live or of places, or a question. With `Update` it fills the open list of its ID, as items read off the loop do. `<C-g>` in a list that is not live tells the callback `all` |
| `Menu` | a numbered menu at the cursor, each item with what it does beside it, whose choice runs the callback |
| `Timers`, `Jobs`, `Requests` | work whose callback runs later. A `Request` goes to the buffer's servers, or to the one it names. A `Stream` job is the module's own long work, whose callback runs for each line it emits |
| `Log` | lines for fude's log |
| `Selected` | the range an object's call selects |
| `Set` | `expand_tab`, `indent_width` or `tab_width` on the buffer, over config.toml |
| `Terminal` | a program in a terminal window below, which the status line names while it runs |
| `Places` | the last list of places, which `]q` steps through, each `path:line:column:text`, or `line:column:text` in `File`, in a window with `Show`, with columns in `Encoding` |
| `Apply` | a language server's workspace edit, applied with the editor's version checks |
| `Cmdline` | a command line started with that text typed, which runs nothing |
| `Complete` | what a command's argument being typed completes with |

## License

[MIT](../LICENSE)
