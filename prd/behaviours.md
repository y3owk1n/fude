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
- B83 [1] A file over the `large_file` setting (2MB by default) opens with
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
- B202 [2] With `autopairs` on, as it is unless set off, typing `(`, `[`,
  `{`, `"`, `'` or `` ` `` in insert mode before a blank, a line end or a
  closing bracket types its closing one too, with the cursor between
  them. A quote after a letter, digit or `_` types only itself. Typing a
  closing one that comes next steps over it, `<BS>` in an empty pair
  deletes both, and `<CR>` in empty brackets puts the closing one a line
  further down with an indented line between (N28). A paste and the
  finder's query are never paired.

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
- B201 [2] `fude dir`, with no other file, makes `dir` the working
  directory, so the finder, grep and relative paths start there. Helix's
  `hx dir` does the same.
- B204 [2] In a command's file or directory argument, as in `:e %:h/x`
  or `:cd ~`, `%` is the current file's path and `#` the alternate
  file's, as in Neovim. Each takes the modifiers `:p` (the full path), `:h` (its
  directory), `:t` (its name), `:r` (without its extension) and `:e` (the
  extension) after it, in turn. A `~` that starts a word is the home
  directory, and `\%` and `\#` are themselves. `<Tab>` completes from what
  the path expands to. Without such a file, the command says so.
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
  ranked as `matchfuzzy()` ranks them. Each shows its name, then its
  directory dimmed, cut in the middle to its first and last directories
  when it does not fit. `<C-n>` and `<C-p>` move the selection,
  `<CR>` opens the selected file, and leaving insert mode closes the
  finder (N11). B142 changes these keys.
- B130 [2] On a screen of 60 columns or more, the finder shows the start
  of the selected file in a box right of the list, as fff.nvim does, with
  its filetype's highlights. The two boxes have the same width and height.
  A file with a zero byte in its first 16KB shows "binary file". When
  nothing matches, the list says "no matches". Before a query is typed,
  `:grep` says "type to search the files' lines".
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
  ignores case. Matches show under a row naming their file, each as its
  line number and its line without leading blanks, at most 1000 of them.
  The selection moves over matches only, and the preview shows the lines
  around the selected one with its line marked.
  `<CR>` opens the file at the match (N13).
- B135 [2] In a command line, `<C-r>` and a register's name put the
  register's text at the cursor, without its last line break, and
  `<C-r><C-w>` the word under the cursor in the text. A mapping to
  `:grep <C-r><C-w><CR>` greps the word under the cursor.
- B136 [2] With `format_on_save` on, `:w` formats the buffer first, as
  conform.nvim's `format_on_save` does. A filetype's `format` names its
  formatters, which run in turn, each reading the text on stdin and writing
  it on stdout. A `[formatter.<name>]` table's `command` is the name when
  unset, and `{path}` in it is the file's path. A filetype without a
  formatter that applies formats through its language server. The
  formatting is one timeline node, and the cursor stays on a line it keeps.
  A formatter that fails or takes over `format_timeout` milliseconds leaves
  the text, which is written anyway, and says why. The write's message
  names the formatters that are not installed.
- B171 [2] With `format_first`, only the first of a filetype's formatters
  that applies runs. A formatter applies when its program is on PATH and,
  with `workspace_required`, when one of its `root` files is found above
  the file. It runs in the folder that holds the root file, or else the
  file's folder. With `format_first`, the write's message does not name
  the formatters that are not installed.
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
  `listing`, `list` for the lists of places such as `grr`'s, `timeline`,
  `finder`, whose keys are insert mode's, and `float` for a float the
  cursor is in, where `q` closes it. Hover while a float shows moves the
  cursor into it, whichever key runs hover.
- B143 [2] With `sign_column` on, every window keeps its sign column, so
  the text does not move when a git or diagnostic sign comes, as with
  Neovim's `signcolumn=yes`. Off, the column shows while the buffer has
  signs. `:set sign_column` changes it for one window.
