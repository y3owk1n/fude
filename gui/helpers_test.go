package gui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/editor/editortest"
)

// onLoop runs fn on w's loop, where the session may be read.
func (w *window) onLoop(fn func()) {
	done := make(chan struct{})
	w.do <- func() { fn(); close(done) }
	<-done
}

// until waits for ok, failing t once it waited too long for what.
func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(editortest.Patience); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("still waiting for %s", what)
		}
	}
}

// hovering is a module whose hover says where it was asked, over more
// lines after it.
type hovering struct{ more int }

func (hovering) Name() string         { return "hovering" }
func (hovering) Subscribed() []string { return nil }
func (hovering) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Plug, Name: "hover", ID: 1}}
}
func (hovering) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }
func (h hovering) Call(_ context.Context, _ uint32, _ api.Scope, args ...int) (api.Result, error) {
	return api.Result{Float: &api.Float{Text: fmt.Sprint("at ", args[0]) + strings.Repeat("\nmore", h.more), Spans: []api.Span{}}}, nil
}

func (hovering) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (hovering) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}
func (hovering) Work(context.Context, api.Job) (string, error) { return "", nil }
func (hovering) Close(context.Context) error                   { return nil }
