# Commands

A command is typed after `:` and runs on `<CR>`. Each answers to its full
name and to the short names its entry lists, and to no other part of its
name, so `:subst` is not a command. The commands that edit text take an
argument that does not start with a letter straight after the name, as
`:s/a/b/`, `:m0` and `:sort!`. Any other argument needs a space before it,
as `:d x`, `:b 2` and `:e #`, since `:b2` is not a command. `!` goes
straight after the name, as in `:q!`, and each entry says what it changes.
A `|` ends a command and starts the next, so `:w | q` writes and closes.
`:global`, `:vglobal`, `:normal`, `:!`, `:r !`, `:w !` and `:terminal` take
the `|` in their argument instead, and `\|` is a `|` of its own, as in a
pattern. In an entry's heading `|` means "or", as in `:read {file} |
!{cmd}`.

```keys
"|a\nb\nc" ":1d|$d<CR>" "|b"
"|ab" ":s/a/x/|s/b/y/<CR>" "|xy"
```

A range of lines may come before the name. Without one, a command acts on
the cursor's line, or on every line where its entry says so. `%` is every
line. Otherwise a range is one address, or two joined by `,` or `;`, and
fude swaps two given last line first. After `;` the cursor moves to the
first line, so the second counts from it, as `:5;+2` is lines 5 to 7. A
second address left out is the cursor's line. An address is one of these,
then any number of `+n` and `-n`:

- a line number. 0 is above the first line for `:move` and `:copy`, and
  the first line for the rest
- `.` for the cursor's line and `$` for the last
- `'a` for the line of mark a. `'<` and `'>` are the ends of the last
  selection, so `:` in visual mode starts the line with `'<,'>`
- `/pat/` for the next line after the cursor's that matches, and `?pat?`
  for the one before
- `+n` or `-n` alone, n lines from the cursor's

A count before `:` gives the range `.,.+n-1`. A range alone, as `:12`,
goes to the first non-blank of its last line, as a jump. A mark that is not
set, or a pattern that matches nowhere, stops the command and says so.

```keys
"|a\nb\nc" ":2,3d<CR>" "|a"
"|a\nb\nc\nd" ":2;+1d<CR>" "a\n|d"
"|a\nb\nc" ":.,$d<CR>" "|"
"|a\nb\nc" ":/c/d<CR>" "a\n|b"
"|a\nb\nc" ":+1d<CR>" "a\n|c"
"|a\nb\nc\nd" "jmajmb:'a,'bd<CR>" "a\n|d"
"|a\nb\nc\nd" "Vj:d<CR>" "|c\nd"
"|a\nb\nc" ":3,1d<CR>" "|"
"|a\nb\nc" "2:d<CR>" "|c"
"|a\nb\nc" ":3<CR>" "a\nb\n|c"
```

`<Tab>` puts the next completion of the word being typed in the line, and
`<S-Tab>` the one before. While the menu shows, `<C-n>` and `<C-p>` step
through it too, and else recall the history. After the last comes the
word as typed. A menu above the line ranks them as a fuzzy finder does, so
`:bde<Tab>` finds `:bdelete`. While the menu shows, `<CR>` runs its lit
item, `<C-y>` keeps the completion and `<C-e>` puts the typed word back.
With `autocomplete` on, the menu shows as the line is typed. The first word
completes as a command's name, and the entries say what each argument
completes as. In a path, as `:edit`, `:write`, `:cd`, `:read`, `:split`
and `:vsplit` take, `%` is the current file's path and `#` the one shown
before. `:p`, `:h`, `:t`, `:r` and `:e` after either make it the full
path, its directory, its name, the name without its extension, or the
extension. A `~` that starts a word is the home directory.

## :&

Runs the last substitute again on the range, or on the cursor's line, with
its pattern and replacement but none of its flags. The pattern is the last
substitute's, not the last search's. With no substitute yet it says "no
previous substitute". `:s` alone does the same.

```keys
"|a a\na a" ":s/a/b/g<CR>j:&<CR>" "b b\n|b a"
```

## :&&

Runs the last substitute again with its flags too, so a `g` from before
replaces every match again.

```keys
"|a a\na a" ":s/a/b/g<CR>j:&&<CR>" "b b\n|b b"
```

## :<

Takes `indent_width` columns of leading blanks off each line of the range,
as `<<` does, and once more for each further `<`. A line with less indent
loses all of it. The cursor goes to the first non-blank of the range's
last line. A count after it shifts that many lines from the range's last,
so `:< 3` shifts three lines, as `:.,.+2<` does.

```keys
"\t\t|x\ny" ":<lt><CR>" "\t|x\ny"
"\t\t|x\ny" ":<lt><lt><CR>" "|x\ny"
```

## :>

Adds `indent_width` columns of indent to each line of the range that is
not empty, as `>>` does, and once more for each further `>`. The indent is
spaces with `expand_tab` on, and else tabs as far as they go. The cursor
goes to the first non-blank of the range's last line. A count after it
shifts that many lines from the range's last, as `:> 2`.

```keys
"|a\nb" ":><CR>" "\t|a\nb"
"|a\nb" ":%>><CR>" "\t\ta\n\t\t|b"
```

## :accept

Applies proposal n, or without n the newest proposal on the current
buffer. A proposal is an edit that a client attached over RPC sent to show
as ghost text, which changes nothing until accepted. Accepting applies it
as one undo step, made by the client that proposed it. Several numbers
apply each, as one undo step on each buffer they change. An edit since
then that overlaps one makes it stale, and a stale one says "proposal n
is stale" and stays while the others apply. A number with no proposal
says "no proposal n", and applies none. A read-only guest may not accept
one. `:review` lists the proposals.

