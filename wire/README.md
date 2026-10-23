# wire

What fude and its clients send each other over the socket, as JSON.

- A `Frame` is what an attached client draws, sent whenever it changes:
  each `Window` with its cursor, signs, decorations and folds, the
  `Split`s of the layout, the `Tab`s of the buffer line, a `Menu`, the
  command line, the mode and the message. A `Text` brings a window's text
  up to date.
- An `Edit`, a `Pulled` and a `Pushed` are what a replica exchanges with
  the server: the edits since a version, or the whole text when the
  version is too old, and whether its own edits applied.
- A `Hello` is the first line a guest sends to join a share, and a
  `Theme` is what a client draws in.

The terminal client in [`ui/term`](../ui/term) draws frames, and
[`client`](../client) keeps replicas. A client in another language reads
these types as its protocol.

## License

[MIT](../LICENSE)
