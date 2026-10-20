# Commands for keys

A `<Plug>(name)` command is something a key runs. No key types it, so it
runs only from a mapping, or from `:plug name`, which runs it as a key
mapped to it does. fude's own have plain names, such as `restart`, and a
module's take the module's name first, such as `lsp.definition`. A
command whose module is not loaded does nothing.

Map a key to one in config.toml. `[keymap.normal]`, `[keymap.visual]` and
`[keymap.insert]` map keys in those modes. A kind of buffer has a table of
its own, such as `[keymap.explorer]`, `[keymap.list]`, `[keymap.finder]`,
`[keymap.float]` or a module's `[keymap.pins]`, whose keys run in that
buffer ahead of the normal mode ones. The finder's keys are insert mode's.
`:config reload` reads the tables again.

```toml
[keymap.normal]
"<Leader>d" = "<Plug>(lsp.definition)"
"<C-h>" = "<Plug>(window.left)"

[keymap.explorer]
l = "<Plug>(explorer.open)"
```

Most modules give their commands a key, such as `grn` for
`<Plug>(lsp.rename)`. Each section below names it, and help's mappings
sections, such as MAPPINGS IN NORMAL MODE and KEYS IN AN EXPLORER, list
the keys mapped now. A config.toml mapping of the same keys wins over the
module's, and mapping them to `""` takes them away. `<Leader>` is the
`leader` setting, `<Space>` unless set. A count typed before the key
reaches the command, and the sections say where it counts. A guest in a
shared session may not run some, such as `<Plug>(restart)`, and the
message says so.

## <Plug>(restart)

Replaces fude's process with a new one, which reads config.toml, plugins,
grammars and language servers again. The new fude opens the same files and
splits, with each window's cursor, top line and options, the same current
window and every unsaved change. `ZR` runs it in normal mode, as `:restart`
does. Only the terminal fude started in can restart it, and from another
one the message says so. A client attached over RPC must attach again.

## <Plug>(finder.open)

Closes the finder and opens its selected match, in the window the finder
opened from. A grep match, a diagnostic or a symbol opens at its line and
column. `<CR>` runs it in the finder. Outside a finder it says "no finder".

## <Plug>(finder.list)

Closes the finder and makes every match a list of places, shown in a
window of its own, which becomes the last list for `]q` and `[q`. A file is
a place at its start, and a grep match, a diagnostic or a symbol one at its
line and column. With no matches the message says so and no list opens.
`<C-q>` runs it in the finder.

## <Plug>(finder.next)

Moves the finder's selection down one match. It stops at the last match
and does not wrap. `<C-n>` runs it in the finder.

## <Plug>(finder.prev)

Moves the finder's selection up one match, stopping at the first.
`<C-p>` runs it in the finder.

## <Plug>(finder.all)

In the finders of `:pick files` and `:pick grep`, searches the files git
ignores and hidden files too, and the count above the matches says "with
ignored". Run again, it leaves them out. `.git` is never searched. In the
other finders it does nothing. `<C-g>` runs it in the finder.


## <Plug>(finder.resume)

Opens the finder that closed last again, as `:pick resume` does, with its
query, its matches and the one selected. No key runs it, so map one to
have it:

```toml
[keymap.normal]
"<Leader>'" = "<Plug>(finder.resume)"
```
## <Plug>(float.close)

Closes the float the cursor is in, and the floats opened from it, and the
cursor goes back to the window the float opened from. In a window that is
not a float it does nothing. `q` runs it in a float, and `q` and `<Esc>`
in the language server panel.

```toml
[keymap.float]
"<Esc>" = "<Plug>(float.close)"
```

## <Plug>(terminal.close)

Closes a terminal or task window whose program has ended, and the ended
program's buffer goes with its last window. While a task runs, it hides the
window and the task goes on. While a `:terminal` shell or command runs, it
leaves the window and says `:q` stops the program. `q` runs it in a
terminal window in normal mode.