## :bufdo

Runs the command in each buffer being edited, from the lowest number up.
It shows each buffer in the current window in turn and runs the command
there, as typed after `:`, so `:bufdo %s/old/new/g` changes every buffer.
It passes help, lists, explorers, terminals and the other windows fude
writes. The last buffer stays shown, and the buffers stay unwritten until
`:wall`. When the command fails, the run stops there and the message says
why.

## :cquit

Quits fude as `:qa!` does, dropping every change, and fude exits with the
code n, or 1 without it. `:cq 0` exits as a plain quit does. When the
editor exits with a code other than 0, git drops the commit message. In a
session attached to a running fude it leaves, and fude goes on.

## :cd

Makes dir the working directory, or the home directory without one, and
shows the new one. Each buffer keeps its file, since fude makes its path
absolute first. Paths typed afterwards, the finder and grep start from the
new directory. `<Tab>` completes a path. A directory that does not exist
says why and changes nothing. A guest may not run it.

## :close

Closes the current window. Its buffer stays loaded with any changes, so
`:b` shows it again. In a float it closes the float, and the focus goes
back to the window the float opened from. A window whose program still
runs, such as a terminal's, hides instead, as `:hide` does, and the program
goes on. The last window does not close and says "cannot close the last
window". `:q` there quits.

## :config

`:config reload` reads config.toml again and never writes it. A setting the
file changed applies as `:set` would. An option it changed applies to every
window, and an option it did not change keeps what `:set` gave it. Its
keys and tables replace the old ones, so a key it no longer maps does what
fude does by default, and its plugins load again. A file with an error
changes nothing, and the message says where. Any other argument says
"config what? reload". A guest may not run it.

## :copy

Puts a copy of the range's lines below the line at address, which is any
address a range takes, or 0 for above the first line. The cursor goes to
the first non-blank of the last line copied. Without an address it says
"bad destination". `:t` is the short name Vim users know.

```keys
"|a\nb\nc" ":t$<CR>" "a\nb\nc\n|a"
"a\nb\n|c" ":1,2t0<CR>" "a\n|b\na\nb\nc"
"|a\nb\nc" ":t.<CR>" "a\n|a\nb\nc"
```

## :delete

Deletes the range's lines into register x, or as `dd` does without one.
The cursor goes to the first non-blank of the line that takes their place.
A count after it deletes that many lines from the range's last, so `:d 3`
deletes three lines, and `:d x 3` puts them in register x.

```keys
"a\n|b\nc" ":d<CR>" "a\n|c"
"|a\nb" ":d x<CR>\"xp" "b\n|a"
"|a\nb\nc" ":d 2<CR>" "|c"
```

## :delmarks

Deletes the marks named, as `:delm a`, `:delm abc` or `:delm a-d`, where
`a-d` names every mark from a to d. Spaces between them do not count.
`:delm!` deletes every mark from a to z. With no name and no `!` it says
so.

```keys
"|a\nb" "jmagg:delm a<CR>`a" "|a\nb"
"|a\nb" "jmamb:delm a-b<CR>gg`b" "|a\nb"
```

## :edit

Opens file in the current window, or shows the buffer that holds it
already. A file that does not exist yet opens empty, and `:w` creates it.
A directory opens as an explorer, and `#` shows the buffer the window
showed before. The buffer the window leaves stays loaded with its changes.

Without a file it reads the buffer's file again, as one undo step. It
refuses while the buffer has changes, and `:e!` drops them. In an explorer
it reads the directory again. `<Tab>` completes a path. A guest opens only
files under the project.

## :file

Says the buffer's name, whether it is modified, how many lines it has and
how far down the cursor's line is, as `<C-g>` does. With a file name it
first makes that the buffer's file, so the next `:w` writes there. The
old file stays as it was. It writes nothing itself, and `:saveas` names
and writes at once. A name that is a file already makes `:w` ask before
writing over it.

## :global

Runs the command after the pattern on each line of the range that matches
it, or of every line without a range. The pattern is a search's, and any
character but a letter or `!` may stand for the `/`s. An empty pattern takes the last search,
and the pattern becomes the last search and lights up. fude marks the
matching lines first and then runs the command on each, so a command that
deletes or adds lines does not upset the rest. The command is any command,
fude's and the modules' too, and it starts on the line with the cursor at
its start. The whole run is one undo step. A command that fails stops the
run on that line, so `:g/a/s/x/y/` stops at the first line without an x.
Without a command it does nothing more.

```keys
"|a1\nb\na2" ":g/a/d<CR>" "|b"
"|a1\nb\na2" ":g/a/s/$/!/<CR>" "a1!\nb\n|a2!"
"|a\nb\na" ":g/a/normal Ax<CR>" "ax\nb\na|x"
"|x\nab\nx" ":g/x/m0<CR>" "|x\nx\nab"
"|ax\na\nax" ":g/a/s/x/y/<CR>" "ay\n|a\nax"
```

## :join

Joins the range's lines into one, or the cursor's line and the next
without a range, as `J` does. Each joined line loses its leading blanks
and one space goes between, but none after a blank, before a `)` or for an
empty line. With `!` nothing goes between and no blanks go. The cursor goes
to the joined line's first non-blank. On the last line it does nothing. A
count after it joins that many lines from the range's last, as `:j 3`.

