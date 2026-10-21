// Package flash shows text just yanked, put, undone or redone for
// flash_time milliseconds, as undo-glow.nvim does, and a change to the
// buffer clears it sooner (B139).
package flash

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
)

// Module is flash. It keeps the flash each buffer shows.
type Module struct {
	mu     sync.Mutex
	shown  map[string][]int   // by buffer, the ids of its flash, one for each place
	timers map[uint32]flashed // by timer, the flash it ends
	ids    int
	next   uint32
}

// flashed is a flash on the buffer buf, by the id of its first place.
type flashed struct {
	buf string
	id  int
}

// New returns flash.
func New() *Module { return &Module{shown: map[string][]int{}, timers: map[uint32]flashed{}} }

func (m *Module) Name() string { return "flash" }

func (m *Module) Subscribed() []string { return []string{"text.touched", "buffer.changed"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Option, Name: "enabled", Default: "true", Help: "text just yanked, put, undone or redone shows highlighted for a moment"},
		{Kind: api.Option, Name: "time", Default: "300", Help: "the milliseconds text just yanked, put, undone or redone shows highlighted"},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event shows the text a key touched, at the cursor and each multicursor,
// in place of the flash before. A change to the buffer ends the flash.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	res := m.clear(buf)
	event, detail, _ := strings.Cut(name, " ")
	ms, _ := s.Option("flash.time")
	wait, _ := strconv.Atoi(ms)
	if on, _ := s.Option("flash.enabled"); event != "text.touched" || on != "true" {
		return res, nil
	}
	// The kind, then a start and an end for each place.
	fields := strings.Fields(detail)
	if len(fields) < 3 || len(fields)%2 == 0 {
		return res, nil
	}
	var ids []int
	for i := 1; i < len(fields); i += 2 {
		start, _ := strconv.Atoi(fields[i])
		end, _ := strconv.Atoi(fields[i+1])
		m.ids++
		ids = append(ids, m.ids)
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: start, End: min(end, s.Text.Len()), Style: "flash"})
	}
	m.next++
	m.shown[buf], m.timers[m.next] = ids, flashed{buf, ids[0]}
	res.Timers = []api.Timer{{ID: m.next, After: time.Duration(wait) * time.Millisecond}}
	return res, nil
}

// Callback ends a flash once its time is up, unless a newer one took its
// place.
func (m *Module) Callback(_ context.Context, id uint32, _ api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.timers[id]
	delete(m.timers, id)
	if ids := m.shown[f.buf]; !ok || len(ids) == 0 || ids[0] != f.id {
		return api.Result{}, nil
	}
	return m.clear(f.buf), nil
}

// clear takes the flash off buf.
func (m *Module) clear(buf string) api.Result {
	ids, ok := m.shown[buf]
	if !ok {
		return api.Result{}
	}
	delete(m.shown, buf)
	return api.Result{Undecorate: ids}
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
