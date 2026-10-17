//go:build !darwin && !linux

// Package pty runs a program in a pseudo-terminal, as :terminal does
// (B205).
package pty

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("pseudo-terminals need macOS or Linux")

// Start says pseudo-terminals are for macOS and Linux only.
func Start(*exec.Cmd, int, int) (*os.File, error) { return nil, errUnsupported }

// Resize says pseudo-terminals are for macOS and Linux only.
func Resize(*os.File, int, int) error { return errUnsupported }
