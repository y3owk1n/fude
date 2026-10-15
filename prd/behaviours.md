# Behaviours

What fude promises. Each line is testable through a public boundary: the CLI,
a key sequence applied to a headless buffer, the file on disk, the plugin
ABI, the RPC protocol, or the exported API of `core/` and `vim/`. The tag
is the build order step in `prd/product.md`.

"As in Neovim" means what current stable Neovim does with its defaults.
Where fude departs, the line says so and cites the departure in
`prd/decisions.md`.

## Files and startup

- B1 [1] `fude <path>` opens the file. A path that does not exist opens an
  empty buffer and creates the file on first write.
- B2 [1] `:w` writes the buffer to its path atomically, through a temp file
  and rename. A failed write leaves the original file unchanged and shows the
  error with the path.
- B3 [1] fude keeps the file's line endings and final newline as it found
  them.
- B4 [1] A file that is not valid UTF-8 opens read-only with a message saying
  so.
- B5 [1] `:q` on a modified buffer refuses and names the buffer. `:q!` quits.
- B80 [1] `fude a b` opens both as buffers. `fude +N path` opens at line N.
- B95 [1] `:wq` writes and quits. `:x` and `ZZ` do the same but write only a
  modified buffer, and `ZQ` is `:q!`. `:wa` writes every modified buffer.
  `:qa` refuses while a buffer is modified, and `:qa!` quits anyway. `:wqa`
  and `:xa` write every modified buffer, then quit.
- B81 [1] `:w` on a file changed on disk since fude read it asks before
  overwriting. `:w!` overwrites without asking.
- B82 [1] A file changed on disk while unmodified in fude reloads when fude
  regains focus. If it is modified in fude, fude warns and keeps its text.
- B83 [1] A file over the `large_file` setting (50MB by default) opens with
  syntax and LSP off and says so. Editing and scrolling behave as for any file.

## Config

- B92 [1] fude starts with no config file and never creates one.
- B93 [1] An error in `config.toml` is shown with its line, and fude starts
  with the defaults for what failed to load.
- B94 [2] Settings in a `[filetype.<name>]` table apply to buffers of that
  filetype and override the top-level ones (D27).

## Modes and insert

- B6 [1] `i`, `a`, `I`, `A`, `o`, `O` enter insert mode at the positions Vim
  uses. `<Esc>` returns to normal mode and moves the cursor left as Neovim does.
- B7 [1] Text typed in insert mode is one timeline node from entering
  insert to leaving it, except where `<Left>`, `<Right>`, `<C-w>`, `<C-u>`
  or `<C-g>u` split it, as in Neovim.
- B8 [1] A bracketed paste is inserted as typed text, with no keymap applied.
- B9 [1] The cursor moves by grapheme cluster. An emoji with a skin tone
  modifier, or a letter with combining marks, is one step for `l` and `h`.

## Grammar

- B10 [2] `h j k l w b e W B E 0 ^ $ gg G f t F T ; ,` move as in Neovim,
  with counts.
- B11 [2] `d`, `c`, `y` combine with every motion and text object, with
  counts on both sides: `2d3w` deletes six words.
- B12 [2] `dd`, `cc`, `yy` act on lines, with counts.
- B13 [2] Text objects `iw aw iW aW is as ip ap il al` and the quote and
  bracket pairs `i" a" i' a' i( a( i[ a[ i{ a{ i< a<` select as in Neovim.
- B14 [2] `"x` before a command uses register `x`. Uppercase appends.
  `"_` discards. `"+` is the system clipboard.
- B15 [2] `p` and `P` put a register, linewise or charwise as it was yanked.
- B36 [2] `"+` and `"*` are the system clipboard: `y`, `d` and `c` write
  it, and `p`, `P`, `@` and insert `<C-r>` read it. Text ending in a line
  break puts as lines.
- B37 [2] A line longer than its window wraps onto the rows below, as with
  Neovim's `wrap`, and the window scrolls so the cursor's whole line shows.
  With `wrap` off, each line takes one row and the view scrolls sideways
  the least it must to show the cursor. Rows past the end of the text show
  `~`.
- B38 [2] Each window's status line ends with Neovim's ruler: the cursor's
  line and column, its screen column when that differs, and `All`, `Top`,
  `Bot` or how far down the view is. The current window's also shows the
  keys of a command being typed, as `showcmd` does.
