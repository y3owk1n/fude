;; fude's text objects for bash (D28).

; A function, and the commands in its body.
(function_definition body: (compound_statement (_)+ @function.inner)) @function.outer