- B144 [2] `<Leader>` in a mapping, from config or a plugin, stands for
  the `leader` setting, `<Space>` unless set, as Neovim's `mapleader` does
  when the mapping is made (N18).
- B145 [2] The terminal client draws in fude's own theme, in true colour:
  `dark` unless the `theme` setting names `light`. The ground, text,
  syntax, signs, selection and search take the theme's colours, not the
  terminal's (D33).
- B146 [2] `theme` may name a base16 scheme file instead, such as
  stylix's `palette.json` or a Tinted Theming scheme, and the client draws
  in its sixteen colours, each in the role base16's styling guide gives
  it. A file without all of `base00` to `base0F` says so, and fude draws in
  `dark`.
- B147 [2] A window's status line shows, in the current window, the mode
  in its colour: NORMAL, INSERT, VISUAL, V-LINE, V-BLOCK, REPLACE or
  COMMAND. Then come the buffer's name with its directory dim and `~` for
  the home directory, a dot when it is modified, the git branch and the
  counts of errors and warnings. The right holds plugins' segments, the
  keys being typed, the filetype and the ruler as `line:col`. The last row
  no longer names the mode.
- B148 [2] The command line floats in a box with a rounded border in the
  upper middle of the screen, its prompt in the colour of commands or of
  search, and its completion menu under the box. The windows take every
  row of the screen.
- B149 [2] A message shows in a box in the top right corner until the next
  key, as a toast. A long message wraps at half the screen's width. A
  message taller than a third of the screen's rows ends in an ellipsis,
  and `:messages` keeps all of it. A macro being recorded shows in the
  status line.
- B150 [2] Floats, the command line, the finder and toasts are panels with
  a rounded border on the editor's ground, a column of room inside it, and
  rules joined to the border. A selected item takes the raised colour and
  an accent bar for its left border. The completion menu of a command line
  is part of its box. The finder heads its list with a prompt and the count
  of matches, and its preview with the file's path, and keeps two columns
  and a row from the screen's edges.
- B151 [2] While the command line, the finder or the `:lsp` panel shows,
  the windows under it are drawn on a darker ground with their text faded
  into it, and the box draws on that ground with its text at full
  strength. The key hints leave the windows as they are (B172).
- B152 [2] The finder marks in the accent colour the letters of each path
  that its query matches, where `matchfuzzypos()` puts them, and a grep
  result marks where the query is in its line. A command line's menu marks
  the letters the typed word matches.
- B153 [2] With `indent_guides` on, as it is unless set off, a faint line
  marks each indent level in a line's leading blanks, at every
  `indent_width` columns, and a blank line shows the guides of the next
  line with text (N19). `list` hides them.
- B197 [2] Every frame shows the same cells as the first frame of a fresh
  client would, whatever frames came before it.
- B208 [2] With `highlight_colors` on, as it is unless set off, text
  that names a colour shows on that colour, in black or white text,
  whichever reads better. A colour is `#rgb`, `#rgba`, `#rrggbb` or
  `#rrggbbaa` without a word character either side, or `rgb()`, `rgba()`,
  `hsl()` or `hsla()` with arguments apart by commas, blanks or a slash.
  The cursor's line keeps them.
- B154 [2] While a `:s` is typed, the window shows what it would do, as
  Neovim's `inccommand` does: the matches of its pattern, then each match
  struck out with its replacement after it. The text does not change until
  `<CR>`, and `<Esc>` leaves nothing behind. The windows stay untinted
  while it shows.
- B155 [2] `:w file` writes a copy of the buffer to `file` and leaves the
  buffer on its own file, and refuses to write over a file that exists
  without `!`. `:e` reads the buffer's file again unless it has changes,
  which `:e!` drops, as one undo step. `:update` writes only a buffer with
  changes. `:enew` shows a new buffer with no file, and `:new` and `:vnew`
  show one in a new split. `:cd dir` changes the working directory, home
  without `dir`, and `:pwd` shows it.
