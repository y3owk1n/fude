# plugin

This module holds the host that runs fude plugins, and `plugintest`, which runs a plugin in `go test`. A plugin is a `.wasm` file written with [`sdk`](../sdk). The host runs it on [wazero](https://wazero.io), with only the capabilities you grant it and a time budget on every call. It requires wazero and fude's [`api`](../api) module, whose `Scope` and `Result` each call reads and returns.

| Package | What it does |
|---|---|
| [`plugin`](.) | Loads a plugin, runs its init, commands, keys and events, and returns what each call asked for |
| [`plugintest`](plugintest) | Runs a plugin's `.wasm` in `go test` as fude runs it, without fude |

```sh
go get github.com/y3owk1n/fude/plugin
```

fude has no release yet. This module requires `api` at a version only the first release tags, so until then it builds only inside a clone of fude.

## Testing a plugin

Build the plugin with `fude plugin build`, then test the `.wasm` from its own module. `Load` grants the capabilities the plugin's `[[plugin]]` table would, and runs its init.

```go
func TestCount(t *testing.T) {
	p := plugintest.Load(t, "count.wasm", plugin.Grants{Capabilities: []string{"buffer.read", "ui"}}, "")
	res := p.Event("buffer.changed", "one two three")
	if res.Status == nil || *res.Status != "3 words" {
		t.Errorf("status %v, want 3 words", res.Status)
	}
}
```

The text is the only open buffer. `Command`, `Key` and `Event` run what the plugin registered against it, and return the call's `api.Result`. `plugintest.Apply` makes the edits a result asks for. A timer, job, request or pick stays in the result, and the test runs its callback with `Callback`. `Log` returns what the plugin logged, and the calls it was denied.

## Hosting plugins

A `*plugin.Plugin` is an `api.Module`, so a host runs plugins and its own Go modules through one interface.

```go
p, err := plugin.Load(ctx, "upper.wasm", plugin.Grants{Capabilities: []string{"buffer.read", "buffer.write"}}, cacheDir, logLine)
if err != nil {
	return err
}
defer p.Close(ctx)
if _, err := p.Init(ctx, scope); err != nil {
	return err
}
for _, r := range p.Registered() {
	// r.Kind, r.Name, r.Help: bind each to your own command or key
}
```

- **A call reads an `api.Scope`.** It holds the text of the buffer the call is about, what the call is about by name (buffer, path, filetype, mode, a command's args), and every open buffer. A `Text` is anything with `Len`, `Slice`, `Lines`, `LineStart` and `LineOf`, and a `kernel.Snapshot` from [`core`](../core) is one as it is.
- **A call returns an `api.Result`.** It lists what the plugin asked for, and the host decides how to apply each: edits, decorations, a statusline segment, a float, a panel, a jump, a pick, timers, background jobs and language server requests.
- **Calls take turns.** `Call` runs a registration by its `ID`, with the arguments its kind takes, under `InputBudget`. It returns `api.ErrBusy` instead of waiting when another call is running. `Event` and `Callback` wait their turn, under `EventBudget`.
- **Budgets.** A call that traps or runs past its budget returns an error, and nothing it asked for applies.
- **Capabilities.** `Grants` lists the capabilities, the programs a plugin may run and the hosts it may fetch from. A call outside them returns an error to the plugin, and the log func gets a line about it.
- **Background work.** `Work` runs a job a result asked for, a program or an HTTP get, with no budget. Then the host runs the plugin's callback with the output.

fude's [architecture spec](../prd/architecture.md#plugin-abi) describes the ABI each plugin exports and every host function it may import.

## Building plugins

`New` writes a new plugin folder, with a `go.mod` that requires the sdk and a `main.go` that adds one command. `Build` builds a folder with TinyGo into a `.wasm` named after it. They are what `fude plugin new` and `fude plugin build` run. `New` requires the sdk of the running fude's release, or the newest sdk in any other build or program.

## License

[MIT](../LICENSE)
