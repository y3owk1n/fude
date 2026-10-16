;; Registers the operator gs, which upper-cases its target, the object aL,
;; which is the whole text, and the key gS and the command :Shout, which
;; insert "!" at the cursor. Needs buffer.read and buffer.write.
(module
  (import "fude" "register" (func $register (param i32 i32 i32 i32 i32 i32) (result i32)))
  (import "fude" "text_len" (func $len (result i32)))
  (import "fude" "text" (func $text (param i32 i32) (result i64)))
  (import "fude" "edit" (func $edit (param i32 i32 i32 i32) (result i32)))
  (memory (export "memory") 1)
  ;; gs at 16, aL at 18, gS at 20, Shout at 22, ! at 27, and the command's
  ;; help, "types ! at the cursor", at 28.
  (data (i32.const 16) "gsaLgSShout!types ! at the cursor")
  (global $next (mut i32) (i32.const 1024))
  (func (export "alloc") (param $n i32) (result i32)
    (global.get $next)
    (global.set $next (i32.add (global.get $next) (local.get $n))))
  ;; Ids: 0 gs, 1 aL, 2 gS, 3 :Shout.
  (func (export "init")
    (drop (call $register (i32.const 1) (i32.const 16) (i32.const 2) (i32.const 0) (i32.const 0) (i32.const 0)))
    (drop (call $register (i32.const 2) (i32.const 18) (i32.const 2) (i32.const 0) (i32.const 0) (i32.const 1)))
    (drop (call $register (i32.const 3) (i32.const 20) (i32.const 2) (i32.const 0) (i32.const 0) (i32.const 2)))
    (drop (call $register (i32.const 0) (i32.const 22) (i32.const 5) (i32.const 28) (i32.const 21) (i32.const 3))))
  (func (export "call") (param $id i32) (param $a i64) (param $b i64) (param $c i64) (result i64)
    (if (i32.eqz (local.get $id))
      (then (call $upper (i32.wrap_i64 (local.get $a)) (i32.wrap_i64 (local.get $b))) (return (i64.const 0))))
    (if (i32.eq (local.get $id) (i32.const 1))
      (then (return (i64.extend_i32_u (call $len)))))
    (drop (call $edit (i32.wrap_i64 (local.get $a)) (i32.wrap_i64 (local.get $a)) (i32.const 27) (i32.const 1)))
    (i64.const 0))
  (func $upper (param $start i32) (param $end i32)
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
    (drop (call $edit (local.get $start) (local.get $end) (local.get $p) (local.get $n)))))