- B156 [2] `:sort` sorts its range, every line without one: `!` in
  reverse, `n` by each line's first number, `i` ignoring case and `u`
  keeping one of lines that sort the same. `:join` joins its range, or the
  line and the next, and `!` adds no spaces. `:retab` rewrites leading
  blanks as `expand_tab` says, as tabs only with `!`. `:!cmd` shows what a
  shell command writes, `:{range}!cmd` puts its lines through the command,
  and `:r file` and `:r !cmd` put a file or a command's output below the
  line.
- B157 [2] `:messages` lists the session's last 200 messages in a window,
  oldest first, since a toast goes away with the next key. `:marks`,
  `:jumps` and `:changes` list the buffer's marks, the window's jumps and
  the buffer's changes with their line, column and text, and `<CR>` on one
  goes there.
- B158 [2] `:theme dark`, `:theme light` or `:theme` with a base16 scheme
  file redraws every client in that theme until fude quits. `:theme` alone
  shows the theme in use, and `<Tab>` after it offers dark, light and the
  scheme `config.toml` names. A scheme that does not load says why and
  leaves the theme as it was.
- B159 [5] `:lsp` opens a panel with a card for each language server
  `config.toml` sets: its state (running, starting, stopped, disabled,
  failed with why, not installed, or no files), its version, and its
  roots and files
  (N20). `j` and `k` move between cards, and a panel taller than the
  screen scrolls to show the card under the cursor. `r`, `s`, `d` and `e`
  restart, stop, disable or enable that server, as `[keymap.lsp]` maps
  them. `:lsp restart` restarts each of the current file's servers, or
  starts one again after it stayed off (B63), and opens its files in the
  new one.
- B160 [5] `:lsp stop` stops each of the current file's servers, and
  opening a file one serves starts it again. `:lsp disable` stops them and
  keeps them off for every file until `:lsp enable`, even through `:lsp
  restart`. `:lsp enable` starts them for the open files of the current
  file's filetype.
- B161 [5] While a file's language server starts or reports work, the
  file's status line shows a spinner, the server's name and what it is
  doing, with a percentage when the server gives one. Once the server is
  ready, its name shows alone, muted.
- B162 [2] `:h` opens help in a window, as markdown, on an index of its
  guide and reference. The guide's pages explain fude with examples, and
  each has a tag such as `guide-start`. The reference lists every command,
  key, `<Plug>` command, option and `config.toml` setting fude has, with
  what each does, and explains each option and setting at length, with an
  example where one helps. It also lists the mappings in use and marks the ones
  `config.toml` sets. `:h name` goes to a page, a reference section such
  as `settings`, or the entry for a command (`:w`), a key (`w`,
  `<Leader>w`), an option or a setting. `<CR>` on a name in help goes to
  it (D34).
- B172 [2] When a command being typed in normal or visual mode waits
  `key_hints` milliseconds for its next key, a panel above the status line
  lists the keys that may follow, with what each does. It lists fude's own
  keys, operators and objects, and the mappings. A mapping replaces the key
  it shadows. A key that starts longer ones is a group, listed first with
  its name and how many keys follow it. `[keymap.groups]` names the keys
  mappings start with, such as `"<Leader>s" = "search"`, and fude names its
  own, such as `gr`, `[` and `]`. The panel follows the keys typed after
  it. While it shows, a mapping's keys wait for the next key instead of
  `key_timeout`. `<BS>` drops the last key typed, and `<Esc>` drops the
  command without running any of its keys (N24). `key_hints = 0` turns the
  panel off.
- B163 [2] `fude --man` prints fude(1): the commands, keys and mappings of
  `:h` with fude's defaults, and where fude keeps its config, state and
  socket. `fude --man config` prints fude-config(5), the guide's page on
  config with the options and settings, and `fude --man guide` prints
  fude-guide(7), the guide. `just man` writes the three to `share/man`.
