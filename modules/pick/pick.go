// Package pick is the :pick sources of fude's own registry: help opens help
// at a tag, keymaps lists every key and runs one picked, and commands lists
// every ex command and runs one picked (B215, B266).
package pick

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
)

// What pick registers, by the ids Call runs them by and its lists answer.
const (
	help uint32 = iota + 1
	keymaps
	commands
)

// Module is pick. It keeps what each list shows, to act on the one picked.
type Module struct {
	mu       sync.Mutex
	tags     []string
	keys     []api.Binding
	commands []api.ExCommand
}

// New returns pick.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "pick" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Source, Name: "help", Help: "what help has an entry for, which opens help there", ID: help},
		{Kind: api.Source, Name: "keymaps", Help: "every key, each with what it does, which runs one picked", ID: keymaps},
		{Kind: api.Source, Name: "commands", Help: "every ex command by what it does, which runs one picked", ID: commands},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call lists help's tags, the keys or the commands.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var items []string
	title := map[uint32]string{help: "help", keymaps: "keymaps", commands: "commands"}[id]
	switch id {
	case help:
		m.tags = s.HelpTags()
		items = m.tags
	case keymaps:
		m.keys = s.Keys()
		keysW, kindW := 0, 0
		for _, k := range m.keys {
			keysW, kindW = max(keysW, utf8.RuneCountInString(k.Keys)), max(kindW, len(k.Kind))
		}
		for _, k := range m.keys {
			items = append(items, fmt.Sprintf("%-*s  %-*s  %s", keysW, k.Keys, kindW, k.Kind, k.Help))
		}
	case commands:
		m.commands = s.Commands()
		nameW := 0
		for _, c := range m.commands {
			nameW = max(nameW, len(c.Name))
		}
		for _, c := range m.commands {
			items = append(items, fmt.Sprintf(":%-*s  %s", nameW, c.Name, c.Help))
		}
	}
	return api.Result{Pick: &api.Pick{ID: id, Title: title, Items: items}}, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback acts on the item picked. Help opens at its tag. A key normal
// mode runs, such as a mapping, a motion or an operator, runs as if typed,
// up to the argument it waits for, and any other says what it does and
// where. A command runs, or waits on the command line for the argument it
// needs.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, _ := strconv.Atoi(s.About["picked"])
	if s.About["ok"] != "true" || i < 0 {
		return api.Result{}, nil
	}
	switch {
	case id == help && i < len(m.tags):
		return api.Result{Ex: []string{"help " + m.tags[i]}}, nil
	case id == keymaps && i < len(m.keys):
		k := m.keys[i]
		typed, _, _ := strings.Cut(k.Keys, "{")
		switch k.Kind {
		case "normal mapping", "normal", "motion", "operator":
			if typed != "" {
				return api.Result{Keys: typed}, nil
			}
		}
		return api.Result{Message: k.Keys + " " + k.Help + ", in " + strings.TrimSuffix(k.Kind, " mapping")}, nil
	case id == commands && i < len(m.commands):
		// A command that needs an argument, whose arguments are not all in
		// brackets, waits on the command line for it.
		c := m.commands[i]
		if c.Args != "" && !strings.HasPrefix(c.Args, "[") {
			return api.Result{Cmdline: c.Name + " "}, nil
		}
		return api.Result{Cmdline: c.Name, Keys: "<CR>"}, nil
	}
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
