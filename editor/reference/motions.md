# Motions

A motion moves the cursor in normal and visual mode, and after an operator
it is the operator's target. n is the count typed before it, or 1 without
one, which repeats it or says where it goes.

After an operator a motion covers text one of three ways:

- An exclusive motion stops short of where it lands, so `dw` leaves the
  next word's first letter.
- An inclusive one takes that character too, so `de` deletes the word's
  last letter.
- A linewise one takes whole lines, so `dj` deletes two.

An exclusive motion that ends at the start of a line stops at the end of
the line before instead. If it began at or before the first non-blank of
its own line, it then takes whole lines.

A motion that cannot move, such as `j` on the last line, fails. The
operator does nothing, and a macro running it stops. A jump records where
it left from, so `<C-o>` comes back and `''` goes there.

## h

Moves n characters left on the line, never past its start.

- A character here is what shows as one, so an emoji with a skin tone
  is one step.
- It is exclusive, so `dh` deletes the character before the cursor.

```keys
"abc|def" "2h" "a|bcdef"
"abc|def" "10h" "|abcdef"
"abc|def" "dh" "ab|def"
```

## l

Moves n characters right on the line.

- In normal mode the cursor stops on the last character.
- After an operator it may reach the line's end, so `dl` on the last
  character deletes it.
- It is exclusive.

```keys
"|abcdef" "3l" "abc|def"
"|abcdef" "10l" "abcde|f"
"|abcdef" "2dl" "|cdef"
```

## j

Moves n lines down.

- The cursor keeps the column it had when it last moved along a line,
  or goes to the end of a line too short for it.
- A closed fold counts as one line.
- A count past the last line goes there, and on the last line `j`
  fails.
- It is linewise, so `dj` deletes the line and the one below.

```keys
"abcd|ef\nab\nabcdef" "j" "abcdef\na|b\nabcdef"
"abcd|ef\nab\nabcdef" "jj" "abcdef\nab\nabcd|ef"
"a|b\nc\nd" "dj" "|d"
```

## k

Moves n lines up, as `j` moves down.

```keys
"abcdef\nab\nabcd|ef" "k" "abcdef\na|b\nabcdef"
"a\n  b|c\n  d" "dk" "  |d"
```

## <BS>

Moves left, n characters.

- From a line's start it moves onto the last character of the line
  before.
- After an operator it takes that line break, so `d<BS>` at a line's
  start joins the line to the one before.
- It is exclusive.

```keys
"ab\n|cd" "<BS>" "a|b\ncd"
"ab\n|cd" "d<BS>" "ab|cd"
```

## gg

Goes to the first line, or to line n, at the column `j` and `k` aim for.
A jump, and linewise.

```keys
"abcdef\nab\nabcd|ef" "gg" "abcd|ef\nab\nabcdef"
"abcd|ef\nab\nabcdef" "2gg" "abcdef\na|b\nabcdef"
"a\nb\n|c" "dgg" "|"
```

## G

Goes to the last line, or to line n, as `gg` goes to the first. A jump,
and linewise, so `dG` deletes to the end of the text.

```keys
"abcd|ef\nab\nabcdef" "G" "abcdef\nab\nabcd|ef"
"abcd|ef\nab\nabcdef" "2G" "abcdef\na|b\nabcdef"
"a\n|b\nc" "dG" "|a"
```

## <C-End>

Goes to the last character of the last line, or of line n. Inclusive,
and a jump. `<C-Home>` is `gg`.

```keys
"|ab\ncd" "<C-End>" "ab\nc|d"
```

## gj

Moves n rows down the screen, where a long line wraps onto several, at
the cursor's column in its row.

- Without `wrap` it moves as `j` does.
- It counts a row as the window draws it, with the text a module shows
  in a line, such as an inlay hint.
- It is exclusive.

## gk

Moves n rows up the screen, as `gj` moves down.

## go

Goes to byte n of the text, counting from 1, or to the start of the
character that byte is in. Exclusive.

```keys
"|abcdef\nxy" "4go" "abc|def\nxy"
"|ab\ncd" "4go" "ab\n|cd"
```

## H

Goes to the top line of the window, or line n from the top, at the column
`j` and `k` aim for. A jump, and linewise.

```keys
"a\nb\n|c" "H" "|a\nb\nc"
"a\nb\n|c" "2H" "a\n|b\nc"
```

## M

Goes to the middle of the lines the window shows, at the column `j` and
`k` aim for. A jump, and linewise.

