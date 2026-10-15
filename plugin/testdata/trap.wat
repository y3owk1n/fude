;; init makes an edit, then traps, so the edit must not land.
(module
  (import "fude" "edit" (func $edit (param i32 i32 i32 i32) (result i32)))
  (memory (export "memory") 1)
  (data (i32.const 16) "x")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (drop (call $edit (i32.const 0) (i32.const 0) (i32.const 16) (i32.const 1)))
    unreachable))
