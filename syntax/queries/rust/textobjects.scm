;; fude's text objects for Rust (D28).

; A function or closure, and what is in its body.
(function_item body: (block (_)* @function.inner)) @function.outer
(closure_expression body: (block (_)* @function.inner)) @function.outer

; An impl, trait, struct or enum, and what is in its body.
(impl_item body: (declaration_list (_)* @class.inner)) @class.outer
(trait_item body: (declaration_list (_)* @class.inner)) @class.outer
(struct_item body: (field_declaration_list [(_) ","]* @class.inner)) @class.outer
(enum_item body: (enum_variant_list [(_) ","]* @class.inner)) @class.outer

; A parameter or argument. Its outer form takes the comma after it, or the
; one before it when it is last.
(parameters (_) @parameter.inner)
(arguments (_) @parameter.inner)
(type_parameters (_) @parameter.inner)
(type_arguments (_) @parameter.inner)
