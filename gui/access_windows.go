package gui

import (
	"fmt"
	"maps"
	"slices"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// UI Automation's calls, and OLE's for the values it takes (D67).
var (
	uiaCore         = windows.NewLazySystemDLL("uiautomationcore.dll")
	oleAut          = windows.NewLazySystemDLL("oleaut32.dll")
	uiaReturn       = uiaCore.NewProc("UiaReturnRawElementProvider")
	uiaHost         = uiaCore.NewProc("UiaHostProviderFromHwnd")
	uiaRaise        = uiaCore.NewProc("UiaRaiseAutomationEvent")
	uiaNotify       = uiaCore.NewProc("UiaRaiseNotificationEvent")
	uiaListening    = uiaCore.NewProc("UiaClientsAreListening")
	uiaDisconnect   = uiaCore.NewProc("UiaDisconnectProvider")
	uiaNotSupported = uiaCore.NewProc("UiaGetReservedNotSupportedValue")
	sysAllocString  = oleAut.NewProc("SysAllocStringLen")
	sysFreeString   = oleAut.NewProc("SysFreeString")
	safeArrayCreate = oleAut.NewProc("SafeArrayCreateVector")
	safeArrayPut    = oleAut.NewProc("SafeArrayPutElement")
)

// UI Automation's values fude uses, from uiautomationclient.h and
// uiautomationcore.h.
const (
	wmGetObject     = 0x003D
	uiaRootObjectID = -25

	sOK             = 0
	eNoInterface    = 0x80004002
	eNotAvailable   = 0x80040201 // UIA_E_ELEMENTNOTAVAILABLE
	serverSide      = 0x2        // ProviderOptions_ServerSideProvider
	appendRuntimeID = 3

	invokePattern    = 10000
	selectionPattern = 10010
	textPattern      = 10014

	propControlType   = 30003
	propName          = 30005
	propHasFocus      = 30008
	propFocusable     = 30009
	propEnabled       = 30010
	propIsSelected    = 30079
	propTextAvailable = 30040

	focusChanged     = 20005
	selectionChanged = 20014
	textChanged      = 20015

	ctlButton   = 50000
	ctlEdit     = 50004
	ctlListItem = 50007
	ctlList     = 50008
	ctlTab      = 50018
	ctlTabItem  = 50019
	ctlText     = 50020
	ctlGroup    = 50026
	ctlDocument = 50030

	vtI4      = 3
	vtR8      = 5
	vtBSTR    = 8
	vtBool    = 11
	vtUnknown = 13
)

// The interfaces a node can be, as the slots of node.vt.
const (
	slotSimple = iota
	slotFragment
	slotRoot
	slotText
	slotInvoke
	slotSelection
	slots
)

// iids are the interfaces' IIDs, by slot, and IUnknown's.
var (
	iids = [slots]windows.GUID{
		{Data1: 0xd6dd68d1, Data2: 0x86fd, Data3: 0x4332, Data4: [8]byte{0x86, 0x66, 0x9a, 0xbe, 0xde, 0xa2, 0xd2, 0x4c}},
		{Data1: 0xf7063da8, Data2: 0x8359, Data3: 0x439c, Data4: [8]byte{0x92, 0x97, 0xbb, 0xc5, 0x29, 0x9a, 0x7d, 0x87}},
		{Data1: 0x620ce2a5, Data2: 0xab8f, Data3: 0x40a9, Data4: [8]byte{0x86, 0xcb, 0xde, 0x3c, 0x75, 0x59, 0x9b, 0x58}},
		{Data1: 0x3589c92c, Data2: 0x63f3, Data3: 0x4367, Data4: [8]byte{0x99, 0xbb, 0xad, 0xa6, 0x53, 0xb7, 0x7c, 0xf2}},
		{Data1: 0x54fcb24b, Data2: 0xe18e, Data3: 0x47a2, Data4: [8]byte{0xb4, 0xd3, 0xec, 0xcb, 0xe7, 0x75, 0x99, 0xa2}},
		{Data1: 0x2acad808, Data2: 0xb2d4, Data3: 0x452d, Data4: [8]byte{0xa4, 0x07, 0x91, 0xff, 0x1a, 0xd1, 0x67, 0xb2}},
	}
	iidUnknown = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidRange   = windows.GUID{Data1: 0x5347ad7b, Data2: 0xc355, Data3: 0x46f8, Data4: [8]byte{0xaf, 0xf5, 0x90, 0x90, 0x33, 0x58, 0x2f, 0x63}}
)

// node is an element of a window that UI Automation hands assistive apps:
// the window, a pane's text or status bar, the tab strip, a tab, its close
// button, a box, the palette's text field, its list or a row. vt holds a
// pointer to each interface's methods, and each interface's pointer is the
// address of its slot.
type node struct {
	vt    [slots]unsafe.Pointer
	refs  int32
	id    int32
	w     *window
	role  int // its control type, 0 for the window, which Windows describes
	is    [slots]bool
	name  func() string
	frame func() rect // in the window's points
	kids  func() []*node
	up    *node
	read  func() (reading, *spoken, bool)
	caret func() (rect, bool) // the cursor's cell in the window's points
	press func()
	on    func() bool // it is selected
	gone  bool
}

// nodes are the nodes by each of their interfaces' pointers, which keep
// them while UI Automation holds them.
var nodes = map[uintptr]*node{}

// lastID counts the nodes made, each its runtime id.
var lastID int32

// newNode returns a node of w of the control type role, with the
// interfaces of is beside IRawElementProviderSimple and
// IRawElementProviderFragment, holding one reference for its maker.
func newNode(w *window, up *node, role int, is ...int) *node {
	lastID++
	n := &node{refs: 1, id: lastID, w: w, up: up, role: role, name: func() string { return "" }, frame: func() rect { return rect{} }}
	n.is[slotSimple], n.is[slotFragment] = true, true
	for _, s := range is {
		n.is[s] = true
	}
	for s := range slots {
		n.vt[s] = unsafe.Pointer(&vtables[s][0])
		nodes[n.ptr(s)] = n
	}
	return n
}

// has reports whether n is the interface of slot s now: a box is text only
// while it holds some.
func (n *node) has(s int) bool {
	if s == slotText {
		_, _, ok := n.reading()
		return ok
	}
	return n.is[s]
}

// ptr is n's pointer as the interface of slot s.
func (n *node) ptr(s int) uintptr { return uintptr(unsafe.Pointer(&n.vt[s])) }

// give returns n as the interface of slot s, with a reference for whoever
// takes it, or 0 for none.
func (n *node) give(s int) uintptr {
	if n == nil {
		return 0
	}
	n.refs++
	return n.ptr(s)
}

// release lets go of a reference to n, which forgets it at the last.
func (n *node) release() {
	if n.refs--; n.refs == 0 {
		for s := range slots {
			delete(nodes, n.ptr(s))
		}
	}
}

// drop disconnects n from assistive apps and lets go of its maker's
// reference.
func (n *node) drop() {
	if n == nil || n.gone {
		return
	}
	n.gone = true
	call(uiaDisconnect, n.ptr(slotSimple))
	n.release()
}

// live reports whether n's window and n are still there.
func (n *node) live() bool { return n != nil && !n.gone && opened[n.w.hwnd] == n.w }

// textRange is an ITextRangeProvider: units from to to of a node's text.
type textRange struct {
	vt       unsafe.Pointer
	refs     int32
	n        *node
	from, to int
}

// ranges are the ranges by their pointers, which keep them while UI
// Automation holds them.
var ranges = map[uintptr]*textRange{}

// newRange returns a range of n's text from to to, with a reference for
// whoever takes it.
func newRange(n *node, from, to int) uintptr {
	r := &textRange{vt: unsafe.Pointer(&rangeVtable[0]), refs: 1, n: n, from: from, to: to}
	n.refs++
	p := uintptr(unsafe.Pointer(r))
	ranges[p] = r
	return p
}

// out is the memory at the address p that Windows gave.
func out[T any](p uintptr) *T { return (*T)(*(*unsafe.Pointer)(unsafe.Pointer(&p))) }

// variant is a VARIANT.
type variant struct {
	vt     uint16
	_      [3]uint16
	val, _ uintptr
}

// bstr returns a BSTR of s, which whoever takes it frees.
func bstr(s string) uintptr {
	u := utf16.Encode([]rune(s))
	if len(u) == 0 {
		return call(sysAllocString, 0, 0)
	}
	return call(sysAllocString, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)))
}

