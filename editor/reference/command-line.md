# Command line

The `:` line and the searches `/` and `?` share these keys. Typed text goes
in at the line's cursor. `:` lines keep one history and searches another,
`/` and `?` alike, so `<Up>` in a search never recalls a command. A count,
a register or an operator typed before `/` or `?` waits for the search, so
`d/x<CR>` deletes up to the next `x`, and in visual mode a search moves the
selection's end.

In a `:` line, `<Tab>` and `<C-n>` put the next completion of the word
being typed in the line, and `<S-Tab>` and `<C-p>` the one before. A menu
above the line shows them, ranked by how well they match, and after the
last comes the word as typed. The word is a command's name, its short
names matching as it does, or an argument the command takes, such as a
path for `:edit` or an option for `:set`. The menu lights the item the
word is, or else its first. While the typed word is in the line, `<CR>`
runs the lit item and `<Tab>` puts it in the line. `<C-y>` keeps the
completion in the line and `<C-e>` puts the typed word back. With
`autocomplete` on, the menu shows as the line is typed. Searches do not
complete.

## <BS>

Deletes the character before the cursor. On an empty line it leaves the
command line, as `<Esc>` does. With the cursor at the start of a line that
is not empty, it does nothing.

```keys
"|ab ac" "/ab<BS>c<CR>" "ab |ac"
"|ab" "/a<BS><BS>x" "|b"
```

## <C-b>

Goes to the start of the line, as `<Home>` does.

```keys
"|ab ac" "/c<C-b>a<CR>" "ab |ac"
```

## <C-e>

Goes to the end of the line, as `<End>` does. While a completion menu
shows in a `:` line, it puts the word back as typed and closes the menu
instead.

```keys
"|xa ax" "/a<Home><C-e>x<CR>" "xa |ax"
```

## <C-h>

Deletes the character before the cursor, or leaves an empty line. The same
as `<BS>`.

```keys
"|ab ac" "/ab<C-h>c<CR>" "ab |ac"
```

## <C-r>

Types the register named next at the cursor, without its last line break,
so `<C-r>"` types what was last yanked. `<C-r><C-w>` types the word under
the text's cursor instead, so `:s/<C-r><C-w>/` starts a substitute of it.
An empty register types nothing.

```keys
"|foo bar foo" "yiw/<C-r>\"<CR>" "foo bar |foo"
"|foo bar foo" "/<C-r><C-w><CR>" "foo bar |foo"
"|ab ab" ":s/<C-r><C-w>/x/g<CR>" "|x x"
```

## <C-u>

Deletes all of the line before the cursor, and keeps what is after it.

```keys
"|ab ac" "/zz<C-u>ac<CR>" "ab |ac"
"|ab ac" "/zzac<Left><Left><C-u><CR>" "ab |ac"
```

## <C-w>

Deletes the word before the cursor, and the spaces after it. A word is a
run of letters, digits and `_`, or a run of other characters, so in
`:e dir/file` it deletes `file`, then `/`.

```keys
"|ab ac" "/ac zz<C-w><BS><CR>" "ab |ac"
"|ab ac" "/ac.<C-w><CR>" "ab |ac"
```

## <CR>

Runs a `:` line as a command, or a search line as a search. A search with an
empty line repeats the last one. A `:` line goes to the register `:`, so
`@:` runs it again, and a search becomes the last search, for `n` and
`N`. Each line goes in its history as the newest, once.

```keys
"|ab ab ab" "/ab<CR>/<CR>" "ab ab |ab"
"|a\nb\nc" ":2d<CR>" "a\n|c"
"|ab ac" "v/ac<CR>d" "|c"
```

## <Del>

Deletes the character under the cursor. At the end of the line it does
nothing.

```keys
"|ab ac" "/axc<Left><Left><Del><CR>" "ab |ac"
```

## <Down>

Recalls the next newer line of the history that starts with what was typed
before the first recall. Past the newest, it puts that typed text back.

```keys
"|b ac b ac" "/ac<CR>/b<CR>/<Up><Up><Down><CR>" "|b ac b ac"
```

## <End>

Goes to the end of the line.

```keys
"|xa ax" "/a<Home><End>x<CR>" "xa |ax"
```

## <Esc>

Leaves the command line without running it. A count, a register or an
operator typed before it is dropped, and a search scrolls the window back
to where it was.

```keys
"|ab ac" "d/ac<Esc>" "|ab ac"
"|ab ac" ":d<Esc>" "|ab ac"
```

## <Home>

Goes to the start of the line.

```keys
"|ab ac" "/c<Home>a<CR>" "ab |ac"
```

## <Left>

Moves the cursor one character left, not past the start of the line.

```keys
"|ab ac" "/c<Left>a<CR>" "ab |ac"
```

## <Right>

Moves the cursor one character right, not past the end of the line.

```keys
"|ab ac" "/a<Left><Right>c<CR>" "ab |ac"
```

## <Up>

Recalls the next older line of the history that starts with what was
typed before the first recall, so `:s<Up>` finds the last substitute. With
none older, the line stays.

```keys
"|b ac b ac" "/ac<CR>/b<CR>/a<Up><CR>" "b ac b |ac"
"|b ac b ac" "/ac<CR>/b<CR>/<Up><CR>" "|b ac b ac"
```
