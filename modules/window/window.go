// Package window moves to the window that way, or inside tmux, with no
// window that way, to tmux's pane there, as nvim-tmux-navigation does
// (B140). It is also :split, :vsplit, :new, :vnew, :only, :resize,
// :vertical, :hide and :window, the <C-w> keys that size a window, and
// <C-w>n (B91, B193, B365, B370, B422).
package window

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	splits "github.com/y3owk1n/fude/internal/split"
)

// dirs are the ways it moves, by the ids Call runs them by, with tmux's
// flag for each.
var dirs = []struct{ name, dir, flag, help string }{
	{"left", "h", "-L", "goes to the window, or tmux pane, to the left"},
	{"down", "j", "-D", "goes to the window, or tmux pane, below"},
	{"up", "k", "-U", "goes to the window, or tmux pane, above"},
	{"right", "l", "-R", "goes to the window, or tmux pane, to the right"},
}

// What window registers besides its moves, by the ids Call runs them by.
const (
	split uint32 = iota + 100
	vsplit
	below
	right
	only
	resize
	vertical
	taller
	shorter
	wider
	narrower
	tallest
	tallestToo
	widest
	hide
	place
	zoom
	newCtrl
)

// ways are where :window puts a window (B365), and zoom (B370).
var ways = []string{"float", "above", "below", "left", "right", "zoom"}

// sizes are the <C-w> keys that size the current window, by their ids:
// each changes its rows or columns by the count, or makes it the count
// tall or wide, or as large as it can be with none (B193).
var sizes = []struct {
	id         uint32
	name, keys string
	do         string
	by         int // the count's sign for a change, 0 for a size
	help       string
}{
	{taller, "taller", "<C-w>+", "taller", 1, "makes the window taller, by the count"},
	{shorter, "shorter", "<C-w>-", "taller", -1, "makes the window shorter, by the count"},
	{wider, "wider", "<C-w>>", "wider", 1, "makes the window wider, by the count"},
	{narrower, "narrower", "<C-w><", "wider", -1, "makes the window narrower, by the count"},
	{tallest, "tallest", "<C-w>_", "height", 0, "makes the window the count tall, or as tall as it can be"},
	{tallestToo, "tallest.ctrl", "<C-w><C-_>", "height", 0, "makes the window the count tall, or as tall as it can be"},
	{widest, "widest", "<C-w>|", "width", 0, "makes the window the count wide, or as wide as it can be"},
}

// Module is window.
type Module struct{}

// New returns window.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "window" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	regs := make([]api.Registration, len(dirs))
	for i, d := range dirs {
		regs[i] = api.Registration{Kind: api.Plug, Name: d.name, Help: d.help, ID: uint32(i + 1)}
	}
	regs = append(regs,
		api.Registration{Kind: api.Command, Name: "split", Short: []string{"sp"}, Args: "[file]", Paths: true, Help: "opens a window below, then opens file in it", ID: split},
		api.Registration{Kind: api.Command, Name: "vsplit", Short: []string{"vs"}, Args: "[file]", Paths: true, Help: "opens a window to the right, then opens file in it", ID: vsplit},
		api.Registration{Kind: api.Command, Name: "new", Help: "opens a window below with a new empty buffer, or above with split_below off", ID: below},
		api.Registration{Kind: api.Command, Name: "vnew", Short: []string{"vne"}, Help: "opens a window to the right with a new empty buffer, or left with split_right off", ID: right},
		api.Registration{Kind: api.Command, Name: "only", Short: []string{"on"}, Help: "closes every other window", ID: only},
		api.Registration{Kind: api.Command, Name: "hide", Short: []string{"hid"}, Help: "hides the window, which keeps its buffer and program and shows again where it was", ID: hide},
		api.Registration{Kind: api.Command, Name: "window", Short: []string{"win"}, Args: "float|above|below|left|right [cmd] | zoom", Help: "opens the window cmd opens there, or moves the window into a float or to that edge. zoom shows the window alone, or every window again", ID: place, Complete: true},
		api.Registration{Kind: api.Plug, Name: "zoom", Keys: "<C-w>z", Help: "shows the window alone, or every window again", ID: zoom},
		api.Registration{Kind: api.Command, Name: "resize", Short: []string{"res"}, Args: "[+-]{n}", Help: "makes the window n rows tall, or n taller or shorter, or as tall as it can be", ID: resize},
		api.Registration{Kind: api.Command, Name: "vertical", Short: []string{"vert"}, Args: "{cmd} | resize [+-]{n}", Help: "runs cmd, whose new window opens right, or left with split_right off. resize makes the window n columns wide, or n wider or narrower, or as wide as it can be", ID: vertical},
		api.Registration{Kind: api.Plug, Name: "new", Keys: "<C-w>n", Help: "shows a new empty buffer below, as :new does", ID: below},
		api.Registration{Kind: api.Plug, Name: "new.ctrl", Keys: "<C-w><C-n>", Help: "shows a new empty buffer below, as :new does", ID: newCtrl},
	)
	for _, sz := range sizes {
		regs = append(regs, api.Registration{Kind: api.Plug, Name: sz.name, Keys: sz.keys, Help: sz.help, ID: sz.id})
	}
	return regs
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call moves to the window that way, or else to tmux's pane there. A
// guest's call runs no program on the host (B244).
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	for _, sz := range sizes {
		if sz.id == id {
			n := args[1]
			if sz.by != 0 {
				n = sz.by * max(n, 1)
			}
			return api.Result{Windows: []api.WindowAction{{Do: sz.do, Size: n}}}, nil
		}
	}
	if id >= split {
		return command(id, s), nil
	}
	if int(id) > len(dirs) || id == 0 {
		return api.Result{}, nil
	}
	d := dirs[id-1]
	switch {
	case s.Window(d.dir):
		return api.Result{Window: d.dir}, nil
	case os.Getenv("TMUX") == "" || s.About["power"] != "host":
		return api.Result{}, nil
	}
	return api.Result{Jobs: []api.Job{{ID: id, Argv: []string{"tmux", "select-pane", "-t", os.Getenv("TMUX_PANE"), d.flag}}}}, nil
}

