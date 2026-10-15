;; fude's text objects for TOML (D28).

; A table, and its pairs.
(table (pair)+ @class.inner) @class.outer
(table_array_element (pair)+ @class.inner) @class.outer
