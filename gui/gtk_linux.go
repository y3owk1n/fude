package gui

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// GTK runs on the main thread, which the main goroutine keeps.
func init() { runtime.LockOSThread() }

// GTK4's, cairo's and Pango's calls, which load opens (D66). A pointer is a
// uintptr, as purego passes it.
var (
	gIdleAdd             func(fn, data uintptr) uint32
	gSignalConnectData   func(inst uintptr, signal string, fn, data, destroy uintptr, flags uint32) uint64
	gObjectRefSink       func(o uintptr) uintptr
	gObjectUnref         func(o uintptr)
	gObjectSetProperty   func(o uintptr, name string, v *gValue)
	gObjectGetProperty   func(o uintptr, name string, v *gValue)
	gValueInit           func(v *gValue, t uint64) *gValue
	gValueSetBoolean     func(v *gValue, b int32)
	gValueGetString      func(v *gValue) string
	gValueUnset          func(v *gValue)
	gApplicationRun      func(app uintptr, argc int32, argv uintptr) int32
	gApplicationHold     func(app uintptr)
	gApplicationQuit     func(app uintptr)
	gtkApplicationNew    func(id string, flags uint32) uintptr
	gtkAppWindowNew      func(app uintptr) uintptr
	gtkWindowSetTitle    func(w uintptr, title string)
	gtkWindowSetSize     func(w uintptr, width, height int32)
	gtkWindowSetChild    func(w, child uintptr)
	gtkWindowPresent     func(w uintptr)
	gtkWindowDestroy     func(w uintptr)
	gtkWindowIsActive    func(w uintptr) int32
	gtkOverlayNew        func() uintptr
	gtkOverlaySetChild   func(o, child uintptr)
	gtkOverlayAdd        func(o, child uintptr)
	gtkOverlayRemove     func(o, child uintptr)
	gtkPanedNew          func(orientation int32) uintptr
	gtkPanedSetStart     func(p, child uintptr)
	gtkPanedSetEnd       func(p, child uintptr)
	gtkPanedSetPosition  func(p uintptr, position int32)
	gtkDrawingAreaNew    func() uintptr
	gtkDrawingAreaSetFn  func(a, fn, data, destroy uintptr)
	gtkWidgetQueueDraw   func(w uintptr)
	gtkWidgetGetWidth    func(w uintptr) int32
	gtkWidgetGetHeight   func(w uintptr) int32
	gtkWidgetSetVisible  func(w uintptr, on int32)
	gtkWidgetSetHalign   func(w uintptr, align int32)
	gtkWidgetSetValign   func(w uintptr, align int32)
	gtkWidgetSetHexpand  func(w uintptr, on int32)
	gtkWidgetSetVexpand  func(w uintptr, on int32)
	gtkWidgetMarginStart func(w uintptr, margin int32)
	gtkWidgetMarginTop   func(w uintptr, margin int32)
	gtkWidgetSizeRequest func(w uintptr, width, height int32)
	gtkWidgetSetTarget   func(w uintptr, on int32)
	gtkWidgetAddControl  func(w, c uintptr)
	gtkWidgetBounds      func(w, target uintptr, out *grect) int32
	gtkWidgetStyle       func(w uintptr) uintptr
	gtkWidgetScale       func(w uintptr) int32
	gtkWidgetParent      func(w uintptr) uintptr
	gtkStyleLookupColor  func(ctx uintptr, name string, out *gdkRGBA) int32
	gtkKeyControllerNew  func() uintptr
	gtkControllerPhase   func(c uintptr, phase int32)
	gtkSettingsDefault   func() uintptr
	gtkAlertDialogNew    func(format string) uintptr
	gtkAlertSetMessage   func(d uintptr, message string)
	gCancellableNew      func() uintptr
	gCancellableCancel   func(c uintptr)
	gdkDisplayDefault    func() uintptr
	gdkDisplayClipboard  func(d uintptr) uintptr
	gdkClipboardSetText  func(c uintptr, text string)
	gdkClipboardFormats  func(c uintptr) uintptr
	gdkFormatsHasType    func(f uintptr, t uint64) int32
	gdkClipboardRead     func(c, cancellable, fn, data uintptr)
	gdkClipboardReadDone func(c, result uintptr, err *uintptr) *byte
	gFree                func(p unsafe.Pointer)
	gtkGestureClickNew   func() uintptr
	gtkGestureSetButton  func(g uintptr, button uint32)
	gtkGestureButton     func(g uintptr) uint32
	gtkMotionNew         func() uintptr
	gtkScrollNew         func(flags uint32) uintptr
	gtkScrollUnit        func(c uintptr) int32
	gtkControllerState   func(c uintptr) uint32
	gtkWidgetSetCursor   func(w uintptr, name string)
	gtkWidgetSetParent   func(w, parent uintptr)
	gtkWidgetUnparent    func(w uintptr)
	gMenuNew             func() uintptr
	gMenuAppend          func(m uintptr, label, action string)
	gMenuAppendSection   func(m, label, section uintptr)
	gSimpleActionNew     func(name string, param uintptr) uintptr
	gSimpleActionEnable  func(a uintptr, on int32)
	gActionMapAdd        func(m, action uintptr)
	gtkPopoverMenuNew    func(model uintptr) uintptr
	gtkPopoverPointTo    func(p uintptr, r *gdkRect)
	gtkPopoverHasArrow   func(p uintptr, on int32)
	gtkPopoverPopup      func(p uintptr)
	gtkHeaderBarNew      func() uintptr
	gtkWindowSetTitlebar func(w, bar uintptr)
	gtkHeaderBarTitle    func(bar, widget uintptr)
	gtkDrawingAreaHeight func(a uintptr, h int32)
	gtkGestureSetState   func(g uintptr, state int32) int32
	pangoFontFree        func(desc uintptr)
	gtkAdjustmentNew     func(value, lower, upper, step, page, pageSize float64) uintptr
	gtkAdjustmentSet     func(a uintptr, value, lower, upper, step, page, pageSize float64)
	gtkAdjustmentValue   func(a uintptr) float64
	gtkScrollbarNew      func(orientation int32, adjustment uintptr) uintptr
	gtkAlertSetButtons   func(d uintptr, labels *uintptr)
	gtkAlertSetCancel    func(d uintptr, button int32)
	gtkAlertChoose       func(d, parent, cancellable, fn, data uintptr)
	gtkAlertChooseFinish func(d, result uintptr, err *uintptr) int32
	gdkKeyvalToUnicode   func(keyval uint32) uint32
	cairoSetRGBA         func(cr uintptr, r, g, b, a float64)
	cairoRectangle       func(cr uintptr, x, y, w, h float64)
	cairoFill            func(cr uintptr)
	cairoStroke          func(cr uintptr)
	cairoMoveTo          func(cr uintptr, x, y float64)
	cairoLineTo          func(cr uintptr, x, y float64)
	cairoLineWidth       func(cr uintptr, w float64)
	cairoArc             func(cr uintptr, x, y, r, from, to float64)
	cairoNewSubPath      func(cr uintptr)
	cairoClosePath       func(cr uintptr)
	cairoSave            func(cr uintptr)
	cairoRestore         func(cr uintptr)
	cairoClip            func(cr uintptr)
	cairoTranslate       func(cr uintptr, x, y float64)
	cairoScale           func(cr uintptr, x, y float64)
	cairoSetSource       func(cr, surface uintptr, x, y float64)
	cairoPaint           func(cr uintptr)
	cairoImageNew        func(format, w, h int32) uintptr
	cairoImageData       func(s uintptr) *byte
	cairoImageStride     func(s uintptr) int32
	cairoImageWidth      func(s uintptr) int32
	cairoImageHeight     func(s uintptr) int32
	cairoSurfaceDirty    func(s uintptr)
	cairoSurfaceFree     func(s uintptr)
	pangoCairoLayout     func(cr uintptr) uintptr
	pangoCairoShow       func(cr, layout uintptr)
	pangoCairoFontMap    func() uintptr
	pangoFontMapContext  func(fm uintptr) uintptr
	pangoLayoutNew       func(ctx uintptr) uintptr
	pangoLayoutSetText   func(l uintptr, text string, n int32)
	pangoLayoutMarkup    func(l uintptr, markup string, n int32)
	pangoLayoutSetFont   func(l, desc uintptr)
	pangoLayoutSetWidth  func(l uintptr, w int32)
	pangoLayoutSetHeight func(l uintptr, h int32)
	pangoLayoutSetWrap   func(l uintptr, wrap int32)
	pangoLayoutEllipsize func(l uintptr, mode int32)
	pangoLayoutSize      func(l uintptr, w, h *int32)
	pangoFontFromString  func(s string) uintptr
	pangoFontSetWeight   func(desc uintptr, weight int32)
	pangoFontSetStyle    func(desc uintptr, style int32)
	pangoFontCopy        func(desc uintptr) uintptr
)

