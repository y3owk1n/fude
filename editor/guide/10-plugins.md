# Plugins

A plugin is a WebAssembly module. It can add commands, operators, text
objects and keys that work like fude's own, in macros and with `.` too.
It can only call what its table in `config.toml` allows.

Plugins others have written carry the `fude-plugin` topic on GitHub, at
https://github.com/topics/fude-plugin. Each one's README gives its table.

## Loading a plugin

```toml
[[plugin]]
path = "plugins/wordcount.wasm"
capabilities = ["buffer.read", "ui", "process"]
programs = ["git"]
```

`path` is the module's file, from the folder of `config.toml`, or the
folder of a plugin's source, which fude builds when the source changes.
In place of `path`, `repo` installs the plugin from a repo:

```toml
[[plugin]]
repo = "github.com/y3owk1n/fude-org"
capabilities = ["buffer.read", "ui"]
```

fude takes the newest release this fude loads, or the tag, branch or
commit `version` names. It installs in the background and says when it
is done. `plugins.lock`, beside `config.toml`, pins what it installed, so
keep it with your config and another machine installs the same.
`disabled = true` keeps it from loading, with the table kept.
`capabilities` lets it call what each one grants.

- `buffer.read` reads the text.
- `buffer.write` asks for edits.
- `ui` shows styles, a status line segment and floats.
- `fs.read` reads and lists files in the folder fude started in and the
  folders `paths` lists, such as `["~/notes"]`.
- `fs.write` writes, moves and deletes files and makes folders there.
- `grammar` builds the grammar the plugin names for a filetype, as fude
  builds its own. The grammar runs in fude itself, outside the sandbox.
- `process` runs the programs `programs` lists.
- `net` gets http and https URLs on the hosts `hosts` lists.
- `lsp` asks the buffer's language servers, sends them notifications and
  answers their requests.
- `keys` runs keys as if typed, which may run any command.

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

`fude plugin new` writes a folder with a `go.mod` that requires the sdk, a
`main.go` with one command, a `fude.toml` and a release workflow.
`fude.toml` is the manifest: the plugin's name and the capabilities,
programs and hosts it asks for. `fude plugin build` builds `wordcount.wasm`
in the folder with the manifest inside, which a `[[plugin]]` table then
loads. The plugin may use only the capabilities both its manifest and its
table list, so add one to the manifest when the plugin starts to need it.
The build also writes the sdk's version into the manifest. fude refuses a
plugin built with an sdk newer than itself, or older than the oldest sdk
it takes, and says which fude the plugin needs. The release workflow builds the plugin on a tag such as
`v1.0.0` and attaches its `.wasm` to the release. `:plugin reload` loads
every plugin anew. With `path` set to the folder, it first builds the
plugin again if its source changed. Give
its repo the `fude-plugin` topic on GitHub so others find it.

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
adds an ex command, `sdk.Key` a `<Plug>(plugin.name)` with its keys in
normal mode, which config.toml can map elsewhere, `sdk.Insert` keys in
insert mode, `sdk.Operator` an
operator and `sdk.Object` a text object, each with a line of help that
`:h` and the key hints show. `sdk.Page` adds a page to this guide. A command's `sdk.Call` has what was typed
after its name in `Args`, `Bang` for a `!` after the name, and its range
in `First` and `Last`, where its lines start. `sdk.On` runs a function
for an event:
`buffer.opened`, `buffer.changed`, `buffer.saved`, `buffer.closed`,
`mode.changed`, `window.moved`, `syntax.updated` or `lsp.diagnostics`.
For `window.moved`, the event's `Cursor` and `Top` say where the cursor is
and where the window's text starts, and for `buffer.changed`, its `Start` and
`End` say what the change covered.

`sdk.Current` gives the id, path and filetype of the buffer a call or
event is about, and `sdk.Mode` the mode. `sdk.Define("depth", "3", "how
deep it looks")` adds the option `wordcount.depth` to a plugin named
wordcount. `options = { depth = 5 }` in its table in `config.toml`
starts it at 5, `:set wordcount.depth 4` changes it, and
`sdk.Option("wordcount.depth")` reads it. A list there, such as
`skip = ["markdown", "text"]`, reaches the plugin as `markdown,text`. `sdk.After` runs a function
once, a number of milliseconds later, as an event runs. `sdk.Start` and `sdk.Fetch` run
a program or get a URL in the background, with no budget, then run a
function with the output. `sdk.Stream` runs a program and runs a
function with each line it prints, as it prints it. `sdk.Spawn` does
the same with the program's input kept open, which its `Send` writes to
and its `Stop` ends. `sdk.Request` asks the buffer's language
servers, at a position, and runs a function with the answer.

The rest of the sdk reaches fude, each function behind its capability.
`sdk.Text` and `sdk.Len` read the text, and `sdk.Lines`, `sdk.LineOf`
and `sdk.LineStart` where its lines start. `sdk.NodeAt` reads the syntax
node at a position and its parents. `sdk.Buffers`, `sdk.LenOf` and
`sdk.TextOf` read the other open buffers. `sdk.Edit` changes the
text.
`sdk.Decorate`, `sdk.Sign`, `sdk.Note`, `sdk.Status`, `sdk.Float` and
`sdk.Show` show things, and `sdk.Pick`, `sdk.Ask` and `sdk.Confirm` ask the
user to pick from a list, to type an answer or to answer yes or no. `sdk.Jump` moves the cursor, and
`sdk.Open` opens a file of the project. `sdk.ReadFile`, `sdk.List`, `sdk.WriteFile`,
`sdk.Run` and `sdk.Get` reach files, programs and the web, and `sdk.Log` writes to fude's log. A call
outside the plugin's capabilities returns `sdk.ErrDenied`.

`sdk.Register` and `sdk.Do` give a plugin all a built-in module has. `sdk.Register` takes an
`api.Registration`, as a built-in module registers, of any kind and with
any field. `sdk.Do` asks for what an `api.Result` holds, as a built-in
module's call returns it, and `sdk.Read` reads any field of an
`api.Scope`, as a built-in module's call reads it, each field behind its
capability. `sdk.Callback` gives the id of a callback a result asks for,
such as a menu's, and `sdk.About` reads the callback's context, such as a job's output.

## Testing a plugin

`plugin/plugintest` runs a built plugin in `go test`, as fude would,
without fude. It works from a plugin's own module once fude has tagged
releases.

```go
func TestWords(t *testing.T) {
	p := plugintest.Load(t, "wordcount.wasm", plugin.Grants{Capabilities: []string{"buffer.read", "ui"}}, "")
	res := p.Command("Words", "", "two words", 0)
	if *res.Status != "2 words" {
		t.Errorf("the statusline says %q", *res.Status)
	}
}
```

`Command`, `Key` and `Event` run what the plugin added against a text and
return what it asked for: its edits, decorations, status and the rest.
`plugintest.Apply` makes its edits, and `Log` returns what it logged.

## Next

- `guide-remote` edits from other programs.
