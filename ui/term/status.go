package term

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
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
func (c *client) status(v wire.View, x, w int) {
	y := v.Y + v.H - 1
	// Every status line has the surface ground. Another window's has muted
	// text, and no mode.
	ground := base.Background(th.surface)
	if !v.Current {
		ground = ground.Foreground(th.muted)
	}
	c.s.FillArea(x, y, w, 1, ' ', ground)
	var left, right []part
	if v.Current {
		label, colour := c.mode()
		left = append(left, part{" " + label + " ", ground.Background(colour).Foreground(th.bg).Bold(true)})
		if r := c.e.Recording(); r != 0 {
			left = append(left, part{" ● @" + string(r), ground.Foreground(th.err).Bold(true)})
		}
	}
	dir, name := splitName(v.Name)
	left = append(left, part{" " + dir, ground.Foreground(th.faint)}, part{name, ground.Bold(v.Current)})
	if v.Modified {
		left = append(left, part{" ●", ground.Foreground(th.cmd)})
	}
	if v.Branch != "" {
		left = append(left, part{"   " + v.Branch, ground.Foreground(th.muted)})
	}
	if v.Errors > 0 {
		left = append(left, part{"   " + strconv.Itoa(v.Errors), ground.Foreground(th.err)})
	}
	if v.Warnings > 0 {
		left = append(left, part{"   " + strconv.Itoa(v.Warnings), ground.Foreground(th.warn)})
	}
	muted := ground.Foreground(th.muted)
	if v.Sharing != "" {
		right = append(right, part{v.Sharing + "  ", ground.Foreground(th.cmd)})
	}
	// The other sessions in the buffer, each in its colour (B253).
	for _, o := range v.Others {
		right = append(right, part{o.Name + "  ", ground.Foreground(presence(o.Colour))})
	}
	for _, s := range v.Segments {
		right = append(right, part{s + "  ", muted})
	}
	if v.Current {
		if shown := c.e.Shown(); shown != "" {
			right = append(right, part{shown + "  ", muted})
		}
		// Tasks that run turn a spinner before their names (B231).
		if len(v.Tasks) > 0 {
			frame := spinner[time.Now().UnixMilli()/spinnerFrame.Milliseconds()%int64(len(spinner))]
			right = append(right, part{string(frame) + " ", ground.Foreground(th.normal)}, part{strings.Join(v.Tasks, ", ") + "  ", muted})
			c.spinning = true
		}
	}
	// The buffer's language server goes before the filetype. While it
	// works, a spinner and what it is doing go with it (B161).
	if v.Progress != "" {
		frame := spinner[time.Now().UnixMilli()/spinnerFrame.Milliseconds()%int64(len(spinner))]
		doing := v.Progress
		if r := []rune(doing); len(r) > 32 {
			doing = string(r[:31]) + "…"
		}
		right = append(right, part{string(frame) + " ", ground.Foreground(th.normal)}, part{v.Server + " " + doing + "  ", muted})
		c.spinning = true
	} else if v.Server != "" {
		right = append(right, part{v.Server + "  ", muted})
	}
	if v.Filetype != "" {
		right = append(right, part{v.Filetype + "  ", muted})
	}
	right = append(right, part{ruler(v.Ruler) + " ", muted})
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

// spinner is the frames of the spinner a busy language server shows, one
// each spinnerFrame.
var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

const spinnerFrame = 80 * time.Millisecond

// mode is the current window's mode as the status line names it, and its
// colour.
func (c *client) mode() (string, color.Color) {
	if c.e.Terminal() {
		return "TERMINAL", th.insert
	}
	switch c.e.Mode() {
	case vim.Insert:
		return "INSERT", th.insert
	case vim.Replace:
		return "REPLACE", th.err
	case vim.Cmdline:
		return "COMMAND", th.cmd
	case vim.Visual:
		sel, _ := c.e.Selection()
		return map[vim.Key]string{"v": "VISUAL", "V": "V-LINE", "<C-v>": "V-BLOCK"}[sel.Kind], th.visual
	}
	return "NORMAL", th.normal
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

// splitName splits a buffer's name into its directory, with a trailing
// slash and the home directory as ~, and the rest. A listing's name keeps
// its own trailing slash.
func splitName(name string) (string, string) {
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(name, home+"/"); ok {
			name = "~/" + rest
		}
	}
	i := strings.LastIndexByte(strings.TrimSuffix(name, "/"), '/')
	return name[:i+1], name[i+1:]
}

// ruler shows a Vim ruler, "21,12-13  48%", as the status line does:
// "21:13  48%", with the screen column when it differs.
func ruler(r string) string {
	f := strings.Fields(r)
	if len(f) < 2 {
		return r
	}
	line, col, _ := strings.Cut(f[0], ",")
	if _, screen, ok := strings.Cut(col, "-"); ok {
		col = screen
	}
	return line + ":" + col + "  " + f[1]
}

// width is how many cells ps take.
func width(ps []part) int {
	n := 0
	for _, p := range ps {
		n += displaywidth.String(p.text)
	}
	return n
}

// bufferLine draws tabs on the top row, the part around the current one
// when they do not fit, with ‹ or › where it is cut (B258).
func (c *client) bufferLine(tabs []wire.Tab, w int) {
	if len(tabs) == 0 {
		return
	}
	ground := base.Background(th.surface)
	c.s.FillArea(0, 0, w, 1, ' ', ground)
	parts := make([]part, len(tabs))
	current := 0
	for i, t := range tabs {
		text, style := " "+strconv.Itoa(t.Number)+" "+t.Name+" ", ground.Foreground(th.muted)
		if t.Modified {
			text += "● "
		}
		if t.Current {
			current, style = i, ground.Background(th.raised).Foreground(th.text).Bold(true)
		}
		parts[i] = part{text, style}
	}
	// first is the first tab shown, the least that keeps the current one in
	// view with room for the marks at the cuts.
	first := 0
	for first < current && width(parts[first:current+1])+2 > w {
		first++
	}
	x := 0
	if first > 0 {
		x = c.putClipped(x, 0, w, part{"‹", ground.Foreground(th.faint)})
	}
	for i, p := range parts[first:] {
		if i > 0 && x+width(parts[first+i:first+i+1])+1 > w {
			c.putClipped(w-1, 0, w, part{"›", ground.Foreground(th.faint)})
			return
		}
		x = c.putClipped(x, 0, w, p)
	}
}