// array returns a SAFEARRAY of values of the VARIANT type vt, each the
// address of a value, or for vtUnknown the interface's pointer.
//
//go:uintptrescapes
func array(vt uint16, values ...uintptr) uintptr {
	a := call(safeArrayCreate, uintptr(vt), 0, uintptr(len(values)))
	for i, v := range values {
		at := int32(i)
		call(safeArrayPut, a, uintptr(unsafe.Pointer(&at)), v)
	}
	return a
}

// vtables are each slot's methods, and rangeVtable ITextRangeProvider's,
// made once as Windows cannot free a callback.
var (
	vtables     [slots][]uintptr
	rangeVtable []uintptr
)

func init() {
	cb := syscall.NewCallback
	unknown := []uintptr{cb(queryInterface), cb(addRef), cb(releaseNode)}
	vtables[slotSimple] = append(slices.Clone(unknown), cb(providerOptions), cb(patternProvider), cb(propertyValue), cb(hostProvider))
	vtables[slotFragment] = append(slices.Clone(unknown), cb(navigate), cb(runtimeID), cb(boundingRect), cb(embeddedRoots), cb(setFocus), cb(fragmentRoot))
	vtables[slotRoot] = append(slices.Clone(unknown), cb(elementFromPoint), cb(getFocus))
	vtables[slotText] = append(slices.Clone(unknown), cb(textSelection), cb(visibleRanges), cb(rangeFromChild), cb(rangeFromPoint), cb(documentRange), cb(supportedSelection))
	vtables[slotInvoke] = append(slices.Clone(unknown), cb(invoke))
	vtables[slotSelection] = append(slices.Clone(unknown), cb(selectItem), cb(selectItem), cb(func(uintptr) uintptr { return sOK }), cb(isSelected), cb(selectionContainer))
	rangeVtable = []uintptr{
		cb(rangeQuery), cb(rangeAddRef), cb(rangeRelease),
		cb(rangeClone), cb(rangeCompare), cb(rangeCompareEndpoints), cb(rangeExpand),
		cb(func(_, _, _, _, ret uintptr) uintptr { *out[uintptr](ret) = 0; return sOK }),
		cb(func(_, _, _, _, ret uintptr) uintptr { *out[uintptr](ret) = 0; return sOK }),
		cb(rangeAttribute), cb(rangeRects), cb(rangeElement), cb(rangeText),
		cb(rangeMove), cb(rangeMoveEndpoint), cb(rangeMoveToRange),
		cb(func(uintptr) uintptr { return sOK }), cb(func(uintptr) uintptr { return sOK }), cb(func(uintptr) uintptr { return sOK }),
		cb(func(uintptr, uintptr) uintptr { return sOK }), cb(rangeChildren),
	}
}

