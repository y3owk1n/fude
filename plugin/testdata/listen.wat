;; init subscribes to buffer.changed and its own listen.ping, and registers
;; the key gZ, which emits ping. on_event logs the event and puts the style keyword on the whole
;; text in place of the one it put before, so a test sees which text it heard
;; of. Once the text is over 8 bytes, it loops until its budget runs out.
(module
  (import "fude" "subscribe" (func $subscribe (param i32 i32) (result i32)))
  (import "fude" "register" (func $register (param i32 i32 i32 i32 i32 i32) (result i32)))
  (import "fude" "log" (func $log (param i32 i32)))
  (import "fude" "emit" (func $emit (param i32 i32) (result i32)))
  (import "fude" "text_len" (func $len (result i32)))
  (import "fude" "decorate" (func $decorate (param i32 i32 i32 i32) (result i32)))
  (import "fude" "undecorate" (func $undecorate (param i32) (result i32)))
  (memory (export "memory") 1)
  (global $last (mut i32) (i32.const 0))
  ;; buffer.changed at 16, keyword at 30, gZ at 37, ping at 39, listen.ping at
  ;; 43.
  (data (i32.const 16) "buffer.changedkeywordgZpinglisten.ping")
  (func (export "alloc") (param i32) (result i32) (i32.const 1024))
  (func (export "init")
    (drop (call $subscribe (i32.const 16) (i32.const 14)))
    (drop (call $subscribe (i32.const 43) (i32.const 11)))
    (drop (call $register (i32.const 3) (i32.const 37) (i32.const 2) (i32.const 0) (i32.const 0) (i32.const 0))))
  (func (export "call") (param i32 i64 i64 i64) (result i64)
    (drop (call $emit (i32.const 39) (i32.const 4)))
    (i64.const 0))
  (func (export "on_event") (param $ptr i32) (param $n i32)
    (call $log (local.get $ptr) (local.get $n))
    (if (i32.gt_u (call $len) (i32.const 8)) (then (loop $spin (br $spin))))
    (if (global.get $last) (then (drop (call $undecorate (global.get $last)))))
    (global.set $last (call $decorate (i32.const 0) (call $len) (i32.const 30) (i32.const 7)))))
