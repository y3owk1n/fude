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

A module exports `memory`, `alloc(n)`, `call(id, a, b, c)` and `init()`.
In `init` it calls `register` to add a command, operator, object or key,
with a line of help and the id `call` runs it by, and `subscribe` to hear
events through `on_event`. The
events are `buffer.opened`, `buffer.changed`, `buffer.saved`,
`buffer.closed`, `mode.changed`, `view.moved`, `syntax.updated` and
`lsp.diagnostics`.

It calls fude through the functions of the module `fude`, such as `text`,
`edit`, `decorate`, `status`, `float`, `read_file`, `run` and `http_get`,
each behind its capability. Text goes in and out through the plugin's
memory as a pointer and a length.

## Next

- `guide-remote` edits from other programs.
