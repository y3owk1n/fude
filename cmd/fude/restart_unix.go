//go:build !windows

package main

import "syscall"

// restart replaces this process with exe, run with argv and env.
func restart(exe string, argv, env []string) error { return syscall.Exec(exe, argv, env) }
