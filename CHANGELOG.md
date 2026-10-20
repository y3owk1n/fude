# Changelog

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