## <Plug>(debug.break)

Puts a breakpoint on the cursor's line, or takes off the one there, as
`:debug break` does. Its key is `<Leader>db` in normal mode. The sign
column shows the breakpoint as ○ until a debug adapter takes it, and as ●
after. A breakpoint stays on its line as the text around it changes, and
a running session hears of each one put or taken off. A buffer with no
file says "a breakpoint needs a file". A count does nothing. A guest may
not run it.

## <Plug>(debug.continue)

Lets the stopped program run on, as `:debug continue` does. Its key is
`<Leader>dc` in normal mode. With no session running it starts one
instead, as `:debug start` does without a name. When `.fude/launch.toml`
has one set of launch settings they start, and with several a list asks
which. While
the program runs and is not stopped, the message says so. A count does
nothing. A guest may not run it.

```toml
[keymap.normal]
"<Leader>r" = "<Plug>(debug.continue)"
```

## <Plug>(debug.next)

Steps over the stopped program's line, so a call on it runs whole. Its
key is `<Leader>dn` in normal mode. With no session, the message says
"no debug session runs", and while the program runs it says the program
is not stopped. A count does nothing, so `3<Leader>dn` steps once. A guest
may not run it.

## <Plug>(debug.in)

Steps into the call on the stopped program's line. Its key is
`<Leader>di` in normal mode. It fails as `<Plug>(debug.next)` does when no
session runs or the program is not stopped.

## <Plug>(debug.out)

Runs the stopped program to the end of its current function and stops in
the one that called it. Its key is `<Leader>do` in normal mode. It fails
as `<Plug>(debug.next)` does when no session runs or the program is not
stopped.

## <Plug>(debug.stop)

Ends the debug session and the program it debugs, as `:debug stop` does.
Its key is `<Leader>dq` in normal mode. The stopped line is no longer lit,
the breakpoints show as ○ again, and the message says "stopped debugging"
and the launch settings' name. With no session, it says "no debug session
runs". A guest may not run it.

## <Plug>(debug.panel)

Opens the `[debug]` window, as `:debug panel` does. Its key is
`<Leader>dp` in normal mode. The window lists the stopped thread's frames,
with → on the chosen one, then the variables of that frame and the
program's last lines of output. It shows anew each time the program
stops. With no session, it says "no debug session runs". A guest may not
run it.

## <Plug>(debug.eval)

Shows in a float the value the debug adapter gives at the chosen frame,
as `:debug eval` does in the message. Its key is `<Leader>de` in normal
and visual mode. In normal mode it evaluates the name under the cursor
with its dots, so on `user.name` it takes all of it. In visual mode it
evaluates the selected text. With no session, it says "no debug session
runs", and an error from the adapter shows in the message after the
expression. A guest may not run it.

## <Plug>(debug.open)

Acts on the `[debug]` window's line under the cursor. `<CR>` runs it
there, from `[keymap.debug]`. On a frame it chooses that frame, shows its
line and lists its variables. On a variable marked ▸ it shows its
members, marked ▾ then, and on one marked ▾ it hides them. On any other
line, and outside the panel, it does nothing. A guest may not run it.

```toml
[keymap.debug]
o = "<Plug>(debug.open)"
```

## <Plug>(explorer.up)

Opens an explorer on the directory holding the current file, or on the
one above the current explorer. Its key is `-` in normal mode, in place
of the `-` motion. With n, it goes up n levels, except that `1-` lists the
working directory. A buffer with no file lists the working directory too.
The cursor lands on the entry that leads back to where it came from.

```toml
[keymap.normal]
"<Leader>e" = "<Plug>(explorer.up)"
```

## <Plug>(explorer.open)

Opens the entry on the cursor's line in the current window. `<CR>` runs it
in an explorer, from `[keymap.explorer]`. A file opens as `:e` opens it,
and a directory opens as an explorer of its own. An empty line does
nothing. Outside an explorer the message says "not in an explorer".