// command splits, sizes or hides the current window, or closes the others,
// as its command does (B91, B193).
func command(id uint32, s api.Scope) api.Result {
	arg := strings.TrimSpace(s.About["args"])
	way := splits.Way(s, id == vsplit || id == right)
	switch id {
	case split, vsplit:
		current, _ := strconv.Atoi(s.About["buffer"])
		res := api.Result{Jump: &api.Jump{Buffer: current, Where: way}}
		if arg != "" {
			res.Ex = []string{"edit " + arg}
		}
		return res
	case below, right, newCtrl:
		return api.Result{Scratch: &api.Scratch{Where: way}}
	case only:
		return api.Result{Windows: []api.WindowAction{{Do: "only"}}}
	case hide:
		return api.Result{Windows: []api.WindowAction{{Do: "hide"}}}
	case zoom:
		return api.Result{Windows: []api.WindowAction{{Do: "zoom"}}}
	case place:
		return placed(s)
	case vertical:
		name, rest, _ := strings.Cut(arg, " ")
		switch {
		case arg == "":
			return api.Result{Error: "usage: :vertical {cmd}, or :vertical resize [+-]{n}"}
		case name == "resize" || name == "res":
			return sized(strings.TrimSpace(rest), true)
		}
		return api.Result{Ex: []string{arg}, Place: splits.Way(s, true)}
	}
	return sized(arg, false)
}

// placed opens the window the command line after the way opens that way,
// or moves the current window that way without one, as :window does.
// zoom shows the current window alone, or every window again. It
// completes the way being typed (B365, B370).
func placed(s api.Scope) api.Result {
	way, cmd, _ := strings.Cut(strings.TrimSpace(s.About["args"]), " ")
	if word, ok := s.About["complete"]; ok {
		if strings.Contains(s.About["args"], " ") {
			return api.Result{}
		}
		var res api.Result
		for _, w := range ways {
			if strings.HasPrefix(w, word) {
				res.Complete = append(res.Complete, w)
			}
		}
		return res
	}
	if !slices.Contains(ways, way) {
		return api.Result{Error: "usage: :window float|above|below|left|right [cmd], or :window zoom"}
	}
	if cmd = strings.TrimSpace(cmd); cmd == "" || way == "zoom" {
		return api.Result{Windows: []api.WindowAction{{Do: way}}}
	}
	return api.Result{Ex: []string{cmd}, Place: way}
}

// sized makes the window n rows tall, or n columns wide when across, n
// more or fewer for +n and -n, or as large as it can be for none (B193).
func sized(arg string, across bool) api.Result {
	n, err := strconv.Atoi(arg)
	if arg != "" && err != nil {
		return api.Result{Error: "not a size: " + arg}
	}
	do := "height"
	if arg != "" && (arg[0] == '+' || arg[0] == '-') {
		do = "taller"
	}
	if across {
		do = map[string]string{"height": "width", "taller": "wider"}[do]
	}
	return api.Result{Windows: []api.WindowAction{{Do: do, Size: n}}}
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback says why tmux could not move.
func (m *Module) Callback(_ context.Context, _ uint32, s api.Scope) (api.Result, error) {
	return api.Result{Error: s.About["error"]}, nil
}

// Work runs tmux.
func (m *Module) Work(ctx context.Context, j api.Job) (string, error) {
	var out bytes.Buffer
	c := exec.CommandContext(ctx, j.Argv[0], j.Argv[1:]...)
	c.Stdout, c.Stderr = &out, &out
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(j.Argv[:2], " ")+" "+j.Argv[len(j.Argv)-1], err, strings.TrimSpace(out.String()))
	}
	return "", nil
}

func (m *Module) Close(context.Context) error { return nil }
