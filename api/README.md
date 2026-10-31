# api

This module is what fude gives the code that runs inside it. A module built into a distribution and a wasm plugin get the same API: a call reads a `Scope` and returns a `Result` of what it asks fude to do. This module imports only the standard library.

```sh
go get github.com/y3owk1n/fude/api
```

`go get` takes the newest release. While fude is 0.x, a release that breaks this API bumps the minor version.

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

Its other four methods do nothing here. `Event` and `Callback` return an empty `Result`, `Work` returns `""`, and `Close` returns `nil`. A distribution adds the module before it loads, with `e.Use(upper{})` on the `*editor.Session` it starts. fude lists its own in `distribution.Modules`, which `cmd/fude` adds.

[`modules/blame`](../modules/blame) is a fuller one. It hears the cursor move, runs git in the background and shows a note.

## How fude calls a module

- **Registration.** `Registered` lists what the module adds, each with the id `Call` runs it by. `Kind`'s comments say which arguments each one's call gets. The two lists below give the kinds and the fields.
- **Calls.** `Call` runs on the input path, which never waits long. While another call into the module runs, `Call` may wait briefly for it, as a plugin's waits at most half its budget, and returns `ErrBusy` instead of waiting longer. An object's call sets `Result.Selected` to the range it selects.
- **Events.** `Subscribed` names the events the module hears, which the list of events below gives. `Event` runs off the editor loop, and gets what the event carries after its name, such as the cursor and the window's top for `window.moved`. A module hears `terminal.exited` without subscribing. Its scope is the terminal's buffer, with `status` and `took` in `About`.
- **Callbacks.** A `Timer`, `Job`, `Pick` or language server `Request` carries an id, and `Callback` runs with it later. Its scope's `About` adds what came back: `output` and `error` for a job or request, `server` and `encoding` for a request, and `picked`, `text` and `ok` for a pick. When the text a callback read has changed since, its message, window, file jump and decorations on other buffers still apply. Its edits, and its decorations and jump on that text, wait for a newer event.
- **Background work.** A `Job` asks for a program to run, with `Input` on its stdin, or a URL to get. fude runs `Work` off the loop, then the callback. A `Fed` stream job keeps its input open. `Work` gets the text each later `Send` to its id carries on `Feed`, in order, so a module can hold a conversation with a program it runs, such as a completion agent. A `Send` with `Stop` ends a stream job's `Work`.
- **Budgets.** fude stops a plugin's call that runs past its budget, but it can only time a Go module's. Keep calls on the input path short.

### What a module registers

- Commands, operators, text objects, keys in normal or insert mode, plugs that only `<Plug>(owner.name)` reaches, and sources of `:pick`. An operator composes with every motion, and an object with every operator, as fude's own do.
- `Write` writes buffers of the kind it names, as `:w` does, with `Entries` in its scope, and its result's `Confirm` asks first.
- `Completer` is a source of insert mode completion by a name `completion_sources` orders, asked after a word character or a character of its `Keys`, whose result's `Completion` lists its items.
- `Typed` is a key, named in notation such as `<Tab>`, that runs before a key typed in insert mode does its own work. Its call's edits take the key's place, and none lets the key do what it does. An open completion menu or a snippet being filled in takes `<Tab>` first.
- `Option` is a value `:set` changes as `owner.name`, a switch, a number or text as its `Default` is. It starts at its key in a plugin's `options` table, or in a module's `[module.owner]` table. With `Window`, each window has its own.
- `ClientCommand` is a command a language server's code action, code lens or completion names. fude runs it by that name in place of sending it to the server, with the command's JSON `arguments` and the `server` in `About`.
- `Language` is a grammar for the filetype `Name`, which fude builds as its own from the git repo, commit and folder its `Source` names, with queries as paths in the repo or as text. A filetype fude has a grammar for keeps fude's.
- `Page` adds a page to help's guide. Its tag is `guide-` and its `Name`, and its `Doc` is the page's markdown, under a `#` title.

### What a registration sets

