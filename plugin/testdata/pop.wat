;; init registers the key gP, which shows **hi** in a float.
(module
  (import "fude" "register" (func $register (param i32 i32 i32 i32 i32) (result i32)))
  (import "fude" "float" (func $float (param i32 i32) (result i32)))
  (memory (export "memory") 1)
  ;; gP at 16, pop at 18, **hi** at 21.
  (data (i32.const 16) "gPpop**hi**")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (drop (call $register (i32.const 3) (i32.const 16) (i32.const 2) (i32.const 18) (i32.const 3))))
  (func (export "pop") (param i32 i32)
    (drop (call $float (i32.const 21) (i32.const 6)))))
