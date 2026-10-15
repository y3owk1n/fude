;; fude's text objects for HTML (D28).

; An element, and what is between its tags.
(element (start_tag) (_)* @class.inner (end_tag)) @class.outer
(script_element (start_tag) (_)* @class.inner (end_tag)) @class.outer
(style_element (start_tag) (_)* @class.inner (end_tag)) @class.outer

; An attribute.
(start_tag (attribute) @parameter.inner)
(self_closing_tag (attribute) @parameter.inner)