// gValue is a GValue, a type and two words of data.
type gValue struct {
	typ  uint64
	data [2]uint64
}

// The GTypes of the values fude reads and writes.
const (
	gTypeBoolean = 5 << 2
	gTypeString  = 16 << 2
)

// grect is a graphene_rect_t, gdkRGBA a GdkRGBA and gdkRect a
// GdkRectangle.
type (
	grect   struct{ X, Y, W, H float32 }
	gdkRGBA struct{ R, G, B, A float32 }
	gdkRect struct{ X, Y, W, H int32 }
)

// pangoScale is how many of Pango's units make a point.
const pangoScale = 1024

var loadOnce sync.Once

// load opens GTK4, cairo and Pango, and finds their calls, once.
func load() error {
	var err error
	loadOnce.Do(func() {
		libs := map[string]uintptr{}
		for _, name := range []string{"libgtk-4.so.1", "libcairo.so.2", "libpango-1.0.so.0", "libpangocairo-1.0.so.0"} {
			h, e := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if e != nil {
				err = fmt.Errorf("--gui on Linux needs GTK 4: open %s: %w", name, e)
				return
			}
			libs[name] = h
		}
		gtk, cairo, pango, pc := libs["libgtk-4.so.1"], libs["libcairo.so.2"], libs["libpango-1.0.so.0"], libs["libpangocairo-1.0.so.0"]
		for _, f := range []struct {
			lib  uintptr
			fn   any
			name string
		}{
			{gtk, &gIdleAdd, "g_idle_add"},
			{gtk, &gSignalConnectData, "g_signal_connect_data"},
			{gtk, &gObjectRefSink, "g_object_ref_sink"},
			{gtk, &gObjectUnref, "g_object_unref"},
			{gtk, &gObjectSetProperty, "g_object_set_property"},
			{gtk, &gObjectGetProperty, "g_object_get_property"},
			{gtk, &gValueInit, "g_value_init"},
			{gtk, &gValueSetBoolean, "g_value_set_boolean"},
			{gtk, &gValueGetString, "g_value_get_string"},
			{gtk, &gValueUnset, "g_value_unset"},
			{gtk, &gApplicationRun, "g_application_run"},
			{gtk, &gApplicationHold, "g_application_hold"},
			{gtk, &gApplicationQuit, "g_application_quit"},
			{gtk, &gtkApplicationNew, "gtk_application_new"},
			{gtk, &gtkAppWindowNew, "gtk_application_window_new"},
			{gtk, &gtkWindowSetTitle, "gtk_window_set_title"},
			{gtk, &gtkWindowSetSize, "gtk_window_set_default_size"},
			{gtk, &gtkWindowSetChild, "gtk_window_set_child"},
			{gtk, &gtkWindowPresent, "gtk_window_present"},
			{gtk, &gtkWindowDestroy, "gtk_window_destroy"},
			{gtk, &gtkWindowIsActive, "gtk_window_is_active"},
			{gtk, &gtkOverlayNew, "gtk_overlay_new"},
			{gtk, &gtkOverlaySetChild, "gtk_overlay_set_child"},
			{gtk, &gtkOverlayAdd, "gtk_overlay_add_overlay"},
			{gtk, &gtkOverlayRemove, "gtk_overlay_remove_overlay"},
			{gtk, &gtkPanedNew, "gtk_paned_new"},
			{gtk, &gtkPanedSetStart, "gtk_paned_set_start_child"},
			{gtk, &gtkPanedSetEnd, "gtk_paned_set_end_child"},
			{gtk, &gtkPanedSetPosition, "gtk_paned_set_position"},
			{gtk, &gtkDrawingAreaNew, "gtk_drawing_area_new"},
			{gtk, &gtkDrawingAreaSetFn, "gtk_drawing_area_set_draw_func"},
			{gtk, &gtkWidgetQueueDraw, "gtk_widget_queue_draw"},
			{gtk, &gtkWidgetGetWidth, "gtk_widget_get_width"},
			{gtk, &gtkWidgetGetHeight, "gtk_widget_get_height"},
			{gtk, &gtkWidgetSetVisible, "gtk_widget_set_visible"},
			{gtk, &gtkWidgetSetHalign, "gtk_widget_set_halign"},
			{gtk, &gtkWidgetSetValign, "gtk_widget_set_valign"},
			{gtk, &gtkWidgetSetHexpand, "gtk_widget_set_hexpand"},
			{gtk, &gtkWidgetSetVexpand, "gtk_widget_set_vexpand"},
			{gtk, &gtkWidgetMarginStart, "gtk_widget_set_margin_start"},
			{gtk, &gtkWidgetMarginTop, "gtk_widget_set_margin_top"},
			{gtk, &gtkWidgetSizeRequest, "gtk_widget_set_size_request"},
			{gtk, &gtkWidgetSetTarget, "gtk_widget_set_can_target"},
			{gtk, &gtkWidgetAddControl, "gtk_widget_add_controller"},
			{gtk, &gtkWidgetBounds, "gtk_widget_compute_bounds"},
			{gtk, &gtkWidgetStyle, "gtk_widget_get_style_context"},
			{gtk, &gtkWidgetScale, "gtk_widget_get_scale_factor"},
			{gtk, &gtkWidgetParent, "gtk_widget_get_parent"},
			{gtk, &gtkStyleLookupColor, "gtk_style_context_lookup_color"},
			{gtk, &gtkKeyControllerNew, "gtk_event_controller_key_new"},
			{gtk, &gtkControllerPhase, "gtk_event_controller_set_propagation_phase"},
			{gtk, &gtkSettingsDefault, "gtk_settings_get_default"},
			{gtk, &gtkAlertDialogNew, "gtk_alert_dialog_new"},
			{gtk, &gtkAlertSetMessage, "gtk_alert_dialog_set_message"},
			{gtk, &gCancellableNew, "g_cancellable_new"},
			{gtk, &gCancellableCancel, "g_cancellable_cancel"},
			{gtk, &gdkDisplayDefault, "gdk_display_get_default"},
			{gtk, &gdkDisplayClipboard, "gdk_display_get_clipboard"},
			{gtk, &gdkClipboardSetText, "gdk_clipboard_set_text"},
			{gtk, &gdkClipboardFormats, "gdk_clipboard_get_formats"},
			{gtk, &gdkFormatsHasType, "gdk_content_formats_contain_gtype"},
			{gtk, &gdkClipboardRead, "gdk_clipboard_read_text_async"},
			{gtk, &gdkClipboardReadDone, "gdk_clipboard_read_text_finish"},
			{gtk, &gFree, "g_free"},
			{gtk, &gtkGestureClickNew, "gtk_gesture_click_new"},
			{gtk, &gtkGestureSetButton, "gtk_gesture_single_set_button"},
			{gtk, &gtkGestureButton, "gtk_gesture_single_get_current_button"},
			{gtk, &gtkMotionNew, "gtk_event_controller_motion_new"},
			{gtk, &gtkScrollNew, "gtk_event_controller_scroll_new"},
			{gtk, &gtkScrollUnit, "gtk_event_controller_scroll_get_unit"},
			{gtk, &gtkControllerState, "gtk_event_controller_get_current_event_state"},
			{gtk, &gtkWidgetSetCursor, "gtk_widget_set_cursor_from_name"},
			{gtk, &gtkWidgetSetParent, "gtk_widget_set_parent"},
			{gtk, &gtkWidgetUnparent, "gtk_widget_unparent"},
			{gtk, &gMenuNew, "g_menu_new"},
			{gtk, &gMenuAppend, "g_menu_append"},
			{gtk, &gMenuAppendSection, "g_menu_append_section"},
			{gtk, &gSimpleActionNew, "g_simple_action_new"},
			{gtk, &gSimpleActionEnable, "g_simple_action_set_enabled"},
			{gtk, &gActionMapAdd, "g_action_map_add_action"},
			{gtk, &gtkPopoverMenuNew, "gtk_popover_menu_new_from_model"},
			{gtk, &gtkPopoverPointTo, "gtk_popover_set_pointing_to"},
			{gtk, &gtkPopoverHasArrow, "gtk_popover_set_has_arrow"},
			{gtk, &gtkPopoverPopup, "gtk_popover_popup"},
			{gtk, &gtkHeaderBarNew, "gtk_header_bar_new"},
			{gtk, &gtkWindowSetTitlebar, "gtk_window_set_titlebar"},
			{gtk, &gtkHeaderBarTitle, "gtk_header_bar_set_title_widget"},
			{gtk, &gtkDrawingAreaHeight, "gtk_drawing_area_set_content_height"},
			{gtk, &gtkGestureSetState, "gtk_gesture_set_state"},
			{pango, &pangoFontFree, "pango_font_description_free"},
			{gtk, &gtkAdjustmentNew, "gtk_adjustment_new"},
			{gtk, &gtkAdjustmentSet, "gtk_adjustment_configure"},
			{gtk, &gtkAdjustmentValue, "gtk_adjustment_get_value"},
			{gtk, &gtkScrollbarNew, "gtk_scrollbar_new"},
			{gtk, &gtkAlertSetButtons, "gtk_alert_dialog_set_buttons"},
			{gtk, &gtkAlertSetCancel, "gtk_alert_dialog_set_cancel_button"},
			{gtk, &gtkAlertChoose, "gtk_alert_dialog_choose"},
			{gtk, &gtkAlertChooseFinish, "gtk_alert_dialog_choose_finish"},
			{gtk, &gdkKeyvalToUnicode, "gdk_keyval_to_unicode"},
			{cairo, &cairoSetRGBA, "cairo_set_source_rgba"},
			{cairo, &cairoRectangle, "cairo_rectangle"},
			{cairo, &cairoFill, "cairo_fill"},
			{cairo, &cairoStroke, "cairo_stroke"},
			{cairo, &cairoMoveTo, "cairo_move_to"},
			{cairo, &cairoLineTo, "cairo_line_to"},
			{cairo, &cairoLineWidth, "cairo_set_line_width"},
			{cairo, &cairoArc, "cairo_arc"},
			{cairo, &cairoNewSubPath, "cairo_new_sub_path"},
			{cairo, &cairoClosePath, "cairo_close_path"},
			{cairo, &cairoSave, "cairo_save"},
			{cairo, &cairoRestore, "cairo_restore"},
			{cairo, &cairoClip, "cairo_clip"},
			{cairo, &cairoTranslate, "cairo_translate"},
			{cairo, &cairoScale, "cairo_scale"},
			{cairo, &cairoSetSource, "cairo_set_source_surface"},
			{cairo, &cairoPaint, "cairo_paint"},
			{cairo, &cairoImageNew, "cairo_image_surface_create"},
			{cairo, &cairoImageData, "cairo_image_surface_get_data"},
			{cairo, &cairoImageStride, "cairo_image_surface_get_stride"},
			{cairo, &cairoImageWidth, "cairo_image_surface_get_width"},
			{cairo, &cairoImageHeight, "cairo_image_surface_get_height"},
			{cairo, &cairoSurfaceDirty, "cairo_surface_mark_dirty"},
			{cairo, &cairoSurfaceFree, "cairo_surface_destroy"},
			{pc, &pangoCairoLayout, "pango_cairo_create_layout"},
			{pc, &pangoCairoShow, "pango_cairo_show_layout"},
			{pc, &pangoCairoFontMap, "pango_cairo_font_map_get_default"},
			{pango, &pangoFontMapContext, "pango_font_map_create_context"},
			{pango, &pangoLayoutNew, "pango_layout_new"},
			{pango, &pangoLayoutSetText, "pango_layout_set_text"},
			{pango, &pangoLayoutMarkup, "pango_layout_set_markup"},
			{pango, &pangoLayoutSetFont, "pango_layout_set_font_description"},
			{pango, &pangoLayoutSetWidth, "pango_layout_set_width"},
			{pango, &pangoLayoutSetHeight, "pango_layout_set_height"},
			{pango, &pangoLayoutSetWrap, "pango_layout_set_wrap"},
			{pango, &pangoLayoutEllipsize, "pango_layout_set_ellipsize"},
			{pango, &pangoLayoutSize, "pango_layout_get_size"},
			{pango, &pangoFontFromString, "pango_font_description_from_string"},
			{pango, &pangoFontSetWeight, "pango_font_description_set_weight"},
			{pango, &pangoFontSetStyle, "pango_font_description_set_style"},
			{pango, &pangoFontCopy, "pango_font_description_copy"},
		} {
			purego.RegisterLibFunc(f.fn, f.lib, f.name)
		}
	})
	return err
}

// The main thread's queue of work from other goroutines, which drain runs.
var (
	mainMu    sync.Mutex
	mainQueue []func()
	drainFn   uintptr
)

// onMain runs fn on the main thread, after what it already has queued, and
// with wait returns once fn has run.
func onMain(fn func(), wait bool) {
	done := make(chan struct{})
	mainMu.Lock()
	mainQueue = append(mainQueue, func() {
		defer close(done)
		fn()
	})
	mainMu.Unlock()
	gIdleAdd(drainFn, 0)
	if wait {
		<-done
	}
}

// drain runs the queued work, as GLib's idle source, which it removes.
func drain(uintptr) uintptr {
	mainMu.Lock()
	q := mainQueue
	mainQueue = nil
	mainMu.Unlock()
	for _, fn := range q {
		fn()
	}
	return 0
}
