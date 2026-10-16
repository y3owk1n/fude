# Windows and buffers

A buffer is the text of a file. A window shows a buffer, and several
windows may show the same one, each with its own cursor.

## Buffers

`:e path` opens a file in a buffer. `:buffers` lists the open buffers with
their numbers, and `:b 3` shows buffer 3. `:bn` and `:bp` go to the next
and previous buffer, and `:bd` closes one. `<C-^>` goes back to the
buffer the window showed before. A buffer with changes stays loaded when
the window shows another.

## Windows

`<C-w>s` splits the window with the new one below, and `<C-w>v` with the
new one right. The new window takes the cursor. `<C-w>h`, `<C-w>j`,
`<C-w>k` and `<C-w>l` move between windows. `<C-w>c` closes a window,
`<C-w>o` closes every other one, and `:q` in a split closes only that
window. `:sp file` and `:vs file` split and open a file.

`<C-w>+` and `<C-w>-` make the window taller or shorter, and `<C-w>>`
and `<C-w><` wider or narrower, by the count typed before them, such as
`5<C-w>+`. `<C-w>_` and `<C-w>|` make it as large as it can be, or the
count tall or wide, and `<C-w>=` makes every window the same size again.
`:resize 20`, `:resize +5` and `:vertical resize 80` do the same from the
command line.

`<Plug>(window.left)` and the others for down, up and right move between
windows too, and inside tmux they move to the next tmux pane when no
window is that way. No key is mapped to them, so map them, such as
`"<C-h>" = "<Plug>(window.left)"`.

## Listings

`-` lists the current file's folder, as a buffer of its entries' names.
`<CR>` opens the entry under the cursor, `<C-s>` opens it in a split
beside and `<C-h>` below, and `-` goes up a folder. `_` lists the working
directory, and `<C-l>` reads the folder again.

Editing a listing and writing it with `:w` changes the folder. A changed
name renames the entry, a deleted line deletes it, and a new line creates
a file, or a folder when it ends in `/`. fude asks before each change.

## Finding files and text

`:files` opens a fuzzy finder over the working directory's files, those
git does not ignore inside a repository. Type part of a path, move with
`<C-n>` and `<C-p>`, and `<CR>` opens the file. A wide screen shows the
selected file beside the list.

`:grep` searches the files' text as the query is typed, with `rg` when it
is installed and `git grep` otherwise. `:grep text` starts with text
typed, and `<CR>` opens the file at the match.

## Lists of places

`grr`, `:marks`, `:jumps` and the others show their places in a list
window, where `<CR>` goes to one. The last list shown is the quickfix
list. `]q` and `[q` go to its next place and the one before, `]Q` and
`[Q` to its ends, and `:cnext`, `:cprev` and `:cc 3` do the same from the
command line, after its window closes too. `:copen` shows it again.

`<C-q>` in the `:grep` or `:files` finder sends every match to a list of
places, and `:diagnostics` lists the buffer's diagnostics, the worst
first. Either becomes the last list.

## Jumping back

A jump, such as `gg`, a search or opening a file, goes on the window's
jump list. `<C-o>` goes back through it and `<C-i>` forward. `g;` and `g,`
go back and forward through the places text changed. `:jumps`, `:changes`
and `:marks` list them, and `<CR>` on one goes there.

## Next

- `guide-timeline` undoes and restores changes.
- `normal-mode` lists the window keys with the other normal mode keys.
