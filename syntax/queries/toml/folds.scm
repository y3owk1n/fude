;; fude's folds for TOML (B591).

; A table folds from its header to its last key, as tables are not
; indented.
[
  (table)
  (table_array_element)
  (array)
  (inline_table)
] @fold