- B39 [2] `:set` turns options on with `name`, off with `noname`, the other
  way with `name!` and shows them with `name?`, or every one with no
  arguments, as Neovim does with fude's option names (N4). `number` shows
  line numbers and `relative_number` numbers lines from the cursor's. Each
  is also a setting in `config.toml`.
- B116 [2] `scroll_off`, set with `:set scroll_off=n` or in `config.toml`,
  keeps n lines in view above and below the cursor's, at most half the
  window, as Neovim's `scrolloff` does.
- B117 [2] `cursor_line` shades the cursor's line and shows its number in
  bold, as Neovim's `cursorline` does.
- B118 [2] `list` shows a tab as `>` and blanks, a trailing space as `-`
  and a no-break space as `+`, Neovim's default `listchars`.
- B119 [5] A buffer with diagnostics shows a sign column, two columns left
  of the line numbers, with the letter of each line's worst diagnostic,
  `E`, `W`, `I` or `H`, as Neovim's `signcolumn=auto` does.
- B121 [2] With the cursor on a bracket, or just after one in insert mode,
  the bracket and its match in the window show bold and underlined, as
  Neovim's matchparen shows them.
- B122 [2] With `expand_tab`, `>`, `<`, `<C-t>` and `<C-d>` indent with
  spaces only, and `<Tab>` in insert mode types spaces to the next
  `indent_width` column (N10).
- B123 [2] A file in a git repository shows in the sign column how it
  differs from git's index, as gitsigns does: `┃` on added and changed
  lines, `▁` under the line before deleted ones, and `~` on a change that
  also deleted lines. A diagnostic's sign wins on its line. Without git
  installed, there are no git signs.
- B124 [2] `:e dir`, `fude dir` and `-` open a directory as a listing: a
  buffer of its entries' names, directories with a trailing `/`, with no
  line numbers. `-` lists the current file's directory with the cursor on
  it, `N-` goes up N levels and `1-` lists the working directory, as
  Neovim's built-in `-` does, in place of the `-` motion. In a listing,
  `<CR>` opens the entry, `<C-s>` and `<C-h>` open it in a split beside or
  below, `_` lists the working directory and `<C-l>` reads it again, as
  oil.nvim's keys do. B142 changes these keys.
- B125 [2] Editing a listing and `:w` changes the directory, as oil.nvim
  does: a changed name renames the entry, a deleted line deletes it, and a
  new line creates a file, or a directory when it ends in `/`. fude first
  asks with each change and the number of files a deleted directory
  holds, and changes nothing on `n`. Open buffers of renamed files follow
  them.
- B126 [2] A listing in a git repository shows each entry's git status in
  the sign column: `✹` for modified, `•` staged, `≠` both, `+` added, `?`
  untracked, `-` deleted and `→` renamed. A directory shows the status of
  the first change below it. Without git installed, there are no signs.
- B127 [2] A listing shows dim text after some names, as `ls -F` marks
  them: `-> target` after a symlink, `*` after an executable file, and `|`,
  `=`, `%` or `#` after a pipe, socket, character device or block device.
- B128 [2] With `icons` on, a listing shows a nerd font icon left of each
  name, in mini.icons' glyphs and colours. A file's icon goes by its name
  or extension, and a directory shows a folder. Diagnostic signs show
  icons in place of `E`, `W`, `I` and `H`. `icons` is off by default.
- B129 [2] `:files` opens a fuzzy file finder, as fff.nvim's is, over the
  working directory's files: in a git repository those git does not
  ignore, and elsewhere every file. The user types the query in insert
  mode. Below it are the files whose paths hold its letters in order,
  ranked as `matchfuzzy()` ranks them. `<C-n>` and `<C-p>` move the selection,
  `<CR>` opens the selected file, and leaving insert mode closes the
  finder (N11).
- B130 [2] On a screen of 60 columns or more, the finder shows the start
  of the selected file in a box right of the list, as fff.nvim does, with
  its filetype's highlights. A file with a zero byte in its first 16KB
  shows "binary file".