## <Plug>(explorer.split)

Opens the entry on the cursor's line in a new window below, or above
with `split_below` off. `<C-h>` runs it in an explorer. It fails as
`<Plug>(explorer.open)` does.

## <Plug>(explorer.vsplit)

Opens the entry on the cursor's line in a new window to the right, or to
the left with `split_right` off. `<C-s>` runs it in an explorer. It fails
as `<Plug>(explorer.open)` does.

## <Plug>(explorer.cwd)

Opens an explorer on the working directory, with the cursor on the entry
that leads to the explorer it ran from when the working directory holds
it. `_` runs it in an explorer. A guest may not run it.

## <Plug>(explorer.reload)

Reads the explorer's directory again, so files made or removed outside
fude show. `<C-l>` runs it in an explorer. It runs `:e!`, so it drops
edits to the explorer not yet written with `:w`.

## <Plug>(fold.toggle)

`za` by default. On a line inside a closed fold, it opens every closed
fold that holds the line. Elsewhere it closes a fold as
`<Plug>(fold.close)` does, and says "no fold here" when there is none.
The count does nothing.

## <Plug>(fold.close)

`zc` by default. Folds go by indent. A fold starts on a line followed by
lines indented more, and holds them and the blank lines among them. On
such a line it closes that fold. Elsewhere it closes the innermost fold
the cursor's line is in. The closed folds inside it join the new one. The
cursor goes to the fold's first line. With no fold there, the message
says "no fold here". `zc` on a closed fold closes the same fold again,
not the one around it as Vim does.

## <Plug>(fold.open)

`zo` by default. It opens every closed fold that holds the cursor's line,
nested ones too, where Vim's `zo` opens one level. On a line in no closed
fold it does nothing. A fold `zf` made is gone once opened, and
`<Plug>(fold.close)` folds by indent after that.

## <Plug>(fold.open_all)

`zR` by default. It opens every closed fold in the buffer, those `zf`
made too. Folds belong to the buffer, so every window on it shows them
open.

## <Plug>(fold.close_all)

`zM` by default. It closes each outermost fold by indent, from the top of
the buffer down, and the cursor goes to the first line of the fold it is
in.

## <Plug>(fold.next)

`zj` by default. It goes to the first line of the n-th fold below the
cursor's line, closed or by indent, nested ones too. A closed fold counts
as one line, so the folds inside it are passed over. With fewer than n
folds below, the cursor stays and the message says "no fold there".

## <Plug>(fold.prev)

`zk` by default. It goes to the last line of the n-th fold above the
cursor's line. A closed fold counts as one line, and the cursor lands on
its first. With fewer than n folds above, the cursor stays and the
message says "no fold there".

## <Plug>(git.next)

`]h` by default. A hunk is a run of lines that differ from the file as
git's index has it, unsaved changes too. It goes to the start of the n-th
hunk below the cursor's line, or to the last hunk when n is past it. With
no hunk below, the message says "no next hunk". It does not wrap around.
Outside a git repository it says "not in a git repository", and in a file
with no changes "no hunks".

## <Plug>(git.prev)

`[h` by default. It goes to the start of the n-th hunk above the cursor's
line, or to the first when n is past it. From inside a hunk below its
first line, that hunk's start counts as the first one above. With no hunk
above, the message says "no previous hunk".

## <Plug>(git.first)

`[H` by default. It goes to the start of the first hunk in the file. The
count does nothing.

## <Plug>(git.last)

`]H` by default. It goes to the start of the last hunk in the file. The
count does nothing.

## <Plug>(git.preview)

Shows the hunk on the cursor's line in a float, its lines in git's index
after `-` and its lines now after `+`. A hunk of deleted lines sits on
the line above them. Off a hunk, the message says "no hunk on this line".
No key is mapped to it.

```toml
[keymap.normal]
"<Leader>gp" = "<Plug>(git.preview)"
```

## <Plug>(help.follow)

