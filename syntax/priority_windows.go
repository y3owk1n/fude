package syntax

import (
	"fmt"
	"syscall"
)

var setThreadPriority = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadPriority")

// background lowers the calling thread's priority below normal. The threads
// that answer keys, in this fude and others, then run before it.
func background() error {
	// ^uintptr(1) is GetCurrentThread's -2, and ^uintptr(0) is
	// THREAD_PRIORITY_BELOW_NORMAL, -1.
	if ok, _, err := setThreadPriority.Call(^uintptr(1), ^uintptr(0)); ok == 0 {
		return fmt.Errorf("set thread priority below normal: %w", err)
	}
	return nil
}
