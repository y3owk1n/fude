;; init shows ready on the statusline of the buffer it read.
(module
  (import "fude" "status" (func $status (param i32 i32) (result i32)))
  (memory (export "memory") 1)
  (data (i32.const 16) "ready")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (drop (call $status (i32.const 16) (i32.const 5)))))
