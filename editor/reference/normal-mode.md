# Normal mode

Normal mode is where fude starts and where `<Esc>` comes back to. Keys
here are commands, not text. n is the count typed before a command, or 1
without one. Most commands repeat n times, and some read n as a line, a
size or a place in a list instead.

A register named with `"` and a count can come in either order before
the command, so `"a3p` and `3"ap` both put register `a` three times. A
count on each side of it multiplies, so `2"a3p` puts it six times. A
command that changes the text is a change, and `.` repeats the last one
with its count and register. Moves, scrolls, yanks and undo are not
changes.

## "{reg}

Names the register the next yank, delete, change or put uses, as `"ayy`
yanks the line into `a`. `a` to `z` keep text until they are written
again, and `A` to `Z` add to the end of the lowercase one. `"_` throws
the text away, so a delete into it keeps the unnamed register. `"+` and
`"*` are the system clipboard. `".` holds the text last typed in insert
mode, `":` the last command line, `"/` the last search and `"%` the
file's name, and none of these four can be written. Without a register a
yank goes to `"0`, a delete of lines to `"1`, which moves the older ones
up to `"9`, and a smaller delete to `"-`. Every one of them also fills the
unnamed register `""`, which a put without a register takes. With
`clipboard` on, that unnamed register is the system clipboard too.

```keys
"|one two" "\"ayiww\"aP" "one on|etwo"
"|x" "\"ayl3\"ap" "xxx|x"
"|x" "\"ayl\"a3p" "xxx|x"
"|one two" "\"ayiww\"Ayiw\"ap" "one tonetw|owo"
"|a\nb" "yy\"_ddp" "b\n|a"
```

## &

Runs the last `:s` again on the cursor's line, with its pattern,
replacement and flags. It is a change, so `.` repeats it on another line.
It says so when no substitute was run yet, or the line has no match.

```keys
"|a a\na a" ":s/a/b/g<CR>j&" "b b\n|b b"
"|a a\na a" ":s/a/b/<CR>j&" "b a\n|b a"
```

## .

Repeats the last change, as the keys that made it, at the cursor. A count
replaces the change's own, and the change keeps that count for the next
`.`. A change made in insert mode repeats with the text typed.

```keys
"|abcdef" "x." "|cdef"
"|abcdef" "2x." "|ef"
"|abcdefgh" "x3.." "|h"
"|a b" "cwx<Esc>w." "x |x"
```

## :

Opens the command line to type an ex command. With a count it starts
with the range of n lines from the cursor's, as `3:` types `:.,.+2`.

```keys
"|a\nb\nc\nd" "2:d<CR>" "|c\nd"
```

## <C-^>

Shows the buffer the window showed before, or with a count buffer n, as
`:b #` and `:b n` do. It says so when there is no buffer to show.

## <C-a>

Adds n to the first number on the line that ends at or after the cursor,
and puts the cursor on its last digit. A number is decimal, hex after
`0x` or binary after `0b`. A `-` right before a decimal makes it
negative. Leading zeros and the case of hex digits stay. It fails when
the line has no number from the cursor on.

```keys
"|x 9 y" "<C-a>" "x 1|0 y"
"|x 007" "<C-a>" "x 00|8"
"|-1" "3<C-a>" "|2"
"|0x0f" "<C-a>" "0x1|0"
```

## <C-b>

Scrolls the view up a page n times, keeping two lines of the old page in
view. The cursor moves only as far as it must to stay in the view.

## <C-d>

Scrolls the view down half its height, and moves the cursor down as far.
With a count it scrolls n lines instead, for this time only. The view stops
once the last line is at its bottom, and on the last line `<C-d>` fails.

```keys
"|a\nb\nc\nd" "2<C-d>" "a\nb\n|c\nd"
"a\n|b" "<C-d>" "a\n|b"
```

## <C-e>

Scrolls the view down n lines. The cursor keeps its place in the text
unless its line leaves the view, when it moves to the top line.

```keys
"|a\nb\nc" "<C-e>" "a\n|b\nc"
```

## <C-f>

Scrolls the view down a page n times, as `<C-b>` scrolls up.

## <C-i>

Goes forward n places in the jump list, back to where `<C-o>` came from.
`<Tab>` is the same key. It fails at the newest place.

```keys
"|a\nb\nc" "G<C-o><C-i>" "a\nb\n|c"
```

