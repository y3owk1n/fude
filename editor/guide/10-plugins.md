# Plugins

A plugin is a WebAssembly module. It can add commands, operators, text
objects and keys that work like fude's own, in macros and with `.` too.
It can only call what its table in `config.toml` allows.

## Loading a plugin

```toml
[[plugin]]
path = "plugins/wordcount.wasm"
capabilities = ["buffer.read", "ui", "process"]
programs = ["git"]
```

`path` is the module's file, from the folder of `config.toml`.
`capabilities` lets it call what each one grants.

- `buffer.read` reads the text.
- `buffer.write` asks for edits.
- `ui` shows styles, a status line segment and floats.
- `fs.read` reads files in the folder fude started in.
- `process` runs the programs `programs` lists.
- `net` gets http and https URLs on the hosts `hosts` lists.

A call outside them gets an error and changes nothing, and fude writes it
to its log in `$XDG_STATE_HOME/fude/log`.

## Limits

Plugins load after the first frame, so they never delay it. Each call has
a time limit. A key gets one frame, an event 100ms and loading 200ms. A
plugin that runs over its limit, or fails, is unloaded, fude says which,
and editing goes on with no change from that call.

## Writing a plugin

Plugins are written in Go with fude's sdk and built with TinyGo, which
must be on PATH.

```sh
fude plugin new wordcount
cd wordcount
fude plugin build
```

`fude plugin new` writes a folder with a `go.mod` that requires the sdk and
a `main.go` with one command. `fude plugin build` builds `wordcount.wasm`
in the folder, which a `[[plugin]]` table then loads.

```go
package main

import (
	"strings"

	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Operator("zu", "upper-cases the text a motion covers", func(r sdk.Range) {
		text, err := sdk.Text(r.Start, r.End)
		if err != nil {
			sdk.Log(err.Error())
			return
		}
		sdk.Edit(r.Start, r.End, strings.ToUpper(text))
	})
	sdk.On("buffer.saved", func(sdk.Event) {
		sdk.Status("saved")
	})
}

func main() {}
```

A plugin registers what it adds from its `init` functions. `sdk.Command`
adds an ex command, `sdk.Key` keys in normal mode, `sdk.Insert` keys in
insert mode, `sdk.Operator` an
operator and `sdk.Object` a text object, each with a line of help that
`:h` and the key hints show. A command's `sdk.Call` has what was typed
after its name in `Args`, `Bang` for a `!` after the name, and its range
in `First` and `Last`, where its lines start. `sdk.On` runs a function
for an event:
`buffer.opened`, `buffer.changed`, `buffer.saved`, `buffer.closed`,
`mode.changed`, `view.moved`, `syntax.updated` or `lsp.diagnostics`.
For `view.moved`, the event's `Cursor` and `Top` say where the cursor is
and where the view starts, and for `buffer.changed`, its `Start` and
`End` say what the change covered.

`sdk.Current` gives the id, path and filetype of the buffer a call or
event is about, and `sdk.Mode` the mode. `sdk.Setting` reads the
plugin's own settings, which its table in `config.toml` gives as
`settings = { depth = 3 }`. `sdk.After` runs a function once, a number
of milliseconds later, as an event runs. `sdk.Start` and `sdk.Fetch` run
a program or get a URL in the background, with no budget, then run a
function with the output.

The rest of the sdk reaches fude, each function behind its capability.
`sdk.Text` and `sdk.Len` read the text, and `sdk.Lines`, `sdk.LineOf`
and `sdk.LineStart` where its lines start. `sdk.Edit` changes it,
`sdk.Decorate`, `sdk.Sign`, `sdk.Note`, `sdk.Status`, `sdk.Float` and
`sdk.Show` show things,
`sdk.ReadFile`, `sdk.Run` and `sdk.Get` reach files, programs and the
web, and `sdk.Log` writes to fude's log. A call outside the plugin's
capabilities returns `sdk.ErrDenied`.

## Next

- `guide-remote` edits from other programs.
