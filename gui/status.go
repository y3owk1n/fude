package gui

import (
	"strings"
	"time"

	"github.com/y3owk1n/fude/frontend/client"
)

// run is a part of a status bar or of the tab strip as it draws, its text
// bold or not in one of the system's colours, which it names as AppKit
// does and each system takes for its own. A pill run has a rounded ground
// when it has one, and a tight run has no room after it. A tab's run ends
// in a slot that shows the system's symbol slot, on slotGround when it has
// one.
type run struct {
	text        string
	bold        bool
	fg, ground  string
	pill, tight bool
	slotted     bool
	slot        string
	slotFg      string
	slotGround  string
}

// width is how wide rs draw.
func width(rs []run) float64 {
	n := 0.0
	for _, r := range rs {
		n += r.size()
	}
	return n
}

// runs are a status line's parts as the bar draws them, by their roles,
// in the system's colours, the mode in its own (B147).
func runs(parts []client.Part, current bool) []run {
	label, muted := "labelColor", "secondaryLabelColor"
	if !current {
		label, muted = "secondaryLabelColor", "tertiaryLabelColor"
	}
	var out []run
	for _, p := range parts {
		r := run{text: p.Text, fg: muted}
		if kind, ok := strings.CutPrefix(p.Role, "mode."); ok {
			r = run{text: p.Text, bold: true, fg: modeColour(kind)}
		}
		switch p.Role {
		case "recording":
			r.text, r.bold, r.fg = "● "+p.Text, true, "systemRedColor"
		case "dir":
			r.fg, r.tight = "tertiaryLabelColor", true
		case "name":
			r.fg = label
			if current {
				r.bold = true
			}
		case "modified":
			r.text, r.fg = "●", "systemOrangeColor"
		case "errors":
			r.fg = "systemRedColor"
		case "warnings":
			r.fg = "systemYellowColor"
		case "sharing", "zoomed":
			r.fg = "systemOrangeColor"
		case "busy":
			r.text, r.fg = string(spinner[time.Now().UnixMilli()/spinnerFrame.Milliseconds()%int64(len(spinner))]), "controlAccentColor"
		}
		if r.text != "" {
			out = append(out, r)
		}
	}
	return out
}

// drawParts draws a status line's parts in r, under a hairline.
func drawParts(left, right []client.Part, current bool, r rect) {
	fill("separatorColor", rect{r.Origin, size{r.Size.W, 1}})
	ls, rs := runs(left, current), runs(right, current)
	if width(ls)+width(rs)+3*pad > r.Size.W {
		rs = nil
	}
	x := r.Origin.X + pad
	for _, p := range ls {
		x = p.draw(x, r.Origin.Y, r.Size.H)
	}
	x = r.Origin.X + r.Size.W - pad - width(rs)
	for _, p := range rs {
		x = p.draw(x, r.Origin.Y, r.Size.H)
	}
}

// modeColour is the system's colour of a mode of the kind client.ModeName
// gives.
func modeColour(kind string) string {
	switch kind {
	case "insert", "terminal":
		return "systemGreenColor"
	case "replace":
		return "systemRedColor"
	case "command":
		return "systemOrangeColor"
	case "visual":
		return "systemPurpleColor"
	}
	return "systemBlueColor"
}
