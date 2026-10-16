# Syntax and colours

fude colours a file through its language's tree-sitter grammar, and the
same grammar gives the syntax text objects and the comment syntax of `gc`.

## Grammars

Every grammar, Go's too, builds the first time a file needs it, with git,
the tree-sitter CLI and a C compiler, which must be on PATH. The
file shows its colours once the build ends. `auto_install = false` stops
the builds.

`:grammar` lists each grammar and its state, and `:grammar install yaml`
builds one again. A grammar built again takes effect after `:restart`.

Text of another language inside a file shows in that language's colours,
such as the code in a markdown fenced block or a `<script>` in HTML. `gc`
comments it in that language too.

## Selecting by syntax

`if` and `af` select a function's body or the whole function, `ic` and
`ac` a class, and `ia` and `aa` an argument. In visual mode, `an` and
`in` grow the selection to the node around it, and `]n` and `[n` move to
the next and previous node.

## Queries

A grammar's queries say what to colour and what the objects are. A file
in the `queries` folder beside `config.toml` replaces the one fude ships,
such as `queries/go/highlights.scm` for Go's colours. A query that does
not load names its file and line.

## Colours

`theme` is `dark`, `light` or a base16 scheme file, such as stylix's
`palette.json`. `:theme light` switches every window until fude quits, and
`:theme` alone shows the one in use.

## Next

- `guide-plugins` adds commands and keys to fude.
