package gui

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/theme"
)

// AppKit runs on the main thread, which the main goroutine keeps.
func init() { runtime.LockOSThread() }

type (
	point struct{ X, Y float64 }
	size  struct{ W, H float64 }
	rect  struct {
		Origin point
		Size   size
	}
)

// The calls whose arguments or results objc.Send cannot pass: structs and
// floats.
var (
	msgInitWindow func(objc.ID, objc.SEL, rect, uint, uint, bool) objc.ID
	msgInitView   func(objc.ID, objc.SEL, rect) objc.ID
	msgRect       func(objc.ID, objc.SEL) rect
	msgSetSize    func(objc.ID, objc.SEL, size)
	msgSetRect    func(objc.ID, objc.SEL, rect)
	msgSetFloat   func(objc.ID, objc.SEL, float64)
	msgBounding   func(objc.ID, objc.SEL, size, uint, objc.ID, objc.ID) rect
	msgDrawIn     func(objc.ID, objc.SEL, rect, uint, objc.ID, objc.ID)
	msgFloat      func(objc.ID, objc.SEL) float64
	msgFont       func(objc.ID, objc.SEL, float64, float64) objc.ID
	msgColor      func(objc.ID, objc.SEL, float64, float64, float64, float64) objc.ID
	msgSizeOf     func(objc.ID, objc.SEL, objc.ID) size
	msgDrawAt     func(objc.ID, objc.SEL, point, objc.ID)
	msgKeyEvent   func(objc.ID, objc.SEL, uint, point, uint, float64, int, objc.ID, objc.ID, objc.ID, bool, uint16) objc.ID
	msgOtherEvent func(objc.ID, objc.SEL, uint, point, uint, float64, int, objc.ID, int16, int, int) objc.ID
	msgUTF8       func(objc.ID, objc.SEL) *byte
	rectFill      func(rect)
	poolPush      func() uintptr
	poolPop       func(uintptr)

	attrFont, attrColor objc.ID
)

var loadOnce sync.Once

// load opens AppKit and binds the calls, once.
func load() {
	loadOnce.Do(func() {
		appkit, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL)
		if err != nil {
			panic(fmt.Sprintf("gui: open AppKit: %v", err))
		}
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL)
		if err != nil {
			panic(fmt.Sprintf("gui: open libobjc: %v", err))
		}
		for _, f := range []any{&msgInitWindow, &msgInitView, &msgRect, &msgSetSize, &msgSetRect, &msgSetFloat, &msgBounding, &msgDrawIn, &msgFloat, &msgFont, &msgColor, &msgSizeOf, &msgDrawAt, &msgKeyEvent, &msgOtherEvent, &msgUTF8} {
			purego.RegisterLibFunc(f, lib, "objc_msgSend")
		}
		purego.RegisterLibFunc(&rectFill, appkit, "NSRectFill")
		purego.RegisterLibFunc(&poolPush, lib, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&poolPop, lib, "objc_autoreleasePoolPop")
		attrFont, attrColor = global(appkit, "NSFontAttributeName"), global(appkit, "NSForegroundColorAttributeName")
	})
}

// global reads the object an AppKit constant holds.
func global(lib uintptr, name string) objc.ID {
	addr, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(fmt.Sprintf("gui: %s: %v", name, err))
	}
	return **(**objc.ID)(unsafe.Pointer(&addr))
}

func sel(s string) objc.SEL { return objc.RegisterName(s) }
func cls(s string) objc.ID  { return objc.ID(objc.GetClass(s)) }

// pooled runs fn with an autorelease pool around it.
func pooled(fn func()) {
	p := poolPush()
	defer poolPop(p)
	fn()
}

// nsString returns an autoreleased NSString holding s.
func nsString(s string) objc.ID {
	if s == "" {
		return cls("NSString").Send(sel("string"))
	}
	id := cls("NSString").Send(sel("alloc")).Send(sel("initWithBytes:length:encoding:"), uintptr(unsafe.Pointer(unsafe.StringData(s))), uintptr(len(s)), uintptr(4))
	runtime.KeepAlive(s)
	return id.Send(sel("autorelease"))
}

// goString returns the text of an NSString.
func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	p := msgUTF8(s, sel("UTF8String"))
	if p == nil {
		return ""
	}
	n := objc.Send[uint](s, sel("lengthOfBytesUsingEncoding:"), uintptr(4))
	return string(unsafe.Slice(p, n))
}

// nsColor returns an autoreleased NSColor of c.
func nsColor(c theme.RGB) objc.ID {
	return msgColor(cls("NSColor"), sel("colorWithSRGBRed:green:blue:alpha:"), float64(c>>16&0xff)/255, float64(c>>8&0xff)/255, float64(c&0xff)/255, 1)
}

// mainQueue holds what onMain runs next on the main thread.
var (
	mainMu    sync.Mutex
	mainQueue []func()
	mainRun   objc.ID // the object whose drain: the main thread performs
)

// onMain runs fn on the main thread, and with wait set returns once it
// ran.
func onMain(fn func(), wait bool) {
	done := make(chan struct{})
	mainMu.Lock()
	mainQueue = append(mainQueue, func() {
		defer close(done)
		fn()
	})
	mainMu.Unlock()
	mainRun.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("drain:"), 0, 0)
	if wait {
		<-done
	}
}

func drain(objc.ID, objc.SEL, objc.ID) {
	mainMu.Lock()
	q := mainQueue
	mainQueue = nil
	mainMu.Unlock()
	pooled(func() {
		for _, fn := range q {
			fn()
		}
	})
}