`<CR>` and `<C-]>` in a help window by default, which `[keymap.help]`
changes. It takes the text under the cursor up to a blank on either side,
without the backticks, parentheses, commas and periods around it, and
opens help at the entry of that name, as `:h` would. A name finds a
command without its colon and a `<Plug>` without its brackets too. On a
blank it does nothing, and a name with no entry says "no help for" it. It
works in any buffer once mapped.

```toml
[keymap.normal]
"<Leader>k" = "<Plug>(help.follow)"
```

## <Plug>(history.commands)

`q:` by default. It opens a window seven lines tall below the current
one, listing the command lines entered, the newest last, then an empty
line with the cursor on it. The lines edit as any buffer's do, and
`<Plug>(history.run)` runs one.

## <Plug>(history.searches)

`q/` by default. It opens the window `<Plug>(history.commands)` opens,
listing the searches instead, and `<Plug>(history.run)` searches forward
for the line, as `/` would.

## <Plug>(history.searches_back)

`q?` by default. It lists the same searches as `<Plug>(history.searches)`,
and `<Plug>(history.run)` searches back for the line, as `?` would.

## <Plug>(history.run)

`<CR>` in a history window by default, which `[keymap.history]` changes.
It closes the window and types the cursor's line after `:`, `/` or `?`,
as the window's key chose, then `<CR>`, in the window the list opened
from. The line runs as written, so a `<` in it is a `<`. On an empty line
it only closes the window. The window's buffer goes with it, and any
edits to the list are lost. Mapped in another window, it says "not in a
history window" and does nothing.

## <Plug>(jump.start)

`s` in normal and visual mode by default. Each character typed next adds
to a text to find in what the window shows, ignoring case unless the text
has a capital. Every match is highlighted, and the nearest ones to the
cursor get a label, a letter drawn over the character after the match.
Typing a label moves the cursor to its match's start, `<CR>` to the
nearest match, `<BS>` drops the last character, and `<Esc>` leaves the
cursor where it was. A label is never the character after a match, so
typing more of the text never jumps. The status line shows what was
typed, and a jump is in the jumplist. It takes no count. While it is
mapped, `s` does not substitute a character as in Vim.

## <Plug>(links.open)

`gx` by default. It opens what is under the cursor with the system's
opener, `open` on macOS and `xdg-open` elsewhere. On a markdown link it
takes the link's destination from anywhere on the link. Elsewhere it takes
the text up to a blank on either side, without the brackets, quotes and
marks around it. A URL or `mailto:` goes to the opener as it is. A path
not absolute must name a file beside the current one or in the working
directory, or the message says "no file named" it. With nothing under the cursor it
says "no link or file under the cursor". Only the host runs it, as a
guest's would open on the host's machine.

## <Plug>(links.file)

`gf` by default. It opens the file under the cursor in the window, found
as `<Plug>(links.open)` finds it. A `:12` after the path puts the cursor
on line 12. It looks beside the current file first, then in the working
directory, and `~/` names the home folder. With no such file, the
message says "no file named" it.

## <Plug>(lsp.hover)

`K` runs it. Asks the buffer's language servers about the symbol under the
cursor and shows their answer, rendered from markdown, in a float at the
cursor. While that float shows, running it again moves the cursor into the
float, where it takes keys as any window does. An answer with no text says
"no hover information", and a buffer with no server says "no language
server".

## <Plug>(lsp.definition)

`<C-]>` runs it. Jumps to the definition of the symbol under the cursor,
opening its file when it is another. When the servers name several
definitions, they show as a list of places, which becomes the last list.
It is mapped with or without a server, and without one it says "no
language server". With nothing found it says "no definition found".

## <Plug>(lsp.type_definition)

`grt` runs it, and is mapped only while a language server is attached to
the buffer. Jumps to the definition of the type of the symbol under the
cursor, such as the struct a variable holds. Several answers show as a
list of places, and none says "no type definition found".