## <C-l>

Hides the highlight of the last search's matches. The next search, `n`,
`N`, `*`, `#` or `:s` shows them again. It also removes every
multicursor, and `gQ` puts them back. Each register a command wrote while
they were placed then holds what every multicursor and the cursor held
there, one line each, in order through the text.

```keys
"|a\nb" "Qj<C-l>x" "a\n|"
```

## <C-n>

Selects the word under the cursor in visual mode, for `<C-n>` in visual
mode to place multicursors at its next whole-word matches. It fails when
the cursor is not on a letter, digit or `_`.

```keys
"|foo bar foo" "<C-n><C-n>cx<Esc>" "x bar |x"
```

## <C-o>

Goes back n places in the jump list. The jump list is the places a jump
left from, such as `G`, `%`, a search or a mark, one for each line, and
the newest 100 of them. The first `<C-o>` adds where the cursor is, so
`<C-i>` comes back to it. A place may be in another buffer, which the
window then shows. It fails at the oldest place.

```keys
"|a\nb\nc" "G<C-o>" "|a\nb\nc"
"|a\nb\nc" "G/b<CR>2<C-o>" "|a\nb\nc"
```

## <C-r>

Redoes n changes that `u` undid. It says so when there is none to redo.

```keys
"|abc" "xxuu2<C-r>" "|c"
```

## <C-u>

Scrolls the view up half its height, and moves the cursor up as far, as
`<C-d>` scrolls down. On the first line it fails.

```keys
"a\nb\nc\n|d" "2<C-u>" "a\n|b\nc\nd"
```

## <C-v>

Starts visual mode selecting a block, the columns from the start to the
cursor on every line between.

```keys
"|ab\ncd" "<C-v>jd" "|b\nd"
```

## <C-w>{key}

Runs a window command, the key after `<C-w>`. fude's own split, close,
arrange and move between windows, as the entries that follow say, and
modules add more, such as the window module's keys that resize. A count
before `<C-w>` reaches the command, which the keys that resize read as a
size. In a float, any `<C-w>`
key closes the float.

## <C-x>

Subtracts n from the number at or after the cursor, as `<C-a>` adds.

```keys
"|x 10" "3<C-x>" "x |7"
"|5" "10<C-x>" "-|5"
```

## <C-y>

Scrolls the view up n lines, as `<C-e>` scrolls down. When the cursor's
line leaves the view, the cursor moves to the bottom line.

## <Del>

The same as `x`.

```keys
"|abc" "2<Del>" "|c"
```

## <Down>

The same as `j`.

## <End>

The same as `$`.

## <Home>

The same as `0`.

## <C-Home>

The same as `gg`.

## <C-Left>

The same as `B`.

## <C-Right>

The same as `W`.

## <Insert>

The same as `i`.

## <Left>

The same as `h`.

```keys
"a|b" "<Left>x" "|b"
```

## <PageDown>

The same as `<C-f>`.

## <PageUp>

The same as `<C-b>`.

## <Right>

The same as `l`.

## <S-Down>

The same as `<C-f>`.

## <S-Left>

The same as `b`.

```keys
"ab |cd" "<S-Left>x" "|b cd"
```

## <S-Right>

The same as `w`.

## <S-Up>

The same as `<C-b>`.

## <Up>

The same as `k`.

## @{reg}

Runs the keys in the register as if typed, n times. `@@` runs the last
register run again, and `@:` the last command line. Mappings apply to the
keys. The run stops at the first motion that fails, so a count larger
than needed is safe. It does nothing for an empty register.

```keys
"|abcdef" "qaxq2@a" "|def"
"|a,b,c,d" "qaf,xq5@a" "abc|d"
"|a\nb\nc" ":d<CR>@:" "|c"
```

## [e

Moves the cursor's line up past the n lines above it, or to the top. The
cursor stays on the line. It fails on the first line.

```keys
"a\nb\n|c" "[e" "a\n|c\nb"
"a\nb\n|c" "5[e" "|c\na\nb"
```

## [P

Puts the register before the cursor as `P` does, but whole lines take
the indent of the cursor's line, each keeping its indent relative to the
first. Text that is not whole lines puts as `P` puts it. `[p` is the
same.

```keys
"  a\n|b" "yyk[P" "  |b\n  a\nb"
```

## ]e

Moves the cursor's line down past the n lines below it, or to the bottom,
as `[e` moves it up. It fails on the last line.

