// Package gui is fude's native window, a frontend of client.Session. Its
// plain files run the session and turn it into frames of cells, from
// frontend/compose, and each system's files draw them in its own window:
// AppKit on macOS and GTK 4 on Linux, through purego with no cgo (D65,
// D66), and Win32 on Windows to come.
package gui
