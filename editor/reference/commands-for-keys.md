# Commands for keys

A `<Plug>(name)` command is something a key runs.

- No key types it, so it runs only from a mapping, or from `:plug name`,
  which runs it as a key mapped to it does.
- fude's own have plain names, such as `restart`, and a module's take the
  module's name first, such as `lsp.definition`.
- A command whose module is not loaded does nothing.

Map a key to one in config.toml:

- `[keymap.normal]`, `[keymap.visual]` and `[keymap.insert]` map keys in
  those modes.
- A kind of buffer has a table of its own, such as `[keymap.explorer]`,
  `[keymap.list]`, `[keymap.finder]`, `[keymap.float]` or a module's
  `[keymap.pins]`, whose keys run in that buffer ahead of the normal mode
  ones. The finder's keys are insert mode's.
- `:config reload` reads the tables again.

```toml
[keymap.normal]
"<Leader>d" = "<Plug>(lsp.definition)"
"<C-h>" = "<Plug>(window.left)"

[keymap.explorer]
l = "<Plug>(explorer.open)"
```

Most modules give their commands a key, such as `grn` for
`<Plug>(lsp.rename)`.

- Each section below names it, and help's mappings sections, such as
  MAPPINGS IN NORMAL MODE and KEYS IN AN EXPLORER, list the keys mapped
  now.
- A config.toml mapping of the same keys wins over the module's, and
  mapping them to `""` takes them away.
- `<Leader>` is the `leader` setting, `<Space>` unless set.
- A count typed before the key reaches the command, and the sections say
  where it counts.
- A guest in a shared session may not run some, such as
  `<Plug>(restart)`, and the message says so.

## <Plug>(restart)

Replaces fude's process with a new one, which reads config.toml, plugins,
grammars and language servers again. `ZR` runs it in normal mode, as
`:restart` does.

- The new fude opens the same files and splits, with each window's
  cursor, top line and options, the same current window and every unsaved
  change.
- Only the terminal fude started in can restart it, and from another one
  the message says so.
- A client attached over RPC must attach again.

## <Plug>(file)

`<C-g>` runs it, and `:file` says the same. Says the buffer's name, then
how many lines it has and how far down the cursor's line is, as
`"main.go" modified, 40 lines, 25% down`.

- After the name it says modified when the buffer has changes, new when
  its file is not written yet and read only when it cannot be written.
- An empty buffer says `"main.go" has no lines`.
- With a count it gives the file's full path.
- With a count over 1 it gives the buffer's number before it, as
  `buf 2:`.

## <Plug>(suspend)

`<C-z>` runs it. Gives the terminal back to the shell that started fude
and stops fude, with the programs and language servers it started, until
the shell resumes it, as `fg` does.

- A client attached to a running fude suspends itself, and that fude goes
  on.
- On Windows it does nothing.

## <Plug>(finder.open)

Closes the finder and opens its selected match, in the window the finder
opened from. `<CR>` runs it in the finder.

- A grep match, a diagnostic or a symbol opens at its line and column.
- Outside a finder it says "no finder".

## <Plug>(finder.list)

Closes the finder and makes every match a list of places, shown in a
window of its own, which becomes the last list for `]q` and `[q`. `<C-q>`
runs it in the finder.

- A file is a place at its start.
- A grep match, a diagnostic or a symbol is a place at its line and
  column.
- With no matches the message says so and no list opens.

## <Plug>(finder.next)

Moves the finder's selection down one match. It stops at the last match
and does not wrap. `<C-n>` runs it in the finder.

## <Plug>(finder.prev)

Moves the finder's selection up one match, stopping at the first.
`<C-p>` runs it in the finder.

## <Plug>(finder.all)

`<C-g>` runs it in the finder. In the finders of `:pick files` and
`:pick grep`, it searches the files git ignores and hidden files too.

- The count above the matches says "with ignored".
- Run again, it leaves them out.
- `.git` is never searched.
- In the other finders it does nothing.

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
cursor goes back to the window the float opened from.

