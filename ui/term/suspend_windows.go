package term

import "github.com/gdamore/tcell/v3"

// suspend does nothing, as Windows has no job control (B424).
func suspend(tcell.Screen) error { return nil }