## <Plug>(lsp.implementations)

`gri` runs it while a language server is attached. Finds the
implementations of the interface or method under the cursor. One
implementation is a jump, and several show as a list of places, the last
list, where `<CR>` goes to the one on the cursor's line.

## <Plug>(lsp.references)

`grr` runs it while a language server is attached. Lists every reference
to the symbol under the cursor, its declaration too, as a list of places.
Unlike a definition, even one reference opens the list rather than
jumping. `]q` and `[q` then step through it.

## <Plug>(lsp.symbols)

`gO` runs it while a language server is attached. Lists the current file's
symbols in a window, each with its line, column, kind in brackets and
name, nested symbols indented under the one that holds them. `<CR>` goes to
the symbol on the cursor's line. A file with none says "no symbols found".
`:pick symbols` searches the symbols of the whole project instead.

## <Plug>(lsp.rename)

`grn` runs it while a language server is attached. Opens the command line
with `:rename` and the word under the cursor, to edit into the new name.
`<CR>` sends it to the first server that offers rename, and the edits it
returns change every file it names, as one undo step labelled rename. An
empty name says "no new name". `<Esc>` leaves the name as it was.

```toml
[keymap.normal]
"<Leader>r" = "<Plug>(lsp.rename)"
```

## <Plug>(lsp.next_diagnostic)

`]d` runs it. Moves to the start of the n-th diagnostic after the cursor,
going round to the first after the last. The diagnostics that start there
show in a float, each in its severity's colour with its source and code,
until the next key. A buffer with none says "no diagnostics".

## <Plug>(lsp.prev_diagnostic)

`[d` runs it. Moves to the n-th diagnostic before the cursor, going round
to the last before the first, as `]d` moves forward.

## <Plug>(lsp.diagnostics)

`<C-w>d` runs it. Shows every diagnostic on the cursor's line in a float,
in full, without moving the cursor. A line with none says "no diagnostics
on this line". It helps when `diagnostic_notes` is off, or a note is cut
at the window's edge.

## <Plug>(lsp.actions)

`gra` runs it, in normal mode and in visual mode, while a language server
is attached. Asks for the code actions at the cursor, or for the
selection's text in visual mode, with the diagnostics on those lines. The
first server that answers fills a numbered menu at the cursor, which shows
what the selected action would change. `<CR>` or the action's digit runs
it, and any other key closes the menu. With none it says "no code
actions".

## <Plug>(lsp.restart)

`r` runs it in the `:lsp` panel. Restarts the server whose part of the
panel holds the cursor, and opens its files in the new one. `:lsp restart`
does the same for the current file's servers. A guest may not run it, nor
the panel's other three. Outside the panel, it and the other three say
"not in the language servers panel" and do nothing.

```toml
[keymap.lsp]
R = "<Plug>(lsp.restart)"
```

## <Plug>(lsp.stop)

`s` runs it in the `:lsp` panel. Stops the server whose part holds the
cursor. Opening a file it serves starts it again, unlike
`<Plug>(lsp.disable)`.

## <Plug>(lsp.disable)

`d` runs it in the `:lsp` panel. Stops the server whose part holds the
cursor and keeps it off for every file, even through a restart, until
`<Plug>(lsp.enable)` or `:lsp enable`.

## <Plug>(lsp.enable)

`e` runs it in the `:lsp` panel. Lets a disabled server under the cursor
start again, which it does for the open files it serves.

## <Plug>(pin.add)

`<Leader>a` by default, and `:pin` does the same. Pins the current file
after the working directory's other pins, with the cursor's place, and
says which pin it is. A file pinned already says which pin it has. A
buffer with no file, such as an explorer, is not pinned, and neither is a
tenth file. Each says why. fude keeps the pins of each working
directory across its runs. A guest's pins are its own and end with its session.

## <Plug>(pin.list)

