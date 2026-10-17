# Editing text

A change in normal mode is an operator followed by a motion or a text
object, and a count may come before either. `2d3w` deletes six words.

## Operators

- `d` deletes, `c` deletes and enters insert mode, and `y` copies.
- `>` and `<` indent and dedent, `gu`, `gU` and `g~` change case.
- `gc` comments lines, or uncomments them, in the language at the cursor.
- `gq` formats lines through the language server.

An operator typed twice acts on the line, so `yy` copies it and `>>`
indents it. `D`, `C` and `Y` act to the end of the line.

## Text objects

After an operator or in visual mode, `i` selects inside a thing and `a`
around it.

- `iw` and `aw` a word, `iW` and `aW` a run of non-blanks.
- `is` and `as` a sentence, `ip` and `ap` a paragraph, `il` and `al` a
  line.
- `i(`, `i[`, `i{`, `i<`, `i"` and `i'` inside a pair, `a(` and the others
  around it.
- `if` and `af` a function, `ic` and `ac` a class, `ia` and `aa` an
  argument, when the file's grammar is built.

`ci"` changes the text inside the quotes around the cursor, wherever the
cursor is in them.

## Several places at once

`<C-n>` selects the word under the cursor, and each `<C-n>` after adds its
next match, so `<C-n><C-n><C-n>c` changes three of them as you type.
`/word<CR>` then `cgn` changes one match, and `.` the next. In visual mode
`:select` takes every node of a tree-sitter query, and `<C-v>` with `I`
or `A` types on every line of a block.

## Surrounding

`ys` puts brackets or quotes around what a motion or object covers, and
the next key says which: `ysiw)` makes `word` into `(word)`, and `ysiw(`
into `( word )`. `yss` surrounds the line, and `S` in visual mode the
selection. `ds"` deletes the quotes around the cursor, and `cs"'` changes
them to single quotes. `b`, `r`, `B` and `a` stand for `)`, `]`, `}` and
`>`, and `.` repeats any of them.

## Registers and the clipboard

Deleted and copied text goes to the unnamed register, and `p` and `P` put
it after or before the cursor, as lines when it was lines. `"a` before a
command uses register `a` instead, and `"A` appends to it. `"_` throws the
text away. `"+` is the system clipboard, and with the `clipboard` option
on, every copy and delete goes there too.

## Repeating

`.` repeats the last change, and a count before it replaces the change's
own. `qa` starts recording keys into register `a` and `q` stops. `@a`
plays them back, `@@` plays the last register played, and `Q` the last one
recorded. `:reg a` shows the keys as text, and editing that text changes
them.

## Searching and substituting

`/text` searches forward and `?text` back, and `n` and `N` repeat. `*` and
`#` search for the word under the cursor. Matches stay highlighted until
`<C-l>`. With `ignore_case` and `smart_case` on, a pattern ignores case
unless it has a capital.

`:s/old/new/g` replaces on the line, and `:%s/old/new/g` in the whole
file. While the command is typed, the window shows each match struck out
with its replacement. `&` repeats the last substitute. A pattern that
needs backtracking, such as a back reference, is refused with a message
naming the part.

## Marks and lines

`ma` sets mark `a`, and `` `a `` goes to it, `'a` to its line. Marks
move with edits above them. `:g/pat/d` deletes every line that matches,
and `:v/pat/d` every line that does not. `:5,9normal A;` types `;` at the
end of lines 5 to 9. `<C-a>` and `<C-x>` add to and subtract from the
number under the cursor.

## Insert mode

In insert mode, `<C-w>` deletes the word before the cursor and `<C-u>`
the line before it. `<C-r>a` types register `a`, `<C-t>` and `<C-d>`
indent and dedent the line, and `<C-o>` runs one normal mode command.

## Next

- `guide-windows` works with several files.
- `operators`, `text-objects` and `motions` list every key.