- B131 [2] In a `:` command line, `<Tab>` and `<C-n>` put the next
  completion of the word being typed in the line, and `<S-Tab>` and
  `<C-p>` the one before. After the last comes the word as typed. `<C-y>`
  keeps the completion and `<C-e>` puts the typed word back. A menu above
  the line shows the completions, ranked as `matchfuzzy()` ranks them, as
  Neovim's wildmenu does with `wildoptions=pum,fuzzy`. The word is a
  command's name, a path for `:edit`, `:split` and `:vsplit`, an option
  for `:set` or a buffer for `:buffer` and `:bdelete`. With `autocomplete`
  on, the menu shows as the line is typed (N12).
- B132 [2] In normal and visual mode `<Up>`, `<Down>`, `<Home>`, `<End>`,
  `<PageUp>`, `<PageDown>` and `<Del>` are `k`, `j`, `0`, `$`, `<C-b>`,
  `<C-f>` and `x`. In insert mode the arrows, `<Home>` and `<End>` move
  the cursor and split the undo step. `<Up>` and `<Down>` keep the column,
  or go to the end of a shorter line. `<Del>` deletes the character under
  the cursor, and at the end of a line joins the next.
- B133 [2] A command line has a cursor. `<Left>` and `<Right>` move it a
  character, `<Home>` and `<C-b>` to the start, and `<End>` and `<C-e>` to
  the end. Typed text goes in at the cursor. `<BS>` and `<Del>` delete a character, `<C-w>` the word before the
  cursor and `<C-u>` all before it. `<Up>` and `<Down>` recall older and
  newer lines that start with the text typed, `:` lines and searches each
  from their own history, as Neovim's command line does.
- B134 [2] `:grep` opens a live grep in the finder, as fff.nvim's is, and
  `:grep text` starts it with text typed. Each change to the query
  searches the working directory's files for it as plain text, with `rg`
  when it is installed and `git grep` otherwise. A query without capitals
  ignores case. Each match shows as `path:line: text`, at most 1000 of
  them, and the preview shows the lines around it with its line marked.
  `<CR>` opens the file at the match (N13).
- B135 [2] In a command line, `<C-r>` and a register's name put the
  register's text at the cursor, without its last line break, and
  `<C-r><C-w>` the word under the cursor in the text. A mapping to
  `:grep <C-r><C-w><CR>` greps the word under the cursor.
- B136 [2] With `format_on_save` on, `:w` formats the buffer first, as
  conform.nvim's `format_on_save` does. A filetype's `format` commands run
  in turn, each reading the text on stdin and writing it on stdout, with
  `{path}` in their arguments as the file's path. A filetype without them
  formats through its language server. The formatting is one timeline
  node, and the cursor stays on a line it keeps. A formatter that fails or
  takes over a second leaves the text, which is written anyway, and says
  why.
- B137 [2] With `clipboard` on, a yank, delete or change without a
  register also copies to the system clipboard, and `"_` still copies
  nothing. A put without a register takes the clipboard when something
  outside fude changed it, and else the unnamed register, so a line or
  block copied in fude puts as one (N14).
- B138 [2] In a file in a git repository, `]h` and `[h` go to the start
  of the next and previous hunk, with a count, and `]H` and `[H` to the
  last and first, as mini.diff's keys do. `gH{motion}` puts the hunks in
  its lines back as git's index has them, as one change.
  `<Plug>(git.preview)` shows the hunk on the cursor's line in a float,
  its old lines after `-` and its new ones after `+`.
- B139 [2] Text just yanked, put, undone or redone shows highlighted for
  300ms, as undo-glow.nvim shows it, and a change to the buffer clears it
  sooner (N15).
- B140 [2] `<Plug>(window.left)`, `<Plug>(window.down)`,
  `<Plug>(window.up)` and `<Plug>(window.right)` move to the window that
  way. Inside tmux, with no window that way, they move to tmux's pane
  there instead, as nvim-tmux-navigation does. No key is mapped to them.
- B141 [2] A terminal's byte 0x08 is `<C-h>` and 0x7f is `<BS>`, as
  Neovim reads them, so `<C-h>` maps apart from `<BS>` inside tmux too. In
  insert mode and a command line, `<C-h>` deletes as `<BS>` does.
- B142 [2] A kind of buffer has keys of its own, which `[keymap.<kind>]`
  in config changes, as `<buffer>` mappings do in Neovim. In that kind of
  buffer, a key mapped there runs that mapping instead of
  `[keymap.normal]`'s, at once, without waiting for a longer one (N17). In
  any keymap table, a key mapped to `""` loses its mapping, fude's own
  default too, and a key mapped to `<Nop>` does nothing. The kinds are
  `listing`, `list` for the lists of places such as `grr`'s, and
  `timeline`.
