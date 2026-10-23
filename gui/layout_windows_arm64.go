package gui

import "unsafe"

// drawLayout draws the text layout l from x, y. Its origin is a struct of
// two floats, which arm64 passes as two floats.
func drawLayout(rt unsafe.Pointer, x, y float32, l, brush unsafe.Pointer) {
	bind[func(this unsafe.Pointer, x, y float32, l, brush unsafe.Pointer, options uint32)](rt, 28)(rt, x, y, l, brush, 4)
}

// fromPointOut is where ElementProviderFromPoint wants its result, after
// its two floats, which arm64 passes in the float registers.
func fromPointOut(out, _, _ uintptr) uintptr { return out }

// rangePointOut is where RangeFromPoint wants its result, after its point,
// which arm64 passes in the float registers.
func rangePointOut(out, _ uintptr) uintptr { return out }
