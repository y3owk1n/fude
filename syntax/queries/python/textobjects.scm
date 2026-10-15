;; fude's text objects for Python (D28).

; A function, and the statements in its body.
(function_definition body: (block) @function.inner) @function.outer

; A class, and the statements in its body.
(class_definition body: (block) @class.inner) @class.outer

; A parameter or argument. Its outer form takes the comma after it, or the
; one before it when it is last.
(parameters (_) @parameter.inner)
(argument_list (_) @parameter.inner)
