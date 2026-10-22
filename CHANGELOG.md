# Changelog

## [0.2.1](https://github.com/y3owk1n/fude/compare/v0.2.0...v0.2.1) (2026-10-22)


### Features

* **api:** :w !cmd pipes the lines, and :! expands % ([265054b](https://github.com/y3owk1n/fude/commit/265054b46c5fc5ac48838dcfc84540c997a428dd))
* **api:** &lt;C-n&gt;, &lt;C-p&gt; and &lt;C-x&gt; complete in insert mode ([3f0d70b](https://github.com/y3owk1n/fude/commit/3f0d70b42db447d5a1de2a172ce53fc51f2b826e))
* **api:** a key waits briefly for its busy plugin ([601ce86](https://github.com/y3owk1n/fude/commit/601ce86929ed6e440600b3714f8b5e791166f6f0))
* **api:** agents propose changes a user reviews ([708dfc5](https://github.com/y3owk1n/fude/commit/708dfc58b387f149fa1c5d8861d2b677e676210d))
* **api:** ghost text of several lines at the cursor ([9dd511e](https://github.com/y3owk1n/fude/commit/9dd511e5f73852551cca1c3629d1b90ecf63cf26))
* **api:** key hints show a page at a time ([8c986dc](https://github.com/y3owk1n/fude/commit/8c986dcb92cb8284a6e1da00f9573db31aa86e40))
* **api:** modules run the client commands a server names ([b2f583c](https://github.com/y3owk1n/fude/commit/b2f583cf452c9e23fb07816c9407de2bd85ba4a7))
* **api:** talk to a program a stream job runs ([627883b](https://github.com/y3owk1n/fude/commit/627883b40076d65e9f878955ce243b81422cfe9f))
* **api:** windows that scroll together ([e0138c3](https://github.com/y3owk1n/fude/commit/e0138c335766695a71f984ba5caebfbd969f8a3d))
* **diff:** diff mode ([a59a501](https://github.com/y3owk1n/fude/commit/a59a501fd0a95d0df209c077546c8087156ae21d))
* **file:** :file and :saveas make the old name the alternate file ([01832ac](https://github.com/y3owk1n/fude/commit/01832ac9320c4926b39ba1696fd0a04940e4632a))
* **filetype:** a #! line and :set filetype give a buffer its filetype ([27de0f3](https://github.com/y3owk1n/fude/commit/27de0f320bb4647eaa729b8955bcf1b623178636))
* **fold:** fold.column marks folds in the sign column ([6916938](https://github.com/y3owk1n/fude/commit/69169384cc3e3a05d23fba76c2b4606769cb4441))
* **fold:** zo and zc go a level at a time, zO and zC every level ([16bfa1b](https://github.com/y3owk1n/fude/commit/16bfa1b380849737b2966ed49fb251aefb65c486))
* **open:** fude open takes path:N ([7e24123](https://github.com/y3owk1n/fude/commit/7e241238889f5ea84e2494df4c723d0ff17cb03f))
* **options:** [list_chars] and [fill_chars] set what list and fillers draw ([09f0f4b](https://github.com/y3owk1n/fude/commit/09f0f4b3e249e02ae84055bc32bcb346a1461358))
* **options:** cursor_column shades the cursor's column ([4c33dc6](https://github.com/y3owk1n/fude/commit/4c33dc6375de76ad7b6109a1ae9003f9a2d9cfba))
* **options:** menu_height caps the completion menu's rows ([2c629be](https://github.com/y3owk1n/fude/commit/2c629bec61c03720da37cedc8312fb4d18d41401))
* **options:** side_scroll_off keeps columns beside the cursor ([2b7cdfb](https://github.com/y3owk1n/fude/commit/2b7cdfb434616374ee8af220774dee67b878b040))
* **options:** title shows the file's name in the terminal's title ([31c895a](https://github.com/y3owk1n/fude/commit/31c895a565c436acdb3b7f62bd5883c6915aee62))
* **places:** :colder and :cnewer go between the last ten lists ([6e65b54](https://github.com/y3owk1n/fude/commit/6e65b5496cfcd7213ddfe762acecd038e58695e9))
* **plugin:** sdk.Now reads the time of day ([52698a8](https://github.com/y3owk1n/fude/commit/52698a883e50b354dbbdf0473b1cebd69c15c0a7))
* **protocol:** ask servers for the diagnostics they do not publish ([c04afa0](https://github.com/y3owk1n/fude/commit/c04afa0fe56a14d88da440ac77ff191d1e60bca3))
* **protocol:** format as text is typed ([29d4328](https://github.com/y3owk1n/fude/commit/29d4328a7ca0c517b549053ef5490eaf703b29b0))
* **protocol:** go to a symbol's declaration ([a77cfdc](https://github.com/y3owk1n/fude/commit/a77cfdcc6fa66e4a61e2290efc641f0d3fd45bbf))
* **protocol:** language servers hear of saves ([909164b](https://github.com/y3owk1n/fude/commit/909164b04a8c32273fcc39841bb01f37281a2e5b))
* **protocol:** light up the references to the symbol under the cursor ([65102fd](https://github.com/y3owk1n/fude/commit/65102fd00e1035f93b8abe5a19349e7da3596c02))
* **protocol:** list call and type hierarchies ([3344ed0](https://github.com/y3owk1n/fude/commit/3344ed065f7048d46ac302b83a6ab1e4a119c4ff))
* **protocol:** modules hear and answer what a server sends fude does not handle ([be46134](https://github.com/y3owk1n/fude/commit/be4613401d3a6b3d883d449719fbbc2cfc8bf1c0))
* **protocol:** semantic tokens colour text over syntax highlights ([6f60be4](https://github.com/y3owk1n/fude/commit/6f60be4104a3b12b20ad4ca15629238dd7e3cb50))
* **protocol:** server tables cover root groups, requires, excludes, fallback, command candidates, capabilities and connect ([36ea029](https://github.com/y3owk1n/fude/commit/36ea029559235b6acb5f348b62a3a351217c8dd8))
* **protocol:** server tables take placeholders, root patterns, env, a language id and answers ([1b0cd2c](https://github.com/y3owk1n/fude/commit/1b0cd2c25357fa2ac319fb3b1258e878c3e8fc1a))
* **protocol:** show a server's inline completion, which &lt;Tab&gt; takes ([705fae9](https://github.com/y3owk1n/fude/commit/705fae926b42a456288914bb540084d681c1fb3b))
* **protocol:** show code lenses and run them with grx ([8302896](https://github.com/y3owk1n/fude/commit/8302896eeaf56ad4e614719d61f698722dc9a25c))
* **protocol:** tell servers of the files fude changes ([b44d803](https://github.com/y3owk1n/fude/commit/b44d803c8bd46b5f40f67ba1e3cb93ec51580e75))
* **protocol:** typing in a linked range types in each ([7b0d214](https://github.com/y3owk1n/fude/commit/7b0d21407013295508777cafeec1d2c87e9b2493))
* **start:** open a file at path:N and run +cmd as a command ([7ec034b](https://github.com/y3owk1n/fude/commit/7ec034befa3d97c100ff5691d971b9579a06fe8e))
* **storage:** list and clean what fude keeps ([4aff8b9](https://github.com/y3owk1n/fude/commit/4aff8b93ff73d98d34f2db620ed124fc91acb553))
* **timeline:** delete the logs of changed and unused files ([10f1b3c](https://github.com/y3owk1n/fude/commit/10f1b3c0d8b05d7f6103116b40c7728b77a7df7a))
* **vim:** abbreviations ([eb26ffb](https://github.com/y3owk1n/fude/commit/eb26ffb712c5fe2e5bff42fd85f7d56456d30a22))
* **vim:** break_indent and show_break start wrapped rows in ([3591bb5](https://github.com/y3owk1n/fude/commit/3591bb5de26fe130b4eef7f0fc5198cc64ebdeeb))
* **vim:** file marks A to Z ([2d3474e](https://github.com/y3owk1n/fude/commit/2d3474ec485a9ff7903feb5c1bad6b46de889e1e))
* **vim:** gcgc takes the gc object, so it uncomments the block ([022c650](https://github.com/y3owk1n/fude/commit/022c6500efcde73016d28eac43f9fdb37383c286))
* **vim:** more marks, ex commands and command-line keys ([92857b1](https://github.com/y3owk1n/fude/commit/92857b12e8b34f947dd83550d1caa187a443832b))
* **vim:** multicursors ([f668250](https://github.com/y3owk1n/fude/commit/f668250c07865d353cdb237da523c2cb8932b804))
* **vim:** registers, file marks and histories outlive a run ([c40ada0](https://github.com/y3owk1n/fude/commit/c40ada076a10f4a267755eede9047d8678966bf4))
* **vim:** text_width breaks typed lines, line_break wraps at words ([513b7bc](https://github.com/y3owk1n/fude/commit/513b7bcd8bfd10419de1ae5b0c5c2dd4012b0f7d))
* **vim:** U puts back the line last changed ([39bec8a](https://github.com/y3owk1n/fude/commit/39bec8ad6c24a58aca990f50300096bd8dda7e46))
* **vim:** virtual_edit lets the cursor past a line's end ([1aa39cd](https://github.com/y3owk1n/fude/commit/1aa39cd097a0eb15f479ddeff8f4a5cf457d2712))
* **vim:** window and buffer keys, :file, :saveas, :cquit and &lt;C-z&gt; ([25b6fff](https://github.com/y3owk1n/fude/commit/25b6fff581a32426bab7e8999369795011b33555))
* **vim:** wrap_scan and hl_search turn off wrapping and lit matches ([adcce78](https://github.com/y3owk1n/fude/commit/adcce78e1819462706c64de274c67049e1098305))
* **window:** a count sizes &lt;C-w&gt;n's window, and &lt;C-w&gt;w, W and p reach floats ([612b2dd](https://github.com/y3owk1n/fude/commit/612b2dd2b47a45482a4b3fae88ad4ab12beda52c))


### Bug Fixes

* **api:** do and dp map only in diff mode ([4ca02f3](https://github.com/y3owk1n/fude/commit/4ca02f3665188165fe9a9878e544c6a2d1272641))
* **buffers:** :b matches a file's own name before its directories ([e98dbde](https://github.com/y3owk1n/fude/commit/e98dbde6bcab40969c27d06d1e0a70d30131ef7f))
* **clipboard:** yanks in terminals, floats, panels and the finder reach the clipboard ([a4af910](https://github.com/y3owk1n/fude/commit/a4af9101564e896b90bc868884160ea7ac344af2))
* **cmdline:** &lt;CR&gt; on a path that exists opens it, not the first entry ([7650acf](https://github.com/y3owk1n/fude/commit/7650acfd9ee63522629f85aeed96ae608369c336))
* **core:** count timeline nodes without copying them ([61fd96c](https://github.com/y3owk1n/fude/commit/61fd96ca2f07b3d2593e76a99b8f68f1912c8f73))
* **diff:** :diffget matches a file's own name before its directories ([27330a1](https://github.com/y3owk1n/fude/commit/27330a1fabd7859ae0e60ef94195699bd15738c0))
* **file:** &lt;C-g&gt; and :file say what they show in words ([822948c](https://github.com/y3owk1n/fude/commit/822948c10c38f1f44db7f474d7783bfa382e3437))
* **finder:** a panel that opens while a finder shows leaves it the keys ([8bcb044](https://github.com/y3owk1n/fude/commit/8bcb044dbe6438d7ca507e6e78bd396fcc724d11))
* **lsp:** a code action's command says nothing when the server answers null ([35692d9](https://github.com/y3owk1n/fude/commit/35692d9f3d90c801ea06f11076746f5290917f93))
* **open:** fude open keeps a:2 a path on Windows ([745ae17](https://github.com/y3owk1n/fude/commit/745ae17e73917973cce7b5f8bc9568c228df4b5d))
* **protocol:** declare publishDiagnostics so servers such as vtsls send diagnostics ([fe4de22](https://github.com/y3owk1n/fude/commit/fe4de2236d714d505d3e785fb946653f4b4a02cf))
* **syntax:** copy the text once a parse, not once an edit ([dcba08f](https://github.com/y3owk1n/fude/commit/dcba08f916cfe2cf36e0c4de3ee7aac156e7be5b))
* **syntax:** move parse results through each change once ([43c3407](https://github.com/y3owk1n/fude/commit/43c34074d56a6ac0c109450fd15973a5677fb1ed))
* **syntax:** parse once a stream of edits stops ([bf49a7c](https://github.com/y3owk1n/fude/commit/bf49a7c10b4cb09849d41e4e71282cf5dc04fbfb))
* **syntax:** run parses below the priority of keys ([0e53225](https://github.com/y3owk1n/fude/commit/0e5322555adb0b60b7ad777fcf5aad61bc1747d1))
* **term:** wait 10ms for a lone Esc ([2646aa4](https://github.com/y3owk1n/fude/commit/2646aa416133bedded7d7609d6e54869fe89c69e))
* **term:** write no frame when nothing on screen changed ([dc0965f](https://github.com/y3owk1n/fude/commit/dc0965f813a330c09a591cbb83cff8f142c6e2c2))
* **timeline:** close the log between writes, and open uncut when a cut fails ([5abac47](https://github.com/y3owk1n/fude/commit/5abac4789d2bf8f675c48880f798640b5782fe06))
* **timeline:** cut an oversized log when a file opens ([996513b](https://github.com/y3owk1n/fude/commit/996513b27c2043edfd9e8bcb5be30a7dc9d0d6b1))
* **timeline:** find each state's parent by id ([6fca1e5](https://github.com/y3owk1n/fude/commit/6fca1e5f2339d52c430453483f1121962dd0e465))
* **timeline:** write the log off the loop ([44e65b4](https://github.com/y3owk1n/fude/commit/44e65b45dee61fe36528e1ec72cbc325f2d590dd))
* **vim:** a module's &lt;Tab&gt; runs before expand_tab's spaces ([34acf46](https://github.com/y3owk1n/fude/commit/34acf464cbf3ae47a99d768516f35ac61aee7852))
* **vim:** a search takes an offset after its pattern ([e05286b](https://github.com/y3owk1n/fude/commit/e05286b293550ee449fe2def90367ae71b34e38a))
* **vim:** counts repeat inserts, &lt;C-c&gt; leaves, arrows move in normal mode ([2ddddec](https://github.com/y3owk1n/fude/commit/2ddddecab9795e552dd7fd7fb0787f695d75928f))
* **vim:** ex lines take | between commands, counts and :s flags c and n ([e4ca28a](https://github.com/y3owk1n/fude/commit/e4ca28a32db8a265b24931c157fdb3aee47a99b7))
* **window:** a height counts the rows of text ([ee870a6](https://github.com/y3owk1n/fude/commit/ee870a67dea17e1dfe1420bf256826e0862c1d8f))
* **wrap:** a line taller than the window shows the cursor's rows ([aa1c99c](https://github.com/y3owk1n/fude/commit/aa1c99c225e48cde89f867e356419767c307c15c))


### Performance Improvements

* **api:** modules read the text a window shows from its view ([3cb56f6](https://github.com/y3owk1n/fude/commit/3cb56f6169be6d1853685b669434cf35ec345cad))
* **attach:** copy and encode a client's whole texts off the loop ([c47bc06](https://github.com/y3owk1n/fude/commit/c47bc06434ca9e2a084052a24a549ac3c3061df4))
* **core:** edits close together build the rope once, and marks move in one pass ([a98530e](https://github.com/y3owk1n/fude/commit/a98530ebfdc306681b471d9fc0716544874678df))
* **core:** long lines are walked eight bytes a step, from the rope's own pieces ([b093045](https://github.com/y3owk1n/fude/commit/b0930453cc7eab096faa00eb41e039b815b0677c))
* **core:** text copies into a buffer the reader reuses, and the syntax worker reads it in small chunks ([0fb13f0](https://github.com/y3owk1n/fude/commit/0fb13f001381a5e1b2376a856f783851ad444b1e))
* **git:** a change diffs only the lines it touched ([157aa09](https://github.com/y3owk1n/fude/commit/157aa097c4c9d314ada74298c9de13d3a0532a5b))
* **save:** write and hash the file from the snapshot a chunk at a time ([c186098](https://github.com/y3owk1n/fude/commit/c186098aeac7a126a416ed36fcdbb7c5b96cb3bb))
* **syntax:** a change of many edits moves highlights by halves and parses afresh ([d74858e](https://github.com/y3owk1n/fude/commit/d74858e55824f78bf55b98f743f2a1e1344ce881))
* **vim:** a long line costs what the window shows of it, not its length ([7e196da](https://github.com/y3owk1n/fude/commit/7e196dafdef9056cd50c14d4078f2bbd2b606045))
* **vim:** a search being typed keeps each pattern's match, and looks for its literal's rarest byte ([9daf85e](https://github.com/y3owk1n/fude/commit/9daf85ede3718c53c341bc2c74e1af5ed7ec353d))
* **vim:** far jumps and screen rows on a long line start near where they go ([28bc124](https://github.com/y3owk1n/fude/commit/28bc1247858334b1e340548b7965907d7ca3a81c))

## [0.2.0](https://github.com/y3owk1n/fude/compare/v0.1.1...v0.2.0) (2026-10-20)


### ⚠ BREAKING CHANGES

* these options moved, and every window now shares them.
* these config.toml keys moved.
* vim.Host's Unhandled returns an error. A host that embeds vim.Defaults gets one that returns nil.
* **vim:** vim.Host has FileName. A host that embeds vim.Defaults gets it, and one that does not must write it.
* **vim:** vim.Host has Sideways and SetLeft. A host that embeds vim.Defaults gets them, and one that does not must write them.

### Features

* a command that fails returns why, so :cdo, :g and macros stop (B112, B257, B372) ([040f73a](https://github.com/y3owk1n/fude/commit/040f73a2faa3fca846b6f703be352f4b76f367a3))
* **api:** modules and plugins write help: Doc and guide pages (B375, B376) ([e1a8bb7](https://github.com/y3owk1n/fude/commit/e1a8bb7484b19d5a4f3ba52a32197895a8d64060))
* **finder:** :pick resume opens the last finder again (B374) ([d5dda40](https://github.com/y3owk1n/fude/commit/d5dda40d299fd7a4baa297af86743e4e39647d4f))
* floats are windows (B364) ([520bd89](https://github.com/y3owk1n/fude/commit/520bd890e33ff6011270eec0c61602cc54df5b2c))
* **help:** every key and command explained at length, with examples (B162) ([c0b557c](https://github.com/y3owk1n/fude/commit/c0b557c7bc17e856fdfcb29a7d9c80376f8639fa))
* **help:** motions explained at length, with examples a test runs (B162) ([79383bf](https://github.com/y3owk1n/fude/commit/79383bf693620b4bff7c79f0a319f86d7975b35c))
* **lsp:** the :lsp panel moves and scrolls as any window (B159) ([92bcfea](https://github.com/y3owk1n/fude/commit/92bcfea2bd7c6efc67f442fb72042ec71b8472ec))
* modules declare options, which :set changes (B377) ([8c6f634](https://github.com/y3owk1n/fude/commit/8c6f634c7caa53d3bfe4c3f8b7bfeb139ea8d2d4))
* **plugin:** disabled keeps a plugin from loading (B378) ([5e87073](https://github.com/y3owk1n/fude/commit/5e8707337587a75e87331df08cf9837cf739a2c2))
* sticky context and indent guides are modules (B153, B217, B379, B380) ([830fe42](https://github.com/y3owk1n/fude/commit/830fe42184f47d73e7a38febe6a4432b2596951e))
* **terminal:** a shell's window closes when it exits (B205) ([46ee230](https://github.com/y3owk1n/fude/commit/46ee2308c4ff3864318063b85c5ef731e009c54b))
* **terminal:** complete :terminal with the session's shells (B205) ([22de59d](https://github.com/y3owk1n/fude/commit/22de59d5905184236644a580a596f421f8d2c409))
* the first file is buffer 1, and a view may take no number (B89, B373) ([489c5f1](https://github.com/y3owk1n/fude/commit/489c5f118f3078551baa2de7465d674bf8c3ad0c))
* **ui:** a focused float shows its status line in its bottom border (B364) ([1906f3a](https://github.com/y3owk1n/fude/commit/1906f3a8d6a027e456aaff9c948ee4184b3d393a))
* Vim's insert keys, digraphs and the command-line window (B29, B142, B371) ([ac549a1](https://github.com/y3owk1n/fude/commit/ac549a187896f4bb98869df5bbb85d39430508f9))
* **vim:** the motions, marks and tag objects Vim has (B6, B10, B13, B20, B27) ([6b69f54](https://github.com/y3owk1n/fude/commit/6b69f5497a7dbcbcf55dca5649c979ebc5c81976))
* **vim:** the operators, puts and registers Vim has (B14, B15, B17, B22, B25, B26, B85, B156, B224, B234) ([c3cb64b](https://github.com/y3owk1n/fude/commit/c3cb64be4526c3196b13ffa2b7745acca954be06))
* windows hide, and :terminal toggles a shell (B364, B205) ([001d9e0](https://github.com/y3owk1n/fude/commit/001d9e05e6bcebb2cdf4798347966fec95ac7e1a))
* windows open where modules, [open] and :window say (B365, B366, B367) ([9a8f7f6](https://github.com/y3owk1n/fude/commit/9a8f7f6971926cf46892c135ee07dc0366ed50e6))
* zoom a window, and bring every window back (B370) ([105fa3b](https://github.com/y3owk1n/fude/commit/105fa3bc6f8764a12f73a9f31916840d70fa8fbe))


### Bug Fixes

* an ended terminal and a closed window let go of what they hold ([e4ad1ba](https://github.com/y3owk1n/fude/commit/e4ad1ba2e4cd79bbb5b87d795c92fd0949c257bd))
* closed views and refreshed panels free their text (B133, B381) ([c2fb001](https://github.com/y3owk1n/fude/commit/c2fb0013774cd0e6188aafbe331979728e3c595c))
* **explorer:** an explorer keeps its icons when another one is read (B128) ([cff4617](https://github.com/y3owk1n/fude/commit/cff461762320633c6525a82435af2a8ce754c5eb))
* **help:** headings nest under one title ([63f1b86](https://github.com/y3owk1n/fude/commit/63f1b866217610a8c4664324163f78bfa0c89303))
* **terminal:** a terminal opened from a file shows no line numbers ([cfb7c83](https://github.com/y3owk1n/fude/commit/cfb7c838367100a13c1d5772d4f0357455ea9018))
* **ui:** clip a float's title to its border ([6032846](https://github.com/y3owk1n/fude/commit/603284629acdd182db24ee9c115e1876fe874949))
* **ui:** draw over a modal box in styles on its ground ([09fe71a](https://github.com/y3owk1n/fude/commit/09fe71a64d77d517b4cd78135c70f9b5ac39030b))
* **vim:** &lt;Esc&gt; after &lt;C-k&gt; types nothing (B29) ([230c181](https://github.com/y3owk1n/fude/commit/230c181c7f7d5b3ab2bd7eecb1344033c22a0fa1))
* **vim:** g& keeps the flags, counts around a register multiply, visual # and &lt;C-g&gt;u (B7, B14, B22, B28) ([2be3485](https://github.com/y3owk1n/fude/commit/2be348512ae3019a2f55e091bb467d19fe176dd8))
* what writing help's reference turned up ([7b01c8f](https://github.com/y3owk1n/fude/commit/7b01c8f865919abe0522f5e2733ec868d322b585))

## [0.1.1](https://github.com/y3owk1n/fude/compare/v0.1.0...v0.1.1) (2026-10-19)


### Features

* **cmd:** name oku's source builds in fude version ([d333734](https://github.com/y3owk1n/fude/commit/d3337349d61e7c9418590cfb99a6db15ca9ea1c6))


### Bug Fixes

* **build:** pin the program's dependencies for go install ([c404e7f](https://github.com/y3owk1n/fude/commit/c404e7fbdc92753552a663ab0c30f519d07a10fc))

## 0.1.0 (2026-10-19)


### Features

* **version:** print the release fude was installed as ([9e2f00f](https://github.com/y3owk1n/fude/commit/9e2f00f6322b92b6d2a6b3976b44ecd7506e4026))


### Bug Fixes

* **vim:** scroll in visual mode ([afc1981](https://github.com/y3owk1n/fude/commit/afc1981b940ad4c8644dffd544d620001a84e46e))
