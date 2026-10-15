;; init inserts "hi" at the start of the buffer, which needs buffer.write.
(module
  (import "fude" "edit" (func $edit (param i32 i32 i32 i32) (result i32)))
  (memory (export "memory") 1)
  (data (i32.const 16) "hi")
  (global $next (mut i32) (i32.const 1024))
  (func (export "alloc") (param $n i32) (result i32)
    (global.get $next)
    (global.set $next (i32.add (global.get $next) (local.get $n))))
  (func (export "init")
    (drop (call $edit (i32.const 0) (i32.const 0) (i32.const 16) (i32.const 2)))))
