;; fude's text objects for CSS (D28).

; A rule, and the declarations in its block.
(rule_set (block (_)* @class.inner)) @class.outer
(media_statement (block (_)* @class.inner)) @class.outer
(keyframe_block (block (_)* @class.inner)) @class.outer

; A declaration, or a function's argument.
(block (declaration) @parameter.inner)
(arguments (_) @parameter.inner)