`<Leader>h` by default, and `:pin list` does the same. Shows the pins in
the window, pin n on line n. The lines edit as text, and `:w` makes the
pins what they say, in their order. A line taken out unpins its file, and
a path typed in pins it. A line naming no file, a file named twice or a
tenth line stops the write, and the message names the line. In the list,
`<CR>` and `q` run `<Plug>(pin.open)` and `<Plug>(pin.close)`, which
`[keymap.pins]` maps.

## <Plug>(pin.next)

Shows the file of the pin after the current file's, from the last back to
the first. From a file not pinned it goes to the first pin. A count does
nothing. With no pins it says so. No key maps it.

```toml
[keymap.normal]
"]p" = "<Plug>(pin.next)"
"[p" = "<Plug>(pin.prev)"
```

## <Plug>(pin.prev)

Shows the file of the pin before the current file's, from the first round
to the last. From a file not pinned it goes to the last pin. No key maps
it.

## <Plug>(pin.open)

`<CR>` in the list `<Plug>(pin.list)` shows. Shows the file named on the
cursor's line, with the cursor where it last was when the file is pinned.
A path typed in but not written yet opens too. The list closes unless its
lines have changes not written. On a blank line it does nothing.

## <Plug>(pin.close)

`q` in the list `<Plug>(pin.list)` shows. Closes the list. When its lines
have changes not written, it says so and the list stays until `:w`
writes them.

## <Plug>(pin.1)

`<Leader>1` by default, and `:pin 1` does the same. Shows pin 1's file in
the window, with the cursor where it last was in that file, even after
fude quit. With fewer pins it says there is no pin 1. A pin whose file is
gone says so and stays a pin. A count does nothing.

## <Plug>(pin.2)

The same as `<Plug>(pin.1)`, for pin 2, on `<Leader>2`.

## <Plug>(pin.3)

The same as `<Plug>(pin.1)`, for pin 3, on `<Leader>3`.

## <Plug>(pin.4)

The same as `<Plug>(pin.1)`, for pin 4, on `<Leader>4`.

## <Plug>(pin.5)

The same as `<Plug>(pin.1)`, for pin 5, on `<Leader>5`.

## <Plug>(pin.6)

The same as `<Plug>(pin.1)`, for pin 6, on `<Leader>6`.

## <Plug>(pin.7)

The same as `<Plug>(pin.1)`, for pin 7, on `<Leader>7`.

## <Plug>(pin.8)

The same as `<Plug>(pin.1)`, for pin 8, on `<Leader>8`.

## <Plug>(pin.9)

The same as `<Plug>(pin.1)`, for pin 9, on `<Leader>9`.

## <Plug>(list.open)

`<CR>` in a list of places, such as the one `grr` or `:copen` shows. Goes
to the place on the cursor's line, in a window that does not show the
list, so the list stays in view. In the last list, that place becomes the
one `]q` and `[q` step from. An older list's window still goes to its own
places. Past the list's last line it says the cursor is not in a list.

## <Plug>(list.next)

`]q` by default, and `:cnext` does the same. Goes to the next place of the
last list, or n places on, and says which place it is, of how many. Before
any place was gone to, `]q` goes to the first. It never wraps, so past the
last place it says there are no more and stays. It works after the list's
window closes, and with no list it says so.

## <Plug>(list.prev)

`[q` by default, and `:cprevious` does the same. Goes n places back in the
last list, as `<Plug>(list.next)` goes on. Before the first place it says
there are no more and stays.

## <Plug>(list.first)

`[Q` by default, and `:cfirst` does the same. Goes to the first place of
the last list. A count does nothing.

## <Plug>(list.last)

`]Q` by default, and `:clast` does the same. Goes to the last place of
the last list. A count does nothing.

## <Plug>(session.restore)

`s` on the start screen, whose `s` line shows when the working directory
has a session. Opens the files of the session fude kept when it last quit
in this directory and builds its windows again, each with its cursor, top
line and options, and the same current window. A file that no longer opens
is left out, and the message says why. With no session kept it says so.
No key maps it in normal mode.

