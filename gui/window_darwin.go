package gui

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// textSize is the text's size in points, which Command-= and Command-minus
// change (B558).
var textSize = 14.0

// makeFonts makes the text's fonts in textSize, and the cells they fill.
func makeFonts() {
	font = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), textSize, 0).Send(sel("retain"))
	boldFont = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), textSize, 0.4).Send(sel("retain"))
	fonts := cls("NSFontManager").Send(sel("sharedFontManager"))
	italicFont = fonts.Send(sel("convertFont:toHaveTrait:"), font, 1).Send(sel("retain"))
	boldItalicFont = fonts.Send(sel("convertFont:toHaveTrait:"), boldFont, 1).Send(sel("retain"))
	cellW = msgSizeOf(nsString("M"), sel("sizeWithAttributes:"), textAttrs(font, 0)).W
	cellH = float64(int(msgFloat(font, sel("ascender")) - msgFloat(font, sel("descender")) + msgFloat(font, sel("leading")) + 0.999))
}

// sizeText makes the text points tall, from 8 to 40, and every window as
// many cells as it then holds.
func sizeText(points float64) {
	for _, f := range []objc.ID{font, boldFont, italicFont, boldItalicFont} {
		f.Send(sel("release"))
	}
	for k, a := range attrsOf {
		a.Send(sel("release"))
		delete(attrsOf, k)
	}
	textSize = min(max(points, 8), 40)
	makeFonts()
	for v, w := range windows {
		if v != w.view {
			continue
		}
		w.cellPx = cellPixels(w.win)
		resizeRoot(w.view, 0, size{})
		for _, p := range w.panes {
			p.text.Send(sel("setNeedsDisplay:"), 1)
		}
	}
}

// cellPixels is how many of the screen's pixels a cell of win takes.
func cellPixels(win objc.ID) [2]int {
	scale := msgFloat(win, sel("backingScaleFactor"))
	return [2]int{int(cellW * scale), int(cellH * scale)}
}

// The View menu's actions, which size the text of every window.
var sizing = []objc.MethodDef{
	{Cmd: sel("bigger:"), Fn: func(objc.ID, objc.SEL, objc.ID) { sizeText(textSize + 1) }},
	{Cmd: sel("smaller:"), Fn: func(objc.ID, objc.SEL, objc.ID) { sizeText(textSize - 1) }},
	{Cmd: sel("actualSize:"), Fn: func(objc.ID, objc.SEL, objc.ID) { sizeText(14) }},
}

// dress shows on w what the frame says of the session: the theme's light
// or dark look on the title bar and the sheets, a dot in the close button
// while a buffer shown has changes, and the current file's icon in the
// title bar (B558).
func (w *window) dress(f frame) {
	look := "NSAppearanceNameAqua"
	if dark(f.ground) {
		look = "NSAppearanceNameDarkAqua"
	}
	if look != w.appearance {
		w.appearance = look
		w.win.Send(sel("setAppearance:"), cls("NSAppearance").Send(sel("appearanceNamed:"), nsString(look)))
	}
	w.win.Send(sel("setDocumentEdited:"), f.edited)
	if f.file != w.file {
		w.file = f.file
		w.win.Send(sel("setRepresentedFilename:"), nsString(f.file))
	}
}

// notify tells the user of text while w is not in front. It bounces the
// Dock's icon once, and from an app bundle it also posts a notification,
// which needs a bundle (B564).
func (w *window) notify(text string) {
	app := cls("NSApplication").Send(sel("sharedApplication"))
	if objc.Send[bool](app, sel("isActive")) && objc.Send[bool](w.win, sel("isKeyWindow")) {
		return
	}
	// NSInformationalRequest bounces the icon once.
	app.Send(sel("requestUserAttention:"), 10)
	if cls("NSBundle").Send(sel("mainBundle")).Send(sel("bundleIdentifier")) == 0 {
		return
	}
	center := notifications()
	if center == 0 {
		return
	}
	content := cls("UNMutableNotificationContent").Send(sel("new"))
	content.Send(sel("setTitle:"), nsString("fude"))
	content.Send(sel("setBody:"), nsString(text))
	notified++
	request := cls("UNNotificationRequest").Send(sel("requestWithIdentifier:content:trigger:"), nsString(fmt.Sprint("fude-", notified)), content, 0)
	center.Send(sel("addNotificationRequest:withCompletionHandler:"), request, 0)
	content.Send(sel("release"))
}

// notified counts the notifications sent, each its own.
var notified int

// notifications returns the system's notification centre, which asks the
// user to allow fude's notifications the first time, or 0 when it is not
// there.
var notifications = sync.OnceValue(func() objc.ID {
	if _, err := purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_GLOBAL); err != nil {
		return 0
	}
	center := cls("UNUserNotificationCenter").Send(sel("currentNotificationCenter"))
	// The answer needs no handling, as a refusal leaves only the Dock.
	center.Send(sel("requestAuthorizationWithOptions:completionHandler:"), 6, objc.NewBlock(func(objc.Block, bool, objc.ID) {}))
	return center
})
