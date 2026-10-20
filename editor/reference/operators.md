# Operators

An operator waits for a target, a motion or a text object, and acts on the
text it covers. `dw` deletes to the next word and `di(` deletes inside the
parentheses. The motion decides whether the operator takes characters or
whole lines, as the motions say. A count may come before the operator, the
target or both, and they multiply, so `2d3w` deletes six words. An operator
typed twice acts on the line, or with n on n lines, as `dd` and `3dd` do.
For an operator of two keys, its second key typed again does the same, so
`guu` is `gugu`.

`<Esc>` while an operator waits cancels it and changes nothing. A target
that cannot act, such as `j` on the last line or `i(` outside any
parentheses, leaves the text as it was. `"x` before the count or the
operator names the register `d`, `c` and `y` use. Without one, a yank goes
to `"0`, a delete of lines or of text with a line break to `"1`, moving the
older ones up to `"9`, and a smaller delete to `"-`. Each also sets `""`,
which `p` puts. An uppercase name appends, `"_` keeps nothing, and `"+` and
`"*` are the clipboard. `.` repeats the last change with its target,
register and count, or with the count typed before the `.` instead. A yank
changes nothing, so `.` never repeats one.

In visual mode an operator acts on the selection at once.

## !{motion}

Opens the command line with the lines the target covers as its range and
`!` after it, as `:.!` for one line or `:.,.+2!` for three, and puts the
cursor at the start of the first line. Typing a shell command and `<CR>`
replaces the lines with what the command writes. `!!` takes the line, and
`3!!` three lines.

## <{motion}

Outdents the lines the target covers by `indent_width` columns, or to none
when they have less. The rest of the indent is rebuilt in tabs and spaces,
or spaces with `expand_tab`. Empty lines stay empty. A count before `<<`
takes n lines, each shifted once. The cursor goes to the first non-blank
of the first line.

```keys
"\t\t|a\n\tb" "<<" "\t|a\n\tb"
"\t|a\n\tb" "<j" "|a\nb"
"|\ta\n\tb\n\tc" "2<<" "|a\nb\n\tc"
```

## >{motion}

Indents the lines the target covers by `indent_width` columns, as `<`
outdents them. Empty lines stay empty, so `>ap` leaves the blank line
after a paragraph alone.

```keys
"|a\nb" ">>" "\t|a\nb"
"|a\n\nb" ">2j" "\t|a\n\n\tb"
```

## c{motion}

Deletes the text the target covers into the register, then starts insert
mode where it was. `.` repeats the change with the text typed. `cw` on a
word changes to the word's end, as `ce` does, and keeps the blank after
it. `cc` changes the line, or n lines, and keeps the first line's indent.
A target that covers nothing, as `ci(` in `()`, starts insert mode
anyway.

```keys
"foo |bar baz" "cwX<Esc>" "foo |X baz"
"  |foo\nbar" "ccX<Esc>" "  |X\nbar"
"f(|a, b)" "ci(x<Esc>" "f(|x)"
"f(|)" "ci(x<Esc>" "f(|x)"
```

## d{motion}

Deletes the text the target covers into the register. After a charwise
delete the cursor is where the text began. After a linewise one it keeps
its column on the line that moved up, unless an exclusive motion became
linewise, which puts it on the first non-blank. `dd` deletes the line, or
n lines, and on the last line takes the line break before it.

```keys
"foo |bar baz" "dw" "foo |baz"
"|a b c d e f g" "2d2w" "|e f g"
"a\nb|c\nd" "dd" "a\n|d"
"a\n|b" "dd" "|a"
"|a\nb\nc\nd" "2dd." "|"
```

## g?{motion}

Encodes the letters a to z and A to Z the target covers in rot13, moving
each 13 places along the alphabet. Other characters stay. Doing it twice
gives the text back. The cursor goes to the start of the text, or of the
first line for lines. `g??` and `g?g?` take the line.

```keys
"|Hello, world" "g?w" "|Uryyb, world"
"ab|c" "g??" "|nop"
```

## gU{motion}

Uppercases the text the target covers, letters of any alphabet. The cursor
goes to the start of the text, or of the first line for lines. `gUU` and
`gUgU` take the line.

```keys
"|foo bar" "gUe" "|FOO bar"
"a|b\ncd" "gUU" "|AB\ncd"
"|été x" "gUiw" "|ÉTÉ x"
```

## gu{motion}

Lowercases the text the target covers, as `gU` uppercases it. `guu` and
`gugu` take the line.

```keys
"|FOO BAR" "guiw" "|foo BAR"
"FO|O\nBAR" "2guu" "|foo\nbar"
```

## gw{motion}

Formats the lines the target covers as `gq` does, then puts the cursor
back where it was. `gww` takes the line. It needs the format module,
which gives `gq`, and without it does nothing.

## g~{motion}

Switches the case of every letter the target covers, uppercase to
lowercase and lowercase to uppercase. The cursor goes to the start of the
text, or of the first line for lines. `g~~` and `g~g~` take the line.

```keys
"|Foo bar" "g~w" "|fOO bar"
"x|aB" "g~~" "|XAb"
```

## y{motion}

Copies the text the target covers into the register and changes nothing.
The text flashes. The cursor goes to the start of the text, so `yb` moves
it back a word. After a linewise yank it moves to the first line only
when that is above, keeping its column. `yy` yanks the line, or n lines.

```keys
"foo b|ar" "yb" "foo |bar"
"|foo bar" "yyp" "foo bar\n|foo bar"
"a\n|b" "ykP" "|a\nb\na\nb"
"|foo bar" "\"ayiwwviw\"ap" "foo fo|o"
```

## gc{motion}

Comments the lines the target covers, in the comment syntax of the
language where the target starts, which may be a language inside the file,
such as the code in a markdown fence. The marks go at the least indent of
the lines. When every line that is not blank is already commented, it
takes the marks off instead. `gcc` toggles the line, or n lines. A file
with no comment syntax gets a message and stays as it was. The comment
module gives it.

## zf{motion}

Closes a fold over the lines the target covers, and opens any closed fold
at its first or last line first. A target within one line makes no fold.
`zo` opens it again. The fold module gives it.

## gq{motion}

Formats the lines the target covers through the filetype's `format`
formatters, run in turn, or else through its language server. Only the
changes inside those lines apply, as one change. Formatting that takes
longer than `format_timeout` leaves the text as it was, and a file that
nothing formats gets a message. fude has no formatter of its own. `gqq` formats
the line. The format module gives it.

```toml
[filetype.go]
format = ["gofumpt"]
```

## ={motion}

Formats the lines the target covers, the same as `gq`. `==` formats the
line.

## gH{motion}

Puts every hunk that meets the lines the target covers back as git's index
has them, undoing the changes not yet staged. `gHH` takes the hunks on the
line. It is one change, so `u` brings them back. The git module gives it,
and only the host's session may run it.

## gh{motion}

Writes every hunk that meets the lines the target covers to git's index,
and leaves the other hunks unstaged. `ghh` stages the hunks on the line. A
target with no hunk says so. The git module gives it, and only the host's
session may run it.

## ys{motion}{char}

Waits for one more character, then puts the pair it names around the text
the target covers. `)` or `b`, `]` or `r`, `}` or `B`, and `>`, `<` or `a`
name brackets, and `(`, `[` and `{` the same brackets with a space inside
each. Any other character goes on both sides, as `"` does. The blanks at
the end of the target stay outside the pair, so `ysaw(` wraps the word as
`ysiw(` does. A linewise target wraps from the first line's first
non-blank to the last line's end, so `yss)` wraps the line's text. `.`
repeats it with the same character. The surround module gives it.