- B164 [5] A file's filetype is its language's name: from its whole name,
  such as `.bashrc` for bash, else from its extension, such as `.yml` for
  yaml and `.rs` for rust, else the extension itself. A `files` pattern in
  a `[filetype.<name>]` table, such as `*.toml.tmpl`, comes before all of
  these, and the files it matches have that filetype. The filetype picks the grammar, language
  server, formatters, indent width and comment syntax, and shows on the
  status line. A table named after an extension such as `[filetype.yml]`
  becomes `[filetype.yaml]`.
- B165 [3] Opening a file whose language fude knows but has no grammar
  for builds the grammar in the background, at the commit fude pins, with
  git and the tree-sitter CLI, and the file shows its highlights once the
  build ends. A build that needs a missing tool says which. `:grammar`
  lists each grammar and whether it is linked in, built, outdated (built at
  an older pin), building, not built or failed. `:grammar install
  <filetype>` builds one again, and `:grammar update` builds every outdated
  one again. A build removes the libraries of older pins. A grammar built by
  hand into the grammars directory loads too.
  `auto_install = false` turns the builds off.
- B166 [3] Text of another language inside a file shows in that
  language's colours, as its grammar's `injections.scm` marks it: the code
  in a markdown fenced block, markdown's inline text, a `<script>` or
  `<style>` in HTML. An injected language whose grammar is not built
  builds on first use, and its colours show once it is.
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
  or right of the current one and takes the cursor (N16). Windows side by
  side have a line between them in the border colour floats have.
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
  keep the previous line's indent, as B211 adds to.
- B211 [2] A new line after a line ending in `(`, `[` or `{` gets one
  more indent level. When that bracket's closing one comes next on the
  line, `<CR>` moves the closing one a line further down, at the line's
  indent. `O` above a line starting with a closing bracket opens a level
  in. A closing bracket typed on a line of blanks takes the indent of the
  line with its opening bracket. Neovim's indent for Go, C and JavaScript
  does the same.
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
- B170 [2] `:restart` and `ZR` replace fude's process with a new fude,
  which loads config.toml, plugins, grammars and language servers again.
  The new process opens the same files and splits, with each window's cursor,
  top line and options, the same current window, and every unsaved change
  (N23). A client attached over RPC must attach again.
- B96 [2] `<C-^>` shows the buffer the window showed before, and `N<C-^>`
  shows buffer N. `#` names that buffer in `:b` and `:e`.
  Switching away from a modified buffer keeps it loaded and modified.
- B193 [2] `<C-w>+`, `<C-w>-`, `<C-w>>` and `<C-w><` make the window
  taller, shorter, wider or narrower by the count. `<C-w>_` and `<C-w>|`
  make it the count tall or wide, or as large as it can be. `<C-w>=` makes
  the windows share their space equally again. All of them work as in
  Neovim.
  `:resize` and `:vertical resize` take a size, or a change of it with `+`
  or `-`. Every other window keeps at least a row and its status line, or
  a column.
- B199 [2] `:center` and `<Plug>(center)` toggle the windows centered
  (N27). Centered, each window gets `color_column` columns of text, or
  100 without one, and its gutter. Windows side by side add up, with a
  separator between them, and a stack takes its widest window's columns.
  The windows sit in the middle of a wider screen, with blank margins on
  either side, and a lone window's text stays put as its gutter grows.
  Each status line spans its own window, and messages and key hints show
  within the windows. When the windows need more than the screen, they
  take its whole width, and `:center` says so. They center again once
  they fit.
  `center = true` starts with the windows centered, and `:restart` keeps
  them so.
- B200 [2] `color_column`, which `:set` changes per window, shades that
  column of the text, counting from 1, on each line. The cursor's line, a
  selection and a match keep their colours there. 0, the default, shades
  none, and neither do centered windows, since it is their edge (B199).
- B205 [2] `:terminal` runs the shell, and `:terminal cmd` runs `cmd`
  through it, in a terminal window below a third of the screen tall, in
  terminal mode (N29). In terminal mode every key and paste goes to the
  program, the window shows its screen in its colours, and the status line
  says TERMINAL. The program's terminal is the window's size and follows
  it. `<C-\><C-n>`, or `<Esc>` twice, goes to normal mode, where the
  window holds the scrollback and screen as text to move through, search
  and yank, and `i` or `a` goes back. When the program ends, the window
  stays, in normal mode, with its exit status in its name. Closing the
  buffer or quitting fude ends the program.
