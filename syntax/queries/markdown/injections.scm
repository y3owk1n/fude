; A table's cells hold inline text, as paragraphs do. The repo's
; injections leave them out.
((pipe_table_cell) @injection.content
  (#set! injection.language "markdown_inline"))