```keys
"|a\nb\nc" "]e" "b\n|a\nc"
"|a\nb\nc" "2]e" "b\nc\n|a"
```

## ]P

The same as `[P`.

## ]p

Puts the register after the cursor as `p` does, with whole lines at the
indent of the cursor's line, as `[P` does before it.

```keys
"  a\n|b" "yyk]p" "  a\n  |b\nb"
```

## A

Starts insert mode at the end of the line. After a count, `<Esc>` types
the text that many times in all.

```keys
"|ab" "Ax<Esc>" "ab|x"
```

## a

Starts insert mode after the character under the cursor, or on an empty
line at its start.

```keys
"|ab" "ax<Esc>" "a|xb"
```

## C

Deletes from the cursor to the end of the line, or of the line n-1 below,
and starts insert mode there, as `c$` does.

```keys
"a|bc" "Cx<Esc>" "a|x"
"a|bc\nde\nf" "2Cx<Esc>" "a|x\nf"
```

## D

Deletes from the cursor to the end of the line, or of the line n-1 below,
as `d$` does.

```keys
"a|bc" "D" "|a"
"a|bc\nde\nf" "2D" "|a\nf"
```

## g&

Runs a substitute on every line, as `:%s//~/&` does, with the last
search as its pattern, the last replacement and the last substitute's
flags. The cursor goes to the last line it changed.

```keys
"|a a\na a" ":s/a/b/<CR>g&" "b b\n|b a"
"|a a\na a" ":s/a/b/g<CR>g&" "b b\n|b b"
```

## g+

Goes n states forward through the text's history, in the order the
states were made. Unlike `<C-r>`, it reaches states that an undo and a
new change left on another branch. It says so at the newest state.

```keys
"|abc" "xxu$xg-g+" "|b"
```

## g,

Goes forward n places in the change list, toward the newest, as `g;`
goes back.

```keys
"|ab\ncd\nef" "xGxggg;g;g," "b\ncd\n|f"
```

## g-

Goes n states back through the text's history, in the order the states
were made, across branches, as `g+` goes forward. After an undo and a new
change, `g-` reaches the state the undo left, which `u` does not. The
cursor goes to where the state changed. It says so at the oldest state.

```keys
"|abc" "xxu$xg-" "|c"
```

## g8

Shows the bytes of the character under the cursor in hex, such as
`61` for `a`. A character made of several, such as a letter and an
accent, shows each joined by ` + `.

## g;

Goes back n places in the change list, the places where the text changed,
one for each line, the newest 100 of them. The first `g;` goes to the
newest change. It fails at the oldest place.

```keys
"|ab\ncd\nef" "xGxggg;" "b\ncd\n|f"
"|ab\ncd\nef" "xGxggg;g;" "|b\ncd\nf"
```

## ga

Shows the number of the character under the cursor in decimal, hex and
octal, and its digraph when it has one, such as `a is 97, hex 61, octal
141`. On an empty line it says there is no character.

## gI

Starts insert mode at the start of the line, before any indent.

```keys
"  a|b" "gIx<Esc>" "|x  ab"
```

## gi

Starts insert mode where insert mode was last left, the mark `^`, or at
the cursor when it is not set.

```keys
"a|bc\nd" "ix<Esc>jgiy<Esc>" "ax|ybc\nd"
```

## gJ

Joins n lines, at least two, as `J` does, but keeps every blank and adds
none.

```keys
"|a\n  b" "gJ" "a|  b"
```

## gN

Selects in visual mode the match of the last search under the cursor, or
else the one before it, going round the text, as `gn` does forward.

## gn

Selects in visual mode the match of the last search under the cursor, or
else the next one, going round the text. After an operator it is the
operator's target, so `cgn` changes a match and `.` changes the next. It
says so when there is no search or no match.

```keys
"|a x a x" "/x<CR>gnd" "a | a x"
"|foo bar foo" "/foo<CR>cgnx<Esc>." "|x bar x"
```

## gP

Puts the register before the cursor, n times, as `P` does, and leaves the
cursor just after the text put.

```keys
"|a\nb" "yygP" "a\n|a\nb"
```

## gp

Puts the register after the cursor, n times, as `p` does, and leaves the
cursor just after the text put.

```keys
"|ab" "ylgp" "aa|b"
"|a\nb" "yygp" "a\na\n|b"
```