```keys
"|a\nb\nc" "M" "a\n|b\nc"
```

## L

Goes to the bottom line of the window, or line n from the bottom, as `H`
goes to the top.

```keys
"|a\nb\nc" "L" "a\nb\n|c"
"|a\nb\nc" "2L" "a\n|b\nc"
```

## %

Goes to the bracket that matches the first `(`, `)`, `[`, `]`, `{` or `}`
at or after the cursor on its line, across lines if it must.

- It fails when the line has none after the cursor or the bracket has
  no match.
- It is inclusive, so `d%` on a bracket deletes both brackets and what
  is between.
- With a count it goes to the line n percent down the text instead,
  linewise.
- It is a jump.

```keys
"|f(a, b) x" "%" "f(a, b|) x"
"f(a, b|) x" "%" "f|(a, b) x"
"f|(a, b) x" "d%" "f| x"
"|1\n2\n3\n4" "50%" "1\n|2\n3\n4"
```

## (

Goes to the start of the sentence, or of the one before when the cursor
is already at a start, n times.

- A sentence ends at `.`, `!` or `?`, and any `)`, `]`, `"` or `'` after
  it, followed by a blank or the line's end.
- An empty line also starts one.
- It is exclusive, and a jump.

```keys
"One. Two |three." "(" "One. |Two three."
"One. Two |three." "2(" "|One. Two three."
```

## )

Goes to the start of the next sentence, n times, as `(` goes back.

```keys
"|One. Two. Three." ")" "One. |Two. Three."
"|One. Two. Three." "2)" "One. Two. |Three."
"One. |Two. Three." "d)" "One. |Three."
```

## {

Goes to the empty line above, n times, or to the start of the text.

- A line of blanks is not empty.
- It is exclusive, and a jump, so `d{` deletes back to the empty line.

```keys
"a\n\nb\n|c" "{" "a\n|\nb\nc"
"a\n\nb\n\n|c" "2{" "a\n|\nb\n\nc"
```

## }

Goes to the empty line below, n times, or to the end of the text, as `{`
goes up.

```keys
"|a\nb\n\nc" "}" "a\nb\n|\nc"
"|a\nb\n\nc" "d}" "|\nc"
```

## 0

Goes to the start of the line. Exclusive, so `d0` deletes from there to
the cursor.

```keys
"  ab|cd" "0" "|  abcd"
"  ab|cd" "d0" "|cd"
```

## ^

Goes to the first character of the line that is not a space or a tab.
Exclusive.

```keys
"  ab|cd" "^" "  |abcd"
"  ab|cd" "d^" "  |cd"
```

## $

Goes to the last character of the line, or of the line n-1 below.

- It is inclusive, so `d$` deletes to the line's end, as `D` does.
- After `$`, `j` and `k` keep to the end of each line they reach.

```keys
"|abc\nde" "$" "ab|c\nde"
"|abc\nde" "2$" "abc\nd|e"
"|abc\nde\nfghij" "$jj" "abc\nde\nfghi|j"
"a|bc" "d$" "|a"
```

## g_

Goes to the last character of the line that is not a space or a tab, or
of the line n-1 below. Inclusive, so `dg_` keeps the blanks at the end.

```keys
"|ab  \ncd " "g_" "a|b  \ncd "
"|ab  \ncd " "2g_" "ab  \nc|d "
"|ab  " "dg_" "|  "
```

## |

Goes to column n of the line, 1 without a count.

- It counts a tab as the columns it shows as.
- On a column inside a tab, it goes to the tab.
- It is exclusive.

```keys
"|abcdef" "4|" "abc|def"
"\t|x" "3|" "|\tx"
"abc|def" "d1|" "|def"
```

## +

Goes to the first non-blank of the line n lines down. It fails on the
last line. Linewise.

```keys
"|a\n  b\nc" "+" "a\n  |b\nc"
"|a\n  b\nc" "2+" "a\n  b\n|c"
"a\n|b" "+" "a\n|b"
```

## <CR>

Goes to the first non-blank of the line n lines down, as `+` does.

```keys
"|a\n  b\nc" "<CR>" "a\n  |b\nc"
```

## -

Goes to the first non-blank of the line n lines up.

- It fails on the first line.
- It is linewise.
- In normal mode `-` alone lists the file's directory, as the explorer
  maps it, so the motion is for after an operator and in visual mode.

