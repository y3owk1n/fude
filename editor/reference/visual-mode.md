# Visual mode

`v` selects characters, `V` whole lines and `<C-v>` a block, the same
columns on every line from the line it started on to the cursor's. The
selection runs from where it started to the cursor, both ends included. A
motion or a scroll moves the cursor, so it grows or shrinks the selection.
`$` in a block takes every line to its end, however long. A text object,
such as `iw` or `ip`, selects what it covers at the cursor, as lines when
it is linewise, and typed again it grows the selection, as the text
objects say.

A count goes to the key after it. A motion moves n, `>` shifts n levels,
`p` puts n times and `<C-a>` adds n. A count before `v`, `V` or `<C-v>` in
normal mode does nothing. A key that acts on the selection returns to
normal mode and leaves the marks `'<` and `'>` at its ends, so `gv`
selects it again and `:'<,'>` names its lines. `.` repeats the change with
the keys that made the selection, from the cursor.

## !

Opens the command line with the selected lines as a range and a `!` after
it, as `:.,.+2!` for three lines, for a shell command to read them. The
shell module's `:!` puts its output in their place. The cursor goes to
the start of the first line. In a block, the range is still whole lines.

## #

Searches back for the selected text as it is, n times, from the
selection's start. No character in it is special, and a line break matches
a line break. It becomes the last search, so `n` and `N` go on with it.

```keys
"a.b axb |a.b" "v2l#" "|a.b axb a.b"
```

## *

Searches forward for the selected text, n times, as `#` searches back.

```keys
"|a.b axb a.b" "v2l*" "a.b axb |a.b"
"|a.b axb a.b" "v2l*n" "|a.b axb a.b"
```

## :

Opens the command line with `'<,'>` typed, the range of the selected
lines, so a command such as `:d` or `:s` acts on them.

```keys
"|a\nb\nc" "Vj:d<CR>" "|c"
"|ab\nab\nab" "Vj:s/a/x/<CR>" "xb\n|xb\nab"
```

## <

Outdents the selected lines by n indent levels and keeps them selected, so
another `<` outdents them again. An empty line stays empty. A line with less
indent than a level loses all of it. `<Esc>` ends the selection.

```keys
"|\t\ta\n\t\tb" "Vj<<Esc>" "\ta\n|\tb"
"\t\t|a\n\t\tb" "Vj<<<Esc>" "a\n|b"
```

## <C-a>

Adds n to the first number in the selection on each selected line, a
decimal, a hex such as `0x1f` or a binary such as `0b101`, as `<C-a>` in
normal mode does. A line with none is left alone, and with none on any
line it beeps. The cursor goes to the start of the selection.

```keys
"|x 1\nx 9" "Vj<C-a>" "|x 2\nx 10"
"|x 1\nx 9" "Vj5<C-a>" "|x 6\nx 14"
```

## <C-c>

The same as `<Esc>`.

## <C-n>

Places a multicursor that keeps the selection, as `Q` places one, and
selects the next match of its text that no multicursor selects, going on
from the start at the end of the text. It looks for the text as it is,
or for the whole word after `<C-n>` in normal mode selected it. The
selection must be on one line, and when no match is left it says "no
more matches". Each key after
runs at every multicursor on its own selection, so `c` types into every
match until `<Esc>`.

```keys
"|ab x ab" "vl<C-n>d" " x| "
"|ab x ab" "vl<C-n>cz<Esc>" "z x |z"
"|foo foobar foo" "<C-n><C-n>cx<Esc>" "x foobar |x"
```

## <C-v>

Selects a block from visual mode of another kind, keeping both ends, or
leaves visual mode when the selection is already a block.

```keys
"|abc\nabc" "<C-v>jld" "|c\nc"
"|abc\nabc" "<C-v>j$d" "|\n"
```

## <C-x>

Subtracts n from the first number in the selection on each selected line,
as `<C-a>` adds. A `-` right before a decimal makes it negative.

```keys
"|x 1\nx 9" "Vj<C-x>" "|x 0\nx 8"
"|x 1\nx 9" "Vj3<C-x>" "|x -2\nx 6"
```

## <Esc>

Leaves visual mode with the cursor where it is, and changes nothing.

```keys
"|abc" "v$<Esc>" "ab|c"
```

## >

Indents the selected lines by n indent levels and keeps them selected, so
`V>>>` indents them three levels and `V3>` does it at once. An empty line
stays empty. With `expand_tab` the indent is spaces only.

```keys
"|a\nb" "Vj><Esc>" "\ta\n\t|b"
"|a\nb" "Vj2><Esc>" "\t\ta\n\t\t|b"
```

## [e

Moves the selected lines n lines up and keeps them selected, so `[e`
again moves them further. At the first line it stops, and when they cannot
move at all it beeps. Each move is one undo step.