func queryInterface(this, iid, ret uintptr) uintptr {
	n := nodes[this]
	want := *out[windows.GUID](iid)
	for s, id := range iids {
		if n != nil && (id == want || want == iidUnknown && s == slotSimple) && n.is[s] {
			*out[uintptr](ret) = n.give(s)
			return sOK
		}
	}
	*out[uintptr](ret) = 0
	return eNoInterface
}

func addRef(this uintptr) uintptr {
	if n := nodes[this]; n != nil {
		n.refs++
		return uintptr(n.refs)
	}
	return 0
}

func releaseNode(this uintptr) uintptr {
	if n := nodes[this]; n != nil {
		n.release()
		return uintptr(n.refs)
	}
	return 0
}

func providerOptions(_, ret uintptr) uintptr {
	*out[int32](ret) = serverSide
	return sOK
}

func patternProvider(this, pattern, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	n := nodes[this]
	if !n.live() {
		return eNotAvailable
	}
	for _, p := range [][2]int{{textPattern, slotText}, {invokePattern, slotInvoke}, {selectionPattern, slotSelection}} {
		if int(pattern) == p[0] && n.has(p[1]) {
			*out[uintptr](ret) = n.give(p[1])
		}
	}
	return sOK
}

func propertyValue(this, prop, ret uintptr) uintptr {
	v := out[variant](ret)
	*v = variant{}
	n := nodes[this]
	if !n.live() {
		return eNotAvailable
	}
	flag := func(on bool) {
		v.vt, v.val = vtBool, 0
		if on {
			v.val = 0xffff
		}
	}
	switch {
	case n.role == 0:
	case prop == propControlType:
		v.vt, v.val = vtI4, uintptr(n.role)
	case prop == propName:
		v.vt, v.val = vtBSTR, bstr(n.name())
	case prop == propHasFocus:
		flag(n.w.focus() == n)
	case prop == propFocusable:
		flag(n.read != nil)
	case prop == propEnabled:
		flag(true)
	case prop == propTextAvailable:
		flag(n.has(slotText))
	case prop == propIsSelected && n.on != nil:
		flag(n.on())
	}
	return sOK
}

func hostProvider(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	if n := nodes[this]; n.live() && n.role == 0 {
		return call(uiaHost, n.w.hwnd, ret)
	}
	return sOK
}

// The directions Navigate takes.
const (
	toParent = iota
	toNext
	toPrevious
	toFirst
	toLast
)

