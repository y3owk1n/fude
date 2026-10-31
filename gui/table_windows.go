package gui

import (
	"cmp"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/y3owk1n/fude/frontend/wire"
)

// A box shows its menu's items in the system's list view over the box.
// fude feeds the list view and draws its rows. The list view never takes
// the keyboard's focus, so keys still go to the session (B560, D68).
var (
	comctl32      = windows.NewLazySystemDLL("comctl32.dll")
	uxtheme       = windows.NewLazySystemDLL("uxtheme.dll")
	initControls  = comctl32.NewProc("InitCommonControlsEx")
	setSubclass   = comctl32.NewProc("SetWindowSubclass")
	defSubclass   = comctl32.NewProc("DefSubclassProc")
	imageListNew  = comctl32.NewProc("ImageList_Create")
	imageListDrop = comctl32.NewProc("ImageList_Destroy")
	imageListDraw = comctl32.NewProc("ImageList_Draw")
	imageListSize = comctl32.NewProc("ImageList_GetIconSize")
	setTheme      = uxtheme.NewProc("SetWindowTheme")
	fileInfo      = shell32.NewProc("SHGetFileInfoW")
	giveFocus     = user32.NewProc("SetFocus")
	sendMessage   = user32.NewProc("SendMessageW")
	setScrollInfo = user32.NewProc("SetScrollInfo")
	getScrollInfo = user32.NewProc("GetScrollInfo")
	beginPaint    = user32.NewProc("BeginPaint")
	endPaint      = user32.NewProc("EndPaint")
	tables        = map[uintptr]*box{} // by list view
	previews      = map[uintptr]*box{} // by preview window
	previewClass  *uint16
	tableCallback = syscall.NewCallback(tableMessage)
	shellIcons    uintptr            // the system's small icons
	iconOf        = map[string]int{} // their indexes, by a file's extension or "/" for a folder
)

// The list view's messages, notifications and styles fude uses.
const (
	wmNotify          = 0x004E
	wmSetFocus        = 0x0007
	wmLButtonDblClk   = 0x0203
	wmRButtonUp       = 0x0205
	wmRButtonDblClk   = 0x0206
	wmVScroll         = 0x0115
	lvmSetBkColor     = 0x1001
	lvmSetImageList   = 0x1003
	lvmEnsureVisible  = 0x1013
	lvmHitTest        = 0x1012
	lvmSetColumnWidth = 0x101E
	lvmSetTextBkColor = 0x1026
	lvmSetItemState   = 0x102B
	lvmSetItemCount   = 0x102F
	lvmSetExStyle     = 0x1036
	lvmInsertColumn   = 0x1061
	lvnGetDispInfo    = -177
	nmCustomDraw      = -12
)

// nmHeader is an NMHDR, the start of every notification.
type nmHeader struct {
	from, id uintptr
	code     int32
	_        int32
}

// lvItem is an LVITEMW, as a notification asks for an item's text in it.
type lvItem struct {
	mask, item, sub, state, stateMask uint32
	text                              *uint16
	cch, image                        int32
	lparam                            uintptr
	indent, group, cols               int32
	colsp, fmts                       uintptr
	groupID                           int32
}

// customDraw is an NMLVCUSTOMDRAW, up to the subitem being drawn.
type customDraw struct {
	hdr    nmHeader
	stage  uint32
	_      uint32
	hdc    uintptr
	rc     rectI
	spec   uintptr
	state  uint32
	_      uint32
	lparam uintptr
	text   uint32
	textBk uint32
	sub    int32
}

// startTables loads the list view's class and the system's small icons,
// and registers the class of the previews' windows.
func startTables() {
	icc := [2]uint32{8, 1}
	call(initControls, uintptr(unsafe.Pointer(&icc)))
	previewClass = cwide("fude-preview")
	wc := wndClass{proc: syscall.NewCallback(previewMessage), instance: call(getModuleHandle, 0), name: previewClass, cursor: call(loadCursor, 0, idcArrow)}
	wc.size = uint32(unsafe.Sizeof(wc))
	call(registerClass, uintptr(unsafe.Pointer(&wc)))
	var info shellFileInfo
	// SHGFI_SYSICONINDEX, SHGFI_SMALLICON and SHGFI_USEFILEATTRIBUTES.
	shellIcons = call(fileInfo, uintptr(unsafe.Pointer(cwide(".txt"))), 0x80, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0x4011)
}