```keys
"|a\nb\nc" ":j<CR>" "|a b\nc"
"|a\nb\nc" ":1,3j<CR>" "|a b c"
"|a\nb\nc" ":j 3<CR>" "|a b c"
"|a\n  b" ":j!<CR>" "|a  b"
```

## :mark

Sets mark x at the start of the range's last line, or of the cursor's, so
`` `x `` and `'x` go there. `:k` does the same, and takes the mark
straight after it, as `:ka`. The mark is a letter from a to z, and any
other says so.

```keys
"|a\nb\nc" ":3mark a<CR>`a" "a\nb\n|c"
"|a\nb\nc" ":2ka<CR>'a" "a\n|b\nc"
```

## :move

Moves the range's lines below the line at address, or above the first line
for 0. The cursor goes to the first non-blank of the last line moved. Lines
cannot move below one of their own, which says "cannot move lines into
themselves". Without an address it says "bad destination".

```keys
"|a\nb\nc" ":m$<CR>" "b\nc\n|a"
"a\nb\n|c" ":m0<CR>" "|c\na\nb"
"|a\nb\nc" ":m+1<CR>" "b\n|a\nc"
```

## :nohlsearch

Takes the highlight off the last search's matches in every window. The
pattern stays, so `n` still finds it and lights them again, as any search
does. `<C-l>` does the same.

## :normal

Types the keys on each line of the range, or the cursor's line, as typed
in normal mode with the cursor at the line's start. Mappings apply, as for
keys typed. After each line fude types `<Esc>` until normal mode is idle
again, so `Ax` adds an x and leaves insert mode. The whole run is one
undo step. `:normal!` types the keys without the mappings.

```keys
"|a\nb" ":%norm Ax<CR>" "ax\nb|x"
"|a b\nc d" ":%normal wD<CR>" "a \nc| "
```

## :pick

Opens the finder on a source's items. Typing narrows them, and the one
picked does what its source says. Text after the source starts the query,
as `:pick grep TODO`. Without a source the finder lists the sources, and
picking one opens it. The sources come from modules:

- `files`, the working directory's files, without those git ignores
- `grep`, the lines of those files that hold the query
- `buffers`, the open buffers that have a file
- `recent`, the files windows showed last
- `diagnostics` and `symbols`, from the language servers
- `help`, `keymaps` and `commands`, which open help, run a key or run a
  command
- `tasks` and `terminals`, which run a task or show a terminal

A plugin may add more. `<Tab>` completes the source, and a name no source
has says which there are. In the finder `<C-q>` closes it and makes its
matches the last list of places.

`:pick resume` opens the finder that closed last again, with the query
typed in it, its matches and the one selected, so `<CR>` picks what was
selected. Any list the finder showed resumes, a module's such as the
debug launch list too. A list that asks a question with a prompt, such as
the spellings `z=` offers, does not. Before any finder it says "no finder
to resume".

## :plugin

`:plugin reload` reads the `[[plugin]]` tables of config.toml again, closes
every plugin with all it added, and loads them anew, a rebuilt plugin from
its new file. A plugin the file no longer lists goes, with its commands,
keys, operators and objects. Any other argument says "plugin what?
reload". A guest may not run it.

## :put

Puts register x, or the register `p` puts without one, on lines of its own
below the range's last line, or the cursor's. Text yanked as characters
goes on a line too. With `!`, or line 0 as `:0put`, the lines go above
instead. The cursor goes to the first non-blank of the last line put. An
empty register says so.

```keys
"|a\nb" "yy:put<CR>" "a\n|a\nb"
"|a\nb" "yy:$put<CR>" "a\nb\n|a"
"a\n|b" "yy:put!<CR>" "a\n|b\nb"
"a\n|b" "yiw:0put<CR>" "|b\na\nb"
```

## :quit

Closes the current window, as `:close` does, and in the last window quits
fude. Quitting refuses while any buffer has changes and names one, and
`:q!` quits anyway and drops them. A float closes, and a window whose
program runs hides. In a session attached to a running fude, the last
window's `:q` leaves, and fude goes on for the other sessions.

## :quitall

Quits fude whatever the windows, closing its files, modules and language
servers. It refuses while any buffer has changes and names one, and
`:qa!` drops every change. In a session attached to a running fude it
leaves, and fude goes on for the other sessions.

## :redo

Redoes one change, as `<C-r>` does.

```keys
"|ab" "x:undo<CR>:redo<CR>" "|b"
```

## :reject

Drops proposal n, or without n the newest proposal on the current buffer,
or each of several numbers. The text stays as it was, and the timeline
gets no state. It drops a stale proposal too. A number with no proposal
says "no proposal n", and drops none.

## :restart

Replaces fude with a new fude that loads config.toml, plugins, grammars
and language servers afresh. The new one opens the same files and splits,
each window with its cursor, top line and options, the same current
window and every unsaved change. `ZR` does the same. It runs only in the
terminal fude started in, and a client attached over RPC must attach
again. It ends a share and every guest's session. A guest may not run it.

## :retab

Rewrites the leading blanks of the range's lines, or of every line, so
each keeps its width. With `expand_tab` on they become spaces. With it off
`:retab` does nothing, and `:retab!` makes them tabs as far as they go,
then spaces. A tab is `tab_width` columns. Blanks after the first
non-blank stay as they are, and a width after the name does nothing. The
cursor stays.

```keys
"        |x" ":retab!<CR>" "\t|x"
```

## :saveas

Makes the file the buffer's file and writes the buffer there, as `:file`
then `:w` would. From then on `:w` writes to the new file, and the old one
stays as it was. It refuses a file that is already there, and `:saveas!`
writes over it.

## :select

Selects every node of the buffer's syntax tree that a capture of the query
names, within the range's lines, or every line without a range. The query
is in the syntax engine's query language, as `(identifier) @name`. Each
node becomes a region of one selection, so `d`, `x`, `y` and the
operators of modules act on every one at once, and `c` types into all of
them until `<Esc>`. Any other key keeps the first region alone. In visual
mode `:` fills in the selection's lines as the range. A file with no
grammar says "no syntax for this file", and a query that finds nothing
says "the query selected nothing".

## :set

`:set name value` sets an option or a setting of config.toml in the
running fude, as config.toml would, and never writes the file. A switch
takes `true` or `false`, a number takes 0 or more, and a list takes its
items apart by commas. An option changes the current window and the
windows opened after it. A setting that is not a window's changes every
session, and a guest may not change one. `:set name` shows the value, and
`:set` alone lists every option and setting with its value in a window.
A name or value fude does not know changes nothing and says why.

`:set filetype yaml` gives the current buffer another filetype until it
closes, with that filetype's grammar, language servers and table in
config.toml, and `:set filetype` shows the one it has.

There is no `:set number` to turn a switch on, no `no` before a name and
no `name=value`. `<Tab>` completes the name, then `true` and `false` for a
switch, the themes for `theme`, the filetypes for `filetype`, or the
sources for `completion_sources`.

```toml
# config.toml sets the start values, as :set relative_number true would
relative_number = true
completion_sources = ["lsp", "buffer"]
```

## :share

Shows a new share string and puts the `fude join` command that uses it on
the clipboard. A string lets one guest in, so each further guest needs a
new one. fude must have started with `fude share`, and else it says so.
A guest may not run it.

## :sort

Sorts the range's lines, or every line, by their text. `i` ignores case.
`n` sorts by the first number in each line, with lines that have none
first, in their order. `u` keeps the first of lines that sort the same,
so with `n` that is the same number. `!` reverses the order, and comes
before the other flags. The flags combine, as `:sort! nu`. The cursor
goes to the start of the range's first line. The patterns and other flags
Vim takes do nothing here.

```keys
"c\n|a\nb" ":sort<CR>" "|a\nb\nc"
"c\n|a\nb" ":sort!<CR>" "|c\nb\na"
"x10\nx9\n|y" ":sort n<CR>" "|y\nx9\nx10"
"b\nB\n|a" ":sort i<CR>" "|a\nb\nB"
"b\na\n|b" ":sort u<CR>" "|a\nb"
```

## :substitute

Replaces the first match of the pattern on each line of the range, or of
the cursor's line, with the replacement. The pattern is a search's, and any
character but a letter may stand for the `/`s. An empty pattern takes the last search,
and a missing replacement deletes the match. The flags are `g` for every
match on a line, `i` to ignore case, `I` to match it, `n` to count the
matches and change nothing, and `c` to ask at each match. While it asks,
the match shows lit and `y` replaces it, `n` skips it, `a` replaces it
and every one after, `l` replaces it and stops, and `q`, `<Esc>` or
`<C-c>` stop. All it replaces is one undo step. In the replacement:

- `&` and `\0` are the match, and `\1` to `\9` its groups
- `~` is the last substitute's replacement
- `\u` and `\l` make the next character upper or lower case, `\U` and `\L`
  all that follow, until `\E` or `\e`
- `\r` breaks the line and `\t` is a tab
- `\` before any other character is that character, as `\&` and `\/`

While the line is typed, the window shows each match struck out with its
replacement after it, and the text changes only on `<CR>`. The cursor goes
to the first non-blank of the last line changed. A pattern that matches
nowhere says "pattern not found". The whole command is one undo step. `:s`
alone repeats the last one, as `:&` does, and `&` in normal mode repeats it
with its flags, as `:&&` does.

```keys
"|a a\na" ":s/a/b/<CR>" "|b a\na"
"|a a\na" ":%s/a/b/g<CR>" "b b\n|b"
"|a a\na" ":%s/a/b/gc<CR>yny" "b a\n|b"
"|a a\na" ":%s/a/b/gn<CR>" "|a a\na"
"|a/b" ":s#/#-#<CR>" "|a-b"
"|a b" ":s/\\(a\\) \\(b\\)/\\2 \\1/<CR>" "|b a"
"|ab" ":s/a/\\u&/<CR>" "|Ab"
"|a b" ":s/ /\\r/<CR>" "a\n|b"
"|Ab" ":s/a/x/i<CR>" "|xb"
"|ab ab" ":s/a/[&]/<CR>:s/b/~/<CR>" "|[a][b] ab"
"|abc" ":s/b//<CR>" "|ac"
```

## :undo

Undoes one change, as `u` does.

```keys
"|ab" "x:undo<CR>" "|ab"
```

## :vglobal

Runs the command on each line that does not match the pattern, as
`:global` does on those that match. `:g!` is the same.

```keys
"|a1\nb\na2" ":v/a/d<CR>" "a1\n|a2"
"|a1\nb\na2" ":g!/a/d<CR>" "a1\n|a2"
```

## :windo

Runs the command in each window, from the top left. It moves to each
window in turn and runs the command there, as typed after `:`, so
`:windo set number true` numbers the lines of every window. The last
window stays current. When the command fails, the run stops there and the
message says why.

## :wall

Writes every buffer with changes, one after the other, and stops at the
first that fails. A file changed on disk since fude read it asks before
fude writes over it, and `:wa!` writes without asking.

## :wq

Writes the buffer, as `:w` does, then closes the window as `:q` does,
quitting in the last one. When the write fails or is declined, nothing
closes. `:wq!` writes without asking about a file changed on disk, and
quits even while other buffers have changes, dropping them.

## :wqall

Writes every buffer with changes, as `:wall` does, then quits once every
write is done. A write that fails stops it, and fude stays. `:wqa!` writes
without asking about files changed on disk and quits anyway.

## :write

Writes the buffer to its file, through a temporary file, so a write that
fails leaves the file as it was and says why. Modules may change the text first, as
`format_on_save` does. A file changed on disk since fude read it asks
before fude writes over it, and `:w!` writes without asking. A buffer with
no file says "no file name", and a read-only file says so. A range does
not narrow what it writes.

`:w file` writes a copy to file, and the buffer stays on its own file and
stays modified. It refuses to write over a file that exists, and `:w!
file` writes over it. `<Tab>` completes a path. In an explorer or a pin
list, `:w` makes the directory or the pins what the lines say.

`:w !cmd` writes nothing. It gives the shell command the range's lines,
or every line, and shows what it writes, as `:!` does. It comes with the
shell module.

## :xall

The same as `:wqall`.

## :xit

Writes the buffer only if it has changes, then closes the window as `:q`
does, quitting in the last one. `ZZ` does the same. `:x!` writes without
asking about a file changed on disk, and quits even while other buffers
have changes.

## :yank

Yanks the range's lines into register x, or as `yy` does without one. The
cursor stays. As with `:delete`, a count after it yanks that many lines
from the range's last, as `:y 3` or `:y x 3`.

```keys
"|a\nb" ":y<CR>p" "a\n|a\nb"
"|a\nb" ":2y<CR>P" "|b\na\nb"
"|a\nb" ":%y x<CR>G\"xp" "a\nb\n|a\nb"
```

## :!

`:!cmd` runs cmd in the shell and shows what it writes in the message
line, or in a window below when it is more than 10 lines. The shell is
`$SHELL -c`, or `/bin/sh` without `SHELL`, and on Windows PowerShell
without its profile. It runs in the working directory. `%` in cmd is the
buffer's file and `#` the alternate one, with the modifiers `:e` takes,
as `%:t`, and `\%` is a `%` of its own.