- `q` runs it in a float, and `q` and `<Esc>` in the language server
  panel.
- In a window that is not a float it does nothing.

```toml
[keymap.float]
"<Esc>" = "<Plug>(float.close)"
```

## <Plug>(terminal.close)

Closes a terminal or task window whose program has ended, and the ended
program's buffer goes with its last window. `q` runs it in a terminal
window in normal mode.

- While a task runs, it hides the window and the task goes on.
- While a `:terminal` shell or command runs, it leaves the window and
  says `:q` stops the program.

## <Plug>(buffers.next)

`]b` by default. Shows the buffer n after the current one, 1 without a
count, as `:bnext` does, going on from the first after the last.

## <Plug>(buffers.prev)

`[b` by default. Shows the buffer n before the current one, as `]b` goes
forward.

## <Plug>(buffers.first)

`[B` by default. Shows the first buffer being edited, the one with the
lowest number. The count does nothing.

## <Plug>(buffers.last)

`]B` by default. Shows the last buffer being edited, the one with the
highest number. The count does nothing.

## <Plug>(debug.break)

Puts a breakpoint on the cursor's line, or takes off the one there, as
`:debug break` does. Its key is `<Leader>db` in normal mode.

- The sign column shows the breakpoint as ○ until a debug adapter takes
  it, and as ● after.
- A breakpoint stays on its line as the text around it changes.
- A running session hears of each one put or taken off.
- A buffer with no file says "a breakpoint needs a file".
- A count does nothing.
- A guest may not run it.

## <Plug>(debug.continue)

Lets the stopped program run on, as `:debug continue` does. Its key is
`<Leader>dc` in normal mode.

- With no session running it starts one instead, as `:debug start` does
  without a name.
- When `.fude/launch.toml` has one set of launch settings they start, and
  with several a list asks which.
- While the program runs and is not stopped, the message says so.
- A count does nothing.
- A guest may not run it.

```toml
[keymap.normal]
"<Leader>r" = "<Plug>(debug.continue)"
```

## <Plug>(debug.next)

Steps over the stopped program's line, so a call on it runs whole. Its
key is `<Leader>dn` in normal mode.

- With no session, the message says "no debug session runs".
- While the program runs it says the program is not stopped.
- A count does nothing, so `3<Leader>dn` steps once.
- A guest may not run it.

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
Its key is `<Leader>dq` in normal mode.

- The stopped line is no longer lit.
- The breakpoints show as ○ again.
- The message says "stopped debugging" and the launch settings' name.
- With no session, it says "no debug session runs".
- A guest may not run it.

## <Plug>(debug.panel)

Opens the `[debug]` window, as `:debug panel` does. Its key is
`<Leader>dp` in normal mode.

- The window lists the stopped thread's frames, with → on the chosen one,
  then the variables of that frame and the program's last lines of
  output.
- It shows anew each time the program stops.
- With no session, it says "no debug session runs".
- A guest may not run it.

## <Plug>(debug.eval)

Shows in a float the value the debug adapter gives at the chosen frame,
as `:debug eval` does in the message. Its key is `<Leader>de` in normal
and visual mode.

- In normal mode it evaluates the name under the cursor with its dots, so
  on `user.name` it takes all of it.
- In visual mode it evaluates the selected text.
- With no session, it says "no debug session runs".
- An error from the adapter shows in the message after the expression.
- A guest may not run it.

## <Plug>(debug.open)

Acts on the `[debug]` window's line under the cursor. `<CR>` runs it
there, from `[keymap.debug]`.

- On a frame it chooses that frame, shows its line and lists its
  variables.
- On a variable marked ▸ it shows its members, marked ▾ then.
- On a variable marked ▾ it hides its members.
- On any other line, and outside the panel, it does nothing.
- A guest may not run it.

```toml
[keymap.debug]
o = "<Plug>(debug.open)"
```

