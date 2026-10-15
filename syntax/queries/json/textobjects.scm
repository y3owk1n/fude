;; fude's text objects for JSON (D28).

; A pair whose value is an object, and that object's pairs.
(pair value: (object (pair)+ @class.inner)) @class.outer

; A pair or an element. Its outer form takes the comma after it, or the one
; before it when it is last.
(object (pair) @parameter.inner)
(array (_) @parameter.inner)