- B16 [2] `.` repeats the last change with its count, and a new count
  replaces it.
- B17 [2] `qx ... q` records keys into register `x` and `@x` replays
  them. `@@` repeats the last executed register and `Q` replays the last
  recorded one. `:reg x` shows the macro as text, and editing that text
  changes the macro.
- B18 [2] `v`, `V` and `<C-v>` select. Any operator, `~ u U r J gJ p P`
  and the line commands `X D Y C S R` apply to the selection as in Neovim,
  and `.` repeats them.
- B19 [2] An operator in operator-pending state waits for a target. `<Esc>`
  cancels and changes nothing.
- B20 [2] `m{a-z}` sets a mark, and `` `x `` and `'x` jump to it. Marks
  move with edits above them.
- B21 [2] `/` and `?` search forward and back, and `n` and `N` repeat.
  Matches highlight in every window while typing and after the search. `<C-l>` clears the
  highlight. Patterns use Vim syntax on a linear-time engine, and a pattern
  that needs backtracking is refused with a message naming the atom (N1).
- B120 [2] With `ignore_case`, searches, `:s` and their highlights ignore
  case, and with `smart_case` too a pattern with an uppercase letter
  matches case. `\c` and `\C` in a pattern override both, as with
  Neovim's `ignorecase` and `smartcase`.
- B22 [2] `:s/pat/rep/flags` substitutes with the pattern syntax of B21 and
  Vim's replacement forms (N1). `&` repeats the last substitute with its
  flags. The whole substitute is one transaction.
- B23 [2] `<C-w>s` and `<C-w>v` split, `<C-w>hjkl` move between splits, and
  two splits on one buffer see each other's edits. A new window goes below
  or right of the current one and takes the cursor (N16).
- B24 [2] Every key sequence in the grammar gives the same result whether
  it comes from typed keys, a macro, a plugin key or a client (D3).
- B25 [2] `x X s S r R J gJ ~ g~ gu gU > < D C Y` act as in Neovim with
  counts. `Y` yanks to the end of the line.
- B26 [2] `<C-a>` and `<C-x>` add to and subtract from the decimal, hex or
  binary number at or after the cursor, with counts.
- B27 [2] `% ( ) { } H M L gj gk go` move as in Neovim. `<C-d> <C-u> <C-f>
  <C-b> <C-e> <C-y> zz zt zb` scroll as in Neovim.
- B28 [2] `*` and `#` search for the word under the cursor. In visual mode
  they search for the selected text literally.
- B29 [2] Insert-mode `<BS> <C-w> <C-u> <C-r>{reg} <C-o> <C-t> <C-d>` act as
  in Neovim. `<C-w>` and `<C-u>` start a new undo step first. New lines
  keep the previous line's indent.
- B84 [2] A typed prefix that is also a complete mapping waits `key_timeout`
  (1000ms by default), then runs the complete mapping.
- B85 [2] `gv` reselects the last selection, `o` in visual mode swaps its
  ends, and `I` and `A` in blockwise visual mode insert on every line.

## Ex commands and buffers

- B86 [2] `:{range}d`, `:{range}y`, `:{range}m`, `:{range}t`, `:{range}>`
  and `:{range}<` act on lines, with ranges written as in Neovim (`.`, `$`,
  `%`, `'<,'>`, `'a`, `/pat/`, offsets).
- B87 [2] `:g/pat/cmd` and `:v/pat/cmd` run an ex command on each matching
  or non-matching line, as one transaction.
- B88 [2] `:{range}normal keys` runs keys on each line of the range, as one
  transaction.
- B89 [2] `:e path`, `:ls`, `:b N`, `:bn`, `:bp` and `:bd` manage buffers.
- B96 [2] `<C-^>` shows the buffer the window showed before, and `N<C-^>`
  shows buffer N. `#` names that buffer in `:b` and `:e`.
  Switching away from a modified buffer keeps it loaded and modified.
- B90 [2] `<C-o>` and `<C-i>` move through the jumplist, and `g;` and `g,`
  through the changelist, as in Neovim.
