package gui

import (
	"maps"
	"runtime"
	"slices"
	"unicode/utf8"
	"unsafe"

	"github.com/ebitengine/purego"
)

// GTK's accessibility calls, which loadAccess finds (D67).
var (
	gTypeQuery         func(t uint64, q *typeQuery)
	gTypeRegister      func(parent uint64, name string, classSize uint32, classInit uintptr, instanceSize uint32, instanceInit uintptr, flags uint32) uint64
	gTypeAddInterface  func(t, iface uint64, info *interfaceInfo)
	gTypeClassRef      func(t uint64) uintptr
	gEnumByNick        func(class uintptr, nick string) *enumValue
	gObjectNew         func(t uint64, n uint32, names, values uintptr) uintptr
	gValueSetEnum      func(v *gValue, e int32)
	gValueSetString    func(v *gValue, s string)
	gBytesNew          func(data unsafe.Pointer, size uintptr) uintptr
	gMalloc            func(n uintptr) unsafe.Pointer
	gtkMinor           func() uint32
	gtkDrawingAreaType func() uint64
	gtkTextIfaceType   func() uint64
	gtkRoleType        func() uint64
	gtkPropertyType    func() uint64
	gtkUpdateProperty  func(a uintptr, n int32, props *int32, values *gValue)
	gtkUpdateCaret     func(a uintptr)
	gtkUpdateSelection func(a uintptr)
	gtkUpdateContents  func(a uintptr, change int32, start, end uint32)
	gtkAnnounce        func(a uintptr, message string, priority int32)
	gtkContextOf       func(a uintptr) uintptr
	gTypeNameOf        func(o uintptr) string
	gtkSetFocusable    func(w uintptr, on int32)
	gtkGrabFocus       func(w uintptr) int32
)

// typeQuery is a GTypeQuery, interfaceInfo a GInterfaceInfo and enumValue
// a GEnumValue.
type (
	typeQuery struct {
		typ                     uint64
		name                    uintptr
		classSize, instanceSize uint32
	}
	interfaceInfo struct{ init, finalize, data uintptr }
	enumValue     struct {
		value      int32
		name, nick uintptr
	}
)

// textType is FudeText, a drawing area whose text GtkAccessibleText gives
// assistive apps, 0 before GTK 4.14, which has none.
var textType uint64

// roleClass and propertyClass are the enums of accessible roles and
// properties, whose values loadAccess looks up by name, as GTK has added
// values between them.
var roleClass, propertyClass uintptr

// The calls of GTK's accessibility and of FudeText's GtkAccessibleText.
var accessCallbacks struct{ init, contents, contentsAt, caret, selection, extents, offset uintptr }

// loadAccess finds GTK's accessibility calls in gtk and makes FudeText,
// from GTK 4.14 on.
func loadAccess(gtk uintptr) {
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&gtkMinor, "gtk_get_minor_version"},
		{&gtkSetFocusable, "gtk_widget_set_focusable"},
		{&gtkGrabFocus, "gtk_widget_grab_focus"},
		{&gTypeClassRef, "g_type_class_ref"},
		{&gEnumByNick, "g_enum_get_value_by_nick"},
		{&gValueSetEnum, "g_value_set_enum"},
		{&gValueSetString, "g_value_set_string"},
		{&gtkRoleType, "gtk_accessible_role_get_type"},
		{&gtkPropertyType, "gtk_accessible_property_get_type"},
		{&gtkUpdateProperty, "gtk_accessible_update_property_value"},
	} {
		purego.RegisterLibFunc(f.fn, gtk, f.name)
	}
	roleClass, propertyClass = gTypeClassRef(gtkRoleType()), gTypeClassRef(gtkPropertyType())
	if gtkMinor() < 14 {
		return
	}
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&gTypeQuery, "g_type_query"},
		{&gTypeRegister, "g_type_register_static_simple"},
		{&gTypeAddInterface, "g_type_add_interface_static"},
		{&gObjectNew, "g_object_new_with_properties"},
		{&gBytesNew, "g_bytes_new"},
		{&gMalloc, "g_malloc"},
		{&gtkDrawingAreaType, "gtk_drawing_area_get_type"},
		{&gtkTextIfaceType, "gtk_accessible_text_get_type"},
		{&gtkUpdateCaret, "gtk_accessible_text_update_caret_position"},
		{&gtkUpdateSelection, "gtk_accessible_text_update_selection_bound"},
		{&gtkUpdateContents, "gtk_accessible_text_update_contents"},
		{&gtkAnnounce, "gtk_accessible_announce"},
		{&gtkContextOf, "gtk_accessible_get_at_context"},
		{&gTypeNameOf, "g_type_name_from_instance"},
	} {
		purego.RegisterLibFunc(f.fn, gtk, f.name)
	}
	c := &accessCallbacks
	c.init = purego.NewCallback(initText)
	c.contents = purego.NewCallback(textContents)
	c.contentsAt = purego.NewCallback(textContentsAt)
	c.caret = purego.NewCallback(textCaret)
	c.selection = purego.NewCallback(textSelection)
	c.extents = purego.NewCallback(textExtents)
	c.offset = purego.NewCallback(func(uintptr, uintptr, *uint32) bool { return false })
	var q typeQuery
	parent := gtkDrawingAreaType()
	gTypeQuery(parent, &q)
	textType = gTypeRegister(parent, "FudeText", q.classSize, 0, q.instanceSize, 0, 0)
	gTypeAddInterface(textType, gtkTextIfaceType(), &interfaceInfo{init: c.init})
}

