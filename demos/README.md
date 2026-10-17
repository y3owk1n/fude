# Demo tapes

`fude.tape` records the clip at the top of the README with
[VHS](https://github.com/charmbracelet/vhs). A recording writes the GIF to
`demos/fude.gif` and an MP4 to `demos/out/`, which git ignores.

## Record

From the repo root, with VHS, ttyd and ffmpeg on PATH:

```sh
just demo
```

Record one tape at a time. VHS keeps only the frames it captures, so a busy
machine gives a clip that plays too fast.

## What a tape runs

`setup.sh` runs hidden at the start. It builds fude from the working tree,
copies the repo with the working tree's changes into a throwaway HOME under
`/tmp/fude-demo`, and gives it `config.toml`, so a recording never reads or
writes your own config or files. gopls, tree-sitter and Go's caches come
from the repo's oku environment. It copies your built grammars, and the
tape builds Go's grammar before the clip starts, out of view.
`style.tape` sets the shell, font, colours, window size and frame rate.
