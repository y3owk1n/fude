package syntax

import (
	"fmt"
	"syscall"
)

// background raises the calling thread's nice value, which Linux keeps per
// thread. The threads that answer keys, in this fude and others, then run
// before it.
func background() error {
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, syscall.Gettid(), 10); err != nil {
		return fmt.Errorf("set nice 10: %w", err)
	}
	return nil
}
