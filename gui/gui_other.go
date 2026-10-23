//go:build !darwin

package gui

import (
	"errors"

	"github.com/y3owk1n/fude/frontend/client"
)

// Run says the window is for macOS only (B547).
func Run(client.Session) error { return errors.New("fude gui runs only on macOS for now") }
