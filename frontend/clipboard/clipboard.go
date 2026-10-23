// Package clipboard reads and writes the system clipboard: through pbcopy
// on macOS, wl-copy or xclip on Linux, and the clipboard's own calls on
// Windows.
package clipboard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
)

// ErrNone is what Read and Write return on a system with no clipboard
// program.
var ErrNone = errors.New("no clipboard on this system")

// The file a test uses as the clipboard while faking it, "" for none.
var (
	fakeMu sync.Mutex
	faking bool
	fake   string
)

// Fake makes the file at path the clipboard, or with path "" leaves no
// clipboard, until the returned func restores the system's. Tests use it,
// as no program stands in for the clipboard on Windows.
func Fake(path string) (restore func()) {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	faking, fake = true, path
	return func() {
		fakeMu.Lock()
		defer fakeMu.Unlock()
		faking, fake = false, ""
	}
}

// faked returns the file a test fakes the clipboard with, and whether one
// does.
func faked() (string, bool) {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	return fake, faking
}

// Read returns the clipboard's text.
func Read() (string, error) {
	path, ok := faked()
	switch {
	case !ok:
		return read()
	case path == "":
		return "", ErrNone
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read the clipboard: %w", err)
	}
	return string(b), nil
}

// Write puts text on the clipboard.
func Write(text string) error {
	path, ok := faked()
	switch {
	case !ok:
		return write(text)
	case path == "":
		return ErrNone
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return fmt.Errorf("write the clipboard: %w", err)
	}
	return nil
}
