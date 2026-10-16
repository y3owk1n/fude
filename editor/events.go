package editor

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/plugin"
)

// event is something a plugin may subscribe to, about a buffer. detail is
// what the event's name carries after a space, such as the new mode. An
// event with no name is a callback the plugin asked for, run by its id.
type event struct {
	name, detail string
	buf          *buffer
	id           uint32
}

// listener hands one plugin the events it subscribed to, one at a time, off
// the editor loop. An event of a name and buffer it has yet to hear replaces
// the older one, so a plugin that falls behind hears the newest state only
// (B57).
type listener struct {
	p       *plugin.Plugin
	mu      sync.Mutex
	pending []event
	wake    chan struct{}
	done    chan struct{}
}

// emit tells every plugin subscribed to name of b.
func (ed *Editor) emit(name, detail string, b *buffer) {
	for _, l := range ed.listeners {
		if !slices.Contains(l.p.Subscribed, name) {
			continue
		}
		ev := event{name: name, detail: detail, buf: b}
		l.mu.Lock()
		if i := slices.IndexFunc(l.pending, func(o event) bool { return o.name == name && o.buf == b }); i >= 0 {
			l.pending[i] = ev
		} else {
			l.pending = append(l.pending, ev)
		}
		l.mu.Unlock()
		select {
		case l.wake <- struct{}{}:
		default: // it is already awake
		}
	}
}

// add puts ev last in what l hands its plugin, as a callback is.
func (l *listener) add(ev event) {
	l.mu.Lock()
	l.pending = append(l.pending, ev)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// listen returns p's listener, starting it the first time, which hands p
// the events it subscribed to and the callbacks it asked for.
func (e *Session) listen(p *plugin.Plugin) *listener {
	if l := e.listeners[p]; l != nil {
		return l
	}
	l := &listener{p: p, wake: make(chan struct{}, 1), done: make(chan struct{})}
	e.listeners[p] = l
	// on runs f on the editor loop and waits for it, or reports false once
	// the plugin is unloaded.
	on := func(f func()) bool {
		ran := make(chan struct{})
		select {
		case e.queue <- func() { f(); close(ran) }:
		case <-l.done:
			return false
		}
		select {
		case <-ran:
			return true
		case <-l.done:
			return false
		}
	}
	go func() {
		for {
			select {
			case <-l.wake:
			case <-l.done:
				return
			}
			for {
				// The loop hands over the next event and its text when the
				// plugin is free to hear it, so both are the newest.
				var ev event
				var t kernel.Snapshot
				var about plugin.Context
				more := false
				if !on(func() {
					l.mu.Lock()
					defer l.mu.Unlock()
					// Drop what is about a buffer closed since.
					for len(l.pending) > 0 && !slices.Contains(e.bufs, l.pending[0].buf) {
						l.pending = l.pending[1:]
					}
					if more = len(l.pending) > 0; more {
						ev, l.pending = l.pending[0], l.pending[1:]
						t, about = ev.buf.text.Snapshot(), e.about(ev.buf)
					}
				}) {
					return
				}
				if !more {
					break
				}
				name := ev.name
				if ev.detail != "" {
					name += " " + ev.detail
				}
				at := time.Now()
				var res plugin.Result
				var err error
				if ev.name == "" {
					res, err = p.Callback(context.Background(), ev.id, t, about)
					api.Trace("plugin", p.Name+" ran a callback", time.Since(at))
				} else {
					res, err = p.Event(context.Background(), name, t, about)
					api.Trace("plugin", p.Name+" heard "+name, time.Since(at))
				}
				if !on(func() { e.handled(p, err, ev.buf, t, res) }) {
					return
				}
			}
		}
	}()
	return l
}

// handled does what p asked for when it heard of b at t. Its edits and
// decorations apply only to the text it read, and an event about the newer
// text follows when it changed since.
func (e *Session) handled(p *plugin.Plugin, err error, b *buffer, t kernel.Snapshot, res plugin.Result) {
	switch {
	case !slices.Contains(e.plugins, p):
	case err != nil:
		e.unload(p, err)
	case b.text.Snapshot().Version() == t.Version():
		e.result(p, b, t, res)
	}
}
