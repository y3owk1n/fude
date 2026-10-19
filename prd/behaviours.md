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
  so, unless it is an image (B304).
- B5 [1] `:q` on a modified buffer refuses and names the buffer. `:q!` quits.
- B80 [1] `fude a b` opens both as buffers. `fude +N path` opens at line N.
- B348 [1] `fude help`, `fude -h` and `fude --help` print fude's
  subcommands and flags, one line each with what it does. Each flag names
  the subcommands that take it. fude(1) lists the same (B163).
- B349 [1] `fude version`, `fude -v` and `fude --version` print the date
  and commit fude was built from, as `fude 2026.10.18 (eda0e6e)`, with
  `, modified` after the commit when it had uncommitted changes, or
  `fude (devel)` when the build does not know them (D52).
- B350 [1] The first word after `fude` that is not a flag names a
  subcommand, or else a file to open. Flags may come before or after the
  words, and `--` ends them, so `fude -- tail` opens the file `tail`. A
  flag fude does not know, or one its subcommand does not take, is an
  error that names it, and fude opens nothing.
- B95 [1] `:wq` writes and quits. `:x` and `ZZ` do the same but write only a
  modified buffer, and `ZQ` is `:q!`. `:wa` writes every modified buffer.
  `:qa` refuses while a buffer is modified, and `:qa!` quits anyway. `:wqa`
  and `:xa` write every modified buffer, then quit.
- B81 [1] `:w` on a file changed on disk since fude read it asks before
  overwriting. `:w!` overwrites without asking.
- B304 [1] A PNG, JPEG or GIF file opens read-only as one line that says
  its format and size, such as `PNG, 80 by 32 pixels`, with the image
  below it where the terminal shows images (B303).
- B302 [1] In a buffer whose text may not change, such as a read-only file
  (B4), a window fude writes (B142) or a read-only guest's (B247), a key
  that would start insert or replace mode, such as `i`, `A`, `o`, `cw` or
  `R`, says why and stays in normal mode. The cursor stays where it was,
  and no completion menu opens.
- B82 [1] A file changed on disk while unmodified in fude reloads when fude
  regains focus. If it is modified in fude, fude warns and keeps its text.
- B307 [1] When fude regains focus, an image whose file changed since fude
  read it loads again, and shows as it is now. An image file's buffer
  reloads too, so its line gives the new size.
- B83 [1] A file over the `large_file` setting (2MB by default) opens with
  syntax and LSP off and says so. Editing and scrolling behave as for any file.
- B301 [2] `gx` opens the link or file under the cursor with the system's
  opener, `open` on macOS and `xdg-open` elsewhere, and `gf` opens the
  file in fude, at line 12 when `:12` follows it. fude looks for a path
  beside the current file, then in the working directory. In markdown,
  either takes a link's destination from anywhere on the link. Elsewhere
  either leaves out the brackets and marks around a link or path. Only the host runs
  `gx`, as a guest's would open on the host's machine. With nothing under
  the cursor, or no such file, the message says so.

## Config

- B92 [1] fude starts with no config file and never creates one.
- B93 [1] An error in `config.toml` is shown with its line, and fude starts
  with the defaults for what failed to load.
- B94 [2] Settings in a `[filetype.<name>]` table apply to buffers of that
  filetype and override the top-level ones (D27). Its `expand_tab`, `wrap`
  and `color_column` hold in a window while it shows a buffer of that
  filetype, and the window gets its own back when it shows another. A
  `:set` typed in that window stays when it shows another buffer.
- B238 [2] Opening a file reads the `.editorconfig` files in its folder and
  each one above, up to the first that sets `root = true`, as
  editorconfig.org's spec says. Their `indent_style`, `indent_size` and
  `tab_width` set the file's `expand_tab`, `indent_width` and `tab_width`,
  over config.toml's. A nearer file wins over one above it, and a later
  section over an earlier one. `editorconfig = false` reads none.
- B270 [2] `fude --clean` starts with the defaults and reads nothing in
  fude's config directory. It loads no `config.toml`, so no plugins, and
  no snippets, queries or own words. It still keeps files' timelines,
  recent files and sessions. fude is still clean after `:restart`, and
  `fude join --clean` joins with the default keys.

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
- B39 [2] `:set name value` sets an option as `config.toml` would, with
  fude's option names (N4): `true` or `false` for a switch and a number of
  0 or more for a number. `:set name` shows its value, and `:set` alone
  lists every option and its value in a window. It changes the current
  window and the windows opened after it, and never `config.toml`.
  `number` shows line numbers and `relative_number` numbers lines from the
  cursor's. A `:set` with a name or value it does not know changes
  nothing and says why.
  A setting of `config.toml` that is not a window's, such as `theme` or
  `key_timeout`, takes `:set` the same way and changes every session. A
  list takes its items apart by commas, as `:set completion_sources
  lsp,buffer`. `theme`, `leader` and `center` act at once, as `:theme`, a
  new leader and `:center` would. A module's setting, such as
  `highlight_colors` or `git_signs`, applies to the current window at once.
  `socket` takes effect when fude starts.
  A guest may not change a setting. `keymap` and the other tables change
  in `config.toml`, and `:config reload` reads it again, never writing
  it. A setting the file changed applies as `:set` would. An option it
  changed applies to every window, and an option it did not change keeps
  what `:set` gave it. A key it no longer maps does what fude does by
  default. Its tables replace the old ones, and its plugins load again. A
  `config.toml` with an error changes nothing, and the message says
  where.
- B116 [2] `scroll_off`, set with `:set scroll_off n` or in `config.toml`,
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
- B237 [2] A tab shows as `tab_width` columns, 8 unless set, and a
  `[filetype.<name>]` table sets it for one filetype. Indents made of tabs
  then spaces count each tab at that width, and `j` and `k` keep the
  column it gives. `indent_width` is the tab width unless set.
- B123 [2] A file in a git repository shows in the sign column how it
  differs from git's index, as gitsigns does: `┃` on added and changed
  lines, `▁` under the line before deleted ones, and `~` on a change that
  also deleted lines. A diagnostic's sign wins on its line. Without git
  installed, there are no git signs. With `git_signs` off there are none
  either, and the hunk keys still work.
- B124 [2] `:e dir`, `fude dir` and `-` open a directory as an explorer: a
  buffer of its entries' names, directories with a trailing `/`, with no
  line numbers. `-` lists the current file's directory, `N-` goes up N
  levels and `1-` lists the working directory, as Neovim's built-in `-`
  does, in place of the `-` motion. An explorer opens with the cursor on the
  entry that leads to the current file or explorer, when its directory
  holds it, and else on the first entry. In an explorer,
  `<CR>` opens the entry, `<C-s>` and `<C-h>` open it in a split beside or
  below, `_` lists the working directory and `<C-l>` reads it again, as
  oil.nvim's keys do. B142 changes these keys.
- B225 [2] `fude` with no file, when not restarting, shows a start screen
  in place of the empty buffer. Under fude's name, a line for each key
  says what it does. `f` runs `:pick files`, `g` runs `:pick grep`, `r`
  runs `:pick recent`, `e` opens an empty buffer, `c` opens config.toml
  and `q` quits. The 9 newest recent files under the working directory
  follow, numbered, or the 9 newest anywhere when it has none, with `~`
  for the home directory. A digit opens its recent file, and `<CR>` runs the
  line under the cursor. The screen shows no line numbers or guides, and
  the window gets its options back when it shows another buffer.
- B226 [2] Quitting fude keeps the working directory's session: its
  windows on files and explorers, each with its cursor, top line and
  options, and which is current, kept across fude's runs in its state
  directory. A quit with no window on a file or explorer keeps the last
  one. The start screen of fude
  started in that directory then has an `s` line first, and `s` opens the
  session's files and builds its windows. A file that no longer opens is
  left out, and the message says why.
- B228 [2] A task is a shell command: a `[task.<name>]` table's
  `command` in config.toml, a recipe of the working directory's justfile,
  a target of its Makefile or a script of its package.json. `:pick tasks`
  lists them, each with where it comes from and what it does, and runs the
  one picked. `:task name` runs one, and `:task` alone runs the last one
  again. A task runs in a terminal window below, as `:terminal` does
  (B205), while the cursor stays in the window it ran from. The window is named after the task, then also with ✓ or ✗,
  the exit code when it failed, and how long it took. Running a task again
  closes the window of its last run first. In a terminal window in normal
  mode, `q` closes it once its program has ended, and while it runs says
  so and leaves the window. B231 says how a running task's window hides. `{file}`, `{dir}`, `{line}` and `{word}` in a command stand
  for the current buffer's file, its folder, the cursor's line and the
  word under it.
- B230 [2] When a task ends, each line of its output that names a place
  in a file that exists, as `path:line: message` or `path:line:column:
  message`, or after Rust's `-->`, becomes the last list of places (N25),
  without a window of its own. The message says how many, and `]q` goes
  to the first, as after `:cnext`. `:copen` shows the list.
