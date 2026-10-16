; fude's own, as the grammar's repo has none.
(comment) @comment

(negation) @operator

[
  (directory_separator)
  (directory_separator_escaped)
] @punctuation.delimiter

[
  (wildcard_char_single)
  (wildcard_chars)
  (wildcard_chars_allow_slash)
] @character.special

[
  (pattern_char_escaped)
  (bracket_char_escaped)
] @string.escape

(bracket_expr
  [
    "["
    "]"
  ] @punctuation.bracket)

(bracket_negation) @operator

(bracket_range
  "-" @operator)

(bracket_char_class) @constant.builtin