## <Plug>(diff.next)

`]c` by default. In a window in diff mode it goes to the first line of
the n-th change below the cursor's line, or to the last change when n is
past it.

- A change with no lines here starts on the line below its fill.
- With no change below, the message says "no change there".
- Outside diff mode it says "not in diff mode".

## <Plug>(diff.prev)

`[c` by default. It goes to the first line of the n-th change above the
cursor's line, or to the first change when n is past it, as
`<Plug>(diff.next)` does going down.

## <Plug>(diff.get)

`do` by default. It replaces the lines of the change on the cursor's line
with the other buffer's lines of it, as `:diffget` does.

- A change with no lines here is on the line below its fill.
- With more than two buffers in diff mode, n is the number of the buffer
  to take from, as `3do` takes buffer 3's.
- `do` is a key only in a window in diff mode, and there `d` waits for
  the key after it, so `dw` and `dd` still delete.

## <Plug>(diff.put)

`dp` by default. It replaces the other buffer's lines of the change on
the cursor's line with this buffer's, as `:diffput` does, or buffer n's
with a count.

## <Plug>(explorer.up)

Opens an explorer on the directory holding the current file, or on the
one above the current explorer. Its key is `-` in normal mode, in place
of the `-` motion.

- With n, it goes up n levels, except that `1-` lists the working
  directory.
- A buffer with no file lists the working directory too.
- The cursor lands on the entry that leads back to where it came from.

```toml
[keymap.normal]
"<Leader>e" = "<Plug>(explorer.up)"
```

## <Plug>(explorer.open)

Opens the entry on the cursor's line in the current window. `<CR>` runs
it in an explorer, from `[keymap.explorer]`.

- A file opens as `:e` opens it.
- A directory opens as an explorer of its own.
- An empty line does nothing.
- Outside an explorer the message says "not in an explorer".

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

`za` by default.

- When a fold that holds the cursor's line is closed, it opens n levels
  as `<Plug>(fold.open)` does.
- Elsewhere it closes n levels as `<Plug>(fold.close)` does, and says
  "no fold here" when there is none.

## <Plug>(fold.close)

`zc` by default. Of the folds that hold the cursor's line, outermost
first, it closes the last one open before the first closed one, which
with every fold open is the innermost.

- With a count it closes that many levels, outward.
- The closed folds inside stay closed, so `<Plug>(fold.open)` shows them
  again.
- The cursor goes to the first line of the fold it is in.
- With no fold there, the message says "no fold here".

Where folds go:

- Folds go by indent. A fold starts on a line followed by lines indented
  more, and holds them and the blank lines among them, so a line can be
  in folds within folds.
- In a file whose grammar says where it folds, such as markdown, Go or
  TOML, folds go there instead.
- A heading's section holds the sections under it, a function's fold
  holds its closing brace, and a TOML table folds under its header.

## <Plug>(fold.close_deep)

`zC` by default. Closes every fold that holds the cursor's line, as many
levels as there are, and the cursor goes to the first line of the
outermost.

## <Plug>(fold.open)

`zo` by default. Opens the outermost closed fold that holds the cursor's
line, and leaves the closed folds inside it closed.

- With a count it opens that many levels. The cursor sits on the first
  line of a closed fold, which the folds inside it do not hold, so a
  count opens more than one level only where folds start on the same
  line.
- On a line in no closed fold it does nothing.
- A fold `zf` made is gone once opened, and `<Plug>(fold.close)` folds as
  before after that.

## <Plug>(fold.open_deep)

`zO` by default. Opens the outermost closed fold that holds the cursor's
line and every closed fold inside it.

## <Plug>(fold.open_all)

`zR` by default. It opens every closed fold in the buffer, those `zf`
made too. Folds belong to the buffer, so every window on it shows them
open.

## <Plug>(fold.close_all)

`zM` by default. It closes every fold, the nested ones too, so
`<Plug>(fold.open)` opens them one level at a time. The cursor goes to
the first line of the fold it is in.