- B231 [2] While tasks run, the current window's status line shows a
  turning spinner and their names. `q` in a running task's window in
  normal mode, or closing it, hides the window, and the task goes on.
  `:pick tasks` marks a running task, and picking it, or `:task` naming
  it, shows its window again instead of running it twice. When a task
  ends, the message says it ended and how long it took, or that it failed
  with its exit code, followed by B230's places.
- B233 [2] A project's own tasks are the `[task.<name>]` tables of
  `.fude/tasks.toml` in the working directory, listed as `project` beside
  config.toml's. The file holds tasks alone, and an unknown key in it
  says where. A task in it runs only when you pick or name it, as any
  other does.
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
- B125 [2] Editing an explorer and `:w` changes the directory, as oil.nvim
  does: a changed name renames the entry, a deleted line deletes it, and a
  new line creates a file, or a directory when it ends in `/`. fude first
  asks with each change and the number of files a deleted directory
  holds, and changes nothing on `n`. Open buffers of renamed files follow
  them. A rename that fails leaves the names not yet moved as they were.
- B126 [2] An explorer in a git repository shows each entry's git status in
  the sign column: `✹` for modified, `•` staged, `≠` both, `+` added, `?`
  untracked, `-` deleted and `→` renamed. A directory shows the status of
  the first change below it. Without git installed, there are no signs.
- B127 [2] An explorer shows dim text after some names, as `ls -F` marks
  them: `-> target` after a symlink, `*` after an executable file, and `|`,
  `=`, `%` or `#` after a pipe, socket, character device or block device.
- B128 [2] With `icons` on, an explorer shows a nerd font icon left of each
  name, in mini.icons' glyphs and colours. A file's icon goes by its name
  or extension, and a directory shows a folder. Diagnostic signs show
  icons in place of `E`, `W`, `I` and `H`. `icons` is off by default.
- B129 [2] `:pick files` opens a fuzzy file finder, as fff.nvim's is, over the
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
  `:pick grep` says "type to search the files' lines".
- B305 [2] The finder previews a PNG, JPEG or GIF as a line that says its
  format and size, with the image below it, fit to the box, where the
  terminal shows images (B303).
- B131 [2] In a `:` command line, `<Tab>` and `<C-n>` put the next
  completion of the word being typed in the line, and `<S-Tab>` and
  `<C-p>` the one before. After the last comes the word as typed.
  `<C-y>` keeps the completion and `<C-e>` puts the typed word back. A
  menu above the line shows the completions, ranked as `matchfuzzy()`
  ranks them, as Neovim's wildmenu does with `wildoptions=pum,fuzzy`.
  The word is a command's name, a path for `:edit`, `:split` and
  `:vsplit`, an option for `:set`, a buffer for `:buffer` and
  `:bdelete`, a subcommand and launch settings for `:debug`, another
  session for `:follow`, a guest for `:kick` or a task for `:task`.
  Completions that need a file read or a program run, such as tasks,
  show in the menu once found, and typing goes on meanwhile. A command's
  short names match as it does, and show dim beside it. The menu lights
  the item the word is, a command by its short names too, or else its
  first. `<CR>` runs the one lit, and `<Tab>` puts it in the line.
  `<C-n>` and `<C-p>` step from it to the next item and the one before.
  So `:bde` runs `:bdelete`, `:w` stays `:write` and `:e rea` opens
  `readme`. `<C-e>` puts back the word
  as typed, such as the name of a new file. With `autocomplete` on, the
  menu shows as the line is typed (N12).
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
- B134 [2] `:pick grep` opens a live grep in the finder, as fff.nvim's
  is, and `:pick grep text` starts it with text typed. Each change to the query
  searches the working directory's files for it as plain text, with `rg`
  when it is installed and `git grep` otherwise. A query without capitals
  ignores case. Matches show under a row naming their file, each as its
  line number and its line without leading blanks, at most 1000 of them.
  The selection moves over matches only, and the preview shows the lines
  around the selected one with its line marked. A match on a line longer
  than 64KB shows the line's start.
  `<CR>` opens the file at the match (N13).
- B135 [2] In a command line, `<C-r>` and a register's name put the
  register's text at the cursor, without its last line break, and
  `<C-r><C-w>` the word under the cursor in the text. A mapping to
  `:pick grep <C-r><C-w><CR>` greps the word under the cursor.
- B136 [2] With `format_on_save` on, `:w` formats the buffer first, as
  conform.nvim's `format_on_save` does. A filetype's `format` names its
  formatters, which run in turn, each reading the text on stdin and writing
  it on stdout. A `[formatter.<name>]` table's `command` is the name when
  unset, and `{path}` in it is the file's path. A filetype without a
  formatter that applies formats through its language server. The
  formatting is one timeline node, and the cursor stays on a line it keeps.
  A formatter that fails or takes over `format_timeout` milliseconds leaves
  the text, which is written anyway, and says why. The write's message
  names the formatters that are not installed. A `[filetype.<name>]`
  table's `format_on_save` wins over the top-level one for that filetype.
- B171 [2] With `format_first`, only the first of a filetype's formatters
  that applies runs. A formatter applies when its program is on PATH and,
  with `workspace_required`, when one of its `root` files is found above
  the file. It runs in the folder that holds the root file, or else the
  file's folder. With `format_first`, the write's message does not name
  the formatters that are not installed.
- B234 [2] `:format` formats the buffer as `:w` with `format_on_save` does,
  without writing it, and says which formatters are not installed. A
  formatter that fails or takes over `format_timeout` milliseconds leaves
  the text and says why. With nothing to format the file, it says so.
  With a range, it formats only those lines, as `gq` does.
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
- B221 [2] `gh{motion}` stages the hunks in its lines in git's index, as
  mini.diff's `gh` does, and leaves the index's other lines as they are.
  The file's line endings and git's filters apply as `git add` would
  apply them. The staged hunks' signs go once git is done. `gh`
  starts no Select mode, as fude has none (N2).
- B222 [2] `:gitdiff` opens a window with the lines of the current buffer
  that differ from git's index, unsaved changes too. Each removed line
  comes after `-` and each added one after `+`, with 3 lines around each
  run, as `:diff` shows two states. Without changes, it says so, and `q`
  closes it. Both windows colour removed lines in the theme's error
  colour, added ones in its insert colour and each run's header dim.
- B220 [2] `:blame` shows, dim after the cursor's line, who changed it
  last, how long ago and the commit's summary, as gitsigns'
  `current_line_blame` does, or "not committed yet" for a line changed
  since. git reads the buffer's text, so lines changed since the last
  write still line up. `:blame` again stops it.
- B139 [2] Text just yanked, put, undone or redone shows highlighted for
  `flash_time` milliseconds, 300 unless set, as undo-glow.nvim shows it,
  and a change to the buffer clears it sooner (N15). `flash_time = 0`
  turns it off.
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
  `explorer`, `list` for the lists of places such as `grr`'s, `timeline`,
  `finder`, whose keys are insert mode's, `float` for a float the cursor
  is in, `lsp`, `help`, `scratch` for the windows fude writes, such as
  `:messages`, `:grammar` and a diff (B212), `start` (B225),
  `terminal` for a terminal or task window in normal mode (B228), and
  any kind a module or plugin adds, such as `pins` (B358) (D53). Hover while a float shows moves the
  cursor into it, whichever key runs hover.
  Nothing types in a window fude writes, such as a float, help, a list, a
  timeline, a panel or an ended terminal. A key that would start insert or
  replace mode there does nothing and says so. A command line typed in a
  float shows on its own, and the float shows again once it is done.
- B212 [2] `q` closes a list of places, a timeline, help, the `lsp`
  panel, a float the cursor is in and every window fude writes, such as
  `:messages` and a diff, instead of recording a macro (N31). An explorer
  keeps `q`, as its text is a directory to edit.
- B143 [2] With `sign_column` on, every window keeps its sign column, so
  the text does not move when a git or diagnostic sign comes, as with
  Neovim's `signcolumn=yes`. Off, the column shows while the buffer has
  signs. `:set sign_column true` changes it for one window.
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
  keys being typed, the filetype and the ruler as `line:col`. A wide
  character takes two cells, and when the two sides do not fit, the status
  line leaves out the right one. The last row no longer names the mode.
  Every status line has the theme's surface ground, and another window's
  has muted text.
- B148 [2] The command line floats in a box with a rounded border in the
  upper middle of the screen, its prompt in the colour of commands or of
  search, and its completion menu under the box. Its cursor counts a wide
  character as the two cells it takes. The windows take every
  row of the screen.
- B149 [2] A message shows in a box in the top right corner until the next
  key, as a toast. A long message wraps at half the screen's width. A
  message taller than a third of the screen's rows ends in an ellipsis,
  and `:messages` keeps all of it. A macro being recorded shows in the
  status line.
- B362 [2] Every yes or no question fude asks, such as B81's, B248's and
  B327's, shows in a box in the middle of the screen over the darker
  windows of B151, with `y` and `n` under a rule. Only `y` and `n` answer
  it, and every other key does nothing until one does. A client over RPC
  reads the question in its state as `confirm`, and a client running the
  grammar sends only `y` and `n` to the server while the question shows.
