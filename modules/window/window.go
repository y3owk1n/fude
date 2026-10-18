// Package window moves to the window that way, or inside tmux, with no
// window that way, to tmux's pane there, as nvim-tmux-navigation does
// (B140).
package window

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// dirs are the ways it moves, by the ids Call runs them by, with tmux's
// flag for each.
var dirs = []struct{ name, dir, flag, help string }{
	{"left", "h", "-L", "goes to the window, or tmux pane, to the left"},
	{"down", "j", "-D", "goes to the window, or tmux pane, below"},
	{"up", "k", "-U", "goes to the window, or tmux pane, above"},
	{"right", "l", "-R", "goes to the window, or tmux pane, to the right"},
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
	return regs
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call moves to the window that way, or else to tmux's pane there. A
// guest's call runs no program on the host (B244).
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
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

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback says why tmux could not move.
func (m *Module) Callback(_ context.Context, _ uint32, s api.Scope) (api.Result, error) {
	return api.Result{Message: s.About["error"]}, nil
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
