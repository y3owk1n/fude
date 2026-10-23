// Package gui is fude's native window, a frontend of client.Session. Its
// plain files run the session and turn it into frames of cells, from
// frontend/compose, and each system's files draw them in its own window:
// AppKit on macOS, GTK 4 on Linux and Win32 with Direct2D on Windows, with
// no cgo (D65, D66).
package gui