```keys
"a\n|b\nc" "d-" "|c"
"a\n|b\nc" "V-d" "|c"
```

## _

Goes to the first non-blank of the line, or of the line n-1 below.
Linewise, so `d_` deletes the line as `dd` does, and `d2_` two lines.

```keys
"  a|b\nc" "_" "  |ab\nc"
"|a\nb\nc" "d2_" "|c"
```

## [(

Goes to the n-th `(` before the cursor that is not closed before it, the
start of the parentheses around the cursor. It looks across lines, and
fails when there is none. Exclusive.

```keys
"f(a, (|b), c)" "[(" "f(a, |(b), c)"
"f(a, (|b), c)" "2[(" "f|(a, (b), c)"
```

## [{

Goes to the n-th `{` before the cursor that is not closed before it, as
`[(` does for `(`.

```keys
"{\n  a|\n}" "[{" "|{\n  a\n}"
```

## ])

Goes to the n-th `)` after the cursor that was not opened after it, the
end of the parentheses around the cursor. Exclusive, so `d])` keeps the
`)`.

```keys
"f(a, (|b), c)" "])" "f(a, (b|), c)"
"f(a, (|b), c)" "2])" "f(a, (b), c|)"
"f(a|b)" "d])" "f(a|)"
```

## ]}

Goes to the n-th `}` after the cursor that was not opened after it, as
`])` does for `)`.

```keys
"{\n  a|\n}" "]}" "{\n  a\n|}"
```

## ]]

Goes to the start of the next line that starts with `{`, n times, or to
the last character of the text when there is none.

- After an operator it also stops below a line that starts with `}`, so
  `d]]` deletes to the end of a function and no further.
- It is exclusive, and a jump.

```keys
"|a\n{\nb\n}\nc\n{\nd" "]]" "a\n|{\nb\n}\nc\n{\nd"
"|a\n{\nb\n}\nc\n{\nd" "3]]" "a\n{\nb\n}\nc\n{\n|d"
"a\n{\n|b\n}\nc\n{\nd" "d]]" "a\n{\n|c\n{\nd"
```

## ][

Goes to the start of the next line that starts with `}`, n times, or to
the last character of the text.

```keys
"|a\n{\nb\n}\nc\n{\nd" "][" "a\n{\nb\n|}\nc\n{\nd"
```

## [[

Goes to the start of the line above that starts with `{`, n times, or to
the first line.

```keys
"a\n{\nb\n}\nc\n{\n|d" "[[" "a\n{\nb\n}\nc\n|{\nd"
"a\n|{\nb" "[[" "|a\n{\nb"
```

## []

Goes to the start of the line above that starts with `}`, n times, or to
the first line.

```keys
"a\n{\nb\n}\nc\n{\n|d" "[]" "a\n{\nb\n|}\nc\n{\nd"
```

## ]C

Goes to the next multicursor, n times.

- After the last it goes round to the first, so `]CQ` removes
  multicursors one at a time.
- With none placed, it goes to the next of those `<C-l>` removed last.
- It is exclusive.

```keys
"|a\nb\nc" "QjQj]C" "|a\nb\nc"
"|a\nb\nc" "QjQj<C-l>ggd]C" "|b\nc"
```

## [C

Goes to the multicursor before, as `]C` goes to the next.

```keys
"|a\nb\nc" "QjQj[C" "a\n|b\nc"
```

## g0

Goes to the start of the screen row the cursor is on, where a long line
wraps. Without `wrap`, it goes to the first character the window shows of
the line. Exclusive.

```keys
"ab|cd" "g0" "|abcd"
```

## g^

Goes to the first non-blank of the screen row, as `g0` goes to its start.

```keys
"  ab|cd" "g^" "  |abcd"
```

## gm

Goes half the window's width along the screen row from its start, or to
the row's last character when it is shorter.

## g$

Goes to the last character of the screen row, as `g0` goes to its start.
Inclusive, and after it `j` and `k` keep to the end of each line, as after
`$`.

```keys
"ab|cd" "g$" "abc|d"
```

## gM

Goes to the middle of the line's text, or with a count n percent of the
way along it, counting columns as they show. Exclusive.

```keys
"|abcdef" "gM" "abc|def"
"|abcdefghij" "20gM" "ab|cdefghij"
```

## w

Goes to the start of the next word, n times.

- A word is a run of letters, digits and `_`, or a run of other
  characters that are not blanks.
- An empty line counts as a word.
- It is exclusive.
- After an operator the last word stops at its line's end, so `dw` on a
  line's last word keeps the line break.