```toml
[keymap.normal]
"<Leader>R" = "<Plug>(session.restore)"
```

## <Plug>(spell.next)

`]s` by default. Goes to the start of the next word spell finds after the
cursor, wrapping from the end of the text to its start. A count does
nothing. With `spell` off it says so, and `:set spell` turns it on. While
the word list is still being read it says so. With no misspelled words it
says so.

## <Plug>(spell.prev)

`[s` by default. Goes to the start of the word spell finds before the
cursor, wrapping from the start of the text to its end, as
`<Plug>(spell.next)` does forward.

## <Plug>(spell.suggest)

`z=` by default. Lists up to 9 words of the word list that differ from the
one under the cursor by at most two letters added, taken out or changed,
closest first. The one picked
replaces the word, keeping its capital first letter. If the text changed
since, the word stays and the message says why. With no word under the
cursor, or no close words, it says so. It works only with `spell` on.

## <Plug>(spell.good)

`zg` by default. Adds the word under the cursor to `words.txt` in fude's
config directory, so spell passes it from then on, and says so. With no
word under the cursor it says so, and it works only with `spell` on.

## <Plug>(start.open)

`<CR>` on the start screen. Reads the key at the start of the cursor's
line and runs it as if typed, so `<CR>` on the `f  find a file` line runs
`f`, and on a recent file's line runs its digit. On a line that starts
with no key of the screen, such as a heading or a blank line, it does
nothing. `[keymap.start]` changes the screen's keys, and this follows
them. A count does nothing.

```toml
[keymap.start]
"o" = "<Plug>(start.open)"
```

## <Plug>(start.config)

`c` on the start screen. Opens config.toml from fude's config directory
in the window, `$XDG_CONFIG_HOME/fude` or else `~/.config/fude`, and the
system's config folder on Windows. When the
directory does not exist yet it makes it, so `:w` can write the new file.
When it cannot, the message says why. It works from any window it is
mapped in.

## <Plug>(start.1)

`1` on the start screen. Opens the first of the recent files the start
screen lists: the 9 newest under the working directory, or the 9 newest
anywhere when it has none. Mapped in normal mode it opens that file from
any window, even when fude started with a file. When the list is shorter
it says "no recent file 1".

## <Plug>(start.2)

The same as `<Plug>(start.1)`, for recent file 2. `2` on the start
screen.

## <Plug>(start.3)

The same as `<Plug>(start.1)`, for recent file 3. `3` on the start
screen.

## <Plug>(start.4)

The same as `<Plug>(start.1)`, for recent file 4. `4` on the start
screen.

## <Plug>(start.5)

The same as `<Plug>(start.1)`, for recent file 5. `5` on the start
screen.

## <Plug>(start.6)

The same as `<Plug>(start.1)`, for recent file 6. `6` on the start
screen.

## <Plug>(start.7)

The same as `<Plug>(start.1)`, for recent file 7. `7` on the start
screen.

## <Plug>(start.8)

The same as `<Plug>(start.1)`, for recent file 8. `8` on the start
screen.

## <Plug>(start.9)

The same as `<Plug>(start.1)`, for recent file 9. `9` on the start
screen.

## <Plug>(theme.center)

Turns the `center` setting on or off, as `:center` does. Centered, each
window gets `color_column` columns of text, or 100 without one, and the
windows sit in the middle of a wider screen with blank margins on either
side. When they need more than the screen they take its whole width. A
guest may not run it. No key runs it by default, and a count does
nothing.

```toml
[keymap.normal]
"<Leader>z" = "<Plug>(theme.center)"
```

## <Plug>(timeline.earlier)

`[` in a timeline window. Moves the buffer's text to the state on the
line above the cursor's, then lists the timeline again with the cursor on
that state, so `[` again goes one further back. On state 0 it does
nothing. It works only in the window `:timeline` opens. In any other
window it says "not in a timeline". A count does nothing.

