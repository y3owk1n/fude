# Insert mode

Text typed from entering insert mode to leaving it is one undo step, so
`u` takes it all back and `.` types it again. `<Left>`, `<Right>`, `<Up>`,
`<Down>`, `<Home>` and `<End>` split it where the cursor moves, `<C-w>`
and `<C-u>` start a new step before they delete, and `<C-o>` ends it
before its command. An example that ends in insert mode shows the cursor
where the next character goes.

These keys work in replace mode too, which `R` and `<Insert>` start, except
where a section says otherwise. There
a typed character takes the place of the one under the cursor, or goes on
the end of the line, and `<BS>` puts back what it typed over.

A new line gets the indent of the line it came from, and one level more
after a line ending in `(`, `[` or `{`. An indent nothing is typed after
goes away on `<Esc>` or the next `<CR>`. A closing bracket typed on a line
of blanks takes the indent of the line with its opening bracket.

`<C-v>` and `<C-k>` type characters a keyboard lacks. `<C-v>` names one
by number, and `<C-k>` by a digraph, two keys that look like the
character, such as `e'` for é or `Eu` for €. The digraphs are Vim's
default table of 1366. `ga` in normal mode shows a character's digraph.

## <BS>

Deletes the character before the cursor. In the indent it deletes back to
the column of the indent level before, and at the start of a line it joins
the line to the one above. In replace mode it puts back the character the
last one typed took the place of, and once nothing typed is left it moves
left. With `autopairs.enabled` on, between an empty pair such as `()` it deletes
both.

```keys
"ab|c" "i<BS>" "a|c"
"a\n|b" "i<BS>" "a|b"
"\t  |x" "i<BS>" "\t|x"
"|abc" "Rxy<BS>" "x|bc"
```

## <C-@>

Types the text last typed in insert mode, as `<C-a>` does, then leaves
insert mode as `<Esc>` does.

```keys
"|b" "ia<Esc>A<C-@>" "ab|a"
```

## <C-a>

Types the text last typed in insert mode, which the register `.` holds.
Before anything was typed it does nothing.

```keys
"|b" "ia<Esc>A<C-a>" "aba|"
```

## <C-c>

Leaves insert mode as `<Esc>` does, but after a count, as `3ix`, types
the text once.

```keys
"|a" "3ix<C-c>" "|xa"
```

## <C-End>

Goes to the end of the text.

## <C-Home>

Goes to the start of the text.

## <C-Left>

Goes back to the start of the word of non-blanks before the cursor.

## <C-Right>

Goes to the start of the next word of non-blanks.

## <C-d>

Outdents the cursor's line by one indent level, keeping the cursor on the
same text. A line with less indent than a level loses all of it, and one
with none stays as it is.

```keys
"\t\t|x" "i<C-d>" "\t|x"
"\tab|c" "i<C-d>" "ab|c"
```

## <C-e>

Types the character on the line below at the cursor's column, so a line
can copy the one under it. Columns count a tab as the columns it shows as.
When there is no line below, or it is too short, it beeps.

```keys
"|\nabc" "A<C-e><C-e>" "ab|\nabc"
```

## <C-g>

With `u` after it, ends the undo step and starts a new one, so `u` later
takes back only what was typed after it. Any other key after `<C-g>` does
nothing.

## <C-h>

Deletes the character before the cursor. The same as `<BS>`.

```keys
"ab|c" "i<C-h>" "a|c"
```

## <C-k>

Types the character the next two keys name as a digraph, in either order,
so `e'` and `'e` both type é. When the two name none, it types the second.
`<Esc>` after it types nothing, and another named key types its name.

```keys
"|" "i<C-k>e'" "é|"
"|" "i<C-k>'e" "é|"
"|" "i<C-k>Eu" "€|"
"|" "i<C-k>xq" "q|"
"|" "i<C-k>e<Esc>" "|"
```

## <C-o>

Runs one normal mode command, then comes back to insert mode where it
left the cursor. The cursor does not move left first, and after `$` it
stays past the line's last character, ready to type. A command that
starts insert mode itself ends this, and from visual mode it comes back
when the selection ends.

```keys
"|abc def" "i<C-o>dwx" "x|def"
"|abc" "i<C-o>$x" "abcx|"
```

## <C-r>

Types the register named next, as typed text, so `<C-r>"` types what was
last deleted or yanked, `<C-r>+` the clipboard, `<C-r>%` the file's name
and `<C-r>/` the last search. An empty register types
nothing.

```keys
"|ab" "yiwA <C-r>\"" "ab ab|"
```

## <C-t>

Indents the cursor's line by one indent level, keeping the cursor on the
same text. With `expand_tab` the indent is spaces only.

```keys
"|x" "i<C-t>" "\t|x"
"a|b" "a<C-t>" "\tab|"
```

