# Clients and proposals

A running fude listens on a socket, and other programs attach to it as
clients. A client sees the buffers, sends keys that act as typed, and
edits text, all on the same timeline as the terminal.

## The socket

fude listens at `fude.<pid>.sock` in `$XDG_RUNTIME_DIR`, or in the
temporary folder without it. The `socket` setting names a path instead,
so a client on another machine reaches it through ssh.

```toml
socket = "/home/me/.local/state/fude/fude.sock"
```

```sh
ssh -L /tmp/fude.sock:/home/me/.local/state/fude/fude.sock host
```

A second fude finds the socket taken and listens at one of its own.

## Attaching

`fude --attach` opens a terminal on a session of a running fude, with
its own windows, cursor and undo, editing the same buffers. Without a
socket it finds the one fude running, as `fude --tail` does.

```sh
fude --attach
fude --attach /tmp/fude.sock
```

`:q` on its last window closes the attached terminal, and the other fude
keeps running. `"+y` copies to the clipboard of the machine the attached
terminal runs on.

## Sharing

`fude --share` lets someone on another machine edit with you, without a
shell on yours. It listens with TLS, and its first message is a share
string to send them. They run:

```sh
fude --join 192.168.1.5:41234/<fingerprint>/<secret>
```

fude asks whether to let them in. On `y` they get a guest session, with
their own windows on your buffers. A guest sees only the project's files,
the directory fude started in, and runs no program of its own choosing.
An address after `--share` listens there, such as a VPN's.

```sh
fude --share 100.64.0.2:7000 main.go
```

A share string lets one guest in, and `:share` shows a new one for the
next. Quitting fude ends the share.

## Clients

Go programs attach through fude's `client` package. A client may keep a
copy of a buffer, so its own edits show at once, and fude fits them with
the edits other clients make meanwhile. Each client's `u` undoes only its
own changes. `:restart` drops every client, which must attach again.

## Proposals

A client may send an edit as a proposal instead of making it. A proposal
shows in the text as ghost text and changes nothing until accepted.

- `:accept` applies the buffer's newest proposal, and `:accept 3`
  proposal 3, as one change on the timeline.
- `:reject` drops it and leaves no trace.

A proposal follows edits made after it. One whose text was edited since
is marked stale and cannot be accepted.

## Watching fude

`fude --tail` in another terminal prints what a running fude does, as it
happens. Each line has the time, the source, what happened and how long
it took.

```
13:02:28.700 cmd    :set nu (0.036ms)
13:02:28.701 msg    no option named nu
13:02:29.759 lsp    gopls answered #1, 1019 bytes (38.513ms)
```

It starts with the last 4096 events, so a tail started after something
went wrong shows what led to it. `--json` prints each event as a line of
JSON, for `jq`. With several fudes running, name the socket of one, as
`fude --tail $XDG_RUNTIME_DIR/fude.1234.sock`.

## Next

- `index` lists every page of help.