With a range, `:{range}!cmd` puts the range's lines through cmd, as its
input, and puts what it writes in their place. `!{motion}` and `!!` in
normal mode, and `!` in visual mode, start the command line with the range
and `!`. Lines that change while cmd runs keep the change, and the message
says so. A command that fails, or exits with a code other than 0, changes
nothing and the message shows its error output. A guest may not run it.

```toml
# the window of long output opens on the right
[open]
shell = "right"
```

## :bdelete

Closes the buffer with that number, or whose name holds the text, or the
current buffer without one. It refuses while the buffer has changes, and
`:bd!` drops them. A window that showed the buffer shows the one it showed
before, or else the next buffer being edited, and closing the last buffer
leaves a new empty one. A range does not close more buffers. `<Tab>`
completes the buffers, each as its number then its path.

## :blame

Turns the blame note on the cursor's line on, or off when it is on. The
note shows dim after the line: who last changed it, how long ago, and the
commit's summary, or "not committed yet" for a line changed since. It
follows the cursor and the edits, and git reads the buffer's text, so
unsaved lines still line up. Outside a git repository, or for a buffer
with no file, it turns on and shows nothing.

## :bnext

Shows the next buffer being edited in the current window, after the last
going on from the first. It passes help, lists, explorers, terminals and
the other windows fude writes. The buffer the window leaves stays loaded
with its changes.

