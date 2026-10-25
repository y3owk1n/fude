;; fude's folds for Lua (B591).

; A statement folds whole, to its end, and an elseif or else folds inside
; its if.
[
  (function_declaration)
  (function_definition)
  (if_statement)
  (elseif_statement)
  (else_statement)
  (for_statement)
  (while_statement)
  (repeat_statement)
  (do_statement)
  (table_constructor)
  (arguments)
  (comment)
] @fold
