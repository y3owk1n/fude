;; fude's text objects for TypeScript (D28).

; A function, and the statements in its body.
(function_declaration body: (statement_block (_)* @function.inner)) @function.outer
(function_expression body: (statement_block (_)* @function.inner)) @function.outer
(generator_function_declaration body: (statement_block (_)* @function.inner)) @function.outer
(arrow_function body: (statement_block (_)* @function.inner)) @function.outer
(arrow_function body: (expression) @function.inner) @function.outer
(method_definition body: (statement_block (_)* @function.inner)) @function.outer

; A class, and its members with the separators between them.
(class_declaration body: (class_body [(_) ";" ","]* @class.inner)) @class.outer

; A parameter or argument. Its outer form takes the comma after it, or the
; one before it when it is last.
(formal_parameters (_) @parameter.inner)
(arguments (_) @parameter.inner)

; An interface or enum, and its members with the separators between them.
(interface_declaration body: (interface_body [(_) ";" ","]* @class.inner)) @class.outer
(enum_declaration body: (enum_body [(_) ";" ","]* @class.inner)) @class.outer
(abstract_class_declaration body: (class_body [(_) ";" ","]* @class.inner)) @class.outer
(type_parameters (_) @parameter.inner)
(type_arguments (_) @parameter.inner)
