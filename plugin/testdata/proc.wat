;; init logs what echo hi prints, and refused when it may not run cat. Then
;; it runs sleep 5, which its budget stops.
(module
  (import "fude" "run" (func $run (param i32 i32) (result i64)))
  (import "fude" "log" (func $log (param i32 i32)))
  (memory (export "memory") 1)
  ;; echo NUL hi at 16, cat at 23, refused at 26, sleep NUL 5 at 33.
  (data (i32.const 16) "echo\00hicatrefusedsleep\005")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (local $r i64)
    (local.set $r (call $run (i32.const 16) (i32.const 7)))
    (if (i64.ge_s (local.get $r) (i64.const 0))
      (then (call $log (i32.wrap_i64 (i64.shr_u (local.get $r) (i64.const 32))) (i32.wrap_i64 (local.get $r)))))
    (if (i64.lt_s (call $run (i32.const 23) (i32.const 3)) (i64.const 0))
      (then (call $log (i32.const 26) (i32.const 7))))
    (drop (call $run (i32.const 33) (i32.const 7)))))
