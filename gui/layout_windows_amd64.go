package gui

import (
	"math"
	"unsafe"
)

// drawLayout draws the text layout l from x, y. Its origin is a struct of
// two floats, which amd64 passes as eight bytes in one register.
func drawLayout(rt unsafe.Pointer, x, y float32, l, brush unsafe.Pointer) {
	com(rt, 28, uintptr(math.Float32bits(x))|uintptr(math.Float32bits(y))<<32, uintptr(l), uintptr(brush), 4)
}

// fromPointOut is where ElementProviderFromPoint wants its result, after
// its two floats, which amd64 counts among the arguments' registers.
func fromPointOut(_, _, out uintptr) uintptr { return out }

// rangePointOut is where RangeFromPoint wants its result, after its point,
// which amd64 passes by its address.
func rangePointOut(_, out uintptr) uintptr { return out }
