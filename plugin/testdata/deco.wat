;; init puts the style keyword on [0, 2), which needs ui, and registers the
;; key gX, which traps, so the plugin unloads.
(module
  (import "fude" "decorate" (func $decorate (param i32 i32 i32 i32) (result i32)))
  (import "fude" "register" (func $register (param i32 i32 i32 i32 i32) (result i32)))
  (memory (export "memory") 1)
  ;; keyword at 16, gX at 23, fail at 25.
  (data (i32.const 16) "keywordgXfail")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (drop (call $decorate (i32.const 0) (i32.const 2) (i32.const 16) (i32.const 7)))
    (drop (call $register (i32.const 3) (i32.const 23) (i32.const 2) (i32.const 25) (i32.const 4))))
  (func (export "fail") (param i32 i32) unreachable))
