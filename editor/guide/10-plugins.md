# Plugins

A plugin is a WebAssembly module. It can add commands, operators, text
objects and keys that work like fude's own, in macros and with `.` too.
It runs in a sandbox and can call only what its table in `config.toml`
grants.

Plugins others have written carry the `fude-plugin` topic on GitHub, at
https://github.com/topics/fude-plugin. Each one's README gives its table.

## Adding a plugin

The quickest way is from a shell:

```sh
fude plugin add github.com/y3owk1n/fude-org
```

It installs the plugin, prints the capabilities, programs and hosts it
asks for, and once you answer y, adds its table to `config.toml`.
`repo@v1.2.0` adds a version too.

Each plugin has a `[[plugin]]` table in `config.toml`, which names where
the plugin comes from and what it may do. A plugin comes from one of
three places.

A repo, which fude installs from:

```toml
[[plugin]]
repo = "github.com/y3owk1n/fude-org"
capabilities = ["buffer.read", "ui"]
```

A folder of a plugin's source, which fude builds with TinyGo:

```toml
[[plugin]]
path = "/home/me/code/wordcount"
capabilities = ["buffer.read", "ui"]
```

A `.wasm` file:

```toml
[[plugin]]
path = "plugins/wordcount.wasm"
capabilities = ["buffer.read", "ui", "process"]
programs = ["git"]
```

Plugins load after the first frame, so they never slow fude's start. A
plugin of a repo that is not installed installs in the background. fude
says "installing" when it starts and "installed" with its version when
it ends.

## A plugin's table

- `repo` is the repo to install from, as `github.com/owner/name` or any
  URL git fetches.
- `version` is the tag, branch or full commit of `repo` to install.
  Unset, fude takes the newest release this fude can load.
- `path` is a `.wasm` file or a folder of source, from the folder of
  `config.toml`. A table has one of `repo` and `path`.
- `capabilities` lists what the plugin may call, as the next section
  says.
- `programs` lists the programs a plugin with `process` may run.
- `hosts` lists the hosts a plugin with `net` may reach.
- `paths` lists folders outside the project that `fs.read` and
  `fs.write` reach, such as `["~/notes"]`.
- `options` starts the plugin's options, such as `options = { depth = 5 }`.
- `disabled = true` keeps the plugin from loading, with its table kept.

## Capabilities

- `buffer.read` reads the text.
- `buffer.write` asks for edits.
- `ui` shows styles, a status line segment, floats and lists to pick from.
- `fs.read` reads and lists files in the folder fude started in and the
  folders `paths` lists.
- `fs.write` writes, moves and deletes files and makes folders there.
- `grammar` builds the grammar the plugin names for a filetype, as fude
  builds its own. The grammar runs in fude itself, outside the sandbox.
- `process` runs the programs `programs` lists.
- `net` gets http and https URLs on the hosts `hosts` lists.
- `lsp` asks the buffer's language servers, sends them notifications and
  answers their requests.
- `keys` runs keys as if typed, which may run any command.

A plugin's manifest lists the capabilities it asks for, and its README
says which. The plugin gets only those that both its manifest and its
table list, so leave one out of the table to deny it. A call outside
them gets an error and changes nothing, and fude writes it to its log in
`$XDG_STATE_HOME/fude/log`.

## Managing plugins

- `:plugin` lists each plugin, where it comes from and whether it loaded,
  is installing or building, is not installed, is disabled, or failed and
  why. It also names a capability a plugin asks for that its table does
  not grant.
- `:plugin install` installs each plugin of a repo that is not installed
  or failed. `:plugin install {name}` installs that one again.
- `:plugin update` moves each plugin of a repo to its newest release, or
  its branch's newest commit. `:plugin update {name}` updates one.
- `:plugin disable {name}` unloads a plugin until fude quits.
  `:plugin enable {name}` loads it again, or loads one whose table has
  `disabled = true`.
- `:plugin reload` reads the tables again and loads every plugin anew.

With `auto_install = false`, a plugin of a repo installs only with
`:plugin install`.

These also run from a shell, for a new machine or a script:

- `fude plugin add {repo}[@version]` installs a plugin and adds its table.
- `fude plugin install` installs each plugin of a repo that is not
  installed, at the commits `plugins.lock` pins.
- `fude plugin update [name]` updates each plugin of a repo, or the one
  named, as `:plugin update` does.

## Versions and plugins.lock

`plugins.lock`, beside `config.toml`, records the commit of each plugin
fude installed from a repo, and the sha256 of a `.wasm` from a release.

- With the lock and the plugin installed, fude loads it with no network.
- Keep the lock with your config, and another machine installs the same
  commits. fude refuses a release's `.wasm` whose sha256 is not the one
  locked.
- A plugin records the version of fude's sdk it was built with. fude
  loads it when that sdk is no newer than fude and no older than the
  oldest sdk fude takes, and otherwise says which fude it needs.
- Installing and updating take the newest release that this fude loads,
  so a plugin stays on a release that works until you update fude.
- `:plugin update` leaves a plugin whose `version` is a release or a
  commit as it is.
- `:plugin update` keeps a plugin at the version it has when a newer
  release asks for a capability its table does not grant, and says which.

## Limits

- Each call has a time limit. A key gets one frame, an event 100ms and
  loading 200ms.
- fude unloads a plugin that runs over its limit or fails, and says
  which. Editing goes on with no change from that call.
- A plugin has 64MB of memory.

## Writing a plugin

You write a plugin in Go with fude's sdk and build it with TinyGo, which
must be on PATH.

```sh
fude plugin new wordcount
cd wordcount
fude plugin build
```

`fude plugin new` writes a folder with:

- `go.mod`, which requires the sdk of this fude.
- `main.go`, with one command.
- `fude.toml`, the manifest, with the plugin's name and the
  capabilities, programs and hosts it asks for.
- `.github/workflows/release.yml`, which builds the plugin on a tag such
  as `v1.0.0` and attaches its `.wasm` to the release.

`fude plugin build` builds `wordcount.wasm` in the folder, with the
manifest and the sdk's version inside. While you work on the plugin, set
its table's `path` to the folder. `:plugin reload` then builds it again
when its source changed, and loads it anew.

The plugin may use only the capabilities its manifest lists, so add one
to `fude.toml` when the plugin starts to need it. The build refuses a
manifest with no name or with a capability fude does not have.

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

## What a plugin adds

A plugin registers what it adds from its `init` functions. Each takes a
line of help that `:h` and the key hints show.

- `sdk.Command` adds an ex command. Its `sdk.Call` has what was typed
  after the name in `Args`, `Bang` for a `!`, and its range in `First`
  and `Last`.
- `sdk.Key` adds a `<Plug>(plugin.name)` mapped to keys in normal mode,
  which `config.toml` can map elsewhere. `sdk.Insert` adds keys in insert
  mode.
- `sdk.Plug` adds a command that keys reach only as
  `<Plug>(plugin.name)`.
- `sdk.Operator` adds an operator that works with every motion and text
  object. `sdk.Object` adds a text object that works with every operator.
- `sdk.Source` adds a source of `:pick`.
- `sdk.Define("depth", "3", "how deep it looks")` adds the option
  `wordcount.depth` to a plugin named wordcount. `options = { depth = 5 }`
  in its table starts it at 5, `:set wordcount.depth 4` changes it, and
  `sdk.Option("wordcount.depth")` reads it. A list there, such as
  `skip = ["markdown", "text"]`, reaches the plugin as `markdown,text`.
- `sdk.Page` adds a page to this guide.
- `sdk.On` runs a function for an event.
- `sdk.Register` adds anything a built-in module registers, such as a
  completion source.

## Events

`sdk.On` takes the name of an event:

- `buffer.opened`, `buffer.changed`, `buffer.saving`, `buffer.saved`,
  `buffer.renamed` and `buffer.closed`. For `buffer.changed`, the
  event's `Start` and `End` say what the change covered.
- `mode.changed`, and `window.moved`, whose `Cursor` and `Top` say where
  the cursor is and where the window's text starts.