func navigate(this, direction, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	n := nodes[this]
	if !n.live() {
		return eNotAvailable
	}
	var to *node
	switch direction {
	case toParent:
		to = n.up
	case toFirst, toLast:
		if n.kids != nil {
			if kids := n.kids(); len(kids) > 0 {
				to = kids[0]
				if direction == toLast {
					to = kids[len(kids)-1]
				}
			}
		}
	default:
		if n.up != nil && n.up.kids != nil {
			kids := n.up.kids()
			if i := slices.Index(kids, n); i >= 0 {
				if j := i + 1 - 2*int(direction-toNext); j >= 0 && j < len(kids) {
					to = kids[j]
				}
			}
		}
	}
	*out[uintptr](ret) = to.give(slotFragment)
	return sOK
}

func runtimeID(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	n := nodes[this]
	if !n.live() {
		return eNotAvailable
	}
	if n.role != 0 {
		ids := [2]int32{appendRuntimeID, n.id}
		*out[uintptr](ret) = array(vtI4, uintptr(unsafe.Pointer(&ids[0])), uintptr(unsafe.Pointer(&ids[1])))
	}
	return sOK
}

func boundingRect(this, ret uintptr) uintptr {
	n := nodes[this]
	*out[[4]float64](ret) = [4]float64{}
	if !n.live() {
		return eNotAvailable
	}
	if n.role != 0 {
		*out[[4]float64](ret) = n.w.onScreen(n.frame())
	}
	return sOK
}

// onScreen is r of w's points in the screen's pixels, as left, top, width
// and height.
func (w *window) onScreen(r rect) [4]float64 {
	pt := [2]int32{int32(r.Origin.X * w.scale), int32(r.Origin.Y * w.scale)}
	call(clientToScreen, w.hwnd, uintptr(unsafe.Pointer(&pt)))
	return [4]float64{float64(pt[0]), float64(pt[1]), r.Size.W * w.scale, r.Size.H * w.scale}
}

func embeddedRoots(_, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	return sOK
}

func setFocus(uintptr) uintptr { return sOK }

func fragmentRoot(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	n := nodes[this]
	if !n.live() {
		return eNotAvailable
	}
	*out[uintptr](ret) = n.w.root.give(slotRoot)
	return sOK
}

// elementFromPoint gives no element, so UI Automation finds the window. Its
// point is two floats, which each CPU passes in its own registers, and
// fromPointOut picks where the result goes.
func elementFromPoint(_, a, b, c uintptr) uintptr {
	*out[uintptr](fromPointOut(a, b, c)) = 0
	return sOK
}

func getFocus(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	if n := nodes[this]; n.live() {
		if f := n.w.focus(); f != n {
			*out[uintptr](ret) = f.give(slotFragment)
		}
	}
	return sOK
}

func textSelection(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	n := nodes[this]
	r, s, ok := n.reading()
	if !ok {
		return eNotAvailable
	}
	rg := newRange(n, s.unit(r.chosen[0]), s.unit(r.chosen[1]))
	*out[uintptr](ret) = array(vtUnknown, rg)
	ranges[rg].release()
	return sOK
}

func visibleRanges(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	n := nodes[this]
	r, s, ok := n.reading()
	if !ok {
		return eNotAvailable
	}
	rg := newRange(n, s.unit(r.view[0]), s.unit(r.view[1]))
	*out[uintptr](ret) = array(vtUnknown, rg)
	ranges[rg].release()
	return sOK
}

func rangeFromChild(_, _, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	return sOK
}

// rangeFromPoint gives the whole text, as cells do not say where other
// text is. Its point is a struct of two floats, which each CPU passes its
// own way, and rangePointOut picks where the result goes.
func rangeFromPoint(this, a, b uintptr) uintptr {
	return documentRange(this, rangePointOut(a, b))
}

func documentRange(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	n := nodes[this]
	_, s, ok := n.reading()
	if !ok {
		return eNotAvailable
	}
	*out[uintptr](ret) = newRange(n, 0, s.total)
	return sOK
}

func supportedSelection(_, ret uintptr) uintptr {
	*out[int32](ret) = 1
	return sOK
}

func invoke(this uintptr) uintptr {
	if n := nodes[this]; n.live() && n.press != nil {
		n.press()
		return sOK
	}
	return eNotAvailable
}

func selectItem(this uintptr) uintptr { return invoke(this) }

func isSelected(this, ret uintptr) uintptr {
	n := nodes[this]
	*out[int32](ret) = 0
	if n.live() && n.on != nil && n.on() {
		*out[int32](ret) = 1
	}
	return sOK
}

