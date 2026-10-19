// Package pty runs a program in a pseudo-terminal, as :terminal does
// (B205).
package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Terminal is a program running in a pseudo console, Windows' kind of
// pseudo-terminal. Reading it reads what the program writes, and writing it
// types to the program.
type Terminal struct {
	in, out *os.File
	console windows.Handle
	proc    *os.Process
	closed  sync.Once
}

// Start runs cmd in a new pseudo console cols by rows, with the console as
// its stdin, stdout and stderr.
func Start(cmd *exec.Cmd, cols, rows int) (*Terminal, error) {
	if cmd.Err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, cmd.Err)
	}
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("open a pseudo console: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("open a pseudo console: %w", err)
	}
	t := &Terminal{in: os.NewFile(uintptr(inWrite), "console input"), out: os.NewFile(uintptr(outRead), "console output")}
	err := windows.CreatePseudoConsole(size(cols, rows), inRead, outWrite, 0, &t.console)
	// The console keeps its own copies of its ends of the pipes.
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	if err != nil {
		t.in.Close()
		t.out.Close()
		return nil, fmt.Errorf("open a pseudo console: %w", err)
	}
	if err := t.spawn(cmd); err != nil {
		t.Close()
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	return t, nil
}

// spawn starts cmd attached to t's console.
func (t *Terminal) spawn(cmd *exec.Cmd) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	// The attribute's value is the console's handle itself, which is no Go
	// memory, though go vet cannot know that of a plain conversion.
	console := *(*unsafe.Pointer)(unsafe.Pointer(&t.console))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, console, unsafe.Sizeof(t.console)); err != nil {
		return err
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// No standard handles, so the program takes the console's, not fude's.
	si.Flags = windows.STARTF_USESTDHANDLES
	app, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return err
	}
	var dir, env *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return err
		}
	}
	if cmd.Env != nil {
		if env, err = envBlock(cmd.Env); err != nil {
			return err
		}
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(app, line, nil, nil, false, flags, env, dir, &si.StartupInfo, &pi); err != nil {
		return err
	}
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)
	t.proc, err = os.FindProcess(int(pi.ProcessId))
	return err
}

// envBlock is env as CreateProcess takes it: each entry ends in a NUL, and
// a NUL follows the last. A later entry for a name wins, and names ignore
// case, as Windows reads them.
func envBlock(env []string) (*uint16, error) {
	var kept []string
	at := map[string]int{}
	for _, kv := range env {
		// A name may start with =, as Windows' =C: does.
		name, _, _ := strings.Cut(kv[min(1, len(kv)):], "=")
		name = strings.ToUpper(kv[:min(1, len(kv))] + name)
		if i, ok := at[name]; ok {
			kept[i] = kv
			continue
		}
		at[name] = len(kept)
		kept = append(kept, kv)
	}
	var b []uint16
	for _, kv := range kept {
		u, err := windows.UTF16FromString(kv)
		if err != nil {
			return nil, fmt.Errorf("environment %q: %w", kv, err)
		}
		b = append(b, u...)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	b = append(b, 0)
	return &b[0], nil
}

// size is cols by rows as a console takes it.
func size(cols, rows int) windows.Coord {
	return windows.Coord{X: int16(cols), Y: int16(rows)}
}

func (t *Terminal) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *Terminal) Write(p []byte) (int, error) { return t.in.Write(p) }

// Resize tells the program that its terminal is cols by rows.
func (t *Terminal) Resize(cols, rows int) error {
	if err := windows.ResizePseudoConsole(t.console, size(cols, rows)); err != nil {
		return fmt.Errorf("resize the pseudo console: %w", err)
	}
	return nil
}

// Wait waits for the program to end, as exec.Cmd's Wait does. The console
// holds its output open until it closes, so it closes then, and reading
// reaches the end of what the program wrote.
func (t *Terminal) Wait() error {
	state, err := t.proc.Wait()
	t.closeConsole()
	switch {
	case err != nil:
		return err
	case !state.Success():
		return &exec.ExitError{ProcessState: state}
	}
	return nil
}

// Kill ends the program.
func (t *Terminal) Kill() error { return t.proc.Kill() }

// Close closes the pseudo console. Closing a console waits until what it
// wrote is read, and the reader may wait on the caller, so the pipes close
// first and nothing is left to read.
func (t *Terminal) Close() error {
	err := errors.Join(t.in.Close(), t.out.Close())
	t.closeConsole()
	return err
}

// closeConsole closes the console once.
func (t *Terminal) closeConsole() {
	t.closed.Do(func() { windows.ClosePseudoConsole(t.console) })
}
