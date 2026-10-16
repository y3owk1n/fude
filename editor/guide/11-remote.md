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

## Next

- `index` lists every page of help.
