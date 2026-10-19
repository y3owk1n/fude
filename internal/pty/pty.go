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

// Terminal is a program running in a pseudo-terminal. Reading it reads
// what the program writes, and writing it types to the program.
type Terminal struct {
	f   *os.File
	cmd *exec.Cmd
}

// Start runs cmd in a new pseudo-terminal cols by rows, as the leader of
// its own session with the terminal as its stdin, stdout and stderr.
func Start(cmd *exec.Cmd, cols, rows int) (*Terminal, error) {
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
	t := &Terminal{f: master, cmd: cmd}
	// macOS sizes a pseudo-terminal only once its slave is open.
	if err := t.Resize(cols, rows); err != nil {
		master.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	return t, nil
}

func (t *Terminal) Read(p []byte) (int, error)  { return t.f.Read(p) }
func (t *Terminal) Write(p []byte) (int, error) { return t.f.Write(p) }

// Resize tells the program that its terminal is cols by rows.
func (t *Terminal) Resize(cols, rows int) error {
	ws := &unix.Winsize{Col: uint16(cols), Row: uint16(rows)}
	if err := unix.IoctlSetWinsize(int(t.f.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("resize the pseudo-terminal: %w", err)
	}
	return nil
}

// Wait waits for the program to end, as exec.Cmd's Wait does.
func (t *Terminal) Wait() error { return t.cmd.Wait() }

// Kill ends the program.
func (t *Terminal) Kill() error { return t.cmd.Process.Kill() }

// Close closes the pseudo-terminal.
func (t *Terminal) Close() error { return t.f.Close() }
