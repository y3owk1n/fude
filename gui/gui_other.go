//go:build !darwin && !linux && !windows

package gui

import (
	"errors"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/theme"
)

// Run says the window is for macOS, Linux and Windows only (B547, B566,
// B573).
func Run(client.Session) error {
	return errors.New("--gui runs only on macOS, Linux and Windows for now")
}

// native, paneNative and boxNative are a window's, a pane's and a box's
// system objects, none until the system has a window.
type (
	native     struct{}
	paneNative struct{}
	boxNative  struct{}
)

// linkKey is the modifier that makes the word under the mouse a link, and
// systemSize the text's size.
const (
	linkKey    = controlKey
	systemSize = 0
)

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
func (p *pane) redraw()                             {}
func sizeText(float64)                              {}
func (p *pane) pointer(bool)                        {}
func (p *pane) showBar(bool)                        {}
