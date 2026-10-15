;; init logs data.txt from the project, refused when it may not read
;; ../out.txt, and unlinked when it may not read link.
(module
  (import "fude" "read_file" (func $read (param i32 i32) (result i64)))
  (import "fude" "log" (func $log (param i32 i32)))
  (memory (export "memory") 1)
  ;; data.txt at 16, ../out.txt at 24, refused at 34, link at 41, unlinked
  ;; at 45.
  (data (i32.const 16) "data.txt../out.txtrefusedlinkunlinked")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (local $r i64)
    (local.set $r (call $read (i32.const 16) (i32.const 8)))
    (if (i64.ge_s (local.get $r) (i64.const 0))
      (then (call $log (i32.wrap_i64 (i64.shr_u (local.get $r) (i64.const 32))) (i32.wrap_i64 (local.get $r)))))
    (if (i64.lt_s (call $read (i32.const 24) (i32.const 10)) (i64.const 0))
      (then (call $log (i32.const 34) (i32.const 7))))
    (if (i64.lt_s (call $read (i32.const 41) (i32.const 4)) (i64.const 0))
      (then (call $log (i32.const 45) (i32.const 8))))))