func selectionContainer(this, ret uintptr) uintptr {
	*out[uintptr](ret) = 0
	if n := nodes[this]; n.live() {
		*out[uintptr](ret) = n.up.give(slotSimple)
	}
	return sOK
}

// reading is n's text, in UTF-16 units, false when n has none or went.
func (n *node) reading() (reading, *spoken, bool) {
	if !n.live() || n.read == nil {
		return reading{}, nil, false
	}
	return n.read()
}

func rangeQuery(this, iid, ret uintptr) uintptr {
	want := *out[windows.GUID](iid)
	if r := ranges[this]; r != nil && (want == iidRange || want == iidUnknown) {
		r.refs++
		*out[uintptr](ret) = this
		return sOK
	}
	*out[uintptr](ret) = 0
	return eNoInterface
}

func rangeAddRef(this uintptr) uintptr {
	if r := ranges[this]; r != nil {
		r.refs++
		return uintptr(r.refs)
	}
	return 0
}

func rangeRelease(this uintptr) uintptr {
	if r := ranges[this]; r != nil {
		r.release()
		return uintptr(r.refs)
	}
	return 0
}

// release lets go of a reference to r, which forgets it and lets go of its
// node at the last.
func (r *textRange) release() {
	if r.refs--; r.refs == 0 {
		delete(ranges, uintptr(unsafe.Pointer(r)))
		r.n.release()
	}
}

// text is the text r is of, false when its node went.
func (r *textRange) text() (*spoken, bool) {
	_, s, ok := r.n.reading()
	if ok {
		r.from, r.to = min(r.from, s.total), min(r.to, s.total)
	}
	return s, ok
}

func rangeClone(this, ret uintptr) uintptr {
	r := ranges[this]
	*out[uintptr](ret) = newRange(r.n, r.from, r.to)
	return sOK
}

func rangeCompare(this, other, ret uintptr) uintptr {
	r, o := ranges[this], ranges[other]
	*out[int32](ret) = 0
	if o != nil && r.n == o.n && r.from == o.from && r.to == o.to {
		*out[int32](ret) = 1
	}
	return sOK
}

// end is where an endpoint of r is, its start for 0 and its end for 1.
func (r *textRange) end(endpoint uintptr) int {
	if endpoint == 0 {
		return r.from
	}
	return r.to
}

func rangeCompareEndpoints(this, endpoint, other, otherEndpoint, ret uintptr) uintptr {
	r, o := ranges[this], ranges[other]
	if o == nil {
		return eNotAvailable
	}
	*out[int32](ret) = int32(r.end(endpoint) - o.end(otherEndpoint))
	return sOK
}

// The units a range moves by.
const (
	unitCharacter = iota
	unitFormat
	unitWord
	unitLine
	unitParagraph
	unitPage
	unitDocument
)

// enclose returns the unit at u that holds it: its character, word or
// line, or the whole text.
func enclose(s *spoken, u, unit int) (int, int) {
	switch unit {
	case unitCharacter:
		b := s.byteAt(u)
		c, size := utf8.DecodeRuneInString(s.text[b:])
		if size == 0 {
			return s.unit(b), s.unit(b)
		}
		return s.unit(b), s.unit(b) + utf16Units(c)
	case unitWord:
		return s.wordAt(u)
	case unitLine, unitParagraph:
		return s.lineUnits(s.line(u))
	}
	return 0, s.total
}

// step returns where the unit after u starts, or before it with back, and
// false at the text's end.
func step(s *spoken, u, unit int, back bool) (int, bool) {
	if back {
		if u <= 0 {
			return 0, false
		}
		from, _ := enclose(s, u-1, unit)
		return from, true
	}
	if u >= s.total {
		return s.total, false
	}
	_, to := enclose(s, u, unit)
	return max(to, u+1), true
}

func rangeExpand(this, unit uintptr) uintptr {
	r := ranges[this]
	s, ok := r.text()
	if !ok {
		return eNotAvailable
	}
	r.from, r.to = enclose(s, r.from, int(unit))
	return sOK
}

func rangeAttribute(_, _, ret uintptr) uintptr {
	v := out[variant](ret)
	*v = variant{vt: vtUnknown}
	return call(uiaNotSupported, uintptr(unsafe.Pointer(&v.val)))
}

func rangeRects(this, ret uintptr) uintptr {
	r := ranges[this]
	*out[uintptr](ret) = 0
	s, ok := r.text()
	if !ok {
		return eNotAvailable
	}
	box := r.n.frame()
	if rd, _, _ := r.n.reading(); r.n.caret != nil && r.from == s.unit(rd.cursor) {
		if c, ok := r.n.caret(); ok {
			box = c
		}
	}
	b := r.n.w.onScreen(box)
	*out[uintptr](ret) = array(vtR8, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&b[1])), uintptr(unsafe.Pointer(&b[2])), uintptr(unsafe.Pointer(&b[3])))
	return sOK
}