- `Visual` maps a key, or a plug's `Keys`, in visual mode too, and its call gets the selection.
- `Attached` maps one only while a language server is attached to the buffer.
- `Bound` maps one only while the window scrolls with others, as windows in diff mode do.
- `Filetype` maps one only in buffers of that filetype, over a key of no filetype. When a `Filetype` key's call asks for nothing, the key does what it does without that mapping.
- `Enters` makes one move the cursor into a float that shows instead of running.
- `Chars` makes an operator or key wait for that many characters typed after it, which its call gets as `chars` in `About`, and dot repeats them. Such a key is read by the vim machine as its own, after an operator's key, as `ds` is, or alone in visual mode with `Visual`, as `S` is.
- `Keys` maps a plug to them, in normal mode or with `In` among a kind of buffer's own keys. config.toml's mappings of those keys win, in `[keymap.normal]`, `[keymap.visual]` or the kind's own table. fude's modules give every key they add a plug this way, named for what it does.
- `Doc` explains a registration at length in markdown, under its entry in `:h`, unless fude's own reference explains it.
- `Short` are names a command answers to too, as `ls` is `buffers`.
- `Args` say what a command takes, for `:h` and `:pick commands`.
- `Paths` makes a command take a path, as `:e` does. Its argument completes as a path, and the editor expands `%`, `#` and `~` in it before the call and refuses a guest's path outside the project.
- `Complete` makes a command complete its argument too. `Call` then runs with the word typed as `complete` in `About`, and the menu shows the `Complete` of its result or of a later callback's.

### Events

- `buffer.opened`, `buffer.changed`, `buffer.saved`, `buffer.closed`, `mode.changed`, `window.moved`, `syntax.updated` and `lsp.diagnostics`.
- `buffer.saving` before a write, which waits for the handler and its callbacks or `format_timeout`.
- `buffer.renamed` with the old path.
- `window.opened`, `window.closed` and `window.focused` with the window's id.
- `editor.focused` when the terminal gets the focus back.
- `editor.started` when fude starts with no file to open.
- `editor.quitting` as fude quits, which runs on the loop before the events stop.
- `config.reloaded` after `:config reload`.
- `option.set` with the name `:set` set.
- `keys.pending` with the command being typed when it changes.
- `terminal.exited` when a program its `Terminal` ran ends.
- `lsp.attached` once a server is ready for a buffer.
- `lsp.published` with a server's diagnostics for a buffer as sent, in `params` with the server's `encoding`.
- `lsp.detached` when a server leaves a buffer.
- `lsp.changed` when a server starts, is ready, stops, fails or is disabled or enabled.
- `lsp.refresh` for each buffer of a server that asks to be asked again, with what for, such as `codeLens`.
- `lsp.notified` and `lsp.requested` for a server's notification or request fude does not handle, with `server`, `method`, `params` and a request's `request` id in `About`, which `Result.Answers` answers.
- `search.changed` with the pattern.
- `macro.recorded` with its register.
- `selection.changed` with the selection's start and end, or nothing once it ends.
- `theme.changed` with the theme.
- `cwd.changed` with the working directory.
- `explorer.moved` with `from` and `to` in `About` for each file or directory an explorer's write moves.
- `confirm.asked` with the question when fude asks one, and `confirm.answered` with `yes` or `no` when it is answered.
- `proposals.changed` when a proposal comes, goes or goes stale.
- `owner.event` for one another module emits.

## What a call reads and asks for

A `Scope` holds what a call reads:

- **The text** of the buffer the call is about. A `Text` has `Len`, `Slice`, `Lines`, `LineStart` and `LineOf`, with byte offsets and lines counted from 0.
- `About`, what the call is about:
  - its `buffer`, `path`, `filetype`, `mode`, `cursor`, `cwd`, `project` and `power`
  - `session`, an id no other session of this fude has
  - `servers`, the language servers attached to it
  - `indent` and `tab`, the buffer's indent and tab widths
  - a command's `args`, `bang`, `first`, `last` and `ranged`
  - `title`, for a buffer with a title, such as a panel
  - `top`, where its text starts, and `bottom`, the end of the line one window height below, for a call about the current window
  - `key`, the key typed, for a call that captured the keys
  - `columns`, the screen's width