```keys
"a\nb\n|c" "V[ed" "a\n|b"
"a\nb\n|c" "V5[ed" "|a\nb"
```

## ]e

Moves the selected lines n lines down and keeps them selected, as `[e`
moves them up, stopping at the last line.

```keys
"|a\nb\nc" "V]e<Esc>" "b\n|a\nc"
"|a\nb\nc" "Vj]e<Esc>" "c\na\n|b"
```

## A

In a block, inserts after its right edge on its first line. On `<Esc>` the
text typed goes after the block on every other line too, and a line too
short to reach the edge gets spaces up to it. After `$` it goes at the end
of each line. Text is copied only when it was typed on the first line,
with no line break. Over characters or lines it inserts at the end of
the last selected line.

```keys
"a|b\ncd\nef" "vjAx<Esc>" "ab\ncd|x\nef"
"|ab\nab" "<C-v>jAx<Esc>" "|axb\naxb"
"|a\nabc" "<C-v>jllAx<Esc>" "|a  x\nabcx"
"|a\nabc" "<C-v>j$A;<Esc>" "|a;\nabc;"
```

## C

Changes the selected lines, as `R` does. In a block it deletes from the
block's left edge to the end of every line, then inserts on the first
line, and `<Esc>` puts the typed text on the other lines too.

```keys
"  |ab\ncd" "vjCx<Esc>" "  |x"
"a|bc\nabc" "<C-v>jCx<Esc>" "a|x\nax"
```

## c

Deletes the selection into the register, then starts insert mode where it
was. Over whole lines it keeps the first line's indent and types on one
line. In a block, the text typed on the first line goes on every line of
the block on `<Esc>`.

```keys
"|abc def" "vecx<Esc>" "|x def"
"  |ab\ncd" "Vjcx<Esc>" "  |x"
"|abc\nabc" "<C-v>jlcx<Esc>" "|xc\nxc"
```

## D

Deletes the selected lines, as `X` does. In a block it deletes from the
block's left edge to the end of every line.

```keys
"a|b\ncd\nef" "vD" "c|d\nef"
"a|bc\nabc" "<C-v>jD" "|a\na"
```

## d

Deletes the selection into the register. Over lines it deletes them whole,
and in a block only its columns. The cursor goes to where the selection
started, or for lines to the line after them.

```keys
"a|bcd" "vld" "a|d"
"|a\nb\nc" "Vjd" "|c"
"|abc\nabc" "<C-v>jd" "|bc\nbc"
"|ab cd ef" "vld." "|d ef"
```

## g<C-a>

Adds n to the first number in the selection on the first line with one, 2n
on the next and on, so a column of zeros becomes 1, 2, 3. A line without a
number does not count.

```keys
"|0\n0\nx\n0" "VGg<C-a>" "|1\n2\nx\n3"
"|0\n0\n0" "VG2g<C-a>" "|2\n4\n6"
```

## g<C-x>

Subtracts n from the first number on the first line with one, 2n from the
next and on, as `g<C-a>` adds.

```keys
"|9\n9\n9" "VGg<C-x>" "|8\n7\n6"
```

## g?

Rot13-encodes the selection. Each ASCII letter goes 13 places along the
alphabet, so doing it twice gives the text back. Other characters stay.
The cursor goes to the start of the selection.

```keys
"|Hello, world" "v$g?" "|Uryyb, jbeyq"
```

## gJ

Joins the selected lines into one, as they are, with no space put in and
no indent taken out. A selection on one line joins it with the line below.
The cursor goes to the last join.

```keys
"|a\n  b\nc" "VjgJ" "a|  b\nc"
"|a\nb" "vgJ" "a|b"
```

## gU

Uppercases the selection, or a block's columns. The cursor goes to its
start.

```keys
"a|bc d" "vlgU" "a|BC d"
"|ab\nab" "<C-v>jgU" "|Ab\nAb"
```

## gu

Lowercases the selection, as `gU` uppercases it.

```keys
"A|BC D" "vlgu" "A|bc D"
```

## gw

Formats the selected lines as `gq` does, and leaves the cursor where it
was. `gq` comes from the format module, and without it `gw` does nothing.

## g~

Switches the case of every letter in the selection. The same as `~`.

```keys
"|aBc" "v$g~" "|AbC"
```

## I

In a block, inserts before its left edge on its first line. On `<Esc>` the
text typed goes there on every other line too, except a line too short to
reach the edge. Text is copied only when it was typed on the first line,
with no line break. Over characters or lines it inserts at the start of
the first selected line.

```keys
"  a|b\ncd\nef" "VjIx<Esc>" "|x  ab\ncd\nef"
"a|b\nab" "<C-v>jIx<Esc>" "a|xb\naxb"
"a|b\n\nab" "<C-v>jjI-<Esc>" "a|-b\n\na-b"
```

