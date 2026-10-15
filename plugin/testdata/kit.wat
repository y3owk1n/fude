;; Registers the operator gs, which upper-cases its target, the object aL,
;; which is the whole text, and the key gS and the command :Shout, which
;; insert "!" at the cursor. Needs buffer.read and buffer.write.
(module
  (import "fude" "register" (func $register (param i32 i32 i32 i32 i32) (result i32)))
  (import "fude" "text_len" (func $len (result i32)))
  (import "fude" "text" (func $text (param i32 i32) (result i64)))
  (import "fude" "edit" (func $edit (param i32 i32 i32 i32) (result i32)))
  (memory (export "memory") 1)
  ;; gs at 16, upper at 18, aL at 23, whole at 25, gS at 30, shout at 32,
  ;; Shout at 37, ! at 42.
  (data (i32.const 16) "gsupperaLwholegSshoutShout!")
  (global $next (mut i32) (i32.const 1024))
  (func (export "alloc") (param $n i32) (result i32)
    (global.get $next)
    (global.set $next (i32.add (global.get $next) (local.get $n))))
  (func (export "init")
    (drop (call $register (i32.const 1) (i32.const 16) (i32.const 2) (i32.const 18) (i32.const 5)))
    (drop (call $register (i32.const 2) (i32.const 23) (i32.const 2) (i32.const 25) (i32.const 5)))
    (drop (call $register (i32.const 3) (i32.const 30) (i32.const 2) (i32.const 32) (i32.const 5)))
    (drop (call $register (i32.const 0) (i32.const 37) (i32.const 5) (i32.const 32) (i32.const 5))))
  (func (export "upper") (param $start i32) (param $end i32) (param $lines i32)
    (local $r i64) (local $p i32) (local $n i32) (local $i i32) (local $c i32)
    (local.set $r (call $text (local.get $start) (local.get $end)))
    (local.set $p (i32.wrap_i64 (i64.shr_u (local.get $r) (i64.const 32))))
    (local.set $n (i32.wrap_i64 (local.get $r)))
    (block $done
      (loop $l
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $c (i32.load8_u (i32.add (local.get $p) (local.get $i))))
        (if (i32.and (i32.ge_u (local.get $c) (i32.const 97)) (i32.le_u (local.get $c) (i32.const 122)))
          (then (i32.store8 (i32.add (local.get $p) (local.get $i)) (i32.sub (local.get $c) (i32.const 32)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $l)))
    (drop (call $edit (local.get $start) (local.get $end) (local.get $p) (local.get $n))))
  (func (export "whole") (param i32 i32) (result i64)
    (i64.extend_i32_u (call $len)))
  (func (export "shout") (param $pos i32) (param $count i32)
    (drop (call $edit (local.get $pos) (local.get $pos) (i32.const 42) (i32.const 1)))))