## gv

Selects the last selection again, of the same kind and with the cursor at
the same end. It does nothing before any selection.

```keys
"|abc" "vl<Esc>gvd" "|c"
```

## I

Starts insert mode before the first character of the line that is not a
space or a tab.

```keys
"  a|b" "Ix<Esc>" "  |xab"
```

## i

Starts insert mode before the character under the cursor. `<Esc>` leaves
it and the cursor moves back one character. After a count, `<Esc>` types
the text that many times in all, and `<C-c>` leaves it typed once. `a`,
`A`, `I` and `gI` take a count the same way, and `o` and `O` open that
many lines. In a buffer that may not change, it says why and stays in
normal mode.

```keys
"a|bc" "ix<Esc>" "a|xbc"
"|a" "3ix<Esc>" "xx|xa"
"|a" "3ox<Esc>" "a\nx\nx\n|x"
```

## J

Joins n lines, at least two, dropping the indent of each line joined and putting one space between. It puts
no space after a blank, before a `)`, or next to an empty line. The cursor
goes to the last join. On the last line it fails.

```keys
"|a\n  b" "J" "a| b"
"|a\nb\nc" "3J" "a b| c"
"|f(\n  )" "J" "f(|)"
```

## m{a-zA-Z}

Sets mark a to z at the cursor, for `` `a `` and `'a` to go to. A mark
moves with its text as lines above it change. Marks a to z belong to the
buffer. A to Z are file marks, one each across every buffer, and going to
one shows the buffer that holds it, though an operator takes it only in
its own buffer. `m'`, `` m` ``, `m[`, `m]`, `m<` and `m>` set those marks
by hand.

```keys
"ab|c\nd" "maj`a" "ab|c\nd"
"ab|c\nd" "mAj`A" "ab|c\nd"
```

## O

Opens a new line above the cursor's and starts insert mode on it, with
the line's indent, or one indent more when the line starts with `)`, `]`
or `}`. After a count, `<Esc>` opens that many lines, each with the text.

```keys
"  |a" "Ob<Esc>" "  |b\n  a"
"|a" "2Ob<Esc>" "b\n|b\na"
```

## o

Opens a new line below the cursor's and starts insert mode on it, with
the line's indent, or one indent more after a line that ends with an
opening bracket. After a count, `<Esc>` opens that many lines, each with
the text.

```keys
"  |a" "ob<Esc>" "  a\n  |b"
"|a" "2ob<Esc>" "a\nb\n|b"
```

## P

Puts the register before the cursor, n times. Text that is whole lines
goes above the cursor's line, with the cursor on its first non-blank. Other
text goes before the cursor, which lands on its last character. It says
so when the register is empty.

```keys
"a|b" "ylP" "a|bb"
"a\n|b" "yyP" "a\n|b\nb"
```

## p

Puts the register after the cursor, n times. Text that is whole lines
goes below the cursor's line, with the cursor on its first non-blank. Other
text goes after the cursor, which lands on its last character, or on its
start when it holds a line break. With `clipboard` on and no register
named, it puts the system clipboard when that holds something other than
what the unnamed register holds.

```keys
"|ab" "yl3p" "aaa|ab"
"|a\nb" "yyjp" "a\nb\n|a"
```

## Q

Places a multicursor where the cursor is, or removes the one there. `1Q`
only places one and `2Q` only removes one. While multicursors are placed,
every command that edits runs again at each of them, on that
multicursor's own text: operators with their targets, `x`, `p`, `.`,
macros, the keys of a mapping, insert mode as it is typed, and visual
mode key by key. Motions move only the cursor, unless `q=` sets follow
on, and ex commands, undo and scrolls run once. `u` takes back a command
at every multicursor as one step. Each multicursor has its own registers
and marks, so `yiw` at each and `p` puts each its own word. A multicursor
where the cursor is moves with it. `<C-l>` removes them. `Q` sets follow
off. They show in the colour of the mode, and in visual mode each with
its own selection. In kitty they are real cursors in the cursor's shape.
`:h multicursor` has more examples.

```keys
"|ab\ncd\nef" "QjQjx" "b\nd\n|f"
"|ab cd\nef gh" "Qjciwx<Esc>" "x cd\n|x gh"
"|a\nbbb" "QjA xy<C-w>z<Esc>" "a z\nbbb |z"
```

## gQ

Puts back the multicursors `<C-l>` removed last, and the cursor where it
was then. It does nothing while multicursors are placed.

```keys
"|ab\ncd" "Qj<C-l>ggxgQx" "\n|d"
```

## g<C-a>

Puts a number before each multicursor and the cursor, counting up from n
through the text, so a column of multicursors becomes 1, 2, 3. It does
nothing without multicursors.

```keys
"|a\nb\nc" "QjQjg<C-a>" "1a\n2b\n3|c"
```

## q=

Toggles multicursor follow. With follow on, a motion that goes from where
the cursor is, such as `w`, `fx`, `j`, `n` or `*`, moves every
multicursor too. `G`, `gg`, `H`, `M`, `L`, marks and the jumplist move
only the cursor. Multicursors that land in one place become one. `1q=`
sets follow on and `2q=` off.

```keys
"|ab cd\nef gh" "Qjq=wx" "ab d\nef |h"
```

## q{reg}

Records the keys typed into register reg until `q` is typed again. An
uppercase name adds the keys to what the lowercase register holds. While
it records, the status line says so. `q:` opens the history of command
lines and `q/` that of searches, when the history module is loaded.

```keys
"|abcdef" "qaxqqAxq@a" "|ef"
```

## R

Starts replace mode, where each character typed takes the place of the
one under the cursor, and past the line's end adds to it. `<BS>` puts back
what the last character typed covered. A count does not repeat the text.

```keys
"|abc" "Rxy<Esc>" "x|yc"
"|ab" "Rxyz<Esc>" "xy|z"
"|abc" "Rxy<BS><Esc>" "|xbc"
```

## r{char}

Replaces n characters from the cursor with char, and leaves the cursor
on the last. `r<CR>` replaces them with one line break. It fails when the
line has fewer than n characters from the cursor on.

```keys
"|abc" "2rx" "x|xc"
"|ab" "3rx" "|ab"
"a|bc" "r<CR>" "a\n|c"
```

## S

Deletes the text of the line, or of n lines, and starts insert mode on
it, as `cc` does. The line keeps its indent.

```keys
"  a|b\ncd" "Sx<Esc>" "  |x\ncd"
```

## s

Deletes n characters from the cursor and starts insert mode, as `cl`
does. fude maps `s` to a jump, which labels the matches of the characters
typed next, so this `s` runs only once `[keymap.normal]` maps `"s" = ""`.
Until then `cl` does what it does.

## u

Undoes n changes. The cursor goes back to where it was before the change
when that is on the change's first line, or else to that line's first
non-blank. Everything typed in one stay in insert mode is one change. It
says so when there is nothing to undo.

```keys
"|abc" "xxu" "|bc"
"|abc" "xx2u" "|abc"
```

## V

Starts visual mode selecting whole lines.

```keys
"|a\nb\nc" "Vjd" "|c"
```

## v

Starts visual mode selecting characters, from where it starts to the
cursor.

```keys
"|abc" "vld" "|c"
```

## X

Deletes n characters before the cursor, as `dh` does. At the start of
the line it fails.

```keys
"abc|d" "2X" "a|d"
"|ab" "X" "|ab"
```

## x

Deletes n characters from the cursor, as `dl` does, never past the end
of the line.

```keys
"|abc" "2x" "|c"
"a|bc" "5x" "|a"
```

## Y

Yanks from the cursor to the end of the line, or of the line n-1 below,
as `y$` does. `yy` yanks the whole line.

```keys
"a|bc" "Yp" "abb|cc"
```

## z-

Scrolls the cursor's line, or line n, to the bottom of the view, as `zb`
does, and puts the cursor on its first non-blank.

```keys
"a\n  b|c" "z-" "a\n  |bc"
```

## z.

Scrolls the cursor's line, or line n, to the middle of the view, as `zz`
does, and puts the cursor on its first non-blank.

```keys
"|a\n  b" "2z." "a\n  |b"
```

## z<CR>

Scrolls the cursor's line, or line n, to the top of the view, as `zt`
does, and puts the cursor on its first non-blank.

```keys
"  a|b" "z<CR>" "  |ab"
```

## zb

Scrolls the view so the cursor's line is at its bottom. With a count the
cursor goes to line n first, keeping its column.

```keys
"a|b\ncd" "2zb" "ab\nc|d"
```

## ze

Scrolls the view sideways so the cursor's column is its right edge. It
works only with `wrap` off, and does nothing while lines wrap.

## zH

Scrolls the view left by half its width, with `wrap` off. The cursor moves
only as far as it must to stay in the view.

## zh

Scrolls the view n columns left, with `wrap` off, and never past the
first column. The cursor moves to the view's last column when the view
passes it.

## zL

Scrolls the view right by half its width, with `wrap` off, as `zH`
scrolls left.

## zl

Scrolls the view n columns right, with `wrap` off. The cursor moves to
the view's first column when the view passes it.

## ZQ

Closes the window without writing, as `:q!` does. In the last window it
quits fude and drops unsaved changes.

## zq{motion}

Places a multicursor at the cursor and at each place the motion reaches
from there, again and again, to the end of the text. A search, `n`, `N`,
`*`, `#`, `/` or `?`, places one at every match instead, and makes each
match its multicursor's last selection, so `zq*gvc` changes every match
of the word under the cursor. In visual mode it stays within the
selection, and a block starts each of its lines at its left column. Text
objects are not motions here.

