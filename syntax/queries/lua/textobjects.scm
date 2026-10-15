;; fude's text objects for Lua (D28).

; A function, and the statements in its body.
(function_declaration body: (block)? @function.inner) @function.outer
(function_definition body: (block)? @function.inner) @function.outer

; A parameter or argument. Its outer form takes the comma after it, or the
; one before it when it is last.
(parameters (_) @parameter.inner)
(arguments (_) @parameter.inner)