- B91 [2] `<C-w>c`, `<C-w>o`, `<C-w>q`, `<C-w>=` and `:q` in a split close
  or arrange splits as in Neovim, and `:sp`, `:vs`, `:on` and `:clo` split,
  keep only, or close the current window.

## Timeline

- B30 [2] `u` and `<C-r>` move along the current branch. An edit after an
  undo starts a new branch and keeps the old one.
- B31 [2] `:timeline` lists every node with its time and command, and
  restoring a node creates a new node rather than discarding later ones. In
  its window, `[` and `]` move the text to the node above or below, `d`
  diffs the node under the cursor against now, and `<CR>` restores it on
  top of the newest node. B142 changes these keys.
- B32 [2] Any two nodes can be diffed.
- B33 [2] The timeline persists across sessions when the file on disk still
  matches the last saved node.
- B34 [2] After a crash, opening the file offers to recover every edit made
  since the last save, and `:recover` brings them back. Declining keeps them
  on the timeline as a branch.
- B35 [2] `g-` and `g+` move through every node in time order across
  branches. `:earlier` and `:later` take a count, a time (`10m`) or saves
  (`1f`).

## Syntax

- B40 [3] With a grammar installed, `if af ia aa ic ac` select function,
  argument and class through tree-sitter queries.
- B41 [3] A query file in the config `queries/` directory overrides the
  shipped one for that language. A query with a predicate fude does not
  know fails to load and names the file and line.
- B42 [3] `:select <query>` in visual mode selects, as a region, every node
  that a capture of the tree-sitter query names within the selection. `d`,
  `x`, `y` and host operators such as `gc` then act on every region as one
  undo step, and `c` types into every region at once until `<Esc>`.
- B43 [3] A keystroke never waits for a reparse. Highlighting may lag by a
  frame, editing does not.
- B44 [3] `gc{target}` and `gcc` toggle comments, using the comment syntax
  of the language at the cursor, including injected languages.
- B45 [3] In visual mode, `an` and `in` select the enclosing syntax node,
  and `]n` and `[n` move to the next and previous node, as in Neovim.
- B46 [3] With a grammar for the buffer's filetype, text shows in the colour
  of its highlight capture. When captures overlap, fude picks one the way
  tree-sitter's own highlighter does. Edited text keeps its colour until the
  next parse.

## Plugins

- B50 [4] A plugin call outside its declared capabilities returns an error
  to the plugin, is logged, and changes nothing.
- B51 [4] A plugin that exceeds its budget is unloaded, the user is told
  which plugin, and editing continues.
- B52 [4] An operator or target a plugin registers composes with every
  built-in target or operator.
- B53 [4] A plugin that traps or panics is unloaded and no buffer changes.
- B54 [4] A plugin can register an ex command and a keymap binding, and
  both behave like built-in ones, including in macros and dot repeat.
- B55 [4] A plugin's decorations render and move with edits, and are
  removed when the plugin unloads.
- B56 [4] Loading plugins never delays the first frame.
- B57 [4] A plugin hears the events it subscribes to off the input path,
  with the text they are about. One that falls behind hears the newest
  state once, not every step, and a key that needs a plugin busy with an
  event says so instead of waiting.
- B47 [4] A plugin with `fs.read` reads files in the project, the
  directory fude started in, and no file outside it.
- B48 [4] A plugin with `process` runs the programs its `[[plugin]]` table
  lists, and no others, in the project and within its call's budget, and
  reads what they print.
- B49 [4] A plugin with `net` gets http and https URLs on the hosts its
  `[[plugin]]` table lists, and no others, even through a redirect.
- B58 [4] A plugin with `ui` shows a segment on the statusline of the
  buffer it read.
- B59 [4] A plugin with `ui` shows markdown in a float at the cursor, as
  hover does.

## LSP

- B60 [5] fude starts the server configured for the buffer's language,
  offers `utf-8` then `utf-16` positions, and sends incremental changes in
  the encoding the server chose.
- B61 [5] Diagnostics show inline and `]d` and `[d` move between them.
- B62 [5] `<C-]>` jumps to the definition and `K` shows hover, as in
  Neovim with a server attached.
- B63 [5] A server that crashes is reported and restarted once. Editing never
  blocks on a server.
- B64 [5] `<C-x><C-o>` in insert mode lists the server's completions.
  `<C-n>` and `<C-p>` select, `<C-y>` accepts, and the accepted text is part
  of the insert's undo step.