func rangeElement(this, ret uintptr) uintptr {
	*out[uintptr](ret) = ranges[this].n.give(slotSimple)
	return sOK
}

func rangeText(this, most, ret uintptr) uintptr {
	r := ranges[this]
	s, ok := r.text()
	if !ok {
		*out[uintptr](ret) = bstr("")
		return eNotAvailable
	}
	to := r.to
	if n := int(int32(most)); n >= 0 {
		to = min(to, r.from+n)
	}
	*out[uintptr](ret) = bstr(s.slice(r.from, to))
	return sOK
}

func rangeMove(this, unit, count, ret uintptr) uintptr {
	r := ranges[this]
	s, ok := r.text()
	if !ok {
		return eNotAvailable
	}
	n, moved := int(int32(count)), 0
	empty := r.from == r.to
	at := r.from
	for ; moved != n; moved += sign(n) {
		next, ok := step(s, at, int(unit), n < 0)
		if !ok || next == s.total && n > 0 {
			break
		}
		at = next
	}
	r.from, r.to = at, at
	if !empty {
		_, r.to = enclose(s, at, int(unit))
	}
	*out[int32](ret) = int32(moved)
	return sOK
}

func rangeMoveEndpoint(this, endpoint, unit, count, ret uintptr) uintptr {
	r := ranges[this]
	s, ok := r.text()
	if !ok {
		return eNotAvailable
	}
	n, moved := int(int32(count)), 0
	at := r.end(endpoint)
	for ; moved != n; moved += sign(n) {
		next, ok := step(s, at, int(unit), n < 0)
		if !ok {
			break
		}
		at = next
	}
	r.put(endpoint, at)
	*out[int32](ret) = int32(moved)
	return sOK
}

func rangeMoveToRange(this, endpoint, other, otherEndpoint uintptr) uintptr {
	r, o := ranges[this], ranges[other]
	if o == nil {
		return eNotAvailable
	}
	r.put(endpoint, o.end(otherEndpoint))
	return sOK
}

// put moves an endpoint of r to at, taking the other one with it past it.
func (r *textRange) put(endpoint uintptr, at int) {
	if endpoint == 0 {
		r.from, r.to = at, max(r.to, at)
	} else {
		r.from, r.to = min(r.from, at), at
	}
}

func rangeChildren(_, ret uintptr) uintptr {
	*out[uintptr](ret) = array(vtUnknown)
	return sOK
}

// sign is n's sign, 1 for 0.
func sign(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}

// accessNative is what a window keeps for UI Automation: its root and the
// nodes of its tab strip.
type accessNative struct {
	root, strip *node
	tabIDs      kidNodes
	focused     *node
}

// kidNodes are nodes made for what a node draws, made again when what it
// draws changes.
type kidNodes struct {
	key   string
	nodes []*node
}

// of returns the nodes for key, which build makes when key changed.
func (k *kidNodes) of(key string, build func() []*node) []*node {
	if key != k.key || k.nodes == nil {
		k.drop()
		k.key, k.nodes = key, build()
	}
	return k.nodes
}

// drop lets go of k's nodes and the nodes in them.
func (k *kidNodes) drop() {
	for _, n := range k.nodes {
		if n.kids != nil {
			for _, kid := range n.kids() {
				kid.drop()
			}
		}
		n.drop()
	}
	k.nodes = nil
}

// newAccess makes w's root and its tab strip's node.
func (w *window) newAccess() {
	w.root = newNode(w, nil, 0, slotRoot)
	w.root.kids = w.rootKids
	w.strip = newNode(w, w.root, ctlTab)
	w.strip.name = func() string { return "tabs" }
	w.strip.frame = func() rect { return rect{Size: size{w.area().W, stripH}} }
	w.strip.kids = w.tabNodes
}

// dropAccess disconnects w's nodes from assistive apps, as w closes.
func (w *window) dropAccess() {
	w.tabIDs.drop()
	for _, p := range w.panes {
		p.dropAccess()
	}
	for _, b := range w.order {
		b.dropAccess()
	}
	w.strip.drop()
	call(uiaReturn, w.hwnd, 0, 0, 0)
	w.root.drop()
}