## <Plug>(timeline.later)

`]` in a timeline window. Moves the text to the state on the line below
the cursor's, as `<Plug>(timeline.earlier)` moves back. On the last state
it does nothing.

## <Plug>(timeline.diff)

`d` in a timeline window. Opens a window with the lines that differ
between the state under the cursor and the state the text is at, with 3
lines around each run. A line only the state under the cursor has comes
after `-`, and a line only the text now has after `+`. The window's title
names both states, such as `[diff 3 7]`, and `q` closes it. Outside a
timeline window it says "not in a timeline".

## <Plug>(timeline.restore)

`<CR>` in a timeline window. Makes the text of the state under the cursor
the newest state, as `:restore` does. The states after it stay, so it
loses nothing. The timeline lists again with the cursor on the new state.
Outside a timeline window it says "not in a timeline".

## <Plug>(timeline.list)

Lists the timeline of the buffer `:timeline` last listed, with the cursor
on the state its text is at. `[`, `]` and `<CR>` run it after they move
the text, so the list shows where the text went. It has no key of its
own.

## <Plug>(window.left)

Moves to the window to the left. Inside tmux, with no window that way, it
moves to tmux's pane to the left instead, so one key crosses from fude
into the next pane. Outside tmux, or with no pane either, it does
nothing, and a tmux that fails says why. A guest's session never moves
tmux's panes. No key is mapped to it, since `<C-w>h` moves only among
fude's windows. A count does nothing.

```toml
[keymap.normal]
"<C-h>" = "<Plug>(window.left)"
"<C-j>" = "<Plug>(window.down)"
"<C-k>" = "<Plug>(window.up)"
"<C-l>" = "<Plug>(window.right)"
```

## <Plug>(window.down)

Moves to the window below, or to tmux's pane below, as
`<Plug>(window.left)` does to the left.

## <Plug>(window.up)

Moves to the window above, or to tmux's pane above, as
`<Plug>(window.left)` does to the left.

## <Plug>(window.right)

Moves to the window to the right, or to tmux's pane to the right, as
`<Plug>(window.left)` does to the left.

## <Plug>(window.zoom)

`<C-w>z`, and `:window zoom`. Shows the current window alone across the
screen, with zoomed on its status line, and again shows every window as
it was. Splitting, closing or moving a window, or moving to another,
shows every window again first. A float does not zoom and says so, and
floats show over a zoomed window.

## <Plug>(window.taller)

`<C-w>+`. Makes the window n rows taller, 1 without a count. The rows
come from the windows above and below it, each of which keeps at least a
row and its status line. A window with no other window above or below it
stays as it is. A float sizes itself, so it says so and stays.

```toml
[keymap.normal]
"<C-Up>" = "<Plug>(window.taller)"
"<C-Down>" = "<Plug>(window.shorter)"
```

## <Plug>(window.shorter)

`<C-w>-`. Makes the window n rows shorter, as `<Plug>(window.taller)`
makes it taller. It never goes below one row.

## <Plug>(window.wider)

`<C-w>>`. Makes the window n columns wider, 1 without a count. The columns
come from the windows beside it, each of which keeps at least a column. A
window with no other window beside it stays as it is.

## <Plug>(window.narrower)

`<C-w><`. Makes the window n columns narrower, as
`<Plug>(window.wider)` makes it wider. It never goes below one column.

## <Plug>(window.tallest)

`<C-w>_`. Makes the window n rows tall. Without a count it makes it as
tall as it can be, and the windows above and below it keep a row and
their status lines each. `:resize` does the same from the command line.

## <Plug>(window.tallest.ctrl)

The same as `<Plug>(window.tallest)`, on `<C-w><C-_>`, so the key works
with Ctrl still held.

## <Plug>(window.widest)

`<C-w>|`. Makes the window n columns wide, or as wide as it can be
without a count, as `<Plug>(window.tallest)` sizes its height.
`:vertical resize` does the same from the command line.
