# Demo tapes

Each tape records a clip in the README with
[VHS](https://github.com/charmbracelet/vhs). `fude.tape` is the one at the
top, and `together.tape` is two people in one fude, under Editing together.
A recording writes the GIF to `demos/` and an MP4 to `demos/out/`, which
git ignores.

## Record

From the repo root, with VHS, ttyd and ffmpeg on PATH, and tmux for
`together.tape`:

```sh
just demo             # fude.tape
just demo together    # together.tape
```

Record one tape at a time. VHS keeps only the frames it captures, so a busy
machine gives a clip that plays too fast.

## What a tape runs

`setup.sh` runs hidden at the start. It builds fude from the working tree,
copies the repo with the working tree's changes into a throwaway HOME under
`/tmp/fude-demo`, and gives it `config.toml`, so a recording never reads or
writes your own config or files. gopls, tree-sitter and Go's caches come
from the repo's oku environment. It copies your built grammars, and the
tape builds Go's grammar before the clip starts, out of view. The sandbox
has a clipboard of its own, so a recording never reads or writes yours.
`together.tape` runs its two panes on a tmux server of its own, set by
`tmux.conf`, and shares on 127.0.0.1 so the clip shows no address of your
network.
`style.tape` sets the shell, font, colours, window size and frame rate.