- `cw` on a word changes to its end, as `ce` does.

```keys
"|foo bar.baz" "w" "foo |bar.baz"
"|foo bar.baz" "2w" "foo bar|.baz"
"foo |bar baz" "dw" "foo |baz"
"one |two\n  three" "dw" "one| \n  three"
"foo |bar baz" "cwX<Esc>" "foo |X baz"
```

## W

Goes to the start of the next run of non-blanks, n times, as `w` goes to
the next word.

```keys
"|foo bar.baz\n  qux" "2W" "foo bar.baz\n  |qux"
```

## b

Goes to the start of the word, or of the word before when the cursor is
at a start, n times. An empty line counts as a word. Exclusive.

```keys
"foo bar.baz\n  |qux" "b" "foo bar.|baz\n  qux"
"foo bar ba|z" "db" "foo bar |z"
```

## B

Goes to the start of the run of non-blanks, or of the one before, n
times, as `b` does with words.

```keys
"foo bar.baz\n  |qux" "B" "foo |bar.baz\n  qux"
```

## e

Goes to the end of the word, or of the next one when the cursor is at an
end, n times. Inclusive, so `de` deletes to the word's last letter.

```keys
"|foo bar.baz" "e" "fo|o bar.baz"
"foo |bar baz" "de" "foo | baz"
```

## E

Goes to the end of the run of non-blanks, or of the next one, n times, as
`e` does with words.

```keys
"|foo bar.baz\n  qux" "3E" "foo bar.baz\n  qu|x"
```

## ge

Goes back to the end of the word before, n times. An empty line counts as
a word. Inclusive, so `dge` deletes from there to the cursor's character.

```keys
"foo bar.ba|z" "ge" "foo bar|.baz"
"foo\n\n|bar" "ge" "foo\n|\nbar"
"foo ba|r" "dge" "f|o"
```

## gE

Goes back to the end of the run of non-blanks before, n times, as `ge`
does with words.

```keys
"foo bar.ba|z" "gE" "fo|o bar.baz"
```

## n

Goes to the next match of the last search, n times, the way it searched.

- It goes forward after `/` or `*` and back after `?` or `#`.
- At an end of the text it goes on from the other.
- It fails when there is no match.
- The matches show highlighted until `<C-l>`.
- It is exclusive, and a jump.

```keys
"|a x a x" "/x<CR>n" "a x a |x"
"|a x a x" "?x<CR>n" "a |x a x"
```

## N

Goes to the match of the last search the other way, n times, as `n` goes
the same way.

```keys
"|a x a x" "/x<CR>nN" "a |x a x"
```

## *

Searches forward for the word under the cursor, or the first word after
it on the line, as a whole word, n times.

- It says so when there is none.
- `n` then finds the next.
- It is exclusive, and a jump.

```keys
"|foo x foobar foo" "*" "foo x foobar |foo"
"foo x foobar |foo" "*" "|foo x foobar foo"
```

## #

Searches back for the word under the cursor, as `*` searches forward.

```keys
"foo x foobar |foo" "#" "|foo x foobar foo"
```

## g*

Searches forward for the word under the cursor as `*` does, but also
where it is part of a longer word.

```keys
"|foo x foobar foo" "g*" "foo x |foobar foo"
```

## g#

Searches back for the word under the cursor as `#` does, but also where
it is part of a longer word.

```keys
"|foo x foobar foo" "g#" "foo x foobar |foo"
```

## gd

Goes to the first match of the word under the cursor in its function.

- The search starts at the blank line above the line starting with `{`
  above the cursor, or at the first line when there is none.
- The word becomes the last search, as with `*`.
- A language server's definition is on `<C-]>`.

```keys
"x := 1\n\nfunc f()\n{\n\tx := 2\n\tprint(|x)\n}" "gd" "x := 1\n\nfunc f()\n{\n\t|x := 2\n\tprint(x)\n}"
```

## gD

Goes to the first match of the word under the cursor in the file.

```keys
"x := 1\n\nfunc f()\n{\n\tx := 2\n\tprint(|x)\n}" "gD" "|x := 1\n\nfunc f()\n{\n\tx := 2\n\tprint(x)\n}"
```

## ;

Repeats the last `f`, `t`, `F` or `T` the same way, n times. After `t` and
`T` it passes over the character next to the cursor, so `;` moves on.