- `Buffers`, every open buffer by id. Each has its `Name`, whether it is `Modified`, its `Kind`, such as `explorer`, or "" for one a user edits, and whether it is `Barred`, one the session may not show.
- **The syntax**, as funcs:
  - `Language`, the language at a position
  - `Comment`, how that language comments a line
  - `Spans`, the highlight captures over a range
  - `Folds`, where the grammar's folds query says the buffer folds
  - `Node`, which finds syntax nodes
  - `Context`, where each function and class that holds a place starts
- `Object` finds the range one of the vim machine's text objects, such as `a(` or `2i"`, selects at a place.
- `Decorations` lists the module's own decorations on every open buffer, each with its `Buffer`, where they were when the call began.
- `Windows` lists the session's windows, each with its id, buffer, the buffer it showed before, cursor, top line and place on the screen. `Selection` and `Cursors` give what visual mode selects and the other cursors.
- `Servers` lists the language servers config.toml sets, each with its state, version, filetypes, roots and files.
- `Option` reads an option, such as `wrap` or `spell.enabled`, as the window has it, a config.toml setting, such as `theme`, or a module's shared option, such as `spell.words`. `Config` reads any value of config.toml by its dotted path, as JSON.
- `Hints` lists the keys that may follow the command being typed, for a module that hears `keys.pending`, with `pending`, `area` and `lines` in `About`.
- `Session` is the windows on files as JSON, in a call and in `editor.quitting`.
- In a call and an event:
  - `Places`, the last list of places
  - `Proposals`, the edits clients proposed that no one has accepted or rejected, each with its number, buffer, range, new text, whether it is stale and who proposed it
- In a call alone:
  - `Keys`, `Commands` and `HelpTags` list every key, ex command and help tag fude knows.
  - `States` and `StateText` read a buffer's states and their text.
  - `Marks`, `Jumps`, `Changes` and `Messages` read those lists, and `Register` one register.
  - `Registers`, `Search`, `Recording` and `History` read every register, the last search, the macro being recorded and the lines entered with `:` and `/`.
  - `Window` says whether a window is next to the current one that way.
  - `Sessions` lists the sessions on this fude, `Grammars` the grammars fude builds, and `HelpAt` help's text at an entry.

A `Result` asks for any of these, and fude applies them together once the call returns:

| Field | Asks for |
|---|---|
| `Edits` | replacements of ranges of the text, on this buffer or on another by its `Buffer` id, each buffer's as one change. fude drops an event's edits to another buffer that changed after the event read it |
| `Decorate`, `Undecorate` | styles, signs and notes on ranges, which move with edits, by the ids the module gives them, on this buffer or on another by its `Buffer` id. On one line the sign at the highest `Layer` shows, with git's at -1, most at 0, a diagnostic's at 1 and a breakpoint at 2. The style `inlay` draws the note among the text at the start. `Insert` draws it there in the decoration's own style, with its `Lines` below, as ghost text. The style `colour #rrggbb` draws the range on that colour. A decoration with `Fold` set closes a fold over its lines, one with `Cover` draws its note over its range, and one with `Icon` draws its sign in an explorer's icon column while `icons` is on |
| `Emit` | events for other modules and plugins |
| `Status` | the module's segment on the statusline |
| `StatusLeft`, `SessionLeft` | its segment on the statusline's left, after the buffer's name, for every session, or by buffer id for the call's session alone |
| `Capture` | the keys typed next, each sent to the same call with `key` in `About`, until a result without it |
| `Message` | a line in the message area |
| `Error` | why the call failed, which shows in place of `Message`. A command that fails stops what ran it, such as `:cdo`, `:g` or a macro. A plugin needs no capability for it |
| `Float` | text at the cursor, markdown, or as it is with its own spans. With `Keys` it is the key hints across the bottom of the windows |
| `Show` | text in a read-only window of its own, of the `Kind` of buffer whose `[keymap.<kind>]` keys go over its own, with styles over it and keys of its own. Showing it again keeps the cursor's line and the focus. `Where` says where its window opens, such as `float` in the middle of the screen, and `Lock` keeps it there. In a float, `Foot` stays at its foot as the text scrolls. With `Screen` it shows in the current window in place of the buffer, without line numbers or guides, and `Cursor` is where the cursor starts. With `Line` it puts that line at the top of its window, with the cursor and the focus there. With `Update` it only replaces a panel that is open. With `Unnumbered` its buffer takes no number of those `:b` and `:ls` count |
| `Servers` | restarts, stops, disables or enables language servers, which a guest may not |
| `Keys` | keys to run once the call is done, as typed |
| `Restore` | a session from `Session`, whose files open and whose windows build again |
| `Confirm` | a `Write`'s yes or no question, with the paths it would change, and the files it would rename, create and delete, which language servers hear of |
| `Moved` | paths a write renamed, whose open buffers follow them |
| `Travel` | a buffer's text moves to one of its states, or restores one as a new state |
| `Copy` | text to put on the clipboard |
| `Completion` | a `Completer`'s items, from a place, the menu's alone with `Only`, or coming in a callback with `Later` |
| `Window` | the window next to the current one that way becomes current |
| `Tabs` | what the buffer line lists, each buffer with the name it shows |
| `Pins` | by window id, the starts of the lines pinned over its top rows, in place of those it had |
| `Guides` | by window id, then by line, the display columns of its indent guides, in place of those it had |
| `Follow`, `Kick` | the session to follow, or the guest whose session ends, by name |
| `Build` | the filetypes whose grammars build again |
| `Jump` | where the cursor goes, in this buffer, or at a line and column of a file or of another buffer by its `Buffer` id, in a window that shows a file, or with `Where` in a new one placed as `Show`'s is |
| `Close`, `Drop`, `Write` | the buffers to close, as `:bd` does, to close dropping their changes, as `:bd!` does, and to write, as `:w` does, by id |
| `Windows` | what to do to windows by id: `close`, `hide`, `float`, `above`, `below`, `left`, `right`, `only`, `focus`, `height` or `width` to a `Size`, or as large as it can be for 0, `taller` or `wider` by `Size`, or move the `cursor` or the `top` line to `Pos` |
| `Scratch` | a new buffer with no file, holding text to edit, in the window, or with `Where` in a new one. Of a `Kind` a `Write` writes, `:w` writes it there. With `Unnumbered` it takes no number |
| `Marks`, `Registers`, `Search` | marks to set, text to put in registers, and the pattern `n` finds |
| `Ex` | command lines to run, without their colon, as typed. `Place` puts the window one opens where it says, as `:window float help` does |
| `Styles` | styles by name, each with colours for its text and ground and how its text draws, over the theme's, which decorations and spans name |
| `Pick` | a list to pick from, live or of places, or a question. With `Update` it fills the open list of its ID, as items read off the loop do. `<C-g>` in a list that is not live tells the callback `all` |
| `Menu` | a numbered menu at the cursor, each item with what it does beside it, whose choice runs the callback |
| `Send` | text for the input of the module's `Fed` stream jobs by id, in order, or with `Stop` the end of a stream job |
| `Timers`, `Jobs`, `Requests` | work whose callback runs later. A `Request` goes to the buffer's servers, or to the one it names. A `Stream` job is the module's own long work, whose callback runs for each line it emits |
| `Log` | lines for fude's log |
| `Selected` | the range an object's call selects |
| `Set` | `expand_tab`, `indent_width` or `tab_width` on the buffer, over config.toml, and any other option as `:set` sets it |
| `Terminals` | programs, each in a terminal window below, or placed by `Where`, which the status line names while it runs. With no `Name` one opens as `:terminal cmd` does, focused and not named, and with no `Command` either it is the session's shell numbered `Shell`. `Buffer` shows a terminal again, and `Close` closes its window once the program ends. With `Unnumbered` it takes no number, and with `Hidden` a named one starts with its window hidden |
| `Places` | the last list of places, which `]q` steps through, each `path:line:column:text`, or `line:column:text` in `File`, or a path alone, in a window with `Show`, with columns in `Encoding` and the place last gone to in `At`, from 1. The editor keeps it and emits `places.set`, with `show` when it asks, and `modules/places` shows it |
| `Each` | a command line to run at each place of a list, or once in each of its files, as `:cdo` and `:cfdo` do |
| `Apply` | a language server's workspace edit, applied with the editor's version checks |
| `Cmdline` | a command line started with that text typed, which runs nothing |
| `Complete` | what a command's argument being typed completes with |

## License

[MIT](../LICENSE)