- B363 [4] A module's or plugin's call may ask a yes or no question as
  B362 shows one, and its callback hears the answer. fude answers no at
  once to a question asked while another shows. Modules hear
  `confirm.asked` with each question fude asks, and `confirm.answered`
  with `yes` or `no` when it is answered.
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
  line. A range that changes while its command runs keeps the change, and
  the message says so.
- B157 [2] `:messages` lists the session's last 200 messages in a window,
  oldest first, since a toast goes away with the next key. `:marks`,
  `:jumps` and `:changes` list the buffer's marks, the window's jumps and
  the buffer's changes with their line, column and text, and `<CR>` on one
  goes there.
- B268 [2] `:messages yank` puts the last message shown, whole, on the
  clipboard, as `"+y` does (B36, B241), so a message that wrapped in its
  toast or was cut short copies as one.
- B158 [2] `:theme dark`, `:theme light` or `:theme` with a base16 scheme
  file redraws every client in that theme until fude quits. `:theme` alone
  shows the theme in use, and `<Tab>` after it offers dark, light and the
  scheme `config.toml` names. A scheme that does not load says why and
  leaves the theme as it was.
- B294 [2] A `[colors]` table in `config.toml` replaces theme colours by
  role, each as `#rrggbb`, in `dark`, `light` or a base16 scheme. The roles are
  `bg`, `surface`, `raised`, `border`, `text`, `muted`, `faint`,
  `cursor_line`, `selection`, `search`, `normal`, `insert`, `visual`,
  `command`, `error`, `warn`, `info`, `hint`, `keyword`, `string`,
  `function`, `type`, `number`, `comment` and `punctuation`. Every style
  drawn in a role's colour changes with it. `:set colors.<role> #rrggbb` sets
  one until fude quits, as for any setting. `:config reload` applies a
  change to the file. A value that is not `#rrggbb` changes nothing and
  says where it is.
- B159 [5] `:lsp` opens a panel with a card for each language server
  `config.toml` sets: its state (running, starting, stopped, disabled,
  failed with why, not installed, or no files), its version, and its
  roots and files
  (N20). `j` and `k` move between cards, and a panel taller than the
  screen scrolls to show all of the card under the cursor, while its keys
  stay at its foot. `r`, `s`, `d` and `e`
  restart, stop, disable or enable that server, as `[keymap.lsp]` maps
  them. The panel takes those keys and `:` alone, and any other key,
  such as `/`, `?` or `i`, does nothing there. `:lsp restart` restarts each of the current file's servers, or
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
- B163 [2] `fude man` prints fude(1): the commands, keys and mappings of
  `:h` with fude's defaults, and where fude keeps its config, state and
  socket. `fude man config` prints fude-config(5), the guide's page on
  config with the options and settings, and `fude man guide` prints
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
  lists each grammar and whether it is built, outdated (built at
  an older pin), building, not built or failed. `:grammar install
  <filetype>` builds one again, and `:grammar update` builds every outdated
  one again. A build removes the libraries of older pins. A grammar built by
  hand into the grammars directory loads too.
  `auto_install = false` turns the builds off.
- B166 [3] Text of another language inside a file shows in that
  language's colours, as its grammar's `injections.scm` marks it: the code
  in a markdown fenced block, markdown's inline text and its tables'
  cells, a `<script>` or
  `<style>` in HTML. An injected language whose grammar is not built
  builds on first use, and its colours show once it is. One whose grammar
  or queries fail to load says why.
- B298 [3] With `render` on, as it is unless set off, a markdown file draws
  as it reads, and the cursor's line shows its text as it is (B295).
  After a change it draws again 50 milliseconds later, once for however
  many changes come meanwhile.
  - A heading's `#`s go, and its line is on a ground of its level's
    colour (B296). With `icons` on, an icon of its level shows in their
    place.
  - A fenced code block is on the surface colour, without its fences.
  - A bullet shows as `•`, `◦` or `▪` by how deep its list is, a task's
    `[ ]` as `☐` and `[x]` as `☑`.
  - A quote's `>` shows as `▌`, and a rule as a line of `─`.
  - Emphasis and code spans lose their marks, a link shows its text
    alone, and an image its description. An image whose path names a
    file, or an http or https address, shows below its line (B303, B308).
    A relative path is looked up beside the markdown file.
  - A table's pipes show as `│`, padded so its columns line up as their
    cells show, and its delimiter row as `├─┼─┤`. A column the delimiter
    row aligns with `-:` or `:-:` lines up on the right or in the middle.
    A row of `┌─┬─┐` shows above the table and `└─┴─┘` below it (B299).
    With the cursor in the table, all of it shows its text as it is
    (B300).
- B16 [2] `.` repeats the last change with its count, and a new count
  replaces it.
- B17 [2] `qx ... q` records keys into register `x` and `@x` replays
  them. `@@` repeats the last executed register and `Q` replays the last
  recorded one. `:reg x` shows the macro as text, and editing that text
  changes the macro.
- B18 [2] `v`, `V` and `<C-v>` select. Any operator, `~ u U r J gJ p P`
  and the line commands `X D Y C S R` apply to the selection as in Neovim,
  and `.` repeats them.
- B235 [2] `>` and `<` in visual mode shift the selected lines and keep
  them selected, so `V>>>` shifts them three times (N36). A count shifts
  that many levels at once, as `V3>` does. `<Esc>` ends the selection.
- B236 [2] `]e` and `[e` move the line down and up, n lines with a count,
  and stop at the first and last lines. In visual mode they move the
  selected lines and keep them selected (N37). Each move is one undo
  step, and `.` repeats it.
- B19 [2] An operator in operator-pending state waits for a target. `<Esc>`
  cancels and changes nothing.
- B20 [2] `m{a-z}` sets a mark, and `` `x `` and `'x` jump to it. Marks
  move with edits above them.
- B21 [2] `/` and `?` search forward and back, and `n` and `N` repeat.
  A search that reaches an end of the text goes on from the other.
  Matches highlight in every window while typing and after the search.
  While a search is typed, with `inc_search` on, as it is unless set off,
  the window shows the match it goes to, lit apart from the rest, and
  `<Esc>` puts the view back. `<C-l>` clears the highlight. Patterns use Vim syntax on a linear-time engine, and a pattern
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
  or right of the current one and takes the cursor (N16). With
  `split_below` or `split_right` off, it goes above or left instead. Windows side by
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
  more indent level, `indent_width` columns more in tabs then spaces as
  `>>` makes them. When that bracket's closing one comes next on the
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
  `:bn` and `:bp` go through the buffers being edited, wrapping at
  either end, and pass help, lists, explorers, terminals and the windows
  fude writes. After `:bd`, a window that showed the buffer shows the
  one it showed before, or else the next buffer being edited. Neither is
  an empty buffer with no file while another is open. A buffer's
  argument completes as its number, then its path from the working
  directory, or from `~`.
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
- B199 [2] `:center` and `<Plug>(theme.center)` toggle the windows centered
  (N27). Centered, each window gets `color_column` columns of text, or
  100 without one, and its gutter. Windows side by side add up, with a
  separator between them, and a stack takes its widest window's columns.
  The windows sit in the middle of a wider screen, with blank margins on
  either side, and a lone window's text stays put as its gutter grows.
  Each status line spans its own window, and messages, key hints and the
  boxes at the cursor, such as hover and code actions, show within the
  windows. When the windows need more than the screen, they
  take its whole width, and `:center` says so. They center again once
  they fit.
  `center = true` starts with the windows centered, and `:restart` keeps
  them so.
- B200 [2] `color_column`, which `:set` changes per window, shades that
  column of the text, counting from 1, on each line. The cursor's line, a
  selection and a match keep their colours there. 0, the default, shades
  none, and neither do centered windows, since it is their edge (B199).
- B205 [2] `:terminal` runs the shell, and `:terminal cmd` runs `cmd`
  through it, in a terminal window below, in terminal mode (N29). The
  window opens `terminal_height` percent of the screen tall, 33 unless set. In terminal mode every key and paste goes to the
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
- B195 [2] In a `:pick` finder, `<C-q>` closes the finder and shows every
  match in a list of places, which becomes the last list. A grep match, a
  diagnostic or a symbol is a place at its line and column, and a file is
  one at its start.
- B210 [2] In the finders of `:pick files` and `:pick grep`, `<C-g>` searches the files
  git ignores and hidden files too, and the count says "with ignored".
  `<C-g>` again leaves them out. The finders never search `.git`.
- B213 [2] `:pick {source}` opens the finder on a source's items, and
  `<Tab>` completes the source's name. `files` and `grep` are B129's and
  B134's finders. `buffers` lists the open buffers that have a file.
  `recent` lists the last 100 files a window showed, newest first, and
  keeps them across sessions. Those under the working directory come
  before the others. Neither lists the current file. B196, B214, B228,
  B215 and B266 add `diagnostics`, `symbols`, `tasks`, `help`, `keymaps`
  and `commands`. A
  `<CR>` typed before the list narrows picks from what was typed. `<CR>` opens
  the file picked, and B130's preview shows it. The finder's border names
  the source (N11).
