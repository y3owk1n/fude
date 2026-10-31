package plugin_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/plugin"
)

// held is a text of three bytes whose first Len waits for release.
type held struct {
	entered, release chan struct{}
	once             sync.Once
}

func (h *held) Len() int {
	h.once.Do(func() {
		close(h.entered)
		<-h.release
	})
	return 3
}

func (h *held) Slice(start, end int) string { return "abc"[start:end] }
func (h *held) Lines() int                  { return 1 }
func (h *held) LineStart(int) int           { return 0 }
func (h *held) LineOf(int) int              { return 0 }

// B57: a key waits for the event its plugin handles, within its budget.
func TestB57KeyWaitsForAnEvent(t *testing.T) {
	ctx := context.Background()
	h := &held{entered: make(chan struct{}), release: make(chan struct{})}
	// The event ends once the key waits for it.
	var waited sync.Once
	trace := func(text string, _ time.Duration) {
		if strings.Contains(text, "waits for the call running") {
			waited.Do(func() { close(h.release) })
		}
	}
	p, err := plugin.Load(ctx, filepath.Join("testdata", "listen", "listen.wasm"), plugin.Grants{Capabilities: []string{"buffer.read", "ui"}}, "", t.TempDir(), func(string) {}, trace)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)
	if _, err := p.Init(ctx, api.Scope{}); err != nil {
		t.Fatal(err)
	}
	heard := make(chan error, 1)
	go func() {
		_, err := p.Event(ctx, "buffer.changed 0 3", api.Scope{Text: h})
		heard <- err
	}()
	<-h.entered
	i := slices.IndexFunc(p.Registered(), func(r api.Registration) bool { return r.Name == "ping" })
	if _, err := p.Call(ctx, p.Registered()[i].ID, api.Scope{Text: h}, 0, 0); err != nil {
		t.Errorf("gZ while the plugin heard an event: %v", err)
	}
	waited.Do(func() { close(h.release) })
	if err := <-heard; err != nil {
		t.Fatal(err)
	}
}