## <Plug>(fold.toggle_all)

`zA` by default.

- When a fold that holds the cursor's line is closed, it opens it and
  every fold inside it, as `<Plug>(fold.open_deep)` does.
- Elsewhere it closes every fold that holds the line, as
  `<Plug>(fold.close_deep)` does, and says "no fold here" when there is
  none.

## <Plug>(fold.view)

`zv` by default. Opens every closed fold that holds the cursor's line, so
the line shows. The closed folds inside them stay closed. On a line in no
closed fold it does nothing.

## <Plug>(fold.more)

`zm` by default. Folds n levels more, 1 without a count.

- A level is how deep a fold may be and stay open, the outermost folds
  being 1 deep.
- Every fold deeper than the level closes and the others open.
- The level starts one less than the depth of the shallowest closed fold,
  or with no fold closed at the deepest fold's depth, so `zm` after `zR`
  closes the innermost folds.
- At 0 it closes every fold, as `zM` does, and goes no lower.

## <Plug>(fold.less)

`zr` by default. Folds n levels less, 1 without a count, as
`<Plug>(fold.more)` folds more. At the deepest fold's depth every fold is
open, as after `zR`.

## <Plug>(fold.enable)

`zi` by default. Opens every closed fold and keeps where each was. Run
again, it closes those folds again, where the edits since have moved them.
With no fold closed and none kept, it does nothing.

## <Plug>(fold.next)

`zj` by default. It goes to the first line of the n-th fold below the
cursor's line, closed or open, nested ones too.

- A closed fold counts as one line, so the folds inside it are passed
  over.
- With fewer than n folds below, the cursor stays and the message says
  "no fold there".

## <Plug>(fold.prev)

`zk` by default. It goes to the last line of the n-th fold above the
cursor's line.

- A closed fold counts as one line, and the cursor lands on its first.
- With fewer than n folds above, the cursor stays and the message says
  "no fold there".

## <Plug>(fold.cycle)

`<Tab>` in a markdown file by default, help's too. On the first line of
a fold, it steps the fold from closed, to open with the folds inside it
closed, to open with every fold inside it open, and back to closed.

- A fold with none inside opens and closes.
- Off a fold's first line it does nothing, so `<Tab>` off a heading goes
  forward in the jump list, as it does in any other file.

## <Plug>(fold.cycle_all)

`<S-Tab>` in a markdown file by default, help's too. It steps the whole
buffer from every outermost fold closed, to every fold closed that holds
no other, to every fold open, and back. It skips a step that would leave
the folds as they are, so a buffer with no nested folds steps between
closed and open.

## <Plug>(git.next)

`]h` by default. It goes to the start of the n-th hunk below the cursor's
line, or to the last hunk when n is past it.

- A hunk is a run of lines that differ from the file as git's index has
  it, unsaved changes too.
- It does not wrap around.
- With no hunk below, the message says "no next hunk".
- Outside a git repository it says "not in a git repository".
- In a file with no changes it says "no hunks".

## <Plug>(git.prev)

`[h` by default. It goes to the start of the n-th hunk above the cursor's
line, or to the first when n is past it.

- From inside a hunk below its first line, that hunk's start counts as
  the first one above.
- With no hunk above, the message says "no previous hunk".

## <Plug>(git.first)

`[H` by default. It goes to the start of the first hunk in the file. The
count does nothing.

## <Plug>(git.last)

`]H` by default. It goes to the start of the last hunk in the file. The
count does nothing.

## <Plug>(git.preview)

Shows the hunk on the cursor's line in a float, its lines in git's index
after `-` and its lines now after `+`. No key is mapped to it.

- A hunk of deleted lines sits on the line above them.
- Off a hunk, the message says "no hunk on this line".

```toml
[keymap.normal]
"<Leader>gp" = "<Plug>(git.preview)"
```

## <Plug>(help.follow)