## :bprevious

Shows the buffer before in the current window, as `:bnext` shows the next,
before the first going on from the last.

## :buffer

Shows the buffer with that number in the current window, as `:b 2` does,
or the one whose name holds the text. Text that more than one name
holds says so. `#` is the buffer the window showed before,
as `<C-^>` shows. `:ls` lists the numbers. `<Tab>` completes the buffers,
each as its number then its path. The buffer the window leaves stays
loaded with its changes.

## :buffers

Lists the buffers in the message line, each as its number, `%` for the
current window's, `+` for one with changes, and its name in quotes. `:b`
takes the number.

## :cc

Goes to place n of the last list of places, or without n to the place
last gone to, or the first. The message says which place it is, of how
many, with its text. A number past the list's ends says "no more places".

## :cclose

Closes the window of the last list of places. The list stays, so
`:cnext` and `:copen` still work.

## :cdo

Runs the command at each place of the last list of places, as `:cfdo` does
once in each file. It opens each place's file, puts the cursor on the
place and runs the command there, as typed after `:`. Each place stays on
its text as the command edits around it. The buffers stay unwritten, and
`:wall` writes them. The message says how many places and files it ran
in. When `:s` or another of the commands that edit text fails at a place,
the run stops there and the message says why.

## :center

Turns centering on or off, as `:set center` would. Centered, each window
gets `color_column` columns of text, or 100 without one, and its gutter,
and the windows sit in the middle of a wider screen with blank margins.
When they need more than the screen they take its whole width, and
`:center` says so. A guest may not run it.