// initText fills FudeText's GtkAccessibleTextInterface after its
// GTypeInterface, whose last two calls GTK 4.16 added.
func initText(iface, _ uintptr) {
	calls := []uintptr{accessCallbacks.contents, accessCallbacks.contentsAt, accessCallbacks.caret, accessCallbacks.selection}
	slots := map[int]uintptr{16: calls[0], 24: calls[1], 32: calls[2], 40: calls[3]}
	if gtkMinor() >= 16 {
		slots[64], slots[72] = accessCallbacks.extents, accessCallbacks.offset
	}
	for at, fn := range slots {
		*(*uintptr)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&iface)), at)) = fn
	}
}

// newArea returns a drawing area of role. A text box's and a label's text
// is for assistive apps to read, from GTK 4.14 on.
func newArea(role string) uintptr {
	area := gtkDrawingAreaNew()
	if textType != 0 && (role == "text-box" || role == "label") {
		area = gObjectNew(textType, 0, 0, 0)
	}
	if v := gEnumByNick(roleClass, role); v != nil {
		var role gValue
		gValueInit(&role, gtkRoleType())
		gValueSetEnum(&role, v.value)
		gObjectSetProperty(area, "accessible-role", &role)
		gValueUnset(&role)
	}
	return area
}

// labels are an area's accessible label and description, as last given.
type labels [2]string

// give gives an area label and description, when they changed.
func (l *labels) give(area uintptr, label, description string) {
	if *l == [2]string{label, description} {
		return
	}
	*l = [2]string{label, description}
	describe(area, "label", label)
	describe(area, "description", description)
}

// describe sets an area's accessible property named name, such as label or
// description, to text.
func describe(area uintptr, name, text string) {
	p := gEnumByNick(propertyClass, name)
	if p == nil {
		return
	}
	var v gValue
	gValueInit(&v, gTypeString)
	gValueSetString(&v, text)
	gtkUpdateProperty(area, 1, &p.value, &v)
	gValueUnset(&v)
}

// readerOf is the reading of an area that shows text, in characters. A
// text's stays as tell last made it, so a removal reads what was removed.
func readerOf(area uintptr) (reading, *spoken, bool) {
	switch p, b := panes[area], boxes[area]; {
	case p != nil:
		if !p.said.made {
			p.said.of(p.read)
		}
		return p.read, &p.said, true
	case b != nil && b.boxed != nil:
		r, ok := b.reading()
		if !ok {
			// A toast's or a menu's text is made again on each read.
			r = typed(b.title, boxText(b.boxed), 0)
			return r, b.said.of(r), true
		}
		if !b.said.made {
			b.said.of(r)
		}
		return r, &b.said, true
	}
	return reading{}, nil, false
}

// boxText is what a box that holds no text says: a toast's message, a
// menu's selected item or its cells' text.
func boxText(b *boxed) string {
	switch l := b.list; {
	case b.text != "":
		return b.text
	case l != nil && l.selected >= 0 && l.selected < len(l.items):
		return itemText(l.items[l.selected])
	}
	return cellsText(b.cells)
}

// bytesOf returns a GBytes holding a copy of text.
func bytesOf(text string) uintptr {
	defer runtime.KeepAlive(text)
	return gBytesNew(unsafe.Pointer(unsafe.StringData(text)), uintptr(len(text)))
}

func textContents(area uintptr, start, end uint32) uintptr {
	_, s, ok := readerOf(area)
	if !ok {
		return bytesOf("")
	}
	return bytesOf(s.slice(int(start), min(int(end), s.total)))
}

// textContentsAt returns the character, word, line or paragraph at
// offset, a sentence being a line, and puts where it starts and ends in
// start and end.
func textContentsAt(area uintptr, offset uint32, granularity int32, start, end *uint32) uintptr {
	_, s, ok := readerOf(area)
	if !ok {
		*start, *end = 0, 0
		return bytesOf("")
	}
	from, to := int(offset), int(offset)
	switch granularity {
	case 0:
		to = min(from+1, s.total)
	case 1:
		from, to = s.wordAt(from)
	default:
		from, to = s.lineUnits(s.line(from))
	}
	*start, *end = uint32(from), uint32(to)
	return bytesOf(s.slice(from, to))
}

func textCaret(area uintptr) uint32 {
	if r, s, ok := readerOf(area); ok {
		return uint32(s.unit(r.cursor))
	}
	return 0
}

