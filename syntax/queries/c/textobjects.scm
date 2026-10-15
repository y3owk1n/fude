;; fude's text objects for C (D28).

; A function, and the statements in its body.
(function_definition body: (compound_statement (_)* @function.inner)) @function.outer

; A struct or enum, and what is in its body.
(struct_specifier body: (field_declaration_list (_)* @class.inner)) @class.outer
(enum_specifier body: (enumerator_list [(_) ","]* @class.inner)) @class.outer

; A parameter or argument. Its outer form takes the comma after it, or the
; one before it when it is last.
(parameter_list (_) @parameter.inner)
(argument_list (_) @parameter.inner)
