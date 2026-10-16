;; init registers the key gP, which shows **hi** in a float.
(module
  (import "fude" "register" (func $register (param i32 i32 i32 i32 i32 i32) (result i32)))
  (import "fude" "float" (func $float (param i32 i32) (result i32)))
  (memory (export "memory") 1)
  ;; gP at 16, **hi** at 18.
  (data (i32.const 16) "gP**hi**")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (drop (call $register (i32.const 3) (i32.const 16) (i32.const 2) (i32.const 0) (i32.const 0) (i32.const 0))))
  (func (export "call") (param i32 i64 i64 i64) (result i64)
    (drop (call $float (i32.const 18) (i32.const 6)))
    (i64.const 0)))