// rootKids are the window's elements: each pane's text and status bar, the
// tab strip and the boxes that show.
func (w *window) rootKids() []*node {
	var kids []*node
	for _, id := range slices.Sorted(maps.Keys(w.panes)) {
		p := w.panes[id]
		kids = append(kids, p.textNode(), p.statusNode())
	}
	if w.tabbed {
		kids = append(kids, w.strip)
	}
	for _, b := range w.order {
		if b.boxed != nil {
			kids = append(kids, b.boxNode())
		}
	}
	return kids
}

// paneAccess are a pane's nodes for its text and its status bar.
type paneAccess struct {
	text, status *node
	said         spoken
	told         told
}

// textNode is p's text, a document named by its buffer, made once.
func (p *pane) textNode() *node {
	if p.text != nil {
		return p.text
	}
	p.said.count = utf16Units
	n := newNode(p.w, p.w.root, ctlDocument, slotText)
	n.name = func() string { return p.read.label() }
	n.frame = func() rect { f := p.frame; f.Size.H -= statusH; return f }
	n.read = func() (reading, *spoken, bool) {
		if p.gone {
			return reading{}, nil, false
		}
		return p.read, p.said.of(p.read), true
	}
	n.caret = func() (rect, bool) {
		r := at(p.cursor.x, p.cursor.y, 1, p.origin())
		r.Origin.X, r.Origin.Y = r.Origin.X+p.frame.Origin.X, r.Origin.Y+p.frame.Origin.Y
		return r, p.cursor.shown
	}
	p.text = n
	return n
}

// statusNode is p's status bar, text of its parts, made once.
func (p *pane) statusNode() *node {
	if p.status == nil {
		n := newNode(p.w, p.w.root, ctlText)
		n.name = func() string { return statusText(p.left, p.right) }
		n.frame = func() rect {
			f := p.frame
			return rect{point{f.Origin.X, f.Origin.Y + f.Size.H - statusH}, size{f.Size.W, statusH}}
		}
		p.status = n
	}
	return p.status
}

// dropAccess lets go of p's nodes, as its window closes.
func (p *pane) dropAccess() {
	p.text.drop()
	p.status.drop()
}

// tabNodes are the strip's tabs, each a tab item that shows its tab, with a
// close button that closes it (B555).
func (w *window) tabNodes() []*node {
	every := w.everyTab()
	return w.tabIDs.of(fmt.Sprint(every, w.tabs), func() []*node {
		var tabs []*node
		for _, t := range every {
			tab := newNode(w, w.strip, ctlTabItem, slotSelection, slotInvoke)
			name, current := w.tabName(t)
			tab.name = func() string { return name }
			tab.on = func() bool { return current }
			tab.frame = func() rect { return rect{point{t.from, 0}, size{t.to - t.from, stripH}} }
			tab.press = func() { w.typeKeys(t.show()) }
			shut := newNode(w, tab, ctlButton, slotInvoke)
			shut.name = func() string { return "close" }
			shut.frame = func() rect { return rect{point{shutFrom(t.to), 0}, size{slotW, stripH}} }
			shut.press = func() { w.typeKeys(t.shut()) }
			tab.kids = func() []*node { return []*node{shut} }
			tabs = append(tabs, tab)
		}
		return tabs
	})
}

// boxAccess are a box's nodes: its own, the palette's text field, its list
// and the list's rows.
type boxAccess struct {
	el, field, items *node
	rows             kidNodes
	said             spoken
	told             told
}

// boxNode is b's node: a document for a float, a group of a text field
// and a list for the palette and the finder, a list for a menu and text
// for the rest (B583).
func (b *box) boxNode() *node {
	if b.el == nil {
		b.said.count = utf16Units
		w := b.w
		n := newNode(w, w.root, ctlGroup, slotText)
		n.frame = func() rect { return b.frame }
		n.name = func() string {
			switch {
			case b.boxed == nil:
				return ""
			case b.read != nil:
				return b.read.label()
			case b.text != "":
				return b.text
			case b.list != nil:
				r, _ := b.reading()
				return r.label()
			}
			return cellsText(b.cells)
		}
		n.read = func() (reading, *spoken, bool) {
			if b.boxed == nil || b.read == nil {
				return reading{}, nil, false
			}
			return *b.read, b.said.of(*b.read), true
		}
		n.kids = b.boxKids
		b.el = n
		field := newNode(w, n, ctlEdit, slotText)
		field.name = n.name
		field.frame = func() rect { return rect{b.frame.Origin, size{b.frame.Size.W, inputH}} }
		field.read = func() (reading, *spoken, bool) {
			if b.boxed == nil || b.list == nil || !b.list.typing {
				return reading{}, nil, false
			}
			r, _ := b.reading()
			return r, b.said.of(r), true
		}
		b.field = field
		list := newNode(w, n, ctlList)
		list.frame = func() rect {
			if b.list == nil {
				return rect{}
			}
			return rect{point{b.frame.Origin.X, b.frame.Origin.Y + b.list.top()}, size{b.frame.Size.W, float64(b.list.shown()) * b.list.rowH}}
		}
		list.kids = b.rowNodes
		b.items = list
	}
	b.el.role = ctlText
	switch {
	case b.read != nil:
		b.el.role = ctlDocument
	case b.list != nil && b.list.typing:
		b.el.role = ctlGroup
	case b.list != nil:
		b.el.role = ctlList
	}
	return b.el
}