- B194 [2] The last list of places a window showed, such as `grr`'s, is
  the quickfix list (N25). `]q` and `:cnext` go to its next place, and
  `[q` and `:cprev` to the one before. `[Q`, `]Q`, `:cfirst` and `:clast`
  go to its ends, and `:cc n` to place n. They go there in a window that
  does not show the list, and work after its window closes too. Each says
  which place it is, of how many. `:copen` shows the list again, and
  `:cclose` closes its window.
- B195 [2] In `:grep`'s and `:files`' finders, `<C-q>` closes the finder
  and shows every match in a list of places, which becomes the last list.
  A grep match's place is its line and column, and a file's is its start.
- B210 [2] In `:grep`'s and `:files`' finders, `<C-g>` searches the files
  git ignores and hidden files too, and the count says "with ignored".
  `<C-g>` again leaves them out. The finders never search `.git`.
- B196 [2] `:diagnostics` lists the buffer's diagnostics as places, each
  with its line, column, severity and message, the worst first and then in
  the order of the text.
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
- B198 [2] Closing a file at its last save cuts a timeline log over 1MB to
  the newest changes on the way to that save that fit in 1MB (N26). The
  timeline that is left persists as B33 says.
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
  know fails to load and names the file and line. `#lua-match?` with a
  pattern that means the same as a regular expression works as `#match?`,
  and `#set! "priority"` is allowed and changes nothing (D28). In an
  `injections.scm` such a predicate leaves out its pattern instead (B166).
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
  tree-sitter's own highlighter does. A capture that colours nothing in
  fude, such as `@spell`, `@nospell`, `@conceal` or one whose name starts
  with `_`, never hides one that does. Edited text keeps its colour until
  the next parse.

## Plugins

- B50 [4] A plugin call outside its declared capabilities returns an error
  to the plugin, is logged, and changes nothing.
- B51 [4] A plugin that exceeds its budget is unloaded, the user is told
  which plugin, and editing continues.
- B52 [4] An operator or target a plugin registers composes with every
  built-in target or operator.
- B53 [4] A plugin that traps or panics is unloaded and no buffer changes.
- B54 [4] A plugin can register an ex command and a keymap binding, and
  both behave like built-in ones, including in macros and dot repeat. The
  help a plugin gives each thing it registers shows in `:h` and the key
  hints.
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
- B173 [4] `fude plugin new <name>` writes a plugin in a new folder, in Go
  with fude's sdk, and `fude plugin build [dir]` builds it with TinyGo into
  `<name>.wasm`. Its commands, keys, operators, objects and event handlers
  are Go functions it registers in its `init`, and they act as any
  plugin's do.
- B58 [4] A plugin with `ui` shows a segment on the statusline of the
  buffer it read.
- B175 [4] A plugin's ex command gets what was typed after its name,
  whether `!` followed the name, and its range, as a built-in command
  does. Without a range, the range is the cursor's line.
- B176 [4] Every call into a plugin and every event it hears tells it the
  id, path and filetype of the buffer it is about, and the mode. With
  `buffer.read`, it reads how many lines the text has, which line holds a
  position and where a line starts, without reading the text.
- B177 [4] A plugin can map keys in insert mode. They run what it
  registered, and the cursor lands after the text it typed (B179).
- B178 [4] A plugin reads its settings by name, as text, from the
  `settings` of its `[[plugin]]` table. Each is a number, a string or a boolean,
  and a table or a list there keeps the plugin from loading, with a
  message that names it.
- B180 [4] A plugin can ask to run a function after a while. The function
  runs once, off the input path, as an event does. It is about the buffer
  the call that asked was about, and does not run when that buffer closed
  meanwhile.
