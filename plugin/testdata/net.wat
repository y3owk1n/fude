;; init gets the URL that is the whole text and logs the body, then logs
;; refused when it may not get http://example.invalid/.
(module
  (import "fude" "http_get" (func $get (param i32 i32) (result i64)))
  (import "fude" "text_len" (func $len (result i32)))
  (import "fude" "text" (func $text (param i32 i32) (result i64)))
  (import "fude" "log" (func $log (param i32 i32)))
  (memory (export "memory") 1)
  ;; http://example.invalid/ at 16, refused at 39.
  (data (i32.const 16) "http://example.invalid/refused")
  (global $next (mut i32) (i32.const 1024))
  ;; alloc hands out memory from 1024 on.
  (func (export "alloc") (param $n i32) (result i32)
    (local $p i32)
    (local.set $p (global.get $next))
    (global.set $next (i32.add (global.get $next) (local.get $n)))
    (local.get $p))
  (func (export "init")
    (local $url i64)
    (local $r i64)
    (local.set $url (call $text (i32.const 0) (call $len)))
    (local.set $r (call $get (i32.wrap_i64 (i64.shr_u (local.get $url) (i64.const 32))) (i32.wrap_i64 (local.get $url))))
    (if (i64.ge_s (local.get $r) (i64.const 0))
      (then (call $log (i32.wrap_i64 (i64.shr_u (local.get $r) (i64.const 32))) (i32.wrap_i64 (local.get $r)))))
    (if (i64.lt_s (call $get (i32.const 16) (i32.const 23)) (i64.const 0))
      (then (call $log (i32.const 39) (i32.const 7))))))