## J

Joins the selected lines into one, as `J` in normal mode does. It takes
out each next line's indent and puts one space between, but none after a
blank, before a `)` or next to an empty line. A selection on one line
joins it with the line below.

```keys
"|a\n  b\nc" "VjJ" "a| b\nc"
"|a\nb\nc" "VGJ" "a b| c"
```

## O

In a block, moves the cursor to the other corner on its line, so the next
motion moves the block's other side. Elsewhere it moves the cursor to the
other end of the selection, as `o` does.

```keys
"|abcd\nabcd" "<C-v>jlOld" "a|cd\nacd"
```

## o

Moves the cursor to the other end of the selection, so the next motion
moves the end where the selection started.

```keys
"ab|cd" "vlohd" "|a"
```

## P

Replaces the selection with the register, put n times, as `p` does, and
leaves the register as it was, so the next `P` puts the same text.

```keys
"|a b c" "yiwwviwPwviwP" "a a |a"
```

## p

Replaces the selection with the register, put n times, in one undo step.
The text it replaces goes to the registers as a delete's does, so the next
`p` puts that. Lines put over characters go on lines of their own, and
characters put over lines take their place as one line. In a block, text
with no line break goes on every line of the block.

```keys
"|a b" "yiwwviwp" "a |a"
"|a b" "yiwwviw3p" "a aa|a"
"|a b" "yiwwviwpbviwp" "|b a"
"|a\nb" "yyjVp" "a\n|a"
"|x\nabc" "yyjlvp" "x\na\n|x\nc"
```

## Q

Places a multicursor on each selected line at the cursor's column, and
sets multicursor follow on, as `1q=` does, so motions move them all.

```keys
"|ab\ncd\nef" "VjjQx" "b\nd\n|f"
```

## R

Changes the selected lines. It deletes them whole, keeps the first one's
indent and starts insert mode on the line left. In a block it takes whole
lines too.

```keys
"  |ab\ncd" "VjRx<Esc>" "  |x"
"|abc\nabc" "<C-v>jRx<Esc>" "|x"
```

## r{char}

Replaces every selected character with char, keeping the line breaks, so
the text keeps its shape. The cursor goes to the start of the selection.

```keys
"a|bcd" "vlrx" "a|xxd"
"|ab\ncd" "Vjr-" "|--\n--"
"|abc\nabc" "<C-v>jlr-" "|--c\n--c"
```

## S

Changes the selected lines, as `R` does. The surround module takes `S` in
visual mode, to put a pair around the selection as `S{char}` says, so in
fude `R` is the key that does this.

## s

The same as `c`. fude maps `s` in visual mode to a jump, as in normal mode,
so `c` is the key that does this. To have `s` back:

```toml
[keymap.visual]
"s" = ""
```

## U

Uppercases the selection. The same as `gU`.

```keys
"|ab cd" "veU" "|AB cd"
```

## u

Lowercases the selection. The same as `gu`. In visual mode `u` does not
undo.

```keys
"|AB CD" "veu" "|ab CD"
```

## V

Selects whole lines, from visual mode of another kind, keeping both ends,
or leaves visual mode when the selection is already of lines.

```keys
"a|bc\nd" "vVd" "|d"
"a|bc\nd" "VVx" "a|c\nd"
```

## v

Selects characters, from visual mode of another kind, keeping both ends,
or leaves visual mode when the selection is already of characters.

```keys
"|abc\nabc" "Vlvd" "|c\nabc"
"a|bc" "vvx" "a|c"
```

## X

Deletes the selected lines whole, whatever the kind of selection.

```keys
"a|b\ncd\nef" "vjX" "e|f"
"|abc\nabc" "<C-v>jX" "|"
```

## x

Deletes the selection. The same as `d`.

```keys
"a|bcd" "vlx" "a|d"
```

## Y

Yanks the selected lines whole, whatever the kind of selection, and puts
the cursor at the start of the first one.

```keys
"a|b\ncd" "vYjp" "ab\ncd\n|ab"
```

## y

Yanks the selection into the register, the columns of a block or whole
lines for `V`, and puts the cursor at its start.

```keys
"a|bc d" "vly$p" "abc db|c"
"|abc\nabc" "<C-v>jly" "|abc\nabc"
```

## ~

Switches the case of every letter in the selection. The same as `g~`.

```keys
"|aBc d" "v2l~" "|AbC d"
```

## S{char}

Puts a pair of brackets or quotes around the selection, from the surround
module. `)` or `b` puts parentheses, `]` or `r` square brackets, `}` or
`B` braces and `>` or `a` angle brackets. An opening bracket puts the same
pair with a blank inside each side, so `S(` makes `( text )`. Any other
character goes on both sides, as `S"` does. Over lines it surrounds their
text from the first line's first non-blank to the end of the last.