// shellFileInfo is an SHFILEINFOW.
type shellFileInfo struct {
	icon       uintptr
	index      int32
	attributes uint32
	name       [260]uint16
	typeName   [80]uint16
}

// newTable makes b's hidden list view: one column with no header, rows
// fude draws, and the system's look for its scroll bar, hover and
// selection.
func (b *box) newTable() {
	// WS_CHILD, WS_CLIPSIBLINGS, LVS_REPORT, LVS_SINGLESEL,
	// LVS_SHOWSELALWAYS, LVS_OWNERDATA and LVS_NOCOLUMNHEADER.
	b.listView = call(createWindow, 0, uintptr(unsafe.Pointer(cwide("SysListView32"))), 0, 0x44000000|1|4|8|0x1000|0x4000, 0, 0, 0, 0, b.w.hwnd, 0, call(getModuleHandle, 0), 0)
	// LVS_EX_FULLROWSELECT and LVS_EX_DOUBLEBUFFER.
	call(sendMessage, b.listView, lvmSetExStyle, 0x10020, 0x10020)
	col := struct {
		mask, format uint32
		cx           int32
	}{mask: 2}
	call(sendMessage, b.listView, lvmInsertColumn, 0, uintptr(unsafe.Pointer(&col)))
	call(setSubclass, b.listView, tableCallback, 0, 0)
	tables[b.listView] = b
}

// showTable shows b's menu in its list view, under the row typed into,
// and keeps its selected item in view when it moves. Without a menu it
// hides the list view.
func (b *box) showTable() {
	if b.boxed == nil || b.list == nil || len(b.list.items) == 0 {
		call(showWindow, b.listView, 0)
		b.listed, b.selectedRow = "", -1
		return
	}
	l, s, lv := b.list, b.w.scale, b.listView
	width := b.frame.Size.W - 2
	if len(b.cells) > 0 {
		width = 8 + l.width + 4
	}
	px := func(v float64) uintptr { return uintptr(math.Round(v * s)) }
	call(setWindowPos, lv, 0, px(b.frame.Origin.X+1), px(b.frame.Origin.Y+l.top()), px(width), px(float64(l.shown())*l.rowH), 0x50)
	// A row is a pixel taller than the small image list's images.
	if h := int32(px(l.rowH)); h != b.rowPx {
		b.rowPx = h
		// ILC_COLOR32.
		old := call(sendMessage, lv, lvmSetImageList, 1, call(imageListNew, 1, uintptr(h-1), 0x20, 1, 0))
		call(imageListDrop, old)
	}
	if theme := systemTheme(); theme != b.themed {
		b.themed = theme
		call(setTheme, lv, uintptr(unsafe.Pointer(cwide(theme))), 0)
		ground := colorRef(system("popoverColor"))
		call(sendMessage, lv, lvmSetBkColor, 0, ground)
		call(sendMessage, lv, lvmSetTextBkColor, 0, ground)
	}
	// The column is as wide as the list, less the scroll bar while one
	// shows, so the list never scrolls across.
	cw := px(width)
	if len(l.items) > l.shown() {
		cw -= call(systemMetrics, 2, uintptr(96*s))
	}
	call(sendMessage, lv, lvmSetColumnWidth, 0, cw)
	changed := false
	if listed := fmt.Sprint(l.items); listed != b.listed {
		b.listed, changed = listed, true
		call(sendMessage, lv, lvmSetItemCount, uintptr(len(l.items)), 0)
		call(invalidateRect, lv, 0, 1)
	}
	if l.selected == b.selectedRow && !changed {
		return
	}
	b.selectedRow = l.selected
	// LVIS_SELECTED and LVIS_FOCUSED, off every row and then on the
	// selected one.
	it := lvItem{stateMask: 3}
	call(sendMessage, lv, lvmSetItemState, ^uintptr(0), uintptr(unsafe.Pointer(&it)))
	if l.selected >= 0 && l.selected < len(l.items) {
		it.state = 3
		call(sendMessage, lv, lvmSetItemState, uintptr(l.selected), uintptr(unsafe.Pointer(&it)))
		call(sendMessage, lv, lvmEnsureVisible, uintptr(l.selected), 0)
	}
}

// dropTable destroys b's list view and preview, as b goes.
func (b *box) dropTable() {
	delete(tables, b.listView)
	delete(previews, b.preview)
	for _, hwnd := range []uintptr{b.listView, b.preview} {
		if hwnd != 0 {
			call(destroyWindow, hwnd)
		}
	}
	b.listView, b.preview = 0, 0
}

