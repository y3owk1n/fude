// Package history opens the command lines or searches entered before in a
// window to edit, as q:, q/ and q? do, where <CR> runs the line under the
// cursor (B371).
package history

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// What history registers, by the ids its calls run them by.
const (
	commands uint32 = iota + 1
	forward
	back
	run
)

// prompts are what each list's lines run after, by the id that opens it.
var prompts = map[uint32]string{commands: ":", forward: "/", back: "?"}

// Module is history.
type Module struct {
	mu     sync.Mutex
	prompt string // what the open window's lines run after
}

// New returns history.
func New() *Module { return &Module{} }

func (m *Module) Name() string         { return "history" }
func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Plug, Name: "commands", Keys: "q:", Help: "lists the command lines entered, to edit and run one", ID: commands},
		{Kind: api.Plug, Name: "searches", Keys: "q/", Help: "lists the searches, to edit and search forward for one", ID: forward},
		{Kind: api.Plug, Name: "searches_back", Keys: "q?", Help: "lists the searches, to edit and search back for one", ID: back},
		{Kind: api.Plug, Name: "run", Keys: "<CR>", In: "history", Help: "runs the line under the cursor in the window the list opened from", ID: run},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call opens a window seven lines tall that lists the lines entered, the
// newest last, then an empty line with the cursor on it. <CR> there closes
// the window and types the line under the cursor after its prompt.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != run {
		m.prompt = prompts[id]
		kept := "/"
		if id == commands {
			kept = ":"
		}
		lines := s.History(kept)
		return api.Result{
			Scratch: &api.Scratch{Text: strings.Join(append(lines, ""), "\n"), Kind: "history", Where: "below"},
			Ex:      []string{"resize 7"},
			Keys:    "G",
		}, nil
	}
	buf, _ := strconv.Atoi(s.About["buffer"])
	if s.Buffers[buf].Kind != "history" {
		return api.Result{Message: "not in a history window"}, nil
	}
	t := s.Text
	pos, _ := strconv.Atoi(s.About["cursor"])
	line := t.LineOf(pos)
	end := t.Len()
	if line+1 < t.Lines() {
		end = t.LineStart(line+1) - 1
	}
	res := api.Result{Drop: []int{buf}, Windows: []api.WindowAction{{Do: "close"}}}
	if text := t.Slice(t.LineStart(line), end); text != "" {
		res.Keys = m.prompt + strings.ReplaceAll(text, "<", "<lt>") + "<CR>"
	}
	return res, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }
