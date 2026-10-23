package gui

import (
	"math"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/vim"
)

// The input method's messages, and what a composition's message says
// changed.
const (
	wmImeStartComposition = 0x010D
	wmImeEndComposition   = 0x010E
	wmImeComposition      = 0x010F
	gcsCompStr            = 0x0008
	gcsResultStr          = 0x0800
)

var (
	immGetContext     = imm32.NewProc("ImmGetContext")
	immReleaseContext = imm32.NewProc("ImmReleaseContext")
	immGetString      = imm32.NewProc("ImmGetCompositionStringW")
	immSetCandidate   = imm32.NewProc("ImmSetCandidateWindow")
	immSetComposition = imm32.NewProc("ImmSetCompositionWindow")
)

// candidateForm is a CANDIDATEFORM, and compositionForm a COMPOSITIONFORM.
type (
	candidateForm struct {
		index, style uint32
		pt           [2]int32
		area         rectI
	}
	compositionForm struct {
		style uint32
		pt    [2]int32
		area  rectI
	}
)

// ime handles a message of the input method, which takes each key first,
// and reports whether it did. What it composes shows at the cursor, its
// candidates show by the cursor, and what it inserts goes to the session
// as keys. A key it does not take comes as WM_KEYDOWN and WM_CHAR (B551,
// B578).
func (w *window) ime(m uint32, lp uintptr) bool {
	switch m {
	case wmImeStartComposition:
		w.placeCaret()
	case wmImeEndComposition:
		w.mark("")
	case wmImeComposition:
		imc := call(immGetContext, w.hwnd)
		defer call(immReleaseContext, w.hwnd, imc)
		if lp&gcsResultStr != 0 {
			w.typeText(composed(imc, gcsResultStr))
		}
		if lp&gcsCompStr != 0 {
			w.mark(composed(imc, gcsCompStr))
		}
	default:
		return false
	}
	return true
}

// composed is the input method's string of the kind, its composition or
// its result.
func composed(imc, kind uintptr) string {
	n := call(immGetString, imc, kind, 0, 0)
	if int32(n) <= 0 {
		return ""
	}
	u := make([]uint16, n/2)
	call(immGetString, imc, kind, uintptr(unsafe.Pointer(&u[0])), n)
	return windows.UTF16ToString(u)
}

// typeText types the text an input method inserts, each character a key.
func (w *window) typeText(text string) {
	for _, r := range text {
		w.keys <- vim.Key(string(r))
	}
}

// mark shows text, what an input method composes, underlined at the
// cursor, or hides it for "".
func (w *window) mark(text string) {
	if text != w.marked {
		w.marked = text
		w.redraw()
	}
}

// drawMarked draws what the input method composes at the cursor, on the
// theme's ground over the text under it.
func (w *window) drawMarked() {
	at, ok := w.caret()
	if w.marked == "" || !ok {
		return
	}
	r := rect{at, size{math.Ceil(measure(w.marked, font).W) + 2, cellH}}
	fillRGB(w.look.Base.Bg, r)
	drawString(w.marked, theme.Style{Fg: w.look.Base.Fg}, "", at)
	fillRGB(w.look.Base.Fg, rect{point{at.X, at.Y + cellH - 2}, size{r.Size.W, 1}})
}

// caret is where the cursor is in the window: in the palette's or the
// finder's line, in a float with the focus, or in a pane.
func (w *window) caret() (point, bool) {
	if b := w.panel; b.boxed != nil && b.list != nil {
		return point{b.frame.Origin.X + b.list.caret.X, b.frame.Origin.Y + b.list.caret.Y}, true
	}
	in := func(r rect, c cursorAt, o point) point {
		return point{r.Origin.X + o.X + float64(c.x)*cellW, r.Origin.Y + o.Y + float64(c.y)*cellH}
	}
	for _, b := range w.floats {
		if b.boxed != nil && b.cursor.shown {
			return in(b.frame, b.cursor, point{inset, inset + b.head()}), true
		}
	}
	for _, p := range w.panes {
		if p.cursor.shown {
			return in(p.frame, p.cursor, p.origin()), true
		}
	}
	return point{}, false
}

// placeCaret tells the input method where the cursor is, so its
// composition and candidates show there.
func (w *window) placeCaret() {
	at, ok := w.caret()
	if !ok {
		return
	}
	imc := call(immGetContext, w.hwnd)
	defer call(immReleaseContext, w.hwnd, imc)
	pt := [2]int32{int32(at.X * w.scale), int32(at.Y * w.scale)}
	below := [2]int32{pt[0], int32((at.Y + cellH) * w.scale)}
	cf := compositionForm{style: 2, pt: pt}
	call(immSetComposition, imc, uintptr(unsafe.Pointer(&cf)))
	cand := candidateForm{style: 0x40, pt: below}
	call(immSetCandidate, imc, uintptr(unsafe.Pointer(&cand)))
}
