;; fude's folds for Go (B591).

; Declarations, from their first line, so a long signature folds with its
; body.
[
  (import_declaration)
  (const_declaration)
  (var_declaration)
  (type_declaration)
  (function_declaration)
  (method_declaration)
] @fold

; Bodies and bracketed lists over more lines.
[
  (block)
  (func_literal)
  (literal_value)
  (argument_list)
  (parameter_list)
  (field_declaration_list)
  (interface_type)
  (expression_switch_statement)
  (type_switch_statement)
  (select_statement)
  (expression_case)
  (type_case)
  (default_case)
  (communication_case)
  (raw_string_literal)
  (comment)
] @fold
