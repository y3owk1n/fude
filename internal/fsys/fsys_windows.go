package fsys

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"
)

// open opens path with access, as create says, sharing it so that another
// process may rename or delete it meanwhile, as Unix allows.
func open(path string, access, create uint32) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	h, err := windows.CreateFile(name, access, share, nil, create, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// Open opens path to read.
func Open(path string) (*os.File, error) {
	return open(path, windows.GENERIC_READ, windows.OPEN_EXISTING)
}

// ReadFile returns what the file at path holds.
func ReadFile(path string) ([]byte, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Append opens path to write at its end, made when missing. Windows has no
// file modes for perm to set.
func Append(path string, _ fs.FileMode) (*os.File, error) {
	return open(path, windows.FILE_APPEND_DATA|windows.SYNCHRONIZE, windows.OPEN_ALWAYS)
}

// Replace renames tmp over path. Windows refuses to replace a read-only
// file, so path loses that first, and tmp carries it from its own mode.
// It also refuses while another program holds path open, so then tmp's
// bytes go into path, as Windows editors write.
func Replace(tmp, path string) error {
	fi, err := os.Stat(path)
	readonly := err == nil && fi.Mode().Perm()&0o200 == 0
	if readonly {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	err = os.Rename(tmp, path)
	if err == nil {
		return nil
	}
	b, rerr := os.ReadFile(tmp)
	if rerr != nil {
		return errors.Join(err, rerr)
	}
	if werr := os.WriteFile(path, b, 0o600); werr != nil {
		return errors.Join(err, werr)
	}
	if readonly {
		return os.Chmod(path, 0o400)
	}
	return nil
}