- B65 [5] `gq{target}` formats that range through the server, as one
  timeline node.
- B66 [5] `grn` renames, `gra` lists code actions, `grr` references, `gri`
  implementations, `grt` the type definition, `gO` document symbols, and
  insert `<C-s>` shows signature help. They exist only while a server is
  attached (N3). In a list they open, `<CR>` jumps to the place on its
  line. B142 changes this key.
- B67 [5] With `autocomplete` on, typing a word character or a trigger
  character in insert mode lists the server's completions, with none put in
  the text until `<C-n>` or `<C-p>`.
- B68 [5] The selected completion's detail and documentation show beside
  the menu, resolved from the server when the item came without them.

## Server and clients

- B70 [6] A client attached over RPC that sends a `Command` gets the same
  result as the same keys typed in the terminal client.
- B71 [6] A client may send commands as a proposal. They show as ghost text
  and change nothing until accepted.
- B72 [6] Accepting a proposal is one timeline node. Rejecting it leaves no
  node.
- B73 [6] Two clients editing one buffer converge to the same text.
- B74 [6] A proposal follows edits made after it. If an edit overlaps it,
  the proposal is marked stale and cannot be accepted.
- B75 [6] A request with a position edits there and leaves every view's
  cursor where it was.
- B76 [6] A request made against an older version applies at the position
  it meant, mapped through the edits since. If those edits overlap it, it
  is refused and the client is told.
- B77 [6] Undo by one client reverts only that client's own transactions.
- B78 [6] A client hears the buffer and its version when the text of the
  buffer it shows changes.
- B79 [6] A client that keeps a copy of a buffer shows its own edits at
  once, and after another client's edits land in between, both copies hold
  both edits. An edit of its own whose text another client changed first
  is dropped and reported.
- B97 [6] The `socket` setting is where fude listens, so a client on
  another machine reaches it through `ssh -L`. A second fude leaves a socket
  another one answers alone. It listens at a socket of its own, as it would
  without the setting, and shows no message.
- B98 [6] A client may run the grammar on its copy. Its keys edit the copy
  at once as they do in fude, an insert reaches the server as one node, and
  `u` from the client reverts that insert whole.
- B99 [6] A client running the grammar keeps its own jumplist and
  changelist, and its `g-` and `g+` move the server's timeline.

## Kernel library

Tested through `core/`'s exported API, with no editor around it.

- B100 [1] A host builds a buffer from bytes, applies a transaction and reads
  the new text from a snapshot. A snapshot taken before still reads the old
  text.
- B101 [1] A transaction whose base version is not current is rejected and
  changes nothing.
- B102 [1] Snapshots read on other goroutines while the host applies
  transactions see consistent text, under the race detector.
- B103 [1] A mark moves with edits before it. An insert at the mark's own
  position goes before or after it by the mark's gravity.
- B104 [1] Transactions inside a group become one timeline node. Another
  origin's transaction inside it ends the node and starts a new one.
- B105 [1] Every node is handed to the store as it is created. A buffer
  rebuilt from those nodes has the same timeline and text.
- B106 [1] A snapshot gives the number of lines, where any line starts and
  which line holds any position, without reading the text before it.
- B107 [1] A listener hears every change to the text, from transactions,
  undo, redo and moves across the timeline, with the text before it.
- B108 [6] A position in an older version maps to where it is now, or to
  nothing when an edit since replaced it.
- B109 [6] Undoing one origin reverts only its newest change, moved through
  the changes others made after it, as a new node. It refuses when one of
  those overlaps it, and when the change is the newest node it is a plain
  undo.
- B115 [6] A buffer gives the edits made since an older version, in order,
  or says it no longer has them.

## Vim library

Tested through `vim/`'s exported API with a minimal host over a byte slice.

- B110 [2] Every grammar behaviour gives the same result with the minimal
  host as with fude's editor.
- B111 [2] `Feed` never blocks. While typed keys start a longer mapping,
  `Waiting` reports true, and `Timeout` resolves them as Neovim does after
  `timeoutlen`.
- B112 [2] Keys the machine does not own, such as `:w` and `<C-w>v`, reach
  the host's `Unhandled` with their full text and count.
- B113 [2] An operator or target the host registers composes with every
  built-in target or operator.
- B114 [2] Removing a mapping removes the prefixes only it used, so typing
  one no longer waits.
