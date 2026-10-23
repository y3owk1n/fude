// Package gui is fude's native window, a frontend of client.Session. Its
// plain files run the session and turn it into frames of cells, from
// frontend/compose, and each system's files draw them in its own window:
// AppKit on macOS, through purego with no cgo (D65), and GTK4 on Linux and
// Win32 on Windows to come (D66).
package gui
