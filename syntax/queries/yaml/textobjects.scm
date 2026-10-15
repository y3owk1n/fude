;; fude's text objects for YAML (D28).

; A key whose value is a mapping, and that mapping.
(block_mapping_pair value: (block_node (block_mapping) @class.inner)) @class.outer