```toml
center = true
color_column = 80
```

## :cfdo

Runs the command once in each file of the last list of places, at its
first place there, as `:cdo` runs it at every place. The command often
takes every line itself, as `:cfdo %s/old/new/g`.

## :cfirst

Goes to the first place of the last list of places, as `[Q` does.

## :changes

Lists where the buffer changed in a window, the newest last, each with
its line, column and the text of its line. `<CR>` on one goes there. The
list becomes the last list of places, so `:cnext` steps through it too.
With no change it says "no changes".

## :clast

Goes to the last place of the last list of places, as `]Q` does.

## :cnext

Goes to the next place of the last list of places, as `]q` does, in a
window that shows a file, not in the list's own window. The message says
which place it is, of how many, with its text. After the last place it
says "no more places", without going on from the first. It takes no
count, and `]q` does. With no list it says "no list of places".

The last list is the latest of: the places of a language server's answer,
such as references, `:marks`, `:jumps` and `:changes`, a finder's matches
after `<C-q>`, and the places a task's output names.

## :copen

Shows the last list of places in a window below, the cursor on the place
last gone to. `<CR>` on a line goes to its place, and `q` closes the
window. When a window shows the list already, it goes there.

```toml
[open]
list = "right"

[keymap.list]
"o" = "<Plug>(list.open)"
```

## :cprevious

Goes to the place before in the last list of places, as `[q` does and as
`:cnext` goes to the next. Before the first it says "no more places".

## :debug

Debugs a program through a debug adapter, and acts on the session it
starts. The word after `:debug` says what:

- `start [name]` starts the launch settings called name, or the only ones,
  or else lists them to pick from
- `stop` ends the session
- `break` sets a breakpoint on the cursor's line, or takes it off. Its
  sign is hollow until the adapter takes it, and full after
- `continue`, `next`, `in` and `out` go on, step over, step in or step
  out. `continue` with no session starts one, as `start` does
- `panel` opens a window of the stopped program's frames, variables and
  output. `<CR>` there chooses a frame or opens a variable's members
- `eval expr` shows the value of expr at the chosen frame

When the program stops, its line lights with an arrow in the sign column
and the message says why. The launch settings are tables of
`.fude/launch.toml` under the working directory, and the adapter is a
table of config.toml. `{file}`, `{dir}` and `{project}` in a launch's
`args` become the current file, its directory and the working directory.
`<Tab>` completes the word, and after `start` the launch names. A guest
may not run it.

```toml
# .fude/launch.toml
[launch.app]
adapter = "dlv"
request = "launch"
args = { program = "{dir}" }
```

```toml
# config.toml
[debug.dlv]
command = ["dlv", "dap", "--listen", "127.0.0.1:{port}"]

[filetype.go]
debug = ["dlv"]
```

## :diff

Shows the lines that differ between states n and m of the buffer's text,
or between state n and now without m, in a window below. The numbers are
those `:timeline` lists, and in the timeline's window n is the state under
the cursor. Each part shows three lines around it, a removed line marked
`-` and an added one `+`. Two states with the same text show "no
difference". A number with no state says "no such state", and no number
outside the timeline's window says "not in a timeline". `q` closes the
window.

## :earlier

Goes n states back through the buffer's text, 1 without n, as `g-` does.
The states are in the order they were made, across every branch the undo
tree has. n may be a time instead: `10s`, `5m`, `2h` or `1d` goes to the
last state made at least that long before the current one. `nf` goes back
n writes, and with changes since the last write that write counts as the
first. Past the oldest state it stops at the text before any change. It
makes no new state, and anything else says "bad count".

## :enew

Shows a new empty buffer with no file in the current window. The buffer
the window leaves stays loaded with its changes. `:w file` gives the new
one a file.

## :follow

Makes the session's windows show what the session called name shows: its
splits, its current window, and each window's buffer, top line and cursor.
They follow it as it moves, until a key or a paste here ends it, which
says "stopped following name". When that session leaves, following stops
and says so. `<Tab>` completes the other sessions' names. A name no other
session has says "no session is called name". A guest's window keeps its
own buffer where the other shows one outside the project.

## :format

Formats the buffer, or the lines of a range, as a write does with
`format_on_save` on, and does not write. The formatters are those the
filetype's table names, each run in turn on the text, and only the first
one installed with `format_first`. Without one, the language server
formats it. Only the lines of the range change, and the whole format is
one undo step. The message names a formatter that is not installed, and
one that fails or takes longer than `format_timeout` leaves the text
as it was and says why. A file nothing formats says "nothing formats this
file". `gq` formats the lines a motion covers the same way.

```toml
[filetype.go]
format = ["gofmt"]

[formatter.prettier]
command = ["prettier", "--stdin-filepath", "{path}"]
```

## :gitdiff

Shows, in a window below, the lines of the current buffer that differ from
the file in git's index, unsaved changes too, with three lines around each
part. A removed line starts with `-` and an added one with `+`. With no
difference it says "no changes from git's index", and a file git does not
track says "not in a git repository". `q` closes the window.

## :grammar

Lists the grammars in a window, each built, outdated (built at an older
pin), building, not built, or failed with why. `:grammar install
{filetype}` builds that one again, even after it failed. `:grammar update`
builds again every grammar that is outdated, or says each is up to date.
A build says when it starts and ends, and a build that needs a missing tool
names it. `<Tab>` completes `update` and `install` with each filetype. A
guest may list the grammars but not build them.