// tableMessage takes a list view's clicks itself: the left button picks
// the item it hits, as a click on a menu's item does, and no click gives
// the list view the keyboard's focus (B556).
func tableMessage(hwnd, m, wp, lp, _, _ uintptr) uintptr {
	defer hold()()
	b := tables[hwnd]
	switch {
	case b == nil:
	case m == wmLButtonDown || m == wmLButtonDblClk:
		hit := struct {
			pt               [2]int32
			flags            uint32
			item, sub, group int32
		}{pt: [2]int32{int32(int16(lp)), int32(int16(lp >> 16))}}
		call(sendMessage, hwnd, lvmHitTest, 0, uintptr(unsafe.Pointer(&hit)))
		if l := b.list; b.boxed != nil && l != nil && hit.item >= 0 && int(hit.item) < len(l.items) && !l.items[hit.item].Header && !l.items[hit.item].Disabled {
			b.w.mice <- mouse{kind: 'p', y: int(hit.item)}
		}
		return 0
	case m == wmRButtonDown || m == wmRButtonUp || m == wmRButtonDblClk || m == wmMButtonDown:
		return 0
	case m == wmSetFocus:
		call(giveFocus, b.w.hwnd)
		return 0
	}
	return call(defSubclass, hwnd, m, wp, lp)
}

// listNotify answers a list view's notification: an item's text, which
// assistive apps read, and the drawing of its rows.
func (w *window) listNotify(lp uintptr) (uintptr, bool) {
	at := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
	h := (*nmHeader)(at)
	b := tables[h.from]
	if b == nil || b.boxed == nil || b.list == nil {
		return 0, false
	}
	items := b.list.items
	switch h.code {
	case lvnGetDispInfo:
		it := &(*struct {
			hdr  nmHeader
			item lvItem
		})(at).item
		// LVIF_TEXT, into a buffer cch long with room for the ending 0.
		if it.mask&1 != 0 && int(it.item) < len(items) && it.cch > 0 {
			buf := unsafe.Slice(it.text, it.cch)
			buf[copy(buf[:it.cch-1], wide(items[it.item].Label))] = 0
		}
		return 0, true
	case nmCustomDraw:
		cd := (*customDraw)(at)
		switch cd.stage {
		case 1, 0x10001:
			// CDDS_PREPAINT and CDDS_ITEMPREPAINT return
			// CDRF_NOTIFYITEMDRAW and CDRF_NOTIFYSUBITEMDRAW, which ask for
			// each row and then each of its columns.
			return 0x20, true
		case 0x30001:
			// The system drew the row's hover and selection. fude draws
			// its text, and CDRF_SKIPDEFAULT tells the system to draw
			// nothing more.
			if i := int(cd.spec); i < len(items) {
				b.drawRow(cd.hdc, cd.rc, items[i])
			}
			return 4, true
		}
		return 0, true
	}
	return 0, false
}

// drawRow draws an item's row in rc of a list view's hdc: its icon, a
// grep match's line, its label with the typed text's matches bold, and its
// detail dim at the right.
func (b *box) drawRow(hdc uintptr, rc rectI, it wire.Item) {
	w := b.w
	if rc.R <= rc.L || rc.B <= rc.T || w.rowTarget == nil && !w.makeRowTarget() {
		return
	}
	at := 8.0
	if icon, ok := iconIndex(it); ok {
		var iw, ih int32
		call(imageListSize, shellIcons, uintptr(unsafe.Pointer(&iw)), uintptr(unsafe.Pointer(&ih)))
		// ILD_TRANSPARENT.
		call(imageListDraw, shellIcons, uintptr(icon), hdc, uintptr(rc.L+int32(at*w.scale)), uintptr(rc.T+(rc.B-rc.T-ih)/2), 1)
		at += math.Ceil(float64(iw)/w.scale) + 6
	}
	com(w.rowTarget, 57, hdc, uintptr(unsafe.Pointer(&rc)))
	rt, brush = w.rowTarget, w.rowBrush
	defer func() { rt, brush = nil, nil }()
	com(rt, 48)
	r := size{float64(rc.R-rc.L) / w.scale, float64(rc.B-rc.T) / w.scale}
	label, detail := "labelColor", "secondaryLabelColor"
	if it.Disabled {
		label = "tertiaryLabelColor"
	}
	f := uiFont
	if it.Header {
		f, label = uiBold, "secondaryLabelColor"
	}
	mid := func(sz size) float64 { return math.Floor((r.H - sz.H) / 2) }
	if it.Line > 0 {
		n := strconv.Itoa(it.Line)
		setColour(detail)
		text(n, uiFont, point{at, mid(measure(n, uiFont))})
		at += 36
	}
	end := r.W - 10
	if it.Detail != "" {
		sz := measure(it.Detail, uiFont)
		dx := max(end-sz.W, at+80)
		setColour(detail)
		fitted(it.Detail, nil, uiFont, point{dx, mid(sz)}, end-dx)
		end = dx - 12
	}
	setColour(label)
	fitted(it.Label, it.Marks, f, point{at, mid(measure(it.Label, f))}, max(end-at, 0))
	com(rt, 49, 0, 0)
}

