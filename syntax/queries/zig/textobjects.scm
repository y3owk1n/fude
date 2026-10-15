;; fude's text objects for Zig (D28).

; A function or test, and the statements in its body.
(function_declaration body: (block (_)* @function.inner)) @function.outer
(test_declaration (block (_)* @function.inner)) @function.outer

; A struct, enum or union, and its members with the separators between them.
(struct_declaration [(_) "," ";"]* @class.inner) @class.outer
(enum_declaration [(_) "," ";"]* @class.inner) @class.outer
(union_declaration [(_) "," ";"]* @class.inner) @class.outer

; A parameter or argument. Its outer form takes the comma after it, or the
; one before it when it is last.
(parameters (_) @parameter.inner)
(arguments (_) @parameter.inner)