`<CR>` and `<C-]>` in a help window by default, which `[keymap.help]`
changes. It opens help at the entry named under the cursor, as `:h`
would.

- It takes the text under the cursor up to a blank on either side,
  without the backticks, parentheses, commas and periods around it.
- A name finds a command without its colon and a `<Plug>` without its
  brackets too.
- On a blank it does nothing.
- A name with no entry says "no help for" it.
- It works in any buffer once mapped.

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
from.

- The line runs as written, so a `<` in it is a `<`.
- On an empty line it only closes the window.
- The window's buffer goes with it, and any edits to the list are lost.
- Mapped in another window, it says "not in a history window" and does
  nothing.

## <Plug>(jump.start)

`s` in normal and visual mode by default. Each character typed next adds
to a text to find in what the window shows, ignoring case unless the text
has a capital.

- Every match is highlighted, and the nearest ones to the cursor get a
  label, a letter drawn over the character after the match.
- A label is never the character after a match, so typing more of the
  text never jumps.
- The status line shows what was typed, and a jump is in the jumplist.
- It takes no count.
- While it is mapped, `s` does not substitute a character as in Vim.

The keys while it runs:

- Typing a label moves the cursor to its match's start.
- `<CR>` moves the cursor to the nearest match.
- `<BS>` drops the last character.
- `<Esc>` leaves the cursor where it was.

## <Plug>(lens.run)

`grx` runs it while a language server is attached. Runs the code lens on
the cursor's line, the command its server gave it, such as running a test
or showing a function's references.

- A line with several shows them in a menu, each with its server's name,
  where `<CR>` or a digit runs one.
- A line with none says "no code lens on this line".
- A guest may not run one.

## <Plug>(links.open)

`gx` by default. It opens what is under the cursor with the system's
opener, `open` on macOS and `xdg-open` elsewhere.

- On a markdown link it takes the link's destination from anywhere on the
  link.
- Elsewhere it takes the text up to a blank on either side, without the
  brackets, quotes and marks around it.
- A URL or `mailto:` goes to the opener as it is.
- A path not absolute must name a file beside the current one or in the
  working directory, or the message says "no file named" it.
- With nothing under the cursor it says "no link or file under the
  cursor".
- Only the host runs it, as a guest's would open on the host's machine.

## <Plug>(links.file)

`gf` by default. It opens the file under the cursor in the window, found
as `<Plug>(links.open)` finds it.

- A `:12` after the path puts the cursor on line 12.
- It looks beside the current file first, then in the working directory.
- `~/` names the home folder.
- With no such file, the message says "no file named" it.

## <Plug>(lsp.hover)

`K` runs it. Asks the buffer's language servers about the symbol under
the cursor and shows their answer, rendered from markdown, in a float at
the cursor.

- While that float shows, running it again moves the cursor into the
  float, where it takes keys as any window does.
- An answer with no text says "no hover information".
- A buffer with no server says "no language server".

## <Plug>(lsp.definition)

`<C-]>` runs it. Jumps to the definition of the symbol under the cursor,
opening its file when it is another.

- When the servers name several definitions, they show as a list of
  places, which becomes the last list.
- It is mapped with or without a server, and without one it says "no
  language server".
- With nothing found it says "no definition found".

## <Plug>(lsp.declaration)

Jumps to the declaration of the symbol under the cursor, as
`<Plug>(lsp.definition)` jumps to its definition, such as a C function's
prototype in its header. Several show as a list of places, and none says
"no declaration found". No key runs it, so map one.

```toml
[keymap.normal]
gD = "<Plug>(lsp.declaration)"
```

## <Plug>(lsp.incoming_calls)

Lists the calls of the function under the cursor as a list of places, the
last list, each at the call in the caller's file with the caller's name.
No key runs it, so map one.

- When the server finds several things under the cursor, a menu offers
  them first, where `<CR>` or a digit picks one.
- With none it says "no incoming calls found".

