//go:build !darwin && !linux && !windows

package gui_test

import (
	"testing"

	"github.com/y3owk1n/fude/gui"
)

// B547
func TestB547OnlyOnMacOS(t *testing.T) {
	if err := gui.Run(nil); err == nil || err.Error() != "--gui runs only on macOS, Linux and Windows for now" {
		t.Errorf("--gui elsewhere said %v", err)
	}
}
