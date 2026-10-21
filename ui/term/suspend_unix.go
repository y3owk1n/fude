//go:build !windows

package term

import (
	"fmt"
	"syscall"

	"github.com/gdamore/tcell/v3"
)

// suspend gives the terminal back and stops fude, as <C-z> asks, until the
// shell resumes it (B424).
func suspend(s tcell.Screen) error {
	if err := s.Suspend(); err != nil {
		return fmt.Errorf("suspend: %w", err)
	}
	// The process group stops, as the shell stops a job, and goes on
	// together on SIGCONT.
	if err := syscall.Kill(0, syscall.SIGTSTP); err != nil {
		return fmt.Errorf("suspend: %w", err)
	}
	if err := s.Resume(); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	return nil
}