```toml
[keymap.normal]
"<Leader>ci" = "<Plug>(lsp.incoming_calls)"
```

## <Plug>(lsp.outgoing_calls)

Lists the calls the function under the cursor makes, as
`<Plug>(lsp.incoming_calls)` lists its callers, each at the call in the
function's own file with the name of the function it calls.

## <Plug>(lsp.supertypes)

Lists the types the type under the cursor comes from, such as the
interfaces a class implements, as a list of places with each one's name
and detail. A menu offers the things under the cursor first when the
server finds several. No key runs it, so map one.

## <Plug>(lsp.subtypes)

Lists the types that come from the type under the cursor, as
`<Plug>(lsp.supertypes)` lists those it comes from.

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

`gO` runs it while a language server is attached. Lists the current
file's symbols in a window, each with its line, column, kind in brackets
and name, nested symbols indented under the one that holds them.

- `<CR>` goes to the symbol on the cursor's line.
- A file with none says "no symbols found".
- `:pick symbols` searches the symbols of the whole project instead.

## <Plug>(lsp.rename)

`grn` runs it while a language server is attached. Opens the command line
with `:rename` and the word under the cursor, to edit into the new name.

- `<CR>` sends it to the first server that offers rename, and the edits
  it returns change every file it names, as one undo step labelled
  rename.
- `<Esc>` leaves the name as it was.
- An empty name says "no new name".

```toml
[keymap.normal]
"<Leader>r" = "<Plug>(lsp.rename)"
```

## <Plug>(lsp.next_diagnostic)

`]d` runs it. Moves to the start of the n-th diagnostic after the cursor,
going round to the first after the last.

- The diagnostics that start there show in a float, each in its
  severity's colour with its source and code, until the next key.
- A buffer with none says "no diagnostics".

## <Plug>(lsp.prev_diagnostic)

`[d` runs it. Moves to the n-th diagnostic before the cursor, going round
to the last before the first, as `]d` moves forward.

## <Plug>(lsp.first_diagnostic)

`[D` runs it. Moves to the first diagnostic in the buffer and shows the
diagnostics that start there in a float, as `]d` does. The count does
nothing.

## <Plug>(lsp.last_diagnostic)

`]D` runs it. Moves to the last diagnostic in the buffer, as `[D` moves to
the first.

## <Plug>(lsp.diagnostics)

`<C-w>d` runs it. Shows every diagnostic on the cursor's line in a float,
in full, without moving the cursor. A line with none says "no diagnostics
on this line". It helps when `lsp.diagnostic_notes` is off, or a note is cut
at the window's edge.

## <Plug>(lsp.actions)

`gra` runs it, in normal mode and in visual mode, while a language server
is attached. Asks for the code actions at the cursor, or for the
selection's text in visual mode, with the diagnostics on those lines.

- The first server that answers fills a numbered menu at the cursor,
  which shows what the selected action would change.
- `<CR>` or the action's digit runs it, and any other key closes the
  menu.
- With none it says "no code actions".

## <Plug>(lsp.restart)

`r` runs it in the `:lsp` panel. Restarts the server whose part of the
panel holds the cursor, and opens its files in the new one.

- `:lsp restart` does the same for the current file's servers.
- A guest may not run it, nor the panel's other three.
- Outside the panel, it and the other three say "not in the language
  servers panel" and do nothing.

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
says which pin it is.

- A file pinned already says which pin it has.
- A buffer with no file, such as an explorer, is not pinned, and neither
  is a tenth file. Each says why.
- fude keeps the pins of each working directory across its runs.
- A guest's pins are its own and end with its session.

## <Plug>(pin.list)

`<Leader>h` by default, and `:pin list` does the same. Shows the pins in
the window, pin n on line n.

- The lines edit as text, and `:w` makes the pins what they say, in their
  order.
- A line taken out unpins its file, and a path typed in pins it.
- A line naming no file, a file named twice or a tenth line stops the
  write, and the message names the line.
