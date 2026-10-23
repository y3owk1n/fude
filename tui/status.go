package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	fudeclient "github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/wire"
)

// part is a run of a status line, in its style.
type part struct {
	text  string
	style tcell.Style
}

// status draws v's status line on its bottom row (B147). On the left: the
// mode in its colour, for the current window, the file's name after its
// directory, dim, a dot while it is modified, the git branch and the
// diagnostics. On the right: plugins' segments, the keys being typed, the
// filetype and the ruler. The right goes first when the line is too narrow.
func (c *client) status(v wire.Window, x, w int) {
	y := v.Y + v.H
	// Every status line has the surface ground. Another window's has muted
	// text, and no mode.
	ground := base.Background(th.surface)
	if !v.Current {
		ground = ground.Foreground(th.muted)
	}
	c.s.FillArea(x, y, w, 1, ' ', ground)
	left, right := c.statusParts(v, ground, true)
	c.putParts(left, right, x, y, w)
}

// putParts draws left from x and right to x+w on row y, and left alone when
// both do not fit.
func (c *client) putParts(left, right []part, x, y, w int) {
	if width(left)+1+width(right) > w {
		right = nil
	}
	at := x
	for _, p := range left {
		at = c.putClipped(at, y, x+w, p)
	}
	at = x + w - width(right)
	for _, p := range right {
		at = c.putClipped(at, y, x+w, p)
	}
}

// statusParts are what v's status line shows on its left and on its right,
// in styles over ground. They hold v's name when named is set.
func (c *client) statusParts(v wire.Window, ground tcell.Style, named bool) (left, right []part) {
	l, r := fudeclient.StatusParts(c.e, v, named)
	for _, p := range l {
		left = append(left, c.statusPart(p, ground, v.Current))
	}
	for _, p := range r {
		right = append(right, c.statusPart(p, ground, v.Current))
	}
	return left, right
}

// statusPart is p as the terminal draws it: its text with the spaces its
// role puts around it, in a style over ground.
func (c *client) statusPart(p fudeclient.Part, ground tcell.Style, current bool) part {
	if kind, ok := strings.CutPrefix(p.Role, "mode."); ok {
		return part{" " + p.Text + " ", ground.Background(modeColour(kind)).Foreground(th.bg).Bold(true)}
	}
	switch p.Role {
	case "recording":
		return part{" ● " + p.Text, ground.Foreground(th.err).Bold(true)}
	case "dir":
		return part{" " + p.Text, ground.Foreground(th.faint)}
	case "name":
		return part{p.Text, ground.Bold(current)}
	case "modified":
		return part{" ●", ground.Foreground(th.cmd)}
	case "segment":
		return part{"   " + p.Text, ground.Foreground(th.muted)}
	case "errors":
		return part{"   " + p.Text, ground.Foreground(th.err)}
	case "warnings":
		return part{"   " + p.Text, ground.Foreground(th.warn)}
	case "sharing", "zoomed":
		return part{p.Text + "  ", ground.Foreground(th.cmd)}
	case "other":
		return part{p.Text + "  ", ground.Foreground(presence(p.Colour))}
	case "busy":
		c.spinning = true
		frame := spinner[time.Now().UnixMilli()/spinnerFrame.Milliseconds()%int64(len(spinner))]
		return part{string(frame) + " ", ground.Foreground(th.normal)}
	case "ruler":
		return part{p.Text + " ", ground.Foreground(th.muted)}
	}
	return part{p.Text + "  ", ground.Foreground(th.muted)}
}

// spinner is the frames of the spinner a busy language server shows, one
// each spinnerFrame.
var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

const spinnerFrame = 80 * time.Millisecond

// mode is the current window's mode as the status line names it, and its
// colour.
func (c *client) mode() (string, color.Color) {
	label, kind := fudeclient.ModeName(c.e)
	return label, modeColour(kind)
}

// modeColour is the colour of a mode of the kind ModeName gives.
func modeColour(kind string) color.Color {
	switch kind {
	case "insert", "terminal":
		return th.insert
	case "replace":
		return th.err
	case "command":
		return th.cmd
	case "visual":
		return th.visual
	}
	return th.normal
}

// putClipped draws p from x on row y, cut at end, and returns the column
// after it.
func (c *client) putClipped(x, y, end int, p part) int {
	for rest := p.text; rest != "" && x < end; {
		var w int
		rest, w = c.s.Put(x, y, rest, p.style)
		x += w
	}
	return x
}

// width is how many cells ps take.
func width(ps []part) int {
	n := 0
	for _, p := range ps {
		n += displaywidth.String(p.text)
	}
	return n
}

// topLine draws the buffers on the top row from its left and the tab pages
// at its right (B258, B545).
func (c *client) topLine(tabs []wire.Tab, pages []wire.Page, w int) {
	if len(tabs) == 0 && len(pages) == 0 {
		return
	}
	ground := base.Background(th.surface)
	c.s.FillArea(0, 0, w, 1, ' ', ground)
	right, at := make([]part, len(pages)), 0
	for i, p := range pages {
		_, name := fudeclient.SplitName(p.Name)
		text, style := " "+strconv.Itoa(i+1)+" "+name+" ", ground.Foreground(th.muted)
		if p.Modified {
			text += "● "
		}
		if p.Current {
			at, style = i, ground.Background(th.normal).Foreground(th.bg).Bold(true)
		}
		right[i] = part{text, style}
	}
	// The pages take up to half the row, with a blank column between them
	// and the buffers.
	end := w
	if len(right) > 0 {
		end = w - min(width(right), w/2)
		c.cut(right, at, end, w, ground)
		end--
	}
	left, current := make([]part, len(tabs)), 0
	for i, t := range tabs {
		text, style := " "+strconv.Itoa(t.Number)+" "+t.Name+" ", ground.Foreground(th.muted)
		if t.Modified {
			text += "● "
		}
		if t.Current {
			current, style = i, ground.Background(th.raised).Foreground(th.text).Bold(true)
		}
		left[i] = part{text, style}
	}
	c.cut(left, current, 0, end, ground)
}

// cut draws parts on the top row from x to end, the part around the one at
// current when they do not fit, with ‹ or › where it is cut.
func (c *client) cut(parts []part, current, x, end int, ground tcell.Style) {
	if width(parts) <= end-x {
		for _, p := range parts {
			x = c.putClipped(x, 0, end, p)
		}
		return
	}
	// first is the first part shown, the least that keeps the current one
	// in view with room for the marks at the cuts.
	first := 0
	for first < current && width(parts[first:current+1])+2 > end-x {
		first++
	}
	if first > 0 {
		x = c.putClipped(x, 0, end, part{"‹", ground.Foreground(th.faint)})
	}
	for i, p := range parts[first:] {
		if i > 0 && x+width(parts[first+i:first+i+1])+1 > end {
			c.putClipped(end-1, 0, end, part{"›", ground.Foreground(th.faint)})
			return
		}
		x = c.putClipped(x, 0, end, p)
	}
}
