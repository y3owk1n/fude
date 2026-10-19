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

`fude attach` opens a terminal on a session of a running fude, with
its own windows, cursor and undo, editing the same buffers. Without
`--socket` it reaches the fude `$FUDE_SOCKET` names, or else the one
fude running.

```sh
fude attach
fude attach --socket /tmp/fude.sock
```

`:q` on its last window closes the attached terminal, and the other fude
keeps running. `"+y` copies to the clipboard of the machine the attached
terminal runs on.

## Sharing

`fude share` lets someone on another machine edit with you, without a
shell on yours. It listens with TLS, shows a share string to send them
as its first message, and copies it. They run:

```sh
fude join 192.168.1.5:41234/<fingerprint>/<secret>
```

fude asks whether to let them in. On `y` they get a guest session, with
their own windows on your buffers. A guest sees only the project's files,
the directory fude started in, and runs no program of its own choosing.
An address after `fude share` listens there, such as a VPN's.

```sh
fude share 100.64.0.2:7000 main.go
```

When neither machine can reach the other, such as across the internet,
run a relay on a machine both reach, and share through it. The relay
passes the encrypted bytes and cannot read them.

```sh
fude relay               # on the server, port 7418
fude share --relay my.server.com main.go
```

A share string lets one guest in, and `:share` shows a new one for the
next. `:who` lists the guests, and `:kick name` ends one's session.
Others see you by `$USER`, or by the name `--name` gives, as in
`fude join --name kyle <string>`. A window shows the
cursor of each other session on its file as a cell in that session's
colour, with its selection underlined, and its status line names them. A
guest's keymaps, leader, window options and theme come from
`config.toml` on its own machine. `fude join --read-only` asks to only
watch, so the guest's session changes no text. `:follow name` keeps your
windows on what the session called name shows, its splits too, until you
type. While fude shares, its status line says so and counts the guests.
A guest's status line says it is a guest. Quitting fude ends the share.

## From the shell

`fude open`, `fude keys` and `fude state` open files in a running fude,
type keys into it and print its state. A relative path is from the
shell's directory.

```sh
fude open main.go util.go   # opens them, as :e does
fude keys ':w<CR>'          # types the keys, as if typed in fude
fude state | jq .mode       # the buffer, cursor and mode as JSON
```

They act on the session of fude's own terminal, or on the one
`--session name` names. In a `:terminal`, fude sets `FUDE_SOCKET` and
`FUDE_SESSION`, so they act on the session the terminal belongs to.

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

`fude tail` in another terminal prints what a running fude does, as it
happens. Each line has the time, the source, what happened and how long
it took.

```
13:02:28.700 cmd    :set nu (0.036ms)
13:02:28.701 msg    no option named nu
13:02:29.759 lsp    gopls answered textDocument/hover #1, 1019 bytes (38.513ms)
```

A background job and the callback it runs show the same id, such as
`#3`. A line an attached client made starts with its name, such as
`client1:`.

It starts with the last 4096 events, so a tail started after something
went wrong shows what led to it. `--json` prints each event as a line of
JSON, for `jq`. With several fudes running, name the socket of one, as
`fude tail --socket $XDG_RUNTIME_DIR/fude.1234.sock`.

## Next

- `index` lists every page of help.