- In the list, `<CR>` and `q` run `<Plug>(pin.open)` and
  `<Plug>(pin.close)`, which `[keymap.pins]` maps.

## <Plug>(pin.next)

Shows the file of the pin after the current file's, from the last back to
the first. No key maps it.

- From a file not pinned it goes to the first pin.
- With no pins it says so.
- A count does nothing.

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

- A path typed in but not written yet opens too.
- The list closes unless its lines have changes not written.
- On a blank line it does nothing.

## <Plug>(pin.close)

`q` in the list `<Plug>(pin.list)` shows. Closes the list. When its lines
have changes not written, it says so and the list stays until `:w`
writes them.

## <Plug>(pin.1)

`<Leader>1` by default, and `:pin 1` does the same. Shows pin 1's file in
the window, with the cursor where it last was in that file, even after
fude quit.

- With fewer pins it says there is no pin 1.
- A pin whose file is gone says so and stays a pin.
- A count does nothing.

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
list, so the list stays in view.

- In the last list, that place becomes the one `]q` and `[q` step from.
- An older list's window still goes to its own places.
- Past the list's last line it says the cursor is not in a list.

## <Plug>(list.next)

`]q` by default, and `:cnext` does the same. Goes to the next place of
the last list, or n places on, and says which place it is, of how many.

- Before any place was gone to, `]q` goes to the first.
- It never wraps, so past the last place it says there are no more and
  stays.
- It works after the list's window closes.
- With no list it says so.

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

## <Plug>(review.accept)

`a` in the `:review` window. Accepts the proposal under the cursor, as
`:accept n` does. On a line of no proposal it says so.

## <Plug>(review.reject)

`r` in the `:review` window. Rejects the proposal under the cursor, as
`:reject n` does.

## <Plug>(review.accept_all)

`A` in the `:review` window. Accepts every proposal the window shows, as
one undo step on each buffer, leaving the stale ones.

## <Plug>(review.reject_all)

`R` in the `:review` window. Rejects every proposal the window shows.

## <Plug>(review.open)

`<CR>` in the `:review` window. Goes to the proposal under the cursor, in
its file.

## <Plug>(session.restore)

`s` on the start screen, whose `s` line shows when the working directory
has a session. Opens the files of the session fude kept when it last quit
in this directory and builds its windows again, each with its cursor, top
line and options, and the same current window.

- A file that no longer opens is left out, and the message says why.
- With no session kept it says so.
- No key maps it in normal mode.

```toml
[keymap.normal]
"<Leader>R" = "<Plug>(session.restore)"
```

## <Plug>(spell.next)

`]s` by default. Goes to the start of the next word spell finds after the
cursor, wrapping from the end of the text to its start.

- A count does nothing.
- With `spell.enabled` off it says so, and `:set spell.enabled true`
  turns it on.
- While the word list is still being read it says so.
- With no misspelled words it says so.

## <Plug>(spell.prev)

`[s` by default. Goes to the start of the word spell finds before the
cursor, wrapping from the start of the text to its end, as
`<Plug>(spell.next)` does forward.

## <Plug>(spell.suggest)

`z=` by default. Lists up to 9 words of the word list that differ from
the one under the cursor by at most two letters added, taken out or
changed, closest first.

- The one picked replaces the word, keeping its capital first letter.
- If the text changed since, the word stays and the message says why.
- With no word under the cursor, or no close words, it says so.
- It works only with `spell.enabled` on.

## <Plug>(spell.good)

`zg` by default. Adds the word under the cursor to `words.txt` in fude's
config directory, so spell passes it from then on, and says so. With no
word under the cursor it says so, and it works only with `spell.enabled`
on.

## <Plug>(start.open)

`<CR>` on the start screen. Reads the key at the start of the cursor's
line and runs it as if typed, so `<CR>` on the `f  find a file` line runs
`f`, and on a recent file's line runs its digit.