## <C-u>

Deletes what was typed on the line. With nothing typed before the cursor,
it deletes back to the indent, then to the start of the line, and at the
start it joins the line to the one above.

```keys
"a|b" "A cd<C-u>" "ab|"
"  a|b" "A<C-u>" "  |"
"  a|b" "A<C-u><C-u>" "|"
"a\n|b" "i<C-u>" "a|b"
```

## <C-v>

Types the next key as it is, with no mapping or indent, so `<C-v><Tab>`
types a tab even with `expand_tab`. A number after it names a character
instead: up to three decimal digits, `o` and three octal digits, `x` and
two hex digits, `u` and four, or `U` and eight. A key that is not a digit
ends the number early and types after it. A named key types the character
it sends, as `<CR>` types a carriage return, or its name when it sends
none.

```keys
"|" "i<C-v>065" "A|"
"|" "i<C-v>x41" "A|"
"|" "i<C-v>u20ac" "€|"
"|" "i<C-v>65x" "Ax|"
```

## <C-w>

Deletes the word before the cursor, and the blanks after it. It stops once
where typing began, so a second `<C-w>` goes on into older text. At the
start of a line it joins the line to the one above.

```keys
"foo ba|r" "A<C-w>" "foo |"
"fo|o" "Abar<C-w>" "foo|"
"fo|o" "Abar<C-w><C-w>" "|"
"a\n|b" "i<C-w>" "a|b"
```

## <C-y>

Types the character on the line above at the cursor's column, as `<C-e>`
types the one below.

```keys
"abc\n|" "A<C-y><C-y>" "abc\nab|"
```

## <CR>

Breaks the line at the cursor. The new line gets the indent of the one it
came from, and one level more after `(`, `[` or `{`. When the matching
closing bracket comes next, it goes a line further down, at the first
line's indent. In replace mode it puts in a line break and types over
nothing.

```keys
"ab|c" "i<CR>" "ab\n|c"
"  a|b" "i<CR>" "  a\n  |b"
"f(|)" "i<CR>" "f(\n\t|\n)"
"  |a" "A<CR><Esc>" "  a\n|"
```

## <Del>

Deletes the character under the cursor. At the end of a line it joins the
next line to it.

```keys
"a|bc" "i<Del>" "a|c"
"a|b\nc" "a<Del>" "ab|c"
```

## <Down>

Moves to the line below, at the column the cursor last moved along a line
to, or the end of a shorter line. On the last line it stays.

```keys
"ab|c\nd\nefg" "a<Down>" "abc\nd|\nefg"
"ab|c\nd\nefg" "a<Down><Down>" "abc\nd\nefg|"
```

## <End>

Goes past the last character of the line, where typing appends.

```keys
"|abc" "i<End>x" "abcx|"
```

## <Esc>

Leaves insert mode. The cursor moves one character left, onto the last
one typed, but not past the start of the line. The text typed goes to the
register `.`, and `gi` starts insert mode again where it left. After a
count, as `3ix`, the text goes in that many times, and after `3o` on that
many lines.

```keys
"|abc" "ix<Esc>" "|xabc"
"|abc" "A<Esc>" "ab|c"
```

## <Home>

Goes to the start of the line, before any indent.

```keys
"  ab|c" "i<Home>x" "x|  abc"
```

## <Insert>

Switches between insert and replace mode. In normal mode it starts insert
mode, as `i` does.

```keys
"|abc" "i<Insert>xy" "xy|c"
"|abc" "i<Insert>x<Insert>y" "xy|bc"
```

## <Left>

Moves one character left, not past the start of the line.

```keys
"ab|c" "i<Left>x" "ax|bc"
"|ab" "ix<Left>y<Esc>u" "x|ab"
```

## <Right>

Moves one character right, up to past the line's last character.

```keys
"a|bc" "i<Right>x" "abx|c"
```

## <S-Left>

Goes back to the start of the word before the cursor.

```keys
"|ab cd" "A<S-Left>x" "ab x|cd"
```

## <S-Right>

Goes to the start of the next word.

## <Up>

Moves to the line above, as `<Down>` moves down.

```keys
"efg\nd\nab|c" "a<Up>" "efg\nd|\nabc"
"a|b" "i<Up>x" "ax|b"
```

## (

With `autopairs.enabled` on, as it is by default, `(`, `[`, `{`, `"`, `'` and
`` ` `` type their closing one too, with the cursor between, when typed
before a blank, the end of a line or a closing bracket. A quote after a
letter, a digit or `_` types only itself. Typing a closing one that comes
next steps over it. Pairs form only in insert mode, never in replace mode
or from a paste. To type each one alone:

```toml
[module.autopairs]
enabled = false
```
