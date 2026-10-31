# Getting started

fude is a modal text editor. A key does different things in each mode, so
most editing is short commands typed in normal mode, not keys held down
together. This page covers enough to open, edit and save files.

## Opening files

- `fude a.go b.go` opens both files, each in a buffer, and shows the
  first.
- `fude a.go:42` starts on line 42, and `fude a.go:42:7` on its seventh
  byte, the form compilers and `grep -n` print.
- `fude +cmd a.go` runs `:cmd` once the file is open, so `+42` also goes
  to line 42 and `+` to the last.
- `fude src/` lists a folder. In an explorer, `<CR>` opens the entry
  under the cursor and `-` goes up a folder.

`fude` alone shows a start screen with fude's version. On it:

- `f` finds a file.
- `r` lists the files you opened last, and a digit opens one of them.
- `e` starts an empty buffer.
- `s` brings back the directory's windows and cursors, which fude keeps
  when you quit.

## Modes

- Normal mode is where fude starts. Keys move and change text.
- In insert mode, keys type text. `i`, `a` and `o` enter it, before the
  cursor, after it and on a new line. `<Esc>` goes back to normal mode.
- In visual mode, motions select text for the next command. `v` selects
  characters, `V` lines and `<C-v>` a block.
- The command line runs a command typed after `:`, such as `:w`.

The status line names the mode.

## Moving

A count repeats a motion, so `3j` goes down three lines.

- `h`, `j`, `k` and `l` move left, down, up and right.
- `w` and `b` go a word forward and back.
- `0` and `$` go to the start and end of the line.
- `gg` and `G` go to the first and last line.
- `/text<CR>` searches forward, and `n` and `N` go to the next and
  previous match.

## Folding

fude folds a block by its indent or, where its grammar says, by a
markdown heading's section or a Go function.

- `zc` folds the block the cursor is in, and `zo` opens it again, one
  level at a time.
- `zC` and `zO` do the same for every level.
- `za` toggles a fold.
- `zM` folds every block and `zR` opens them all.
- `zm` and `zr` fold one level more or less.
- `j` and `k` pass over a closed fold as one line.
- In a markdown file or help, `<Tab>` on a heading closes its section.
  Pressed again, it opens the section with the sections under it closed,
  then opens them all.
- `<S-Tab>` steps the whole file the same way.

## Jumping in view

`s` jumps anywhere in view. Type a character or two of where you want to
go, and each match gets a highlighted label. Then:

- Type the label to go there.
- `<CR>` goes to the nearest match.
- `<Esc>` stays.

## Changing text

An operator followed by a motion acts on the text the motion covers.

- `dw` deletes to the next word.
- `c$` changes to the end of the line.
- `y}` copies to the end of the paragraph.
- An operator typed twice acts on the line, so `dd` deletes it.

A text object names the thing around the cursor.

- `ciw` changes the word.
- `di(` deletes inside the parentheses.
- `daf` deletes the function when the file's grammar knows functions.

A few more keys change text on their own:

- `x` deletes the character under the cursor.
- `p` puts what was deleted or copied.
- `u` undoes and `<C-r>` redoes.
- `.` repeats the last change.

## Saving and quitting

fude keeps every change on the file's timeline, including changes not
yet written.

- `:w` writes the buffer.
- `:q` closes the window, and quits with the last one.
- `:wq` writes and quits.
- `:q!` quits without writing.
- `:recover` brings back the changes after a crash.
- `:restart` starts fude again on the same files after a change to its
  config.

## Finding things

- `:e path` opens a file.
- `:pick files` finds a file by name.
- `:pick grep text` searches the files' text.
- `:pick buffers` finds an open buffer.
- `:pick recent` finds a file you opened before.

## When you forget a key

Type the start of a command and wait. A panel lists the keys that may
follow, such as every command after `g`.

- `<BS>` goes back a key.
- `<Esc>` drops the command.
- `:h w` explains a key, and `:h :write` a command.

A message shows in the top right until the next key.

- `:messages` lists the session's messages.
- `:messages yank` puts the last one on the clipboard, whole.

## Next

- `guide-config` sets fude up through its config file.
- `guide-editing` goes further with operators, objects and registers.
- `index` lists every page of help.