- B181 [4] A plugin can run a program it declared, or get a URL on a host
  it declared, in the background, with no budget. When the work ends, a
  function of the plugin's runs with its output or error, as a timer's
  does (B180). At most 8 of a plugin's jobs run at once, and the rest
  wait. Unloading the plugin stops them.
- B182 [4] A plugin that hears `view.moved` hears where the cursor is and
  where the view starts, in the session whose view moved.
- B183 [4] A plugin that hears `buffer.changed` hears the range the change
  covers in the text it reads. Changes it heard of as one, because it fell
  behind, come as one range that covers them all.
- B184 [4] A plugin with `ui` shows text in the sign column of a line, its
  first two characters, and dim text after the end of a line. Both move
  with edits and go when the plugin removes them or unloads. On one line,
  a diagnostic's sign wins over a plugin's, and a plugin's over git's.
- B185 [4] A plugin with `ui` asks the user to pick from a list it gives,
  or to type an answer to a question. The list narrows as they type, as
  the file finder's does. A function of the plugin's then runs, as a
  timer's does (B180), with the item picked or the text typed, or with
  none when they cancel. A list asked for while one shows is cancelled at
  once.
- B188 [4] A plugin with `buffer.read` lists the open buffers, each with
  its id, path and filetype, and reads the text of any of them as it was
  when the call began.
- B189 [4] A plugin with `buffer.read` reads the kind, start and end of
  the named syntax node at a position in the buffer its call is about,
  and of that node's named parents.
- B190 [4] A plugin with `lsp` sends a request to the language servers of
  the buffer its call is about. fude fills in the document, and the
  position in each server's encoding when the plugin gives one. A function
  of the plugin's then runs, as a job's does (B181), with the first answer
  that is not null.
- B191 [4] `:plugin reload` reads the `[[plugin]]` tables of config.toml
  again and loads every plugin anew, a rebuilt one from its new file. What
  a plugin it no longer lists added goes with it.
- B192 [4] A plugin's author tests the plugin in `go test` with
  `plugin/plugintest`. It loads the plugin's `.wasm` with the capabilities
  a `[[plugin]]` table would give it. It runs a command, a key, an event
  or a callback against a text, and returns what the call asked for and
  what the plugin logged.
- B186 [4] A plugin with `ui` shows text in a read-only window with a
  title, coloured as a filetype it names. Showing text under a title
  already open replaces what that window shows. `:q` closes it without
  asking to save.
- B187 [4] A plugin with `ui` moves the cursor in the window that shows
  the buffer its call was about, once the call and its edits are done. It
  also opens a file of the project at a position, in the current window.
  A path that leads outside the project opens nothing.
- B59 [4] A plugin with `ui` shows markdown in a float at the cursor, as
  hover does.

## LSP

- B60 [5] fude starts the servers configured for the buffer's language,
  offers `utf-8` then `utf-16` positions, and sends incremental changes in
  the encoding the server chose, or the whole text to a server that syncs
  whole texts.
- B61 [5] Diagnostics show inline and `]d` and `[d` move between them.
  Each move shows the diagnostics that start at the cursor in a float
  there, in their severity's colour, with their source and code, until the
  next key.
- B62 [5] `<C-]>` jumps to the definition and `K` shows hover, as in
  Neovim with a server attached.
- B63 [5] A server that crashes is reported and restarted once. Editing never
  blocks on a server.
- B64 [5] `<C-x><C-o>` in insert mode lists the server's completions.
  `<C-n>` and `<C-p>` select, `<C-y>` accepts, and the accepted text is part
  of the insert's undo step. A menu longer than its box scrolls to keep the
  selected item in view.
- B65 [5] `gq{target}` formats that range through the server, as one
  timeline node.
- B66 [5] `grn` renames, `gra` lists code actions, `grr` references, `gri`
  implementations, `grt` the type definition, `gO` document symbols, and
  insert `<C-s>` shows signature help. They exist only while a server is
  attached (N3). In a list they open, `<CR>` jumps to the place on its
  line. B142 changes this key.