## :help

Opens help in a window. Without an argument it opens at the index of the
guide and the reference. `:h name` goes to a page of the guide, a section
of the reference such as `commands`, or the entry for a command (`:w`), a
key (`w`, `<Leader>w`), a `<Plug>` command, an option or a setting.
`<CR>` on a name in help goes to it, and `q` closes the window. `<Tab>`
completes the names. A name help does not know says "no help for name".

## :hide

Takes the current window out of sight. It keeps its buffer, its cursor
and any program running in it, and the window before it, or else the one
after, takes the focus. A terminal's hidden window shows again in the place
it had when its terminal shows, as `:terminal n` or `:pick terminals` do.
The last window shown says "cannot hide the last window", and a window a
module locked in place says it cannot move.

## :jumps

Lists the window's jumps in a window, each numbered by how far back it
is, with its buffer, line, column and the text of its line. `<CR>` on one
goes there. The list becomes the last list of places. With no jump it says
"no jumps".

## :kick

Ends the session of the guest called name. The guest's client says the
host ended it, and the host's message says whose session ended. `<Tab>`
completes the guests' names. Without a name it says "kick whom? :kick
name", and a name no guest has says so. A guest may not run it.

## :later

Goes n states forward through the buffer's text, 1 without n, as `g+`
does, as `:earlier` goes back. A time such as `5m` goes to the first state
made at least that long after the current one, or the newest. `nf` goes
to the nth write after the current state, or the newest state.

## :lsp

Without an argument it opens a float with a part for each language server
config.toml sets: its state, its version, its filetypes, and its roots and
files. The state is running, starting, stopped, disabled, failed with why,
not installed, or no files. In it `r`, `s`, `d` and `e` restart, stop,
disable or enable the server under the cursor, and `q` closes it.

With a word, it acts on every server set for the current file's
filetype, or with a server's name after the word, on that server:

- `restart` stops them and starts them again, one that was not installed
  too
- `stop` stops them, and opening a file one serves starts it again
- `disable` stops them and keeps them off for every file until `:lsp
  enable`, even through `restart`
- `enable` starts them again for the open files

`<Tab>` completes the word, and after it the servers' names. A file no
server is set for says so, and a guest may only open the float.

```toml
[lsp.gopls]
command = ["gopls"]

[filetype.go]
lsp = ["gopls"]
```

## :marks

Lists the buffer's marks in a window, each with its name, line, column and
the text of its line. `<CR>` on one goes there. The list becomes the last
list of places. With no mark it says "no marks".

## :messages

Lists the messages the session showed, the last 200, oldest first, in a
window, since a message goes away with the next key. `:messages yank`
puts the last one, whole, on the clipboard, as `"+y` does, so a message
cut short copies as one. `<Tab>` completes `yank`, and any other argument
says what it takes.

## :new

Opens a window with a new empty buffer that has no file, below the current
window, or above it with `split_below` off. `[open] window` in config.toml
or `:window` before it puts it elsewhere. It takes no file, and `:split
file` opens one.

## :only

Closes every window but the current one, splits and floats alike, and
keeps the floats the current one opened from. The buffers stay loaded with
their changes, so nothing asks to write first. A window whose program
runs hides, as closing it does, and shows again where it was. With one
window it does nothing.

## :pin

Without an argument it pins the current file, as `<Leader>a` does, and
says its number. A pin is a file to come back to, at the cursor it last
had, kept for the working directory across sessions. There are at most 9.

- `:pin n`, for n from 1 to 9, shows pin n's file at its cursor, as
  `<Leader>n` does
- `:pin remove` takes the current file off, and the pins after it move up
  a number
- `:pin list` opens the pins in a window, pin n on line n. The lines edit
  as text, and `:w` makes the pins what they say

A buffer with no file has nothing to pin, and a file pinned already says
its number. Any other argument says what `:pin` takes. A guest pins only
files under the project, and its pins last as long as its session.

```toml
# open the pins and their files in a float
[open]
pin = "float"
```

## :plug

Runs `<Plug>(name)` as a key mapped to it does, for fude's plugs and the
keys modules and plugins add. `<Tab>` completes the names, and `:pick
commands` lists each as `:plug name` with what it does. A name that is no
plug says "no plug is called name".

## :pwd

Shows the working directory in the message line. `:cd` changes it.

## :read

`:r file` puts the file's text below the cursor's line, or below the last
line of a range. `:r !cmd` puts there what cmd writes, run as `:!` runs
it. A file that cannot be read says why. `<Tab>` completes a path. A
guest reads only files under the project, and may not run a command.

## :recover

Brings back the newest change that a session left unsaved when it did not
close, such as after a crash, as the buffer's text. It makes no new state.
With nothing left that way it says "nothing to recover".

## :registers

Lists every register that holds text in the message line, each as its
name and its text, with `^J` for a line break. `:reg ab` lists only the
registers a and b.

## :rename

Renames the symbol under the cursor in every file through the language
server, to the name after `:rename`. `grn` starts the command line with
`:rename` and the word under the cursor. Files without a buffer open in one and stay unwritten. The
message says how many files changed, or why one was left alone. Without a
name it says "no new name", and a buffer no server offers renames for says
so.

## :resize

Makes the window n rows tall, or n rows taller with `+n` and shorter with
`-n`. Without n, or with 0, it makes the window as tall as it can be.
Every other window keeps at least a row and its status line. A window with
no split above or below it does not change, and a float says it sizes
itself. Anything else after the name says "not a size".

