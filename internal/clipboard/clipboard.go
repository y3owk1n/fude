// Package clipboard reads and writes the system clipboard: through pbcopy
// on macOS, wl-copy or xclip on Linux, and the clipboard's own calls on
// Windows.
package clipboard

import "errors"

// ErrNone is what Read and Write return on a system with no clipboard
// program.
var ErrNone = errors.New("no clipboard on this system")
