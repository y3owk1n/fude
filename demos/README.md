# Demo tapes

Each tape records a clip in the README with
[VHS](https://github.com/charmbracelet/vhs). `fude.tape` is the one at the
top, `recover.tape` is a crash and `:recover`, under Nothing is lost, and
`together.tape` is two people in one fude, under Edit together. A recording
writes the GIF to `demos/` and an MP4 to `demos/out/`, which git ignores.

## Record

From the repo root, with VHS, ttyd and ffmpeg on PATH, and tmux for
`together.tape`:

```sh
just demo             # fude.tape
just demo together    # together.tape
just demo recover     # recover.tape
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

## The window

VHS records only a terminal, so the window's screenshots and clip come
from the steps below, run by hand. Each one runs `fude --gui` with
`config.toml` and its own config, state and data directories, as
`setup.sh` sets them. The window is at its default size of 100 columns by
32 rows. On macOS that means no saved frame and no window manager tiling
it.

- `window-macos.png`, `window-linux.png` and `window-windows.png` open
  `protocol/framing/framing.go` and `framing_test.go`, then
  `fude keys ':split<CR>:b 2<CR>'` and `fude keys '<Space><Space>framing'`.
  On macOS `screencapture -o -x -l <window id>` takes the shot. On Linux
  fude runs in Docker under Xvfb with `GDK_SCALE=2` and
  `GSK_RENDERER=cairo`, and `import -window root` takes it. On Windows a
  scheduled task runs the steps in the signed-in session, and PowerShell
  copies the window's rectangle with System.Drawing.
- `window.gif` opens `framing_test.go` at line 10, and
  `screencapture -v -R` records the window's rectangle. The mouse rests on
  `NewReader` for its hover, Command-clicks `Write`, clicks the first tab,
  drags over `framing.Read(r: in)` and right-clicks it. ffmpeg makes the
  GIF at 15 frames a second, 1100 pixels wide.
