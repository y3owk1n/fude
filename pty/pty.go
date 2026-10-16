//go:build darwin || linux

// Package pty runs a program in a pseudo-terminal, as :terminal does
// (B205).
package pty

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// Start runs cmd in a new pseudo-terminal cols by rows, as the leader of
// its own session with the terminal as its stdin, stdout and stderr. It
// returns the side fude reads the program's output from and writes its
// input to.
func Start(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	master, name, err := open()
	if err != nil {
		return nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}
	slave, err := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer slave.Close()
	// macOS sizes a pseudo-terminal only once its slave is open.
	if err := Resize(master, cols, rows); err != nil {
		master.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	return master, nil
}

// Resize tells the program in the pseudo-terminal f that it is cols by rows.
func Resize(f *os.File, cols, rows int) error {
	ws := &unix.Winsize{Col: uint16(cols), Row: uint16(rows)}
	if err := unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("resize the pseudo-terminal: %w", err)
	}
	return nil
}
