# Options

The settings of a window. `:set name value` changes one in the current
window and those opened after it, and `:set name` shows its value. A
switch takes true or false. config.toml sets their starting values, and a
`[filetype.<name>]` table sets some for one filetype.

## number

Shows each line's number on the left. With `relative_number` on too, the
cursor's line shows its number and the others their distance from it.

## relative_number

Numbers each line by its distance from the cursor's, for counts such as
`5j`.

## wrap

A line longer than the window goes on over the rows below. Off, each line
takes one row and the window scrolls sideways to keep the cursor in view.

## line_break

With `wrap` on, a long line goes on over the rows below between words, so
no word is cut in two at the window's edge. A word longer than the window
still breaks. `gj` and `gk` count the rows as they show.

## text_width

Typing past this column in insert mode breaks the line at the last blank
that leaves it within the column. The new line keeps the line's indent
and a comment's marker, as `//` or `#`, and goes under the text of a list
item. `gq` fills prose to this width when nothing else formats the file,
or to 79 columns when it is 0. 0, the default, never breaks a line typed.
A `[filetype.markdown]` table may set it for one filetype.

## scroll_off

How many lines to keep in view above and below the cursor's, at most half
the window. `8` keeps eight lines between the cursor and the window's top
and bottom.

## side_scroll_off

With `wrap` off, how many columns to keep in view left and right of the
cursor's, at most half the window. The window scrolls sideways one column
at a time to keep them.

## cursor_line

Shades the line the cursor is on, and shows its number in bold.

## cursor_column

Shades the column the cursor shows in, on every row of text in the
window, so text above and below can be lined up with it.

## list

Shows a tab as `>` and blanks, a trailing space as `-` and a no-break space
as `+`. The indent guides hide while it is on.

## ignore_case

Searches, `:s` and their highlights ignore case. `\C` in a pattern matches
case anyway.

## smart_case

With `ignore_case` on, a pattern with a capital letter matches case. `\c` in
a pattern ignores case anyway.

## expand_tab

Indents with spaces only. `<Tab>` in insert mode types spaces to the next
indent, and `:retab` turns a file's leading tabs into spaces.

## clipboard

A yank, delete or change without a register also copies to the system
clipboard. A put without a register takes the clipboard when something
outside fude changed it, and else the unnamed register. `"_` still copies
nothing.

## sign_column

Keeps the sign column in every window, so the text does not move when a git
or diagnostic sign comes. Off, the column shows while the buffer has signs.

## color_column

Shades this column of the text, counting from 1, on each line. It is also
how many columns of text `:center` leaves, and centered windows end there,
so they shade none. The default is 0, for none.

## icons

Shows nerd font icons left of an explorer's names, and in place of the letters
of diagnostic signs. Needs a terminal font with nerd font glyphs.

## spell.enabled

Underlines the words the word list lacks: all the text of Markdown, text
and git commit files but their code and links, and the comments and
strings of other files. `]s` and `[s` go to them, `z=` lists words for
the one under the cursor, and `zg` adds it to your words. A filetype
table may turn it on, as `[filetype.markdown] spell = true`. Off unless
set.

## inc_search

While a search is typed, the window shows the match it goes to, lit apart
from the other matches, and `<Esc>` scrolls the window back. It is on unless
set off.

## lsp.diagnostic_notes

After a line with a diagnostic, its message shows in the severity's colour.
The default is on, and `:set lsp.diagnostic_notes false` turns the messages off
in every window of the buffer. The signs stay, and `]d` and `<C-w>d` still
show each one.

## markdown.enabled

A markdown file draws as it reads. A heading's `#`s go and its line is on
its level's colour, a code block is on the surface colour without its
fences, bullets, checkboxes, quotes and rules show as marks, and a
table's columns line up between box lines. Emphasis loses its marks, a
link shows its text alone, and an image file shows below its line where
the terminal shows images. The cursor's line, and
a selection's, show their text as it is. The default is on, and
`:set markdown.enabled false` shows the text as it is everywhere.
