//go:build !darwin && !linux

package gui

import (
	"errors"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/theme"
)

// Run says the window is for macOS and Linux only (B547, B566).
func Run(client.Session) error { return errors.New("--gui runs only on macOS and Linux for now") }

// native is a window's system objects, none until the system has a window.
type native struct{}

func onMain(fn func(), _ bool)                      { fn() }
func (w *window) apply()                            {}
func (w *window) close()                            {}
func fill(string, rect)                             {}
func (r run) size() float64                         { return 0 }
func (r run) draw(x, _, _ float64) float64          { return x }
func toastSize(string, float64) size                { return size{} }
func (l *list) natural() float64                    { return 0 }
func fillRGB(theme.RGB, rect)                       {}
func drawString(string, theme.Style, string, point) {}
func wavy(theme.RGB, float64, float64, float64)     {}
func fillRounded(string, rect, float64)             {}
func drawToast(string, rect)                        {}
func drawPrompt(string, rect)                       {}
func (l *list) draw(float64)                        {}
func (w *window) drawPics([]pic, point)             {}
