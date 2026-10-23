//go:build !darwin

package gui_test

import (
	"testing"

	"github.com/y3owk1n/fude/gui"
)

// B547
func TestB547OnlyOnMacOS(t *testing.T) {
	if err := gui.Run(nil); err == nil || err.Error() != "--gui runs only on macOS for now" {
		t.Errorf("--gui elsewhere said %v", err)
	}
}