// makeRowTarget makes the render target for w's list view rows, at w's
// DPI. Each row binds it to the list view's device context.
func (w *window) makeRowTarget() bool {
	props := rtProps{format: 87, alpha: 1, dpiX: float32(96 * w.scale), dpiY: float32(96 * w.scale)}
	if com(d2d, 16, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&w.rowTarget))) != 0 {
		w.rowTarget = nil
		return false
	}
	var c colorF
	com(w.rowTarget, 8, uintptr(unsafe.Pointer(&c)), 0, uintptr(unsafe.Pointer(&w.rowBrush)))
	// The rows draw on the list view's own ground, so their text is
	// smoothed in grey, D2D1_TEXT_ANTIALIAS_MODE_GRAYSCALE.
	com(w.rowTarget, 34, 2)
	return true
}

// iconIndex is the system's small icon for an item's kind, a file's by its
// extension or a folder's, and whether it has one.
func iconIndex(it wire.Item) (int, bool) {
	key, name, attr := "", "", uintptr(0x80)
	switch it.Kind {
	case "file", "buffer":
		key = cmp.Or(filepath.Ext(it.Path), filepath.Ext(it.Label), ".")
		name = "a" + key
	case "folder", "path":
		key, name, attr = "/", "a", 0x10
	default:
		return 0, false
	}
	if i, ok := iconOf[key]; ok {
		return i, true
	}
	var info shellFileInfo
	call(fileInfo, uintptr(unsafe.Pointer(cwide(name))), attr, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0x4011)
	iconOf[key] = int(info.index)
	return int(info.index), true
}

// systemTheme is the name of the system's look for a control, dark on a
// dark theme.
func systemTheme() string {
	if darkLook {
		return "DarkMode_Explorer"
	}
	return "Explorer"
}

// scrollInfo is a SCROLLINFO.
type scrollInfo struct {
	size, mask uint32
	min, max   int32
	page       uint32
	pos, track int32
}

// paintInfo is a PAINTSTRUCT.
type paintInfo struct {
	hdc      uintptr
	erase    int32
	rc       rectI
	restore  int32
	inc      int32
	reserved [36]byte
}

// showPreview shows the finder's whole preview, or a menu's whole
// documentation, in a window of its own over b's cells, which scrolls by
// the system's scroll bar and the wheel. It goes back to the top whenever
// another file shows, and hides when there is no whole preview (B130,
// B560).
func (b *box) showPreview() {
	if b.boxed == nil || b.whole == nil || len(b.cells) == 0 {
		if b.preview != 0 {
			call(showWindow, b.preview, 0)
		}
		b.previewed = ""
		return
	}
	b.scrolls = true
	if b.preview == 0 {
		// WS_CHILD, WS_CLIPSIBLINGS and WS_VSCROLL.
		b.preview = call(createWindow, 0, uintptr(unsafe.Pointer(previewClass)), 0, 0x44200000, 0, 0, 0, 0, b.w.hwnd, 0, call(getModuleHandle, 0), 0)
		previews[b.preview] = b
	}
	if theme := systemTheme(); theme != b.previewThemed {
		b.previewThemed = theme
		call(setTheme, b.preview, uintptr(unsafe.Pointer(cwide(theme))), 0)
	}
	s := b.w.scale
	px := func(v float64) int32 { return int32(math.Round(v * s)) }
	o := b.cellsOrigin()
	view := px(float64(len(b.cells)) * cellH)
	bar := int32(call(systemMetrics, 2, uintptr(96*s)))
	call(setWindowPos, b.preview, 0, uintptr(px(b.frame.Origin.X+o.X)), uintptr(px(b.frame.Origin.Y+o.Y)), uintptr(px(float64(len(b.cells[0]))*cellW)+bar), uintptr(view), 0x50)
	si := scrollInfo{mask: 3, max: px(float64(len(b.whole.cells))*cellH) - 1, page: uint32(view)}
	if seen := cellsText(b.whole.cells); seen != b.previewed {
		b.previewed = seen
		si.mask |= 4
	}
	si.size = uint32(unsafe.Sizeof(si))
	call(setScrollInfo, b.preview, 1, uintptr(unsafe.Pointer(&si)), 1)
	call(invalidateRect, b.preview, 0, 0)
}