```keys
"|a b\nc d" "zqwx" "| \n "
"|ab x ab" "zq*gvcz<Esc>" "|z x z"
```

## zs

Scrolls the view sideways so the cursor's column is its left edge, with
`wrap` off.

## zt

Scrolls the view so the cursor's line is at its top. With a count the
cursor goes to line n first, keeping its column.

```keys
"|ab\ncd" "2zt" "ab\n|cd"
```

## ZZ

Writes the buffer when it is modified, then closes the window, as `:x`
does. In the last window it quits fude.

## zz

Scrolls the view so the cursor's line is in its middle. With a count the
cursor goes to line n first, keeping its column.

```keys
"a|b\ncd" "2zz" "ab\nc|d"
```

## {count}

Digits typed before a command are its count, n. `0` starts a count only
after another digit, and alone goes to the start of the line. A count
before an operator and one before its motion multiply, so `2d3w` deletes
six words. A command that takes no count ignores it.

```keys
"|abcdef" "3x" "|def"
"|a b c d e f" "2d2w" "|e f"
```

## ~

Switches the case of n characters from the cursor and moves past them, to
the next character, or the last of the line. It stops at the end of the
line.

```keys
"|abc" "~" "A|bc"
"|abc" "2~" "AB|c"
"|ab" "5~" "A|B"
```