- B196 [2] `:pick diagnostics` opens the finder on the diagnostics of
  every open buffer, the current buffer's first. They show under their
  file, the worst first and then in the order of the text, each as its
  line, severity and message. `<CR>` goes to the one picked, and the
  preview shows its line.
- B215 [2] `:pick` alone lists the sources, and picking one opens its
  finder. `:pick help` lists help's names and opens help at the one
  picked. `:pick keymaps` lists every key. The mappings of normal, visual
  and insert mode come first, then the keys of every mode and fude's
  operators, objects and window commands. Each shows its kind and what it
  does, as `:h` describes it. Typing narrows by keys, kind or description. A key
  normal mode runs, such as a mapping, a motion or an operator, runs as if
  typed, up to the character or target it waits for. Any other says what
  it does and in which mode. These lists, and a plugin's list
  (B185), show each item as text with no preview.
- B266 [2] `:pick commands` lists every ex command, fude's, the vim
  machine's and plugins', each by its name and what it does, so typing
  narrows by either. The one picked runs. One that needs an argument
  goes on the command line, which waits for it.
- B271 [2] `:plug name` runs `<Plug>(name)` as a key mapped to it does.
  It takes fude's plugs and plugins' keys, and completes their names. For
  a name that is no plug, fude says so. `:pick commands` lists each plug
  as `:plug name` with what it does.
- B214 [5] `:pick symbols` opens the finder on the symbols the current
  buffer's language server finds for the query, asking again as it is
  typed. They show under their file, each as its line, name and kind, and
  `<CR>` goes to the one picked. Without a server that finds symbols, it
  says so.
- B218 [2] `s` in normal and visual mode starts a jump (N32). Each
  character typed after it adds to a text to find in the current window's
  view, ignoring case unless it has a capital. Each match is highlighted,
  and the nearest ones to the cursor get a label, a letter drawn over the
  character after the match. A label is never the character after any
  match, so typing more of the text never jumps. Typing a label moves the
  cursor to its match's start, `<CR>` to the nearest match's, `<BS>` drops
  the last character, and `<Esc>` stays. A jump is in the jumplist. The
  status line shows what was typed.
- B219 [2] As vim-surround does, `ys{motion}{char}` puts brackets or
  quotes around what the motion or object covers, `yss{char}` around the
  line's text from its first non-blank, and `S{char}` in visual mode
  around the selection (N33). `ds{char}` deletes the pair around the
  cursor and `cs{old}{new}` changes it. A closing bracket, or `b`, `r`, `B`
  or `a`, names a pair. An opening bracket names the same pair with a
  blank inside each side, and `ds` and `cs` also take those blanks away.
  Any other character goes on both sides. Dot repeats each, with its characters, and
  `<Esc>` cancels one waiting for its character.
- B223 [2] The vim machine asks its host for the closed fold that holds a
  line. `j` and `k` pass over a closed fold as one line, landing on its
  first line, and a linewise command, such as `dd`, `dj` or `V`, takes
  every line of the folds its ends are in, as Neovim does. A host with
  `vim.Defaults` has no folds.
- B224 [2] The fold module folds a buffer by indent (N34). A fold starts
  on a line followed by lines indented more, and holds them and the blank
  lines among them, up to the last line indented more. `zc` closes the fold the
  cursor's line starts, or else the innermost one it is in. `zo` opens the
  closed fold it is on, and `za` does one or the other. `zR` opens every
  fold, and `zM` closes every outermost one. A closed fold shows its first
  line, then how many lines it hides. `j` and `k` pass over it as B223
  says, relative numbers count it as one line, and any other move that
  lands the cursor inside it, such as a search, `G` or a mark, opens it.
  Folds belong to the buffer, so every window on it shows them, and
  edits move them.
- B232 [2] `gn` selects in visual mode the match of the last search
  under the cursor, or else the next one, and `gN` the one before, going
  round the text. After an operator they are its target, so `cgn` changes
  a match and `.` changes the next. `<C-n>` in normal mode selects the
  word under the cursor as a region, and in visual mode the selection.
  Each `<C-n>` after adds the next match of that text as another region,
  the word as a whole word, going round the text, until none is left.
  `c`, `d`, `x`, `y` and host operators then act on every region, as on
  `:select`'s (B42), so `c` types into all of them at once (N35).
- B90 [2] `<C-o>` and `<C-i>` move through the jumplist, and `g;` and `g,`
  through the changelist, as in Neovim. Each keeps its newest 100 places.
- B91 [2] `<C-w>c`, `<C-w>o`, `<C-w>q`, `<C-w>=` and `:q` in a split close
  or arrange splits as in Neovim, and `:sp`, `:vs`, `:on` and `:clo` split,
  keep only, or close the current window.
- B257 [2] `:cdo cmd` runs the ex command cmd at each place of the last
  list of places (N25), and `:cfdo cmd` runs it once in each file the
  list names, at its first place there. Each place stays on its text as
  cmd edits around it, and the buffers stay unwritten until `:wall`. The
  message says how many places or files cmd ran in. If cmd fails, the
  run stops there and the message says why.
- B258 [2] While a buffer with a file is open, the top row of the screen
  lists those buffers, each by its number and file name, with the
  folder too when two names are the same. The current window's buffer
  shows lit and bold, and a modified one shows ●. When they do not fit, the row
  shows the part around the current one, with ‹ or › where it is cut.
  `buffer_line = false` hides it. A guest's row lists only the buffers
  it may show (B244, B245).
- B356 [2] `:pin` and `<Leader>a` pin the current file, after the working
  directory's other pins. A file pinned already says which pin it is, and
  a buffer with no file, or a tenth pin, says why it is not pinned.
  `:pin remove` takes the current file off. The pins are kept across
  fude's runs in its state directory.
- B357 [2] `:pin N` and `<Leader>N`, for N from 1 to 9, show pin N's file
  in the window, with the cursor where it last was in that file, even
  after fude quit. A pin whose file is gone says so and stays.
  `<Plug>(pin.next)` and `<Plug>(pin.prev)` go to the pin after and
  before the current file's, from the last to the first and back, or to
  the first and the last from a file not pinned.
- B358 [2] `:pin list` and `<Leader>h` open the pins in a window, pin N on
  line N. The lines edit as text, and `:w` makes the pins what the lines
  say, in their order. A line taken out unpins its file, and a path typed
  in pins it. A line naming no file, a file named twice, or a tenth line
  stops the write, and the message names the line. `<CR>` shows the file
  on the cursor's line, and `q` closes the window, or says its changes are
  not written and stays (N39).
- B359 [2] A pinned file's status line shows its pin and how many pins
  there are, such as `2/4`, on the left after the git branch. A file not
  pinned shows none. Every window's count changes as soon as the pins do.
- B360 [2] When an explorer renames or moves a pinned file, or a directory
  holding one, the pin follows it to its new path, whether or not the file
  is open.

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
  on the timeline as a branch. A formatter's or plugin's answer that comes
  after its buffer closed is dropped, so it leaves nothing to recover.
- B35 [2] `g-` and `g+` move through every node in time order across
  branches. `:earlier` and `:later` take a count, a time (`10m`) or saves
  (`1f`).

## Syntax

- B40 [3] With a grammar installed, `if af ia aa ic ac` select function,
  argument and class through tree-sitter queries.
- B217 [3] With `sticky_context` on, as it is unless `:set` or config.toml
  sets it off, a window
  whose top is inside a function or class pins that one's first line over
  its top row when the line is above it. Nested ones pin too, the
  outermost first. At most `sticky_context_lines` pin, 3 by default, and
  when the top is inside more than that, the innermost ones pin. Each has
  its number and highlights on the surface colour. The grammar's text objects say what a function or class
  is, and the first line left in view under the pinned ones decides which
  pin. They never cover the cursor's line. A pinned line shows as it
  would in the window, with its replacements and its line's ground (B295,
  B296), such as a rendered markdown heading.
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
  of the language where the target starts, including injected languages.
- B45 [3] In visual mode, `an` and `in` select the enclosing syntax node,
  and `]n` and `[n` move to the next and previous node, as in Neovim.
- B46 [3] With a grammar for the buffer's filetype, text shows in the colour
  of its highlight capture. When captures overlap, fude picks one the way
  tree-sitter's own highlighter does. A capture that colours nothing in
  fude, such as `@spell`, `@nospell`, `@conceal` or one whose name starts
  with `_`, never hides one that does. fude leaves out a highlight
  pattern with a predicate it does not evaluate, such as Objective-C's
  `#has-ancestor?`, and applies the rest of the query. Edited text keeps
  its colour until the next parse.
