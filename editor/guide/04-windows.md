# Windows and buffers

A buffer is the text of a file. A window shows a buffer, and several
windows may show the same one, each with its own cursor.

## Buffers

`:e path` opens a file in a buffer. `:buffers` lists the open buffers with
their numbers, and `:b 3` shows buffer 3. `:bn` and `:bp` go to the next
and previous buffer you edit, past help and lists, and `:bd` closes one. `<C-^>` goes back to the
buffer the window showed before. A buffer with changes stays loaded when
the window shows another.

Once two or more files are open, the top row lists them by number, the
current one lit and a modified one marked ●. `:set bufferline.enabled
false` hides it.

## Windows

`<C-w>s` splits the window with the new one below, and `<C-w>v` with the
new one right. The new window takes the cursor. `<C-w>h`, `<C-w>j`,
`<C-w>k` and `<C-w>l` move between windows. `<C-w>c` closes a window,
`<C-w>o` closes every other one, and `:q` in a split closes only that
window. `:sp file` and `:vs file` split and open a file. `:hide` takes a
window out of sight. The window keeps its buffer, a terminal's program
goes on, and the window shows again where it was, at the same size.

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

tmux decides which keys reach fude. With vim-tmux-navigator, add `fude`
to the plugin's pattern. Otherwise tmux switches panes and fude never
sees the key. `fude tail` and `fude relay` have the same process name
but read no keys. Leave them out of the plugin's check, or `<C-h>` and
the others stop in their panes. Set both before the plugin loads:

```
set -g @vim_navigator_pattern '(\S+/)?g?\.?(view|l?n?vim?x?|fzf|fude)(diff)?(-wrapped)?'
set -g @vim_navigator_check 'ps -o state= -o comm= -t "#{pane_tty}" | grep -iqE "^[^TXZ ]+ +@vim_navigator_pattern$" && ! ps -o args= -t "#{pane_tty}" | grep -qE "(^|/)fude +(tail|relay)( |$)"'
```

## Placing windows

`:window float`, `:window above`, `:window below`, `:window left` and
`:window right` before a command open its window there, such as
`:window float help` or `:window right terminal`. Alone, they move the
current window into a float, or to that edge of the screen. `<C-w>z` or
`:window zoom` shows the current window alone across the screen, and
either one again brings every window back as it was. In
`config.toml`, `[open]` sets where each module's windows open, such as
`help = "float"`. A window its module locks stays where it opened.

## Explorers

`-` lists the current file's folder, as a buffer of its entries' names.
`<CR>` opens the entry under the cursor, `<C-s>` opens it in a split
beside and `<C-h>` below, and `-` goes up a folder. `_` lists the working
directory, and `<C-l>` reads the folder again.

Editing an explorer and writing it with `:w` changes the folder. A changed
name renames the entry, a deleted line deletes it, and a new line creates
a file, or a folder when it ends in `/`. fude asks before each change.

## Finding files and text

`:pick files` opens a fuzzy finder over the working directory's files,
those git does not ignore inside a repository. Type part of a path, move
with `<C-n>` and `<C-p>`, and `<CR>` opens the file. A wide screen shows
the selected file beside the list.

`:pick grep` searches the files' text as the query is typed, with `rg`
when it is installed and `git grep` otherwise. `:pick grep text` starts
with text typed, and `<CR>` opens the file at the match.

`:pick buffers` finds an open buffer, and `:pick recent` one of the last
100 files you opened. `:pick help` finds a help topic, and `:pick keymaps`
any key by what it does, such as `delete` or `fold`. A normal mode key
runs when picked. `:pick commands` finds an ex command the same way, and
runs it, or puts it on the command line when it needs an argument.
`:pick` alone lists every source.

## Pins

`<Leader>a` or `:pin` pins the current file, up to 9 for each working
directory, and `<Leader>1` to `<Leader>9` go back to one with the cursor
where you left it. The status line shows a pinned file's place, such as
`2/4`. `<Leader>h` or `:pin list` shows the pins one to a line. Move,
delete or type lines and `:w` keeps them, `<CR>` opens one and `q`
closes the list. `:pin remove` takes the current file off, and a pin follows its
file when an explorer renames it.

## Terminals

`:terminal` shows a shell in a window below, in terminal mode, and hides
it again when you are in it. The same shell comes back where it was, with
its history and directory, so one key mapped to `:terminal<CR>` shows
and hides it. `:terminal 2` is a second shell, and any number
another, as `:b 2` names a buffer. `exit` in a shell closes its window.
`:terminal cmd` runs `cmd` in a new window each time, which stays once
`cmd` ends, to read and copy its output. `<Esc><Esc>` leaves terminal mode to move through the
output, and `i` goes back. `:pick terminals` lists every terminal, those
out of sight too, and shows the one picked.

## Tasks

`:pick tasks` lists the tasks of the working directory: those
`config.toml` names under `[task.<name>]`, the project's in
`.fude/tasks.toml`, the justfile's recipes, the Makefile's targets and
package.json's scripts. The one picked runs in a
terminal window below, while the cursor stays where it was, and the
window's name says ✓ or ✗ and how long it took. `:task name` runs one
directly, and `:task` alone runs the last one again. In its window, `q`
closes it once the task is done, and while it runs, hides it, as the task
goes on with a spinner on the status line. Picking it again shows it. The
places a task's output names, as
`file.go:12:3: message`, become the last list of places, so `]q` goes
through a build's errors.

## Lists of places

`grr`, `:marks`, `:jumps` and the others show their places in a list
window, where `<CR>` goes to one. The last list shown is the quickfix
list. `]q` and `[q` go to its next place and the one before, `]Q` and
`[Q` to its ends, and `:cnext`, `:cprev` and `:cc 3` do the same from the
command line, after its window closes too. `:copen` shows it again.

`<C-q>` in a `:pick` finder sends every match to a list of places, which
becomes the last list. `:pick diagnostics` finds the open buffers'
diagnostics, and `:pick symbols` the language server's symbols as you
type their name.

`:cdo cmd` runs an ex command at each place of the last list, and
`:cfdo cmd` once in each of its files. To replace across the project,
find the text with `:pick grep`, send it to the list, then:

```
:cdo s/old/new/
:wall
```

## Jumping back

A jump, such as `gg`, a search or opening a file, goes on the window's
jump list. `<C-o>` goes back through it and `<C-i>` forward. `g;` and `g,`
go back and forward through the places text changed. `:jumps`, `:changes`
and `:marks` list them, and `<CR>` on one goes there.

## Next

- `guide-timeline` undoes and restores changes.
- `normal-mode` lists the window keys with the other normal mode keys.
