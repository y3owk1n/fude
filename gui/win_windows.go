package gui

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

// A window's messages go to the thread that made it, which the main
// goroutine keeps.
func init() { runtime.LockOSThread() }

// Win32's calls, and the DLLs of Direct2D and DirectWrite, whose objects
// com calls (D66).
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	d2d1     = windows.NewLazySystemDLL("d2d1.dll")
	dwrite   = windows.NewLazySystemDLL("dwrite.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")

	registerClass       = user32.NewProc("RegisterClassExW")
	createWindow        = user32.NewProc("CreateWindowExW")
	defWindowProc       = user32.NewProc("DefWindowProcW")
	destroyWindow       = user32.NewProc("DestroyWindow")
	showWindow          = user32.NewProc("ShowWindow")
	getMessage          = user32.NewProc("GetMessageW")
	translateMessage    = user32.NewProc("TranslateMessage")
	dispatchMessage     = user32.NewProc("DispatchMessageW")
	postMessage         = user32.NewProc("PostMessageW")
	postQuit            = user32.NewProc("PostQuitMessage")
	invalidateRect      = user32.NewProc("InvalidateRect")
	validateRect        = user32.NewProc("ValidateRect")
	getClientRect       = user32.NewProc("GetClientRect")
	setWindowText       = user32.NewProc("SetWindowTextW")
	setWindowPos        = user32.NewProc("SetWindowPos")
	loadCursor          = user32.NewProc("LoadCursorW")
	getDpiForWindow     = user32.NewProc("GetDpiForWindow")
	setDpiAwareness     = user32.NewProc("SetProcessDpiAwarenessContext")
	adjustWindowRect    = user32.NewProc("AdjustWindowRectExForDpi")
	getKeyState         = user32.NewProc("GetKeyState")
	mapVirtualKey       = user32.NewProc("MapVirtualKeyW")
	messageBox          = user32.NewProc("MessageBoxW")
	getWindow           = user32.NewProc("GetWindow")
	endDialog           = user32.NewProc("EndDialog")
	getForeground       = user32.NewProc("GetForegroundWindow")
	systemParams        = user32.NewProc("SystemParametersInfoForDpi")
	getModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	d2dCreateFactory    = d2d1.NewProc("D2D1CreateFactory")
	dwriteCreateFactory = dwrite.NewProc("DWriteCreateFactory")
	dwmSetAttribute     = dwmapi.NewProc("DwmSetWindowAttribute")
	dwmColorization     = dwmapi.NewProc("DwmGetColorizationColor")
)

// The messages fude handles, and its own for the main thread's queue.
const (
	wmDestroy    = 0x0002
	wmSize       = 0x0005
	wmPaint      = 0x000F
	wmClose      = 0x0010
	wmEraseBkgnd = 0x0014
	wmKeyDown    = 0x0100
	wmChar       = 0x0102
	wmSysKeyDown = 0x0104
	wmSysChar    = 0x0106
	wmDpiChanged = 0x02E0
	wmApp        = 0x8000
)

// msg is a MSG.
type msg struct {
	hwnd           uintptr
	message        uint32
	wparam, lparam uintptr
	time           uint32
	pt             [2]int32
	private        uint32
}

// wndClass is a WNDCLASSEXW.
type wndClass struct {
	size, style                uint32
	proc                       uintptr
	clsExtra, wndExtra         int32
	instance, icon, cursor, bg uintptr
	menu, name                 *uint16
	iconSm                     uintptr
}

// rectI is a RECT, in pixels.
type rectI struct{ L, T, R, B int32 }

// The structs Direct2D and DirectWrite take: a rectangle, a colour, a
// rounded rectangle, a transform, a pixel size, a render target's and a
// bitmap's properties and a text layout's metrics.
type (
	rectF    struct{ L, T, R, B float32 }
	colorF   struct{ R, G, B, A float32 }
	roundedF struct {
		r      rectF
		rx, ry float32
	}
	matrixF [6]float32
	sizeU   struct{ W, H uint32 }
	rtProps struct {
		typ, format, alpha uint32
		dpiX, dpiY         float32
		usage, minLevel    uint32
	}
	hwndProps struct {
		hwnd    uintptr
		size    sizeU
		present uint32
	}
	bitmapProps struct {
		format, alpha uint32
		dpiX, dpiY    float32
	}
	textMetrics struct {
		left, top, width, widthTrailing, height, layoutWidth, layoutHeight float32
		bidi, lines                                                        uint32
	}
)

