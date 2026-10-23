//go:build !darwin

package gui

import (
	"errors"

	"github.com/y3owk1n/fude/frontend/client"
)

// Run says the window is for macOS only (B547).
func Run(client.Session) error { return errors.New("--gui runs only on macOS for now") }

// native is a window's system objects, none until the system has a window.
type native struct{}

func onMain(fn func(), _ bool) { fn() }
func (w *window) apply()       {}
func (w *window) close()       {}