- B259 [3] With `spell` on, a window underlines the words its word list
  lacks. The list is the file `spell_words` names, or
  `/usr/share/dict/words` without it, and `words.txt` in fude's config
  directory. In Markdown, text, git commit and files of no filetype,
  spell checks all the text but code and links. Elsewhere it checks the
  comments and strings. A word passes when the
  list has it, in lower case too, or has it without an ending of a
  plural or a contraction, which lists such as macOS's leave out. A word
  with a capital after its first letter, or a digit or one of `_ . / @ \`
  beside it, is a name or a path and passes. A list that fails to read
  says so once, and spell then finds nothing. `spell` is an option, and
  a filetype table may set it.
- B260 [3] `]s` and `[s` go to the next and previous word spell finds,
  wrapping at the ends. `z=` lists the words of the list closest to the
  one under the cursor, and the one chosen replaces it. `zg` adds the
  word under the cursor to `words.txt`, so spell passes it.

## Plugins

- B50 [4] A plugin call outside its declared capabilities returns an error
  to the plugin, is logged, and changes nothing.
- B51 [4] A plugin that exceeds its budget is unloaded, the user is told
  which plugin, and editing continues. Loading it runs under the budget
  its init has.
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
  reads what they print. A child a program leaves running does not hold the
  call.
- B49 [4] A plugin with `net` gets http and https URLs on the hosts its
  `[[plugin]]` table lists, and no others, even through a redirect. It
  follows at most 10 redirects.
- B173 [4] `fude plugin new <name>` writes a plugin in a new folder, in Go
  with fude's sdk, and `fude plugin build [dir]` builds it with TinyGo into
  `<name>.wasm`. Its commands, keys, operators, objects and event handlers
  are Go functions it registers in its `init`, and they act as any
  plugin's do.
- B58 [4] A plugin with `ui` shows a segment on the statusline of the
  buffer it read.
- B272 [4] A plugin with `ui` shows a line in the message area.
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
- B181 [4] A plugin can run a program it declared, with text on its stdin
  or none, or get a URL on a host it declared, in the background, with no
  budget. When the work ends, a function of the plugin's runs with its
  output or error, as a timer's does (B180). At most 8 of a plugin's jobs
  run at once, and the rest wait. Unloading the plugin stops them.
- B182 [4] A plugin that hears `view.moved` hears where the cursor is and
  where the view starts, in the session whose view moved.
- B183 [4] A plugin that hears `buffer.changed` hears the range the change
  covers in the text it reads. Changes it heard of as one, because it fell
  behind, come as one range that covers them all.
- B184 [4] A plugin with `ui` shows text in the sign column of a line, its
  first two characters, and dim text after the end of a line. Both move
  with edits and go when the plugin removes them or unloads. On one line,
  the sign at the highest layer shows. A plugin's signs are at layer 0,
  a diagnostic's at 1, and a sign the plugin puts under at -1, as git's
  are, so it loses to any other.
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
- B273 [4] A plugin with `buffer.read` reads the language of the text at a
  position, the buffer's or that of an injected region holding it, and
  the highlight captures over a range, as their names and ranges.
- B297 [4] A module runs tree-sitter queries of its own over its buffer,
  one for each language, by filetype. Each runs on the buffer's tree and
  on the trees of the regions injected in it, so a markdown file's
  inline text answers the `markdown_inline` query. It gets each capture's
  range and name. A query that does not compile says so.
- B274 [4] A plugin reads an option by its name in config.toml, as the
  window has it, or a top-level setting of config.toml.
- B279 [4] A plugin reads any value of config.toml by its dotted path, such
  as `filetype.go.format` or `formatter.gofmt.command`, as JSON.
- B280 [4] Every call into a plugin tells it the indent width and the tab
  width of the buffer it is about, as the file's .editorconfig, its
  filetype and config.toml settle them.
- B275 [4] A plugin with `buffer.read` reads how the language at a
  position comments a line, the syntax `gc` uses.
- B190 [4] A plugin with `lsp` sends a request to the language servers of
  the buffer its call is about. fude fills in the document, and the
  position in each server's encoding when the plugin gives one. A function
  of the plugin's then runs, as a job's does (B181), with the first answer
  that is not null.
- B281 [4] A plugin sends a request about a range of the buffer, as
  `textDocument/rangeFormatting` takes, and hears the server's position
  encoding with the answer, so it can read the positions in it.
- B191 [4] `:plugin reload` reads the `[[plugin]]` tables of config.toml
  again and loads every plugin anew, a rebuilt one from its new file. What
  a plugin it no longer lists added goes with it, its operators and
  objects too.
- B192 [4] A plugin's author tests the plugin in `go test` with
  `plugin/plugintest`. It loads the plugin's `.wasm` with the capabilities
  a `[[plugin]]` table would give it. It runs a command, a key, an event
  or a callback against a text, and returns what the call asked for and
  what the plugin logged.
- B186 [4] A plugin with `ui` shows text in a read-only window with a
  title, coloured as a filetype it names. Showing text under a title
  already open replaces what that window shows. `:q` closes it without
  asking to save.
- B278 [4] A plugin adds a command that keys reach only as
  `<Plug>(plugin.name)`, which `:plug` and a mapping run, with the help
  `:h` shows.
- B276 [4] A plugin with `ui` styles ranges of the text it shows in a
  window, such as a line in `diff.plus`, over what the filetype colours.
- B277 [4] A plugin hears `editor.focused` when the terminal gets the
  focus back, as git's index may have changed while fude was away.
- B336 [4] A module is a source of insert mode completion by a name
  `completion_sources` orders. It offers items that replace the text
  from a place. They may be the menu's alone, and they may come later.
  It names the characters besides word characters after which
  autocomplete asks it. The editor expands a snippet's items. Buffer
  words, paths and user snippets complete this way (B206, B207).
- B338 [4] A plugin registers what a module registers, of every kind and
  with every field, reads what a module's scope has, and its result asks
  for what a module's asks for, within its capabilities. Keys a result
  runs need `keys`, and a `Typed` key, a `Completer` or a `Write` needs
  `buffer.read`, as each hears the text being typed or written.
- B339 [4] A plugin's job may stream a program's output. Its callback
  runs with each line as the program prints it, then once more when the
  program ends, with any error.
- B340 [4] A module reads the session's windows, each with its id, its
  buffer, its cursor, its top line and where it is on the screen, and
  what visual mode selects. Every call and event says where the cursor is
  in its buffer, the working directory and the project.
- B341 [4] A module reads what every register holds, the last search and
  its direction, the register a macro is recorded into, the lines entered
  with `:` and `/`, and the last list of places and where `]q` is in it.
- B342 [4] A module's result edits any open buffer, each buffer's edits as
  one change. fude drops an event's edits to another buffer that changed
  after the event read it.
- B343 [4] A module's result shows a buffer by its id, in the window or a
  split, closes and writes buffers, closes, focuses and resizes windows,
  moves a window's cursor and top line, and opens a buffer with no file
  holding text to edit, which may be of a kind the module writes (B327).
  fude does these after it makes the call's edits.
- B344 [4] A module's result sets marks, puts text in registers, makes a
  pattern the last search, sets any option as `:set` does, and runs ex
  command lines. A plugin needs `keys` for the command lines and for an
  option that is not a buffer's own.
- B345 [4] A module's result defines styles by name, with a colour for
  the text and one for its ground, bold, italic, underline or strike, over
  the theme's styles. Its decorations and spans name them.
- B346 [4] A module hears when a window opens, closes or becomes current,
  with the window's id. It hears when the search, the selection, the theme
  or the working directory changes, when a macro's recording ends, with
  its register, and when an explorer renames a buffer's file, with the old
  path. It hears when an explorer moves a file or directory, open or not,
  with its old and new paths.
- B347 [4] The editor keeps the last list of places a module or the
  finder gives, with the place last gone to. A module shows the list in a
  window, steps through it and runs a command at its places. A module's panel may be of a kind, such as list, whose
  `[keymap.<kind>]` keys change its own.
- B337 [4] A module's decoration may show its sign in an explorer's icon
  column while `icons` is on, instead of in the sign column. An explorer's
  notes and icons are a module's decorations (B127, B128).
- B335 [4] A module's terminal with no name opens anew each time and
  takes the focus to type into. The status line does not name it as a
  task. `:terminal` opens one this way (B205).
- B334 [4] A module's call reads help's text and the line of the entry a
  name has. Its panel may put a line at the top of its window, with the
  cursor and the focus there, each time it shows, as `:help` does (B162).
- B333 [4] A module's call lists the grammars fude builds, each with its
  state, and its result may build grammars again, which only the host
  may ask, as `:grammar` does (B165).
- B332 [4] A module's call lists the sessions on this fude, each with
  its name and power and whether it is the call's own. A call's scope,
  and an event's that a session's keys or commands make, names that
  session by an id no other session on this fude has. `buffer.changed`
  may name another session, as an edit keeps no session. Its result may
  end a guest's session, which only the host may ask, or follow a
  session, as `:who`, `:kick` and `:follow` do (B250, B256).
- B331 [4] A module says what the buffer line lists, each buffer by the
  name it shows, and the editor lights the current window's, marks a
  modified one, and leaves out what a guest may not show. A module hears
  `buffer.closed` when a buffer closes, `config.reloaded` after
  `:config reload`, and `option.set` with the name `:set` set (B258).
- B330 [4] A module's call reads whether a window is next to the
  current one each way, and its result may move to that window, as
  `<Plug>(window.left)` and the others do (B140).
- B329 [4] A module's call reads the current buffer's marks and the
  places it changed, the current window's jumps, the messages shown and
  any register, and each open buffer's name and whether it is modified.
  Its list of places may say what each line shows. Its result may put
  text on the clipboard. Its command may answer to short names too.
  `:buffers`, `:marks`, `:jumps`, `:changes`, `:registers` and
  `:messages` work this way, and `:registers` with no name lists every
  register that holds text (B89, B157, B268, B341).
- B328 [4] A module's call reads the states of a buffer's text, each
  with its time, the command that made it, the state it follows, and
  whether it is current, written, or a change a crashed session left
  unsaved. It reads any state's text. Its result may move the text to a
  state, and each window on the buffer moves its cursor there. Or it may
  make a state's text the newest, as a new state. `:timeline`,
  `:earlier`, `:later`, `:restore`, `:diff` and `:recover` work this way
  (B31, B32, B34, B35).
- B327 [4] A module writes a kind of buffer in place of a file, as
  `modules/explorer` writes an explorer. It reads where each entry the
  buffer showed is now. Its write may ask a yes or no question first,
  naming the paths it would change, which a guest may change only under
  the project. What follows the write, such as `:wq`'s quit, runs once it
  is done. The write may say which paths it renamed, and their open
  buffers follow. A module's or plugin's plug may come with its keys, in
  normal mode, in visual mode too, only while a language server is
  attached, or among a kind of buffer's own keys. A config.toml mapping of
  the same keys wins over them, and `:config reload` without it gives them
  back. Every key a module adds has a plug named for what it does, such
  as `<Plug>(lsp.rename)` for `grn`, which any key may map (D53). A jump
  may open its file in a split.
- B326 [4] A module hears `keys.pending` when the command being typed
  changes, with its keys, and reads the keys that may follow it, each with
  what it does or its group's name. It may show them as the key hints,
  which keep the command waiting for its next key, as B172 does.
- B325 [4] A module hears `editor.quitting` as fude quits, before its
  events stop, and reads the session then: the windows on files, each
  with its cursor, top line and options, and which is current. A call
  reads it too, and its result may open a session's files and build its
  windows again, as B226 keeps the working directory's.
- B324 [4] A module's call lists every key fude knows, each with its kind
  and help, every ex command with its arguments and help, and the tags
  help opens at, as `:pick keymaps`, `:pick commands` and `:pick help`
  do (B215, B266).
- B323 [4] A module's key typed in insert mode runs before the key does
  its own work. Its edits go in as typed text does, and dot repeats
  them. With none the key does what it does, as `<BS>` deletes outside a
  pair. A finder's query and a paste never run it (B202).
- B322 [4] A module's operator or key may wait for characters typed
  after it, which its call gets, and dot repeats it with them. Such a key
  in normal mode follows an operator's key, as `ds` does, and `d` does
  not wait to see whether it comes. In visual mode it acts on the
  selection, as `S` does. A call finds the range of one of the vim
  machine's own text objects, such as `a(` or `2i"`, at a place (B219).
- B321 [4] A module's list may fill the finder that shows it after it
  opens, keeping the query, and fills none once the finder is closed.
  `<C-g>` in a module's list that is not live tells the module whether
  to list the files git ignores too, as `:pick files` does (B129, B210).
- B320 [4] A module's panel may show in the current window in place of
  the buffer its call is about, as the start screen does (B225). It
  shows no line numbers, guides or wrap. The window gets its options
  back once it shows another buffer. The panel may say where its cursor
  starts. A call's result may run keys once the call is done, as typed,
  so a panel's own keys and `[keymap.*]` apply to them.
- B319 [4] A module hears `editor.started` when fude starts with no file
  to open and is not restarting. The start screen shows then (B225).
- B318 [4] A module's panel may show in a box of its own in the middle
  of the screen, which takes the focus. Its parts may be cards, which
  the cursor moves between, and its keys may stay at its foot as it
  scrolls, as the `:lsp` panel's do (B159). A panel shown only to update
  replaces the text of the one open, and opens none once it is closed.
- B317 [4] A module hears `lsp.changed` when a language server starts,
  is ready, stops, fails to start, or is disabled or enabled. A call
  lists the language servers `config.toml` sets, each with its state,
  version, filetypes, roots and files, and asks to restart, stop,
  disable or enable one, which a guest may not (B159, B160).
- B316 [4] A module's call may take the keys typed next. Each key goes
  to that call before any other key handling, until a result lets them
  go. A jump reads its characters and labels this way (B218). A call
  about the current window names where its view starts, and the end of
  the line one window height below it.
- B315 [4] A module's decoration covers a range on one line with its
  note, padded to the range's width. It shows on the cursor's line too,
  as a jump's labels cover the character after each match (B218). An
  empty range at the line's end draws the note after the line.
- B314 [4] A module hears `buffer.shown` when the current window shows
  another buffer, as recent files keep what windows show (B213). A
  module's list may be of file paths, which show, preview and open as
  `:pick files`'s do.
- B313 [4] A module hears `text.touched` when a key yanks, puts, undoes or
  redoes text, with what it did, `yank`, `put`, `undo` or `redo`, and
  where the text is, as flash shows it (B139).
- B311 [4] A module hears `explorer.read` when an explorer reads its
  directory: as it opens, as it reads it again, and when the terminal gets
  the focus back. A call about an explorer names its directory, which git's
  status signs need (B126).
- B312 [4] A module shows a segment on the left of the status line, after
  the buffer's name, as git's branch is (B147). Its result sets the
  segment of the buffer it is about, or of any open buffer by id, and a
  segment may be one session's own, which only that session's status
  lines show.
- B282 [4] A plugin hears `buffer.saving` before a write. The write waits
  for what the plugin asks for, edits included, and for the callbacks its
  answer asks for. After `format_timeout` fude writes the text as it is,
  says so, and drops what comes late. The plugin's message follows the
  write's.
- B284 [4] A plugin hears `lsp.attached` once a language server is ready
  for a buffer, with the server's name.
- B285 [4] A plugin shows a live list in the finder, with a title in its
  border, a hint before a query is typed and a query typed at the start.
  Each change to the query runs a function of the plugin's with the
  query, and with whether `<C-g>` asked for the files git ignores too. The
  items it returns replace the list.
- B286 [4] A plugin's list may be of places, each path:line:column:text,
  shown under their file with the line previewed. `<CR>` opens the place
  picked, after the plugin hears which, and `<C-q>` lists them as B195
  says.
- B287 [4] A plugin adds a source to `:pick`, which `:pick name text`
  runs with text, `:pick` alone lists and `<Tab>` completes.
- B288 [4] A plugin with `ui` closes a fold over a range of lines of the
  buffer its call is about. The fold shows and opens as B224 says, moves
  with edits, and opens when the plugin removes it by its id.
- B295 [4] A module's decoration, or a plugin's with `ui`, may replace its
  range, on one line, with its note in its style, and an empty note hides
  the range. The text is
  unchanged, so search, motions and edits act on it. The cursor's line,
  and the lines a selection covers, show their text as it is. Wrap,
  scrolling sideways and down, `gj` and `gk` count a line's rows and
  columns as it draws, inlay hints included.
- B296 [4] A module's decoration, or a plugin's with `ui`, may shade the
  whole rows of its lines in its style's ground, to the window's edge. The text keeps its own
  colours on that ground.
- B299 [4] A module's decoration, or a plugin's with `ui`, may draw rows
  of its own below its line, or above it, which hold no text, so the cursor passes over them. Each
  is cut at the window's edge. They count as rows when the window
  scrolls, and a line's rows above it show when it is the window's top
  line. A line that shows its text as it is, as the cursor's does (B295),
  draws none.
- B300 [4] A module's decoration, or a plugin's with `ui`, may make its
  lines one block. While the
  cursor or a selection is on any of them, every line of the block shows
  its text as it is, without replacements or rows of its own.
- B303 [4] A module's decoration may name an image file, a PNG, JPEG or
  GIF, to show in rows of its own below its line (B299). Once the client
  says how many pixels a cell of its terminal takes, the image takes as
  many columns as its pixels fill, at most the window's, and the rows
  that keep its shape, at most all but two of the window's. It takes them
  on the cursor's line too, so passing it moves no text. fude reads the
  file once, off the loop, and an image that does not load takes no rows.
  The terminal client shows images in kitty and Ghostty, through tmux
  too, when the terminal shows true colour and says its cells' pixels, and
  in WezTerm and iTerm2 as B310 says. It sends each image to the terminal
  once, and an attached client gets it in a frame once. Elsewhere images
  take no rows.
- B306 [4] An image leaves fude's memory once nothing shows it, when the
  buffer that showed it closes or the finder that previewed it closes.
  Every client then has the terminal drop it.
- B308 [4] An image decoration may name an http or https address. With
  `remote_images` on, as it is unless set off, fude fetches it off the
  loop, in at most 10 seconds and 16MB, and shows it as it would a file.
  With it off, no new one loads.
- B310 [4] In WezTerm and iTerm2, outside tmux, when the terminal says its
  cells' pixels, the client paints each image over its cells after the
  frame shows, once all its rows show, and keeps tcell off those cells
  until the image moves or goes. A box drawn over it, such as a float, takes its place
  until the box closes. WezTerm gets each image once, iTerm2 each time it
  paints, at the size it shows.
- B289 [4] A plugin with `ui` reads its own decorations on the buffer its
  call is about, each with its id, where it is now and what it shows.
- B290 [4] A module's command completes its argument through the
  module, as B131 says of a task for `:task`. The menu shows once the
  module answers, ranked against the word typed, unless the command line
  changed meanwhile.
- B291 [4] A module runs its own long work as a stream job. Each line
  it hands fude runs its callback, about the buffer current then, and
  the end of the work runs it once more. A module decorates any open
  buffer by id and reads its decorations on every buffer. A sign it puts
  over shows over a diagnostic's. It jumps to a line and column of a
  file, in a window that shows a file. A window it shows has keys of its
  own, and showing it again keeps the cursor's line and the focus. It
  may map a key in visual mode too, called with the selection. When the
  text a callback read has changed since, its message, window, file jump
  and decorations on other buffers still apply. Its edits, and its
  decorations and jump on that text, wait for a newer event.
- B292 [4] A module works with the language servers the editor runs. A
  key it marks attached is mapped only while a server is attached to the
  buffer, as B66's are. A key it marks as entering moves the cursor into
  a float that shows, as `K` does. It hears `lsp.published`, each
  diagnostics a server publishes for a buffer, as sent, and
  `lsp.detached` when a server leaves a buffer. It applies a server's
  workspace edit with the editor's version checks. It shows a float
  styled by its own spans. It lists places in a window, in one file, or
  in a server's column encoding. It starts a command line with text
  typed, which runs nothing. The layer of its sign orders it on a line
  with the others. `servers` in its scope names the servers attached to
  the buffer.
- B293 [4] A module shows a numbered menu at the cursor, as code actions
  show, each item with its kind and what it does beside it, and hears
  which was picked. A menu of the same id while one shows replaces its
  items. A module asks one language server by name, as resolving a code
  action needs.
- B283 [4] A plugin sets `expand_tab`, `indent_width` or `tab_width` on the
  buffer its call is about, over config.toml's, until the buffer closes,
  as .editorconfig does.
- B187 [4] A plugin with `ui` moves the cursor in the window that shows
  the buffer its call was about, once the call and its edits are done. It
  also opens a file of the project at a position, in the current window.
  A path that leads outside the project opens nothing. The move joins the
  jumplist, so `<C-o>` goes back.
- B59 [4] A plugin with `ui` shows markdown in a float at the cursor, as
  hover does.
- B309 [4] An image in a float's markdown, such as hover's, shows its
  description in the text, and the first one fude can load shows under
  the text where the terminal shows images, when both fit. A relative
  path is looked up beside the buffer's file.

## LSP

- B60 [5] fude starts the servers configured for the buffer's language,
  offers `utf-8` then `utf-16` positions, and sends incremental changes in
  the encoding the server chose, or the whole text to a server that syncs
  whole texts.
- B61 [5] Diagnostics show inline, and `]d` and `[d` move between them.
  After a line with a diagnostic, two columns on, its message shows in its
  severity's colour on a tint of that colour, unless `diagnostic_notes` is
  off.
  Each move shows the diagnostics that start at the cursor in a float
  there, in their severity's colour, with their source and code, until the
  next key. `<C-w>d` shows those on the cursor's line in the same float,
  in full, without moving.
- B62 [5] `<C-]>` jumps to the definition and `K` shows hover, as in
  Neovim with a server attached.
- B63 [5] A server that crashes is reported and restarted once. Editing never
  blocks on a server. The log keeps what it wrote to stderr, to its last
  line.
- B64 [5] `<C-x><C-o>` in insert mode lists the server's completions.
  `<C-n>` and `<C-p>` select, `<C-y>` accepts, and the accepted text is part
  of the insert's undo step. A menu longer than its box scrolls only when
  the selection would leave the rows shown, and then only as far as it
  must. The finder's list and the command line's menu scroll the same way.
- B65 [5] `gq{target}` formats the lines it covers as `:format` does,
  through the filetype's formatters or else the server, which hears the
  file's indent width and whether it indents with spaces. The formatting is
  one timeline node. A formatter formats the whole file, and fude applies
  only its changes inside the lines.
- B66 [5] `grn` renames, `gra` lists code actions, `grr` references, `gri`
  implementations, `grt` the type definition, `gO` document symbols, and
  insert `<C-s>` shows signature help. They exist only while a server is
  attached (N3). In a list they open, `<CR>` jumps to the place on its
  line. B142 changes this key.
- B227 [5] `gra` shows the code actions in a menu at the cursor, or for
  the selection in visual mode, from the first server that answers. Each row has its number, title and kind's
  family, such as `quickfix` or `source`. Preferred actions come first,
  then fixes, refactors and the rest. Disabled ones come last, dim. Beside
  the menu, on whichever side has room, or else under it, the selected
  action shows what it does. For an edit, that is
  the lines it changes, removed after `-` and added after `+`, under each
  file's name when it changes another file. Otherwise it is the command it
  runs, or why it is disabled. fude resolves an action that needs it to
  show it. `<C-n>`, `<C-p>`, `j`, `k` and
  the arrows move the selection, `<CR>` takes it, a digit takes the action
  it numbers, and any other key closes the menu. The edits of an action
  undo as one step labelled code action.
- B209 [5] With `highlight_colors` on, the colours a language server
  reports for a buffer show as B208's do, and the server is asked again
  after each change.
- B216 [5] With `inlay_hints` on, as it is unless set off, the inlay
  hints a language server gives for a buffer show among its text, dim on
  the surface colour, before the character they are at, with the padding
  the server asks for. They are not part of the text, so the cursor,
  motions and edits pass over them. fude asks the server again after each
  change, and the hints move with the text until it answers.
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

## Debugging

- B261 [8] The debug module runs debug adapters (B291). `[debug.{name}]`
  in config.toml names one by its
  `command`, and a filetype table's `debug` lists its adapters by those
  names. A command with `{port}` in it listens there, so fude picks a
  free port, runs it and dials it. Any other command talks on its stdin
  and stdout. `.fude/launch.toml` in the project holds launch settings
  as `[launch.{name}]` tables. Each uses the first adapter of the
  current file's filetype, or the one `adapter` names. Its `request` is
  `launch`, or `attach`. Its `args` go to the adapter as they are,
  once `{file}`, `{dir}` and `{project}` in their strings become the
  current file, its folder and the project.
- B262 [8] `:debug start [name]` starts the launch settings called name,
  or the only ones, or else lists them to choose from. `:debug break`
  and `<Leader>db` put a breakpoint on the cursor's line, or take one
  off. The sign column shows one as ● once the adapter takes it, and ○
  before or when it does not. A breakpoint stays on its line as the text
  around it changes. `:debug stop` and `<Leader>dq` end the session. A
  `:debug` line completes its subcommands, and after `start` the names
  of the launch settings. When the program stops, the window shows its
  line, lit and with → in the sign column, and the message says why it
  stopped. When the session ends, by the program or the adapter or
  `:debug stop`, the message says so and the line is no longer lit.
- B263 [8] `:debug continue`, `next`, `in` and `out`, and `<Leader>dc`,
  `dn`, `di` and `do`, let the stopped thread run on, or step over, into
  or out of its line. `<Leader>dc` starts a session when none runs. Each
  says so when the program is not stopped.
- B264 [8] `:debug panel` and `<Leader>dp` open a window of the stopped
  thread's frames, the variables of the chosen frame and the program's
  output. `<CR>` on a frame chooses it and shows its line. `<CR>` on a
  variable with members shows or hides them. The panel shows anew each
  time the program stops.
- B265 [8] `:debug eval expr` shows the value of expr at the chosen
  frame, and `<Leader>de` shows that of the word under the cursor, or of
  the selection, in a float.

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
  cursor where it was. A request on another buffer leaves the window's
  `#` as it was.
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
  another machine reaches it through `ssh -L`. fude makes the folder it
  names when that folder is missing. fude listens on no network
  address unless `fude share` asks it to (D40). A second fude leaves a socket
  another one answers alone. It listens at a socket of its own, as it would
  without the setting, and shows no message.
- B98 [6] A client may run the grammar on its copy. Its keys edit the copy
  at once as they do in fude, an insert reaches the server as one node, and
  `u` from the client reverts that insert whole. A `u` inside `:normal`
  runs on the server, and the copy takes it at the next sync.
- B99 [6] A client running the grammar keeps its own jumplist and
  changelist, and its `g-` and `g+` move the server's timeline.
- B174 [6] `fude tail` prints what a running fude does as it happens,
  one line per event with how long it took. Events cover keys and the
  mode each ran in, command lines, messages and questions. They also
  cover frames, parses, language server and debug adapter messages and
  answers, and module and plugin calls. Files read, written, renamed or deleted, the
  programs fude runs and the URLs it gets, terminals, client calls, guests
  joining and leaving, and log lines are events too. A job shows when it
  starts and ends, with the id of the callback it runs. A line an
  attached session made names that session. It first prints the last
  4096 events, so a tail started after a problem shows what led to it.
  `--json` prints each event as JSON. While nothing tails, fude keeps
  events in memory and writes nothing.
- B351 [6] `fude open file...` opens the files in the session B352
  finds, as `:e` does in its current window. The first one shows, and
  the others are buffers. A relative path is from the directory `fude
  open` runs in.
- B352 [6] `fude open`, `keys`, `state`, `attach` and `tail` reach the
  fude at `--socket path`. Without it they reach the fude at
  `$FUDE_SOCKET`, or else at the socket setting, or else the one fude
  that answers in `XDG_RUNTIME_DIR` or the temporary directory. When
  several answer, they name each and do nothing. `open`, `keys` and
  `state` act on the session `--session` names, or else the one
  `$FUDE_SESSION` names, or else the session of fude's own terminal. They
  fail when no session has the name. Over the socket, a `session` call
  makes a client's later calls act on the session it names instead of
  the client's own. It names a session as B252 does, or by the id in
  `$FUDE_SESSION`.
- B353 [6] `fude keys keys` types keys into the session, as typing them
  there does, so in a terminal window they go to its program (B70).
- B354 [6] `fude state` prints the session's buffer, name, version, text,
  cursor, mode and message as a line of JSON.
- B355 [6] `:terminal` sets `FUDE_SOCKET` to where fude listens and
  `FUDE_SESSION` to the id of its session, in the program it runs. So
  `fude open`, `keys` and `state` in that program act on that session.
  When fude has no socket, it sets neither.

## Sharing

- B239 [7] `fude attach` opens a terminal client on a new session of the
  fude B352 finds.
  It draws what fude's own terminal draws for that session: its windows,
  highlights, decorations, floats, command line, key hints and status
  line, in the server's theme. Its keys, pastes, size and focus go to that
  session, so two attached clients of one fude each have their own
  windows, cursor, mode and undo (D39).
- B240 [7] In an attached session, `:q` on its last window, `:qa` and `ZZ`
  leave fude instead of quitting it. The session and its windows go, and
  the server, its buffers and the other sessions go on. Changes the
  session made stay in the buffers, unsaved, for any session to write.
  `:restart` there says it runs only in fude's own terminal.
- B241 [7] An attached session's `"+` and `"*` are the clipboard of the
  machine its client runs on. A copy goes there, through its clipboard
  program or else OSC 52, and a put takes the text the session last
  copied. An attached session never reads or writes the system clipboard
  of the machine fude runs on.
- B242 [7] When fude quits, each attached client says so and exits. A
  client whose connection breaks exits and says why.
- B243 [7] A guest is a session with less power than the host's, as B244
  to B247 say. The project is the directory fude started in. A session
  from `fude share` is always a guest, and one from the local socket
  never is (D40).
- B244 [7] A guest runs no program of its choosing and changes nothing
  every session shares, such as the working directory, git's index, the
  theme and the language servers. Formatters, language servers and git
  run for it as the host's config says (D40). `:!`, `:r !`, `:{range}!`,
  `:terminal`, `:task`, `:pick tasks`, `:restart`, `:cd`,
  `:plugin reload`, `:grammar install` and `update`, `:lsp` with a
  subcommand, `:theme`, `:share`, `:kick`, `:debug`, `gh`, `gH` and a
  code action that runs a command each say a guest may not, and change
  nothing. So
  do keys mapped to them, such as `ZR`. A guest's `:q` on its last
  window and `:qa` leave fude, as B240 says. A guest shows no terminal
  or task window's buffer, whose keys would go to a program on the host.
- B245 [7] A guest opens, shows, writes, creates, renames and deletes only
  files under the project, after symlinks resolve, as a plugin with
  `fs.read` reads (B47). That holds for `:e`, `:w file`, `:r file`, the
  splits, `:b` on a buffer whose file is outside, an explorer's changes
  (B125), the finder, grep and a request naming a buffer. A `.git` and
  what is under it count as outside. A path outside says so, and nothing
  happens.
- B361 [7] Each session has pins of its own (B356). A guest's start
  empty, are never written, and end with its session, and the host's
  status lines never show them. A guest pins and goes to only files it may
  show (B245).
- B246 [7] A guest's key or command from a plugin that declared `process`,
  `net`, `fs.read` or `keys` says a guest may not, and the plugin is not
  called.
  Those of other plugins run as they do for any session.
- B247 [7] A read-only guest changes no text. Its keys, its requests, a
  replica's pushes and its `:accept` each say it is read-only, whichever
  edit they would make, and the buffer stays as it was. It still moves,
  searches, yanks and proposes.
- B248 [7] `fude share [address] [file...]` opens fude as usual and
  listens on address with TLS, or on a free port of every interface
  without one. Its first message is the share string. The string is one
  word that needs no quoting, and it names every address of fude, its
  certificate's fingerprint and a secret. Without an address, those are
  the machine's IPv4 addresses except loopback and link-local ones. A
  guest on its network, on a VPN such as Tailscale or on a VM's bridge
  joins with the same string. fude puts `fude join` and
  the string on the clipboard (B36). `fude join string` on another
  machine dials every address at once and goes on with the first that
  answers, and the host's fude asks whether to let the guest in, naming
  them by `$USER`. On y the guest's terminal draws a guest session of the
  host's fude, as `fude attach` does (B239, B243). Quitting fude ends the
  share and every guest session (B242), and so does `:restart`.
- B249 [7] A share string lets one guest in, and `:share` shows a new
  one and puts it on the clipboard as B248 does. A join ends the guest's client, saying why, when no
  address of the host answers within five seconds, with each address's error, when the host's
  certificate does not match the string's fingerprint, when its secret
  is wrong or used, when the host answers n, when another join waits for
  the host's answer, and when the host is answering another question.
- B250 [7] `:who` lists the guests by name, and `:kick name` ends that
  guest's session, whose client says the host ended it. A second guest
  with a name already taken gets a number after it.
- B251 [7] `fude relay [address]` passes shares through, on port 7418 of
  every interface without an address. `fude share --relay host
  [file...]` shares through the relay at host, port 7418 unless host
  names one, and its share string names the relay and a room in place
  of an address. A join with it works as B248 and B249 say. The TLS runs
  end to end, so the relay copies only ciphertext. A join to a room no
  one shares in says so. A relay that cannot be reached when
  fude starts is an error. When the relay goes away the host says so,
  and `:share` holds a new place at it.
- B252 [7] Each session has a name and a colour. The terminal fude
  started in takes `--name`, or else `$USER`, and `fude attach` and
  `fude join` take and send the name the same way, from the machine they run on. A client over
  the socket that sends none takes its origin as its name. Sessions take
  six colours in turn, in the order they began.
- B253 [7] A window shows the cursor of each other session whose current
  window shows its buffer, as a cell in that session's colour, and that
  session's selection underlined in it. The window's status line names
  those sessions, each in its colour.
- B254 [7] A guest's session takes the keymaps, leader and window
  options of `config.toml` on the guest's machine, never the host's, and
  its leader stands for `<Leader>` in plugins' keys too. The guest's
  terminal draws in that file's theme. Filetypes, language servers,
  formatters, plugins and every other setting stay the host's.
- B255 [7] `fude join --read-only string` asks to join as a read-only
  guest (B247). The host's question says so, and on y the guest's
  session is read-only.
- B256 [7] `:follow name` makes the session's windows show what the
  windows of the session called name show: their layout, which one is
  current, and each one's buffer, top line, left column and cursor. They
  keep up as that session moves. A window too short to show the cursor
  from that top line scrolls the least it must to show it. A key typed
  or a paste in the following session stops following, and the windows
  stay as they are. So does that session leaving, and the message says
  so. A guest's window keeps its buffer when the other's is one the
  guest may not show (B244, B245).
- B269 [7] The current window's status line says when fude shares,
  `sharing`, or `sharing with 2` once two guests are in. A guest's says
  `guest`, or `read-only guest`.

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
  origin's transaction inside it ends the node and starts a new one, and
  so do an undo, a redo and a move along the timeline. The text at a node
  leaves out the edits of a group still open.
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
  undo. A change it undid stays undone after the timeline loads again.
- B115 [6] A buffer gives the edits made since an older version, in order,
  or says it no longer has them.

## Vim library

Tested through `vim/`'s exported API with a minimal host over a byte slice.

- B110 [2] Every grammar behaviour gives the same result with the minimal
  host as with fude's editor, once a config maps the editor's own
  mappings of the grammar's keys, such as `s` (N32), to `""`.
- B229 [2] In insert mode, a character that may start a longer
  insert-mode mapping, such as the `j` of a `jk` mapped to `<Esc>`, types
  at once instead of waiting. When the mapping comes, the characters typed
  ahead go and the mapping runs. When another key or the timeout comes,
  they stay. Dot repeats what stayed. A bracket or quote, several cursors
  and a block insert still wait, as B84 says.
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
