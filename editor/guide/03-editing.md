# Editing text

A change in normal mode is an operator followed by a motion or a text
object, and a count may come before either. `2d3w` deletes six words.

## Operators

- `d` deletes, `c` deletes and enters insert mode, and `y` copies.
- `>` and `<` indent and dedent, `gu`, `gU` and `g~` change case. In visual
  mode, `>` and `<` keep the selection, so `V>>>` indents three times.
- `gc` comments lines, or uncomments them, in the language at the cursor.
- `gq` and `=` format lines through the filetype's formatters or the
  language server, and `gw` does the same and leaves the cursor where it
  was.
- `g?` rot13-encodes, and `!` opens the command line to put the lines
  through a shell command.
- `]e` and `[e` move the line down and up. In visual mode they move the
  selected lines and keep them selected.

An operator typed twice acts on the line, so `yy` copies it and `>>`
indents it. `D`, `C` and `Y` act to the end of the line. Each example
shows the text before, the keys and the text after, `|` at the cursor:

```keys
"|one two three four five six seven" "2d3w" "|seven"
"a\n|b\nc" "yyP" "a\n|b\nb\nc"
"|x\ny" ">>" "\t|x\ny"
"ab|cd" "D" "a|b"
```

## Text objects

After an operator or in visual mode, `i` selects inside a thing and `a`
around it.

- `iw` and `aw` a word, `iW` and `aW` a run of non-blanks.
- `is` and `as` a sentence, `ip` and `ap` a paragraph, `il` and `al` a
  line.
- `i(`, `i[`, `i{`, `i<`, `i"` and `i'` inside a pair, `a(` and the others
  around it.
- `it` and `at` inside and around a tag pair, such as `<b>` and `</b>`.
- `if` and `af` a function, `ic` and `ac` a class, `ia` and `aa` an
  argument, when the file's grammar is built.

`ci"` changes the text inside the quotes around the cursor, wherever the
cursor is in them.

```keys
"say \"h|i there\" now" "ci\"bye<Esc>" "say \"by|e\" now"
"f(a, |b)" "di(" "f(|)"
```

## Multicursor

fude edits several places at once through multicursors. `Q` places one
where the cursor is, or removes it. While multicursors are placed, every
command that edits runs again at each of them, on that multicursor's own
text, so `ciw` changes each one's own word and `<C-w>` in insert mode
takes each one's own word. Motions move only the cursor, so you can walk
to the next place and press `Q` again. `q=` turns follow on, and then
`w`, `j`, `fx` and the like move every multicursor. `u` takes back one
command at all of them. `<C-l>` removes them and `gQ` puts them back.

```keys
"|ab\ncd\nef" "QjQjA;<Esc>" "ab;\ncd;\nef|;"
```

There are faster ways to place them:

- `zq{motion}` places a multicursor at each place the motion reaches, to
  the end of the text, so `zqj` places one on every line and
  `zq*gvcnew<Esc>` renames every match of the word under the cursor.
- `<C-n>` on a word selects it, and each `<C-n>` after places a
  multicursor there and selects the next match, so
  `<C-n><C-n><C-n>cnew<Esc>` renames three matches. In visual mode it
  starts from the selected text.
- `Q` in visual mode places one on each selected line, with follow on.
- `:select` in visual mode places one at every node a tree-sitter query
  names in the selection, as `:select (identifier) @x` does.

Each multicursor keeps its own registers, so `yiw` at each and `p` puts
each its own word. When `<C-l>` removes them, the register holds every
one's text, one line each.

`/word<CR>` then `cgn` changes one match and `.` the next, so `n` can skip
one on the way. `<C-v>` with `I` or `A` types on every line of a block.

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
on, every copy and delete goes there too. `gp` and `gP` leave the cursor
after the text, and `]p` puts lines at the cursor line's indent. `".`
holds the text last typed in insert mode, `":` the last command line,
`"/` the last search and `"%` the file's name.

## Repeating

`.` repeats the last change, and a count before it replaces the change's
own.

```keys
"|a b c d" "dw." "|c d"
"|a b c d e" "dw2." "|d e"
```

`qa` starts recording keys into register `a` and `q` stops. `@a` plays
them back, and `@@` plays the last register played. `@:` runs the last
command line again. `:reg a` shows the keys
as text, and editing that text changes them.

## Searching and substituting

`/text` searches forward and `?text` back, and `n` and `N` repeat. `*` and
`#` search for the word under the cursor. Matches stay highlighted until
`<C-l>`, or never with `hl_search` off. A search that reaches the end goes
on from the start, unless `wrap_scan` is off. While you type a search, the
window shows the match it goes to, and `<Esc>` scrolls the window back.
With `ignore_case` and `smart_case` on, a pattern ignores case unless it
has a capital.

`:s/old/new/g` replaces on the line, and `:%s/old/new/g` in the whole
file. While the command is typed, the window shows each match struck out
with its replacement. `&` repeats the last substitute, and `g&` repeats
it on every line. `q:` lists the command lines entered and `q/` the
searches, in a window to edit, and `<CR>` there runs the line under the
cursor. A pattern that
needs backtracking, such as a back reference, is refused with a message
naming the part.

## Marks and lines

`ma` sets mark `a`, and `` `a `` goes to it, `'a` to its line. Marks
move with edits above them. `''` goes back to where the last jump left
from, `` `. `` to the last change and `` `^ `` to where insert mode was
left, and `gi` inserts there again. `:g/pat/d` deletes every line that matches,
and `:v/pat/d` every line that does not. `:5,9normal A;` types `;` at the
end of lines 5 to 9. `<C-a>` and `<C-x>` add to and subtract from the
number under the cursor.

## Insert mode

In insert mode, `<C-w>` deletes the word before the cursor and `<C-u>`
the line before it. `<C-r>a` types register `a`, `<C-t>` and `<C-d>`
indent and dedent the line, and `<C-o>` runs one normal mode command.
`<C-v>` types the next key as it is, `<C-k>e'` types é and the other
digraphs, `<C-a>` types the text last typed, and `<C-e>` and `<C-y>`
copy the character below and above the cursor. A control key typed after
`<C-v>` shows as `^` and a letter, as `<C-a>` shows as `^A`.

## Spelling

`:set spell.enabled true` underlines words the word list lacks: all of a Markdown,
text or git commit file but its code, and the comments and strings of
code. `]s` and `[s` go to them, `z=` lists words for the one under the
cursor, and `zg` adds it to your own words. `:set spell.enabled false`
turns it off. The list is `/usr/share/dict/words`, or the file `spell.words`
names.

```toml
[filetype.markdown]
spell = true
```

## Next

- `guide-windows` works with several files.
- `operators`, `text-objects` and `motions` list every key.
