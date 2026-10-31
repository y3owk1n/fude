# The timeline

fude keeps every state of a file's text as a node on its timeline, on
disk beside fude's state. An undo keeps the change it undid, and the
timeline holds changes not yet written, so a crash loses nothing typed.
The timeline is still there when the file opens again.

## Undo and redo

- `u` undoes a change and `<C-r>` redoes it.
- Typing between entering and leaving insert mode is one change.
- An edit after an undo starts a new branch, and the old one stays on
  the timeline.
- `g-` and `g+` move through every state in the order it was made,
  across branches.
- `:earlier 10m` goes back to the text as it was ten minutes ago, and
  `:earlier 1f` to the last write.
- `:later` goes the other way.
- `:earlier` and `:later` take a count of changes too.

## The timeline window

`:timeline` lists every state with its time and the command that made
it. In its window:

- `[` and `]` show the text at the state above or below.
- `d` shows how the state under the cursor differs from now.
- `<CR>` restores it. A restore is a new state on top of the newest, so
  the states after the restored one stay.

## After a crash

The timeline is written as changes are made.

- When a file opens with changes that were never written, fude says so,
  and `:recover` brings them back.
- Leaving them keeps them on the timeline as a branch.
- The timeline of a file carries over from session to session while the
  file on disk is the one fude last wrote.
- fude deletes it when the file changes outside fude, and 30 days after
  the last change to it.

## Storage

- `fude storage` in a shell lists where fude keeps timelines and its
  other files, and how much space each takes.
- `fude storage clean` deletes the ones fude makes again.
- `fude storage clean --force` deletes the timelines too, after it says
  what that loses.

## Restarting

`:restart`, or `ZR`, starts fude again in its place, so changes to the
config, plugins, grammars and language servers take effect. The new fude
opens the same files and windows, with each cursor where it was and every
change not yet written.

## Next

- `guide-keys` maps keys and shows what keys do.
