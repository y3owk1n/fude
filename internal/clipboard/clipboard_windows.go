package clipboard

import (
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	formatAvailable  = user32.NewProc("IsClipboardFormatAvailable")
	getClipboardData = user32.NewProc("GetClipboardData")
	setClipboardData = user32.NewProc("SetClipboardData")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalFree       = kernel32.NewProc("GlobalFree")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
)

const (
	unicodeText = 13 // CF_UNICODETEXT
	moveable    = 2  // GMEM_MOVEABLE
)

// open opens the clipboard for this thread, waiting a little while another
// program holds it.
func open() error {
	var err error
	for range 10 {
		r, _, e := openClipboard.Call(0)
		if r != 0 {
			return nil
		}
		err = e
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("open the clipboard: %w", err)
}

// pointer is the memory at p, an address Windows returned. go vet warns
// that a plain conversion may point at Go memory that moves, which this is not.
func pointer(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// Read returns the clipboard's text, with Windows' \r\n as \n.
func Read() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := open(); err != nil {
		return "", err
	}
	defer closeClipboard.Call()
	if r, _, _ := formatAvailable.Call(unicodeText); r == 0 {
		return "", nil
	}
	h, _, err := getClipboardData.Call(unicodeText)
	if h == 0 {
		return "", fmt.Errorf("read the clipboard: %w", err)
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		return "", fmt.Errorf("read the clipboard: %w", err)
	}
	defer globalUnlock.Call(h)
	return strings.ReplaceAll(windows.UTF16PtrToString((*uint16)(pointer(p))), "\r\n", "\n"), nil
}

// Write puts text on the clipboard, with its \n as Windows' \r\n.
func Write(text string) error {
	u, err := windows.UTF16FromString(strings.ReplaceAll(text, "\n", "\r\n"))
	if err != nil {
		return fmt.Errorf("write the clipboard: %w", err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := open(); err != nil {
		return err
	}
	defer closeClipboard.Call()
	if r, _, err := emptyClipboard.Call(); r == 0 {
		return fmt.Errorf("write the clipboard: %w", err)
	}
	h, _, err := globalAlloc.Call(moveable, uintptr(len(u)*2))
	if h == 0 {
		return fmt.Errorf("write the clipboard: %w", err)
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		globalFree.Call(h)
		return fmt.Errorf("write the clipboard: %w", err)
	}
	copy(unsafe.Slice((*uint16)(pointer(p)), len(u)), u)
	globalUnlock.Call(h)
	// The clipboard owns the memory once it takes it.
	if r, _, err := setClipboardData.Call(unicodeText, h); r == 0 {
		globalFree.Call(h)
		return fmt.Errorf("write the clipboard: %w", err)
	}
	return nil
}