// rf is r as Direct2D's rectangle.
func rf(r rect) *rectF {
	return &rectF{float32(r.Origin.X), float32(r.Origin.Y), float32(r.Origin.X + r.Size.W), float32(r.Origin.Y + r.Size.H)}
}

// The interfaces fude asks for.
var (
	iidD2DFactory = windows.GUID{Data1: 0x06152247, Data2: 0x6f50, Data3: 0x465a, Data4: [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
	iidDWFactory  = windows.GUID{Data1: 0xb859ee5a, Data2: 0xd838, Data3: 0x4b5b, Data4: [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}
)

// d2d and dw are the factories of Direct2D and DirectWrite, which load
// makes.
var d2d, dw unsafe.Pointer

// vtbl is method i of the COM object at obj.
func vtbl(obj unsafe.Pointer, i int) uintptr { return (*(**[128]uintptr)(obj))[i] }

// com calls method i of the COM object obj with args, none a float, and
// returns what it returns. A pointer made a uintptr in the call stays on
// the heap until it returns.
//
//go:uintptrescapes
func com(obj unsafe.Pointer, i int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(vtbl(obj, i), append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// call calls a Win32 function and returns what it returns.
//
//go:uintptrescapes
func call(p *windows.LazyProc, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(p.Addr(), args...)
	return r
}

// release lets go of a COM object.
func release(obj unsafe.Pointer) {
	if obj != nil {
		com(obj, 2)
	}
}

// failed is an error for an HRESULT that failed, nil for one that did not.
func failed(what string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s: %#x", what, uint32(hr))
	}
	return nil
}

// bound are the COM methods that take floats, by their addresses, which
// purego passes in the float registers as each CPU wants.
var bound = map[uintptr]any{}

// bind returns method i of obj as F, a function that takes obj first.
func bind[F any](obj unsafe.Pointer, i int) F {
	addr := vtbl(obj, i)
	if f, ok := bound[addr]; ok {
		return f.(F)
	}
	var f F
	purego.RegisterFunc(&f, addr)
	bound[addr] = f
	return f
}

// cwide is s in UTF-16 ending in 0, as Win32 takes a string.
func cwide(s string) *uint16 { return &wide(s + "\x00")[0] }

// wide is s in UTF-16, never empty, so its first element has an address.
func wide(s string) []uint16 {
	if s == "" {
		return []uint16{0}
	}
	return utf16.Encode([]rune(s))
}

var loadOnce sync.Once

// load finds Win32's calls and makes the factories of Direct2D and
// DirectWrite, once.
func load() error {
	var err error
	loadOnce.Do(func() {
		for _, dll := range []*windows.LazyDLL{user32, kernel32, d2d1, dwrite, dwmapi} {
			if err = dll.Load(); err != nil {
				err = fmt.Errorf("--gui on Windows needs %s: %w", dll.Name, err)
				return
			}
		}
		if err = failed("make Direct2D's factory", call(d2dCreateFactory, 0, uintptr(unsafe.Pointer(&iidD2DFactory)), 0, uintptr(unsafe.Pointer(&d2d)))); err != nil {
			return
		}
		err = failed("make DirectWrite's factory", call(dwriteCreateFactory, 0, uintptr(unsafe.Pointer(&iidDWFactory)), uintptr(unsafe.Pointer(&dw))))
	})
	return err
}

// The main thread's queue of work from other goroutines, which drain runs
// as the message window gets wmApp.
var (
	mainMu    sync.Mutex
	mainQueue []func()
	mainHwnd  uintptr
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
	call(postMessage, mainHwnd, wmApp, 0, 0)
	if wait {
		<-done
	}
}

// drain runs the queued work.
func drain() {
	mainMu.Lock()
	q := mainQueue
	mainQueue = nil
	mainMu.Unlock()
	for _, fn := range q {
		fn()
	}
}

// pump dispatches the main thread's messages until PostQuitMessage.
func pump() {
	var m msg
	for int32(call(getMessage, uintptr(unsafe.Pointer(&m)), 0, 0, 0)) > 0 {
		call(translateMessage, uintptr(unsafe.Pointer(&m)))
		call(dispatchMessage, uintptr(unsafe.Pointer(&m)))
	}
}
