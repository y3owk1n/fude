;; fude's text objects for Go (D28).

; A function, and the statements in its body.
(function_declaration
  body: (block (statement_list)? @function.inner)) @function.outer
(method_declaration
  body: (block (statement_list)? @function.inner)) @function.outer
(func_literal
  body: (block (statement_list)? @function.inner)) @function.outer

; A type declaration, and its fields or methods.
(type_declaration
  (type_spec type: (struct_type (field_declaration_list (_)+ @class.inner)))) @class.outer
(type_declaration
  (type_spec type: (interface_type (_)+ @class.inner))) @class.outer

; A parameter or argument. Its outer form takes the comma after it, or the
; one before it when it is last.
(parameter_list (_) @parameter.inner)
(argument_list (_) @parameter.inner)
(type_parameter_list (_) @parameter.inner)