## :restore

Makes the text of state n the newest text, as a new state, so nothing is
lost and `u` goes back. In the timeline's window n is the state under the
cursor, which `<CR>` restores too. A state the text already is says so,
and a number with no state says "no such state". With no number outside
the timeline's window it says "not in a timeline".

## :review

Shows every proposal clients made in a window, by file. Each shows its
number, its line, who proposed it and whether it is stale, then the lines
it changes, those it removes after `-` and those it adds after `+`. `a`
accepts the proposal under the cursor and `r` rejects it, `A` accepts
every one the window shows and `R` rejects them, and `<CR>` goes to it.
The window follows as proposals come, go and go stale. With none it says
"no proposals".

```toml
[keymap.review]
"y" = "<Plug>(review.accept)"
```

## :split

Opens a window below the current one, or above with `split_below` off,
showing the same buffer, then opens file in it as `:edit` does. `[open]
window` in config.toml or `:window` before it puts the window elsewhere.
`<Tab>` completes a path, and a guest opens only files under the project.

```toml
split_below = false
split_right = false
```

## :task

`:task name` runs that task in a terminal window below, and the cursor
stays where it was. `:task` alone runs the last task run again, with its
`{file}`, `{dir}`, `{line}` and `{word}` from the current buffer. The tasks
come, in this order, from `[task.<name>]` tables of config.toml and of
`.fude/tasks.toml`, the recipes of a justfile, the targets of a Makefile
and the scripts of package.json. `{file}`, `{dir}`, `{line}` and `{word}`
in a task's command become the current file, its directory, the cursor's
line and the word under the cursor.

The window's title shows a tick and the time taken when the task ends, or
a cross and its exit code. Running a task again closes the last run's
window first, and a task still running shows its window again instead of
running twice. Places in its output, as `path:line:col:`, become the last
list of places, and `]q` goes to the first. `<Tab>` completes the names,
and `:pick tasks` lists them. A guest may not run one.

```toml
[task.test]
command = "go test ./..."

[task.run-file]
command = "go run {file}"
```

## :terminal

Shows shell 1 in a terminal window below, starting it the first time, and
hides it when the current window shows it. `:terminal n` does the same for
shell n, as `:b n` names a buffer. Shells are numbered from 1. `<Tab>`
completes the shells that started, each as its number then `shell`.
`:terminal cmd` runs cmd through the shell in a new window each time, and
when it ends the window stays and its title shows how it exited. A shell's
window closes when the shell exits.

Keys typed in the window go to the program while in terminal mode.
`<C-\><C-n>` or `<Esc><Esc>` leaves terminal mode for normal mode, and
`i` goes back. The window is `terminal_height` percent of the screen tall.
When fude listens on a socket, the program gets it as `FUDE_SOCKET`. A
guest may not run it.

```toml
terminal_height = 40

[open]
terminal = "right"
```

## :theme

`:theme dark` and `:theme light` switch every client to that theme until
fude quits, as `:set theme` does. A path to a base16 scheme file
switches to that scheme, and one that does not load says why and leaves
the theme as it was. `:theme` alone shows the theme in use. `<Tab>`
completes `dark`, `light` and the scheme config.toml names. A guest may
not run it.

```toml
theme = "~/.config/fude/gruvbox.yaml"

[colors]
bg = "#1d2021"
```

## :timeline

Lists every state of the current buffer's text in a window below, one a
line: its number, the time it was made and the command that made it.
State 0 is the text before any change. A `*` marks the current state, and
a state made from one other than the line above it says which. In the
window `[` and `]` go one state back or forward from the one under the
cursor, `d` shows the lines that differ from it to now, `<CR>` restores it
as a new state, and `q` closes the window.

```toml
# open the timeline, and its diffs, in a float
[open]
timeline = "float"
```

## :update

Writes the buffer as `:w` does, but only when it has changes. `:up!`
writes over a file changed on disk without asking.

## :vertical

Runs the command after it, and the window it opens goes right of the
current one, or left with `split_right` off. So `:vertical split` splits
as `:vsplit` does, `:vertical new` opens as `:vnew` does, and `:vertical
help` shows help beside the window.

`:vertical resize` makes the window n columns wide, or n columns wider
with `+n` and narrower with `-n`, as `:resize` does with rows. Without n it
makes the window as wide as it can be. Every other window keeps at least a
column.

## :vnew

Opens a window with a new empty buffer that has no file, to the right of
the current window, or to the left with `split_right` off, as `:new` does
below.

## :vsplit

Opens a window to the right of the current one, or to the left with
`split_right` off, then opens file in it, as `:split` does below.

## :who

Lists the guests by name in the message line, read-only ones too, or says
"no guests". A guest sees itself in the list.

## :window

Moves the current window, or places the next window a command opens:

- `:window float` puts the window in a float in the middle of the screen
- `:window above`, `below`, `left` and `right` put it at that edge of the
  screen, across the whole of it
- with a command after the place, as `:window float help` or `:window
  right terminal`, the window that command opens goes there instead
- `:window zoom` shows the current window alone, and a second
  `:window zoom` shows every window again. `<C-w>z` does the same

A moved window keeps its buffer, cursor and program. The last window
cannot move, a float does not zoom, and a window a module locked in place
says it cannot move. A placement given this way beats `[open]` in
config.toml. `<Tab>` completes the place. Anything else says how to use
it.
