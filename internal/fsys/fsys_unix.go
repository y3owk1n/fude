//go:build !windows

package fsys

import (
	"io/fs"
	"os"
)

// Open opens path to read.
func Open(path string) (*os.File, error) { return os.Open(path) }

// ReadFile returns what the file at path holds.
func ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// Append opens path to write at its end, made with perm when missing.
func Append(path string, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, perm)
}

// Replace renames tmp over path.
func Replace(tmp, path string) error { return os.Rename(tmp, path) }
