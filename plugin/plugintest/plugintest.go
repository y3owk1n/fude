// Package plugintest runs a plugin's .wasm in go test as fude runs it,
// without fude. A test loads the plugin with the capabilities its
// [[plugin]] table would give it, runs a command, a key or an event against
// a text, and reads what the call asked for: its edits, decorations, status
// and the rest of api.Result. A call reads the plugin's options as its
// table starts them (B192).
package plugintest

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/plugin"
)

// Plugin is a plugin loaded for a test. Path and Filetype are the file
// and filetype of the buffer its calls are about, none by default.
type Plugin struct {
	Path, Filetype string

	t   testing.TB
	p   *plugin.Plugin
	mu  sync.Mutex
	log []string
}

// Load loads the plugin at path with g, as fude would with the table that
// grants g, and runs its init against text. It fails t when the plugin does
// not load or its init fails, and closes the plugin when t ends.
func Load(t testing.TB, path string, g plugin.Grants, text string) *Plugin {
	t.Helper()
	tp := &Plugin{t: t}
	p, err := plugin.Load(context.Background(), path, g, t.TempDir(), tp.logged, func(string, time.Duration) {})
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	tp.p = p
	t.Cleanup(func() { p.Close(context.Background()) })
	if _, err := p.Init(context.Background(), tp.scope(text, nil)); err != nil {
		t.Fatalf("init %s: %v", path, err)
	}
	return tp
}

func (p *Plugin) logged(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, line)
}

// Log returns the lines the plugin logged, and those fude logged of it, such
// as a call it was denied.
func (p *Plugin) Log() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.log)
}

// Command runs the plugin's ex command name with args against text, with
// the cursor at cursor, and returns what it asked for.
func (p *Plugin) Command(name, args, text string, cursor int) api.Result {
	p.t.Helper()
	id := p.id(api.Command, name)
	return p.call(id, p.scope(text, api.Context{"args": args, "bang": "false", "first": "0", "last": "0", "ranged": "false"}), cursor, 0)
}

// Key runs what the plugin maps keys to in normal mode, a key or a plug's
// keys, against text, with the cursor at cursor and count typed before it,
// and returns what it asked for.
func (p *Plugin) Key(keys, text string, cursor, count int) api.Result {
	p.t.Helper()
	i := slices.IndexFunc(p.p.Registered(), func(r api.Registration) bool {
		return r.Kind == api.Key && r.Name == keys || r.Kind == api.Plug && r.Keys == keys && r.In == ""
	})
	if i < 0 {
		p.t.Fatalf("the plugin maps no %s", keys)
	}
	return p.call(p.p.Registered()[i].ID, p.scope(text, nil), cursor, count)
}

// Event tells the plugin of the event name, such as buffer.changed, about
// text, and returns what it asked for. The name may carry its detail after
// a space, as fude sends it, such as "buffer.changed 0 3".
func (p *Plugin) Event(name, text string) api.Result {
	p.t.Helper()
	res, err := p.p.Event(context.Background(), name, p.scope(text, nil))
	if err != nil {
		p.t.Fatalf("event %s: %v", name, err)
	}
	return res
}

// Callback runs the callback a timer, job, request or pick asked for by id,
// against text, with what its context adds, such as a job's output.
func (p *Plugin) Callback(id uint32, text string, extra api.Context) api.Result {
	p.t.Helper()
	res, err := p.p.Callback(context.Background(), id, p.scope(text, extra))
	if err != nil {
		p.t.Fatalf("callback %d: %v", id, err)
	}
	return res
}

// id returns the id the plugin registered name of kind under, or fails.
func (p *Plugin) id(kind api.Kind, name string) uint32 {
	p.t.Helper()
	i := slices.IndexFunc(p.p.Registered(), func(r api.Registration) bool { return r.Kind == kind && r.Name == name })
	if i < 0 {
		p.t.Fatalf("the plugin registered no %s", name)
	}
	return p.p.Registered()[i].ID
}

// call runs what the plugin registered as id with args.
func (p *Plugin) call(id uint32, s api.Scope, args ...int) api.Result {
	p.t.Helper()
	res, err := p.p.Call(context.Background(), id, s, args...)
	if err != nil {
		p.t.Fatalf("call: %v", err)
	}
	return res
}

// Apply returns text with the edits res asks for made.
func Apply(text string, res api.Result) string {
	edits := slices.Clone(res.Edits)
	slices.SortStableFunc(edits, func(a, b api.Edit) int { return b.Start - a.Start })
	for _, e := range edits {
		text = text[:e.Start] + e.Text + text[e.End:]
	}
	return text
}

// scope is what a call against text reads, as the only open buffer, with
// what extra adds to its context.
func (p *Plugin) scope(text string, extra api.Context) api.Scope {
	about := api.Context{"buffer": "1", "path": p.Path, "filetype": p.Filetype, "mode": "normal"}
	for k, v := range extra {
		about[k] = v
	}
	t := str(text)
	return api.Scope{Text: t, About: about, Buffers: map[int]api.Buffer{1: {Path: p.Path, Filetype: p.Filetype, Text: t}}, Option: p.option}
}

// option returns the plugin's option name as its table or its default
// starts it, as fude has it before a :set (B377).
func (p *Plugin) option(name string) (string, bool) {
	short, own := strings.CutPrefix(name, p.p.Name()+".")
	if !own {
		return "", false
	}
	if v, ok := p.p.Options[short]; ok {
		return v, true
	}
	i := slices.IndexFunc(p.p.Registered(), func(r api.Registration) bool { return r.Kind == api.Option && r.Name == short })
	if i < 0 {
		return "", false
	}
	return p.p.Registered()[i].Default, true
}

// str is a text held in a string.
type str string

func (s str) Len() int                    { return len(s) }
func (s str) Slice(start, end int) string { return string(s[start:end]) }
func (s str) Lines() int                  { return strings.Count(string(s), "\n") + 1 }

func (s str) LineStart(line int) int {
	at := 0
	for range line {
		i := strings.IndexByte(string(s[at:]), '\n')
		if i < 0 {
			return len(s)
		}
		at += i + 1
	}
	return at
}

func (s str) LineOf(pos int) int { return strings.Count(string(s[:pos]), "\n") }