- B209 [5] With `highlight_colors` on, the colours a language server
  reports for a buffer show as B208's do, and the server is asked again
  after each change.
- B203 [5] In insert mode, typing a character the server names as a
  signature trigger, such as `(` or `,`, shows the signature of the call
  above the cursor's line, with the parameter being typed in bold, and
  under a rule that parameter's documentation and the function's. It asks
  again after each key while it shows, and goes when the server has none
  for the cursor or insert mode ends. `<C-s>` shows it or hides it.
- B206 [5] Completion lists, beside a language server's items, the entries
  of the directory a path before the cursor names when it holds a `/`,
  relative to the file's directory, and the words of the open buffers that
  start with the word typed, nearest the cursor first. So it works in a
  file with no server. `completion_sources` picks the sources and orders
  them in the menu, `["lsp", "snippet", "path", "buffer"]` by default, and
  the menu
  names the source of each item that is not a server's. With
  `autocomplete` on, a `/` lists a path's entries.
- B207 [5] A completion can be a snippet, from a server or from
  `snippets/<filetype>.json` or `snippets/all.json` in fude's config
  directory, in VS Code's format (N30). It goes in with its placeholders,
  first choices and variables filled in, and its lines indented as the line
  it goes on. `<C-y>`, or `<Tab>` on it in the menu, starts it at its first
  stop, with the placeholder shown selected. Typing replaces the
  placeholder, and every other place the stop shows follows each key.
  `<Tab>` and `<S-Tab>` go to the next stop and the one before. The last
  stop, `$0` or the snippet's end, ends it, and so do leaving insert mode
  and moving out of the stop. fude tells servers it takes snippets.
- B67 [5] With `autocomplete` on, typing a word character or a trigger
  character in insert mode lists the server's completions, with none put in
  the text until `<C-n>` or `<C-p>`.
- B68 [5] The selected completion's detail and documentation show beside
  the menu, resolved from the server when the item came without them.
- B167 [5] `[filetype.<name>] lsp` names a filetype's servers. An
  `[lsp.<name>]` table sets a server's `command`, which is its name when
  unset, and its `root`, the files that mark a project's root, most wanted
  first. A leading `~/` in a word of the command is the home directory.
  fude starts one server per root. The root is the nearest folder
  above the file that holds the first of those files found, or else the
  file's folder. When a server's program is not on PATH, fude says so once
  and does not start the server for files opened later, until `:lsp
  restart` or `:lsp enable` (N21).
- B168 [5] An `[lsp.<name>]` table's `init_options` go to the server when
  it starts, and its `settings` go to it once it has started. A server
  that asks for a section of its settings, such as `gopls.ui`, gets that
  part of them.
- B169 [5] Each server a filetype names starts for its files, and the
  diagnostics of every one show. Hover, definitions, references,
  implementations, type definitions, symbols, code actions and completions
  come from every server that offers them, in the filetype's order. With
  several servers, each code action names its own. Rename, signature help
  and formatting go to the first server that offers them (N22). An
  `[lsp.<name>]` table's `files` keeps its server to the files that match
  one of its patterns: a name, such as `*.go`, or with a slash the end of a
  path, such as `.github/workflows/*`. `workspace_required` keeps it to
  files under one of its root files.

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
- B174 [6] `fude --tail` prints what a running fude does as it happens,
  one line per event with how long it took. Events cover keys and the
  mode each ran in, command lines and messages. They also cover frames,
  parses, language server messages and answers, and plugin calls. Files,
  the programs fude runs, client calls and log lines are events too. It
  first prints the last 4096 events, so a tail started after a problem
  shows what led to it. `--json` prints each event as JSON. While nothing
  tails, fude keeps events in memory and writes nothing.

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
- B179 [2] A command the host adds, run from a mapping in insert mode,
  leaves the cursor after the text its first edit puts in, as typing that
  text would.
- B114 [2] Removing a mapping removes the prefixes only it used, so typing
  one no longer waits.