```keys
"|a,b,c,d" "f,;" "a,b|,c,d"
"|a,b,c,d" "t,;" "a,|b,c,d"
```

## ,

Repeats the last `f`, `t`, `F` or `T` the other way, n times.

```keys
"|a,b,c,d" "f,;," "a|,b,c,d"
```

## `{mark}

Goes to the mark, where `m` set it or fude did, as `` `a `` goes to mark
`a`.

- Marks move with the text when lines above them change.
- It fails and says so when the mark is not set.
- It is exclusive, and a jump.

fude sets these marks:

- `` ` `` and `'` are where the last jump left from.
- `.` is the last change.
- `[` and `]` are the start and end of the last change, put or yank.
- `^` is where insert mode was last left.
- `<` and `>` are the ends of the last selection.
- `"` is where the cursor was when the buffer was last left.
- `(` and `)`, `{` and `}` are where those motions go from the cursor.

```keys
"x\nfoo|bar" "max0`a" "x\nfoo|ar"
"|a\nb\nc" "G``" "|a\nb\nc"
"|ab cd ef" "wyw0`]" "ab cd| ef"
"One. |Two. Three." "d`)" "One. |Three."
```

## '{mark}

Goes to the first non-blank of the mark's line, as `` ` `` goes to the
mark itself. Linewise, so `d'a` deletes the lines from the cursor's to the
mark's.

```keys
"  foo\nbar" "llmaj'a" "  |foo\nbar"
"|a\nb\nc" "G''" "|a\nb\nc"
"foo bar\nbaz" "jmaggd'a" "|"
```

## /{pattern}<CR>

Searches forward for the pattern typed between `/` and `<CR>`, n times.

- At the end of the text it goes on from the start.
- The pattern is a regular expression in Vim's syntax, and fude runs it
  in time linear in the text, so it refuses one that needs backtracking.
- An empty pattern searches for the last one again.
- With `ignore_case` it ignores case, and with `smart_case` too a
  capital letter makes it match case.
- While it is typed, with `inc_search`, the window shows where it goes.
- It is exclusive, and a jump, so `d/x<CR>` deletes up to the match.

An offset may follow a second `/`:

- `e` goes to the match's last character, inclusive, and `s` or `b` to
  its first, each with `+n` or `-n` characters after it, as `/foo/e+1`.
- A number alone, as `/foo/+2`, goes n lines below the match, linewise.
- With `e`, `s` or `b`, the match the cursor is on counts when the place
  the offset names is past the cursor.
- `n` and `N` keep the offset, and `//e` takes the last pattern with a
  new offset.

```keys
"|a\nfoo\nbar" "/oo<CR>" "a\nf|oo\nbar"
"|a\nfoo\nbar" "d/oo<CR>" "|oo\nbar"
"|a.b\nab" "/a\\.b<CR>n" "|a.b\nab"
"|foo bar foo" "/foo/e<CR>" "fo|o bar foo"
"|foo bar foo" "/foo/e<CR>n" "foo bar fo|o"
"|foo bar foo" "d/foo/e<CR>" "| bar foo"
"|a\nfoo\nbar" "/foo/+1<CR>" "a\nfoo\n|bar"
```

## ?{pattern}<CR>

Searches back for the pattern, n times, as `/` searches forward. At the
start of the text it goes on from the end.

```keys
"a x a |x" "?a<CR>" "a x |a x"
```

## f{char}

Goes to the n-th char after the cursor on its line.

- It fails when there are fewer.
- `;` and `,` repeat it.
- It is inclusive, so `df,` deletes the comma too.

```keys
"|a,b,c,d" "f," "a|,b,c,d"
"|a,b,c,d" "2f," "a,b|,c,d"
"|a,b,c" "df," "|b,c"
```

## F{char}

Goes back to the n-th char before the cursor on its line, as `f` goes
forward. Exclusive, so `dF(` keeps the character under the cursor.

```keys
"a,b,c,|d" "F," "a,b,c|,d"
"a(b|c)" "dF(" "a|c)"
```

## t{char}

Goes to just before the n-th char after the cursor on its line, as `f`
goes to it. Inclusive, so `dt)` deletes up to the `)` and keeps it.

```keys
"|a,b,c,d" "t," "|a,b,c,d"
"|foo(bar)" "dt)" "|)"
```

## T{char}

Goes back to just after the n-th char before the cursor on its line, as
`F` goes to it.

```keys
"a,b,c,|d" "2T," "a,b,|c,d"
```
