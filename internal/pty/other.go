//go:build !darwin && !linux && !windows

// Package pty runs a program in a pseudo-terminal, as :terminal does
// (B205).
package pty

import (
	"errors"
	"os/exec"
)

var errUnsupported = errors.New("pseudo-terminals need macOS, Linux or Windows")

// Terminal is a program running in a pseudo-terminal, which this system
// has none of.
type Terminal struct{}

// Start says this system has no pseudo-terminals.
func Start(*exec.Cmd, int, int) (*Terminal, error) { return nil, errUnsupported }

func (*Terminal) Read([]byte) (int, error)  { return 0, errUnsupported }
func (*Terminal) Write([]byte) (int, error) { return 0, errUnsupported }
func (*Terminal) Resize(int, int) error     { return errUnsupported }
func (*Terminal) Wait() error               { return errUnsupported }
func (*Terminal) Kill() error               { return errUnsupported }
func (*Terminal) Close() error              { return errUnsupported }