// previewMessage handles a preview window's messages. It draws the
// preview from its scroll position, scrolls by the bar and the wheel, and
// never takes the keyboard's focus.
func previewMessage(hwnd, m, wp, lp uintptr) uintptr {
	defer hold()()
	b := previews[hwnd]
	switch {
	case b == nil || b.boxed == nil || b.whole == nil:
	case m == wmPaint:
		var ps paintInfo
		hdc := call(beginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		b.drawPreview(hdc)
		call(endPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case m == wmEraseBkgnd:
		return 1
	case m == wmVScroll:
		si := scrollInfo{mask: 0x17}
		si.size = uint32(unsafe.Sizeof(si))
		call(getScrollInfo, hwnd, 1, uintptr(unsafe.Pointer(&si)))
		line, pos := int32(cellH*b.w.scale), si.pos
		// SB_LINEUP, SB_LINEDOWN, SB_PAGEUP, SB_PAGEDOWN, SB_THUMBTRACK,
		// SB_TOP and SB_BOTTOM.
		switch wp & 0xffff {
		case 0:
			pos -= line
		case 1:
			pos += line
		case 2:
			pos -= int32(si.page)
		case 3:
			pos += int32(si.page)
		case 5:
			pos = si.track
		case 6:
			pos = 0
		case 7:
			pos = si.max
		}
		b.scrollPreview(pos)
		return 0
	case m == wmMouseWheel:
		lines := uint32(3)
		call(systemParameters, 0x68, 0, uintptr(unsafe.Pointer(&lines)), 0)
		si := scrollInfo{mask: 4}
		si.size = uint32(unsafe.Sizeof(si))
		call(getScrollInfo, hwnd, 1, uintptr(unsafe.Pointer(&si)))
		notches := float64(int16(wp>>16)) / 120
		b.scrollPreview(si.pos - int32(notches*float64(lines)*cellH*b.w.scale))
		return 0
	case m == wmSetFocus:
		call(giveFocus, b.w.hwnd)
		return 0
	case m == wmLButtonDown || m == wmLButtonDblClk || m == wmRButtonDown || m == wmMButtonDown:
		return 0
	}
	return call(defWindowProc, hwnd, m, wp, lp)
}

// scrollPreview scrolls b's preview to pos pixels from its top, kept
// within its text.
func (b *box) scrollPreview(pos int32) {
	si := scrollInfo{mask: 4, pos: pos}
	si.size = uint32(unsafe.Sizeof(si))
	call(setScrollInfo, b.preview, 1, uintptr(unsafe.Pointer(&si)), 1)
	call(invalidateRect, b.preview, 0, 0)
}

// drawPreview draws b's whole preview on hdc of its window, on the
// popover's ground, from its scroll position.
func (b *box) drawPreview(hdc uintptr) {
	w := b.w
	var rc rectI
	call(getClientRect, b.preview, uintptr(unsafe.Pointer(&rc)))
	if rc.R <= rc.L || rc.B <= rc.T || w.rowTarget == nil && !w.makeRowTarget() {
		return
	}
	si := scrollInfo{mask: 4}
	si.size = uint32(unsafe.Sizeof(si))
	call(getScrollInfo, b.preview, 1, uintptr(unsafe.Pointer(&si)))
	com(w.rowTarget, 57, hdc, uintptr(unsafe.Pointer(&rc)))
	rt, brush = w.rowTarget, w.rowBrush
	defer func() { rt, brush = nil, nil }()
	com(rt, 48)
	ground := system("popoverColor")
	com(rt, 47, uintptr(unsafe.Pointer(&ground)))
	drawPreview(b.whole.cells, cursorAt{}, b.whole.chosen, b.whole.rules, point{0, -float64(si.pos) / w.scale}, b.look.Base.Bg)
	com(rt, 49, 0, 0)
}

// colorRef is c as a COLORREF, 0x00BBGGRR.
func colorRef(c colorF) uintptr {
	ch := func(v float32) uintptr { return uintptr(math.Round(float64(v) * 255)) }
	return ch(c.R) | ch(c.G)<<8 | ch(c.B)<<16
}