- On a line that starts with no key of the screen, such as a heading or a
  blank line, it does nothing.
- `[keymap.start]` changes the screen's keys, and this follows them.
- A count does nothing.

```toml
[keymap.start]
"o" = "<Plug>(start.open)"
```

## <Plug>(start.config)

`c` on the start screen. Opens config.toml from fude's config directory
in the window, `$XDG_CONFIG_HOME/fude` or else `~/.config/fude`, and the
system's config folder on Windows.

- When the directory does not exist yet it makes it, so `:w` can write
  the new file.
- When it cannot, the message says why.
- It works from any window it is mapped in.

## <Plug>(start.1)

`1` on the start screen. Opens the first of the recent files the start
screen lists: the 9 newest under the working directory, or the 9 newest
anywhere when it has none.

- Mapped in normal mode it opens that file from any window, even when
  fude started with a file.
- When the list is shorter it says "no recent file 1".

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

Turns the `center` setting on or off, as `:center` does. No key runs it
by default.

- Centered, each window gets `color_column` columns of text, or 100
  without one, and the windows sit in the middle of a wider screen with
  blank margins on either side.
- When they need more than the screen they take its whole width.
- A guest may not run it.
- A count does nothing.

```toml
[keymap.normal]
"<Leader>z" = "<Plug>(theme.center)"
```

## <Plug>(timeline.earlier)

`[` in a timeline window. Moves the buffer's text to the state on the
line above the cursor's, then lists the timeline again with the cursor on
that state, so `[` again goes one further back.

- On state 0 it does nothing.
- It works only in the window `:timeline` opens. In any other window it
  says "not in a timeline".
- A count does nothing.

## <Plug>(timeline.later)

`]` in a timeline window. Moves the text to the state on the line below
the cursor's, as `<Plug>(timeline.earlier)` moves back. On the last state
it does nothing.

## <Plug>(timeline.diff)

`d` in a timeline window. Opens a window with the lines that differ
between the state under the cursor and the state the text is at, with 3
lines around each run.

- A line only the state under the cursor has comes after `-`, and a line
  only the text now has after `+`.
- The window's title names both states, such as `[diff 3 7]`, and `q`
  closes it.
- Outside a timeline window it says "not in a timeline".

## <Plug>(timeline.restore)

`<CR>` in a timeline window. Makes the text of the state under the cursor
the newest state, as `:restore` does.

- The states after it stay, so it loses nothing.
- The timeline lists again with the cursor on the new state.
- Outside a timeline window it says "not in a timeline".

## <Plug>(timeline.list)

Lists the timeline of the buffer `:timeline` last listed, with the cursor
on the state its text is at. `[`, `]` and `<CR>` run it after they move
the text, so the list shows where the text went. It has no key of its
own.

## <Plug>(window.left)

Moves to the window to the left. No key is mapped to it, since `<C-w>h`
moves only among fude's windows.

- Inside tmux, with no window that way, it moves to tmux's pane to the
  left instead, so one key crosses from fude into the next pane.
- Outside tmux, or with no pane either, it does nothing.
- A tmux that fails says why.
- A guest's session never moves tmux's panes.
- A count does nothing.

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
it was.

- Splitting, closing or moving a window, or moving to another, shows
  every window again first.
- A float does not zoom and says so.
- Floats show over a zoomed window.

## <Plug>(window.new)

`<C-w>n` by default. Opens a window below with a new empty buffer, or
above with `split_below` off, as `:new` does. A count makes the new window
that many rows tall, as `:resize` does.

## <Plug>(window.new.ctrl)

The same as `<Plug>(window.new)`, on `<C-w><C-n>`, so the key works with
Ctrl still held.

## <Plug>(window.taller)

`<C-w>+`. Makes the window n rows taller, 1 without a count.

- The rows come from the windows above and below it, each of which keeps
  at least a row and its status line.
- A window with no other window above or below it stays as it is.
- A float sizes itself, so it says so and stays.

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
