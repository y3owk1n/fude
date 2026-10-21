# sdk

Write fude plugins in Go. A plugin is a `.wasm` file that fude runs in a sandbox, with only the capabilities its table in `config.toml` grants. A plugin adds commands, keys, operators and text objects, reacts to events, and asks fude for edits, decorations and windows.

You need Go and [TinyGo](https://tinygo.org).

```sh
fude plugin new upper && cd upper && fude plugin build
```

`fude plugin new` writes a `go.mod` and a `main.go` and gets this module. `fude plugin build` builds the plugin with TinyGo into `upper.wasm`.

## Example

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
}

func main() {}
```

```toml
[[plugin]]
path = "plugins/upper.wasm"
capabilities = ["buffer.read", "buffer.write"]
```

`zu` now works with every motion and text object, with counts, in macros and with `.`.

## What a plugin registers

Call these from an `init` function.

| Func | Adds |
|---|---|
| `Command` | the ex command `:name` |
| `Key`, `Insert` | a key in normal or insert mode |
| `Plug` | a command keys reach only as `<Plug>(plugin.name)` |
| `Source` | a source of `:pick`, which shows a list with `Picker` or `Pick` |
| `Operator` | an operator that composes with every motion and text object |
| `Object` | a text object that composes with every operator |
| `Page` | a page of help's guide, `guide-name`, in markdown under a `#` title |
| `Define` | the option `plugin.name`, which `:set` changes, starting at its key in the plugin's `options` table |
| `On` | a handler for an event, such as `buffer.saved` or `mode.changed` |
| `Register` | anything a module registers, an `api.Registration` of any kind and with any field |

All but `On` and `Page` take a help text, which `:h` and the key hints show. `Register` may also set `Doc`, a longer explanation that `:h` shows under the entry.

## What a plugin may call

A call outside the plugin's capabilities fails with an error, and fude logs it.

| Capability | Funcs |
|---|---|
| none | `Log`, `Now`, `Current`, `Mode`, `Option`, `Config`, `IndentWidth`, `TabWidth`, `SetOption`, `Emit`, `After`, `ID`, `Callback`, `About` |
| each field's | `Do`, which asks for anything a module's `api.Result` asks for, and `Read`, which reads any field of a module's `api.Scope` |
| `buffer.read` | `Len`, `Lines`, `LineOf`, `LineStart`, `Text`, `NodeAt`, `Language`, `Spans`, `Comment`, `Buffers`, `LenOf`, `TextOf` |
| `buffer.write` | `Edit` |
| `ui` | `Decorate`, `Undecorate`, `Fold`, `Replace`, `Shade`, `Rows`, `Block`, `Decorations`, `Sign`, `SignUnder`, `Note`, `Status`, `Message`, `Float`, `Show`, `ShowStyled`, `Pick`, `Picker`, `Ask`, `Confirm`, `Jump`, `Open` |
| `fs.read` | `ReadFile`, inside the folder fude started in |
| `process` | `Run`, `Start`, `Pipe`, `Stream`, `Spawn` and its program's `Send` and `Stop`, for the programs the plugin's table lists |
| `net` | `Get`, `Fetch`, for the hosts the plugin's table lists |
| `lsp` | `Request`, `RequestRange`, to the buffer's language servers |
| `keys` | `Do` with `Keys`, which runs keys as typed |

Edits, decorations and windows a call asks for apply together once it returns. `After`, `Start`, `Pipe`, `Stream`, `Spawn`, `Fetch`, `Request`, `Pick`, `Ask`, `Confirm` and the ids `Callback` gives run their func later, off the input path, as an event runs.

A call on the input path, such as a key or an operator, has a time budget. fude stops a plugin that runs past it and tells the user.

## More

- [`plugin/testdata`](../plugin/testdata) holds small plugins, each written with this module.
- [`plugintest`](../plugin/plugintest) runs a plugin's `.wasm` in `go test`, as fude runs it.
- `:h guide-plugins` in fude covers settings, events and the budget.

## License

[MIT](../LICENSE)