- `window.opened`, `window.closed` and `window.focused`.
- `syntax.updated`, `lsp.attached` and `lsp.diagnostics`.
- `search.changed`, `selection.changed`, `macro.recorded`,
  `theme.changed` and `cwd.changed`.
- A plugin's own events, named `plugin.event`, which `sdk.Emit` sends.

## What a plugin calls

Most functions need a capability, and return `sdk.ErrDenied` without it.

- With none: `sdk.Log`, `sdk.Now`, `sdk.Current` for the buffer's id,
  path and filetype, `sdk.Mode`, `sdk.Option`, `sdk.SetOption` and
  `sdk.Emit`.
- `buffer.read`: `sdk.Text` and `sdk.Len` read the text. `sdk.Lines`,
  `sdk.LineOf` and `sdk.LineStart` read where its lines start.
  `sdk.NodeAt` reads the syntax node at a position and its parents.
  `sdk.Buffers`, `sdk.LenOf` and `sdk.TextOf` read the other open
  buffers.
- `buffer.write`: `sdk.Edit`.
- `ui`: `sdk.Decorate`, `sdk.Sign`, `sdk.Note`, `sdk.Status`,
  `sdk.Message`, `sdk.Float` and `sdk.Show` show things. `sdk.Pick`,
  `sdk.Ask` and `sdk.Confirm` ask the user to pick from a list, type an
  answer or answer yes or no. `sdk.Jump` moves the cursor, and `sdk.Open`
  opens a file of the project.
- `fs.read`: `sdk.ReadFile`, `sdk.List` and `sdk.Stat`.
- `fs.write`: `sdk.WriteFile`, `sdk.MakeDir`, `sdk.Rename` and
  `sdk.Remove`.
- `process`: `sdk.Run`, and the background work below.
- `net`: `sdk.Get` and `sdk.Fetch`.
- `lsp`: `sdk.Request` and `sdk.RequestRange`.

Edits, decorations and windows a call asks for apply together once it
returns.

`sdk.Do` asks for anything a built-in module's call returns, an
`api.Result`. `sdk.Read` reads any field of the `api.Scope` a built-in
module's call reads. Each field needs its capability. `sdk.Callback`
gives the id of a callback a result asks for, such as a menu's, and
`sdk.About` reads the callback's context, such as a job's output.

## Background work

These run with no time limit, then run a function with what they got,
as an event runs.

- `sdk.After` runs a function once, a number of milliseconds later.
- `sdk.Start` runs a program, and `sdk.Fetch` gets a URL.
- `sdk.Stream` runs a program and runs a function with each line it
  prints, as it prints it. `sdk.Spawn` does the same with the program's
  input kept open, which its `Send` writes to and its `Stop` ends.
- `sdk.Request` asks the buffer's language servers about a position.

## Testing a plugin

`plugin/plugintest` runs a built plugin in `go test`, as fude would,
without fude.

```go
func TestWords(t *testing.T) {
	p := plugintest.Load(t, "wordcount.wasm", plugin.Grants{Capabilities: []string{"buffer.read", "ui"}}, "")
	res := p.Command("Words", "", "two words", 0)
	if *res.Status != "2 words" {
		t.Errorf("the statusline says %q", *res.Status)
	}
}
```

- `Command`, `Key` and `Event` run what the plugin added against a text,
  and return what it asked for: its edits, decorations, status and the
  rest.
- `plugintest.Apply` makes the edits a result asks for.
- `Log` returns what the plugin logged.
- A test grants the plugin only what its manifest lists too, as fude
  does.

## Releasing a plugin

- Push a tag such as `v1.0.0`. The release workflow `fude plugin new`
  wrote builds the plugin and attaches `<repo>.wasm` to the tag's
  release, where fude installs it from.
- A repo with no release installs too, by building, which needs Go and
  TinyGo on the user's machine.
- fude does not install a release built against an sdk that is no
  release, as from a `replace` in `go.mod`. Require a released sdk
  before you tag.
- Give the repo the `fude-plugin` topic so others find it, and put its
  `[[plugin]]` table in its README.

## Next

- `guide-remote` edits from other programs.
