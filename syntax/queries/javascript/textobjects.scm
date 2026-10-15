;; fude's text objects for JavaScript (D28).

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