// textSelection gives the visual selection as one range, in a block GTK
// frees.
func textSelection(area uintptr, n *uintptr, ranges *unsafe.Pointer) bool {
	r, s, ok := readerOf(area)
	if !ok || r.chosen[0] == r.chosen[1] {
		return false
	}
	from, to := s.unit(r.chosen[0]), s.unit(r.chosen[1])
	*n = 1
	if ranges != nil {
		block := gMalloc(2 * unsafe.Sizeof(uintptr(0)))
		*(*[2]uintptr)(block) = [2]uintptr{uintptr(from), uintptr(to - from)}
		*ranges = block
	}
	return true
}

// textExtents puts where text from start is in the area in extents: the
// cursor's cell at the cursor, else the whole area, as cells do not say
// where other text is.
func textExtents(area uintptr, start, _ uint32, extents *grect) bool {
	*extents = grect{W: float32(gtkWidgetGetWidth(area)), H: float32(gtkWidgetGetHeight(area))}
	if p := panes[area]; p != nil && p.cursor.shown && start == textCaret(area) {
		r := at(p.cursor.x, p.cursor.y, 1, p.origin())
		*extents = grect{float32(r.Origin.X), float32(r.Origin.Y), float32(r.Size.W), float32(r.Size.H)}
	}
	return true
}

// tell tells assistive apps what changed in the frame w shows: the focus,
// a text, a cursor, a list's selected item and a new message (B582).
func (w *window) tell() {
	if f := w.focus(); f != 0 && f != w.focused {
		w.focused = f
		gtkSetFocusable(f, 1)
		gtkGrabFocus(f)
	}
	for _, p := range w.panes {
		p.labels.give(p.area, p.read.label(), statusText(p.left, p.right))
		w.tellText(p.area, p.read, &p.said, &p.told)
	}
	w.barLabels.give(w.bar, w.tabsText(), "")
	for _, b := range append([]*box{w.panel, w.toast}, slices.Collect(maps.Values(w.floats))...) {
		if b.boxed == nil {
			b.told = told{}
			continue
		}
		if r, ok := b.reading(); ok {
			b.labels.give(b.area, r.label(), "")
			w.tellText(b.area, r, &b.said, &b.told)
			if b.list == nil {
				continue
			}
		}
		if said := boxText(b.boxed); said != b.told.spoke {
			b.told.spoke = said
			if said != "" && textType != 0 && speaks(b.area) {
				gtkAnnounce(b.area, said, 1)
			}
		}
	}
}

// speaks reports whether area's assistive context is AT-SPI's, the one
// that can announce. Without an accessibility bus GTK gives a test
// context, which crashes on an announcement.
func speaks(area uintptr) bool {
	c := gtkContextOf(area)
	if c == 0 {
		return false
	}
	defer gObjectUnref(c)
	return gTypeNameOf(c) == "GtkAtSpiContext"
}

// tellText tells of a change to an area's text and its cursor. Once an
// assistive app has read the text, tellText tells it of a change as the
// removal and the insertion that make it, so the app speaks only what
// changed.
func (w *window) tellText(area uintptr, r reading, s *spoken, t *told) {
	if k := r.key(); k != t.key {
		t.key = k
		if s.made && textType != 0 {
			text := r.whole()
			from, gone, added := changed(s.text, text)
			// GTK reads what was removed from s, which still holds it.
			if gone > 0 {
				gtkUpdateContents(area, 1, uint32(from), uint32(from+gone))
			}
			s.hold(k, text)
			if added > 0 {
				gtkUpdateContents(area, 0, uint32(from), uint32(from+added))
			}
		}
	}
	if r.chosen != t.chosen && textType != 0 {
		t.chosen = r.chosen
		gtkUpdateCaret(area)
		gtkUpdateSelection(area)
	}
}

// changed returns where in characters old and text first differ, and how
// many characters of old went and of text came in place of them.
func changed(old, text string) (from, gone, added int) {
	same := 0
	for same < len(old) && same < len(text) && old[same] == text[same] {
		same++
	}
	for same > 0 && same < len(old) && !utf8.RuneStart(old[same]) {
		same--
	}
	tail := 0
	for tail < len(old)-same && tail < len(text)-same && old[len(old)-1-tail] == text[len(text)-1-tail] {
		tail++
	}
	for tail > 0 && !utf8.RuneStart(old[len(old)-tail]) {
		tail--
	}
	return utf8.RuneCountInString(old[:same]), utf8.RuneCountInString(old[same : len(old)-tail]), utf8.RuneCountInString(text[same : len(text)-tail])
}

// focus is the area with the focus: the palette or the finder while one
// shows, else a float with the cursor, else the current window's text.
func (w *window) focus() uintptr {
	if w.panel.boxed != nil {
		return w.panel.area
	}
	for _, b := range w.floats {
		if b.boxed != nil && b.read != nil && b.cursor.shown {
			return b.area
		}
	}
	for _, p := range w.panes {
		if p.current {
			return p.area
		}
	}
	return 0
}

// tabsText is what the tab strip says: its tabs, the current one marked.
func (w *window) tabsText() string {
	text := ""
	for _, h := range w.everyTab() {
		name, current := w.tabName(h)
		if current {
			name += " (current)"
		}
		if text != "" {
			text += ", "
		}
		text += name
	}
	return text
}