## <C-w>s

Splits the window in two, both on the same buffer, and moves to the new
one. It goes below, or above with `split_below` off. `<C-w>S` and
`<C-w><C-s>` are the same.

## <C-w>v

Splits the window side by side, both on the same buffer, and moves to the
new one. It goes right, or left with `split_right` off. `<C-w><C-v>` is
the same.

## <C-w>h

Moves to the window left of the current one, on the cursor's row. It does
nothing when there is none.

## <C-w>j

Moves to the window below the current one, on the cursor's column, as
`<C-w>h` moves left.

## <C-w>k

Moves to the window above the current one, as `<C-w>j` moves below.

## <C-w>l

Moves to the window right of the current one, as `<C-w>h` moves left.

## <C-w>c

Closes the window. The buffer stays open, and the window before it in its
split, or else the one after, takes its place. A window whose program runs, such as a terminal,
hides instead and the program goes on. It says so in the last window.

## <C-w>q

Closes the window as `<C-w>c` does, and in the last window quits fude, as
`:q` does. It refuses to quit while a buffer has unsaved changes.

## <C-w>o

Closes every other window, and every float but the one the cursor is in
and those it opened from.

## <C-w>=

Makes the windows of each split share its space equally again.

## ds{char}

Deletes the pair of brackets or quotes around the cursor that char names,
and leaves the cursor where the first was. A closing bracket, or `b`, `r`,
`B` or `a`, names `()`, `[]`, `{}` or `<>`. An opening bracket names the
same pair and also takes away the blanks inside it, so `ds(` on `( a )`
leaves `a`. Any other character names a pair of itself, such as a quote.
It does nothing when there is no such pair. `.` repeats it. It comes with
the surround module.

## cs{char}{char}

Changes the pair of brackets or quotes around the cursor that the first
char names into the pair the second names, as `cs"'` turns `"a"` into
`'a'`. Characters name pairs as for `ds`, and an opening bracket as the
second puts a blank inside each side. `.` repeats it. It comes with the
surround module.