// boxKids are the palette's and the finder's text field and list, and a
// menu's rows.
func (b *box) boxKids() []*node {
	switch {
	case b.boxed == nil || b.list == nil:
		return nil
	case b.list.typing:
		return []*node{b.field, b.items}
	}
	return b.rowNodes()
}

// rowNodes are b's list's rows, each its item's text, which picks it when
// pressed (B556).
func (b *box) rowNodes() []*node {
	l := b.list
	if b.boxed == nil || l == nil {
		return nil
	}
	up := b.el
	if l.typing {
		up = b.items
	}
	return b.rows.of(fmt.Sprint(l.typing, l.items, l.selected, l.scroll), func() []*node {
		var rows []*node
		for i, it := range l.items {
			row := newNode(b.w, up, ctlListItem, slotSelection, slotInvoke)
			text := itemText(it)
			row.name = func() string { return text }
			row.on = func() bool { return b.list != nil && b.list.selected == i }
			row.frame = func() rect {
				return rect{point{b.frame.Origin.X + 8, b.frame.Origin.Y + l.top() + float64(i)*l.rowH - l.scroll}, size{b.frame.Size.W - 16, l.rowH}}
			}
			row.press = func() { b.w.mice <- mouse{kind: 'p', y: i} }
			rows = append(rows, row)
		}
		return rows
	})
}

// dropAccess lets go of b's nodes, as it or its window closes.
func (b *box) dropAccess() {
	b.rows.drop()
	b.field.drop()
	b.items.drop()
	b.el.drop()
}

// focus is the node with the focus: the palette's or the finder's text
// field while one shows, else a float with the cursor, else the current
// window's text.
func (w *window) focus() *node {
	if b := w.panel; b.boxed != nil {
		b.boxNode()
		if b.list != nil && b.list.typing {
			return b.field
		}
		return b.el
	}
	for _, b := range w.order {
		if b.boxed != nil && b.read != nil && b.cursor.shown {
			return b.boxNode()
		}
	}
	for _, p := range w.panes {
		if p.current {
			return p.textNode()
		}
	}
	return nil
}

// raise tells assistive apps of the event on n.
func raise(n *node, event int) { call(uiaRaise, n.ptr(slotSimple), uintptr(event)) }

// announce has assistive apps speak text, from Windows 10's 1709 on.
func announce(n *node, text string) {
	if uiaNotify.Find() != nil {
		return
	}
	s, id := bstr(text), bstr("fude")
	// NotificationKind_Other, NotificationProcessing_ImportantMostRecent.
	call(uiaNotify, n.ptr(slotSimple), 4, 1, s, id)
	call(sysFreeString, s)
	call(sysFreeString, id)
}

// tell tells assistive apps what changed in the frame w shows: the focus,
// a text, a cursor, a list's selected item and a new message (B583).
func (w *window) tell() {
	if call(uiaListening) == 0 {
		return
	}
	if f := w.focus(); f != nil && f != w.focused {
		w.focused = f
		raise(f, focusChanged)
	}
	text := func(n *node, r reading, t *told) {
		if k := r.key(); k != t.key {
			t.key = k
			raise(n, textChanged)
		}
		if r.chosen != t.chosen {
			t.chosen = r.chosen
			raise(n, selectionChanged)
		}
	}
	for _, p := range w.panes {
		text(p.textNode(), p.read, &p.told)
	}
	for _, b := range w.order {
		if b.boxed == nil {
			b.told = told{}
			continue
		}
		n := b.boxNode()
		if b.read != nil {
			text(n, *b.read, &b.told)
		} else if r, ok := b.reading(); ok {
			text(b.field, r, &b.told)
		}
		// A list's selected item, or a message, is spoken once it changes.
		spoken := b.text
		if l := b.list; l != nil && l.selected >= 0 && l.selected < len(l.items) {
			spoken = itemText(l.items[l.selected])
		}
		if spoken != b.told.spoke {
			b.told.spoke = spoken
			if spoken != "" {
				announce(n, spoken)
			}
		}
	}
}
