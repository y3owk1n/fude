package editor

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/trace"
)

// event is something a plugin may subscribe to, about a buffer. detail is
// what the event's name carries after a space, such as the new mode. An
// event with no name is a callback the plugin asked for, run by its id.
type event struct {
	name, detail string
	buf          *buffer
	id           uint32
	extra        api.Context // what a callback's context adds, such as a job's output
	start, end   int         // the range buffer.changed covers, in the text after it
	save         *saving     // the write that waits for this event, or nil (B282)
	from         *Session    // the session whose call asked for this callback, or nil
}

// listener hands one plugin the events it subscribed to, one at a time, off
// the editor loop. An event of a name and buffer it has yet to hear replaces
// the older one, so a plugin that falls behind hears the newest state only
// (B57).
type listener struct {
	p       api.Module
	mu      sync.Mutex
	pending []event
	wake    chan struct{}
	// ctx ends when the plugin unloads, which stops its jobs.
	ctx  context.Context
	stop context.CancelFunc
}

// emit tells every plugin subscribed to name of b.
func (ed *Editor) emit(name, detail string, b *buffer) {
	ed.emitTo(event{name: name, detail: detail, buf: b})
}

// emitTo tells every plugin subscribed to ev's name of it, and returns how
// many it told.
func (ed *Editor) emitTo(ev event) int {
	told := 0
	for _, l := range ed.listeners {
		if !slices.Contains(l.p.Subscribed(), ev.name) {
			continue
		}
		l.put(ev, func(ev, _ event) event { return ev })
		told++
	}
	return told
}

// add puts ev last in what l hands its plugin, as a callback is.
func (l *listener) add(ev event) { l.put(ev, nil) }

// put adds ev to what l hands its plugin, and wakes l. With merge, a
// waiting event of the same name about the same buffer is replaced by what
// merge returns for ev and it.
func (l *listener) put(ev event, merge func(ev, old event) event) {
	l.mu.Lock()
	if i := slices.IndexFunc(l.pending, func(o event) bool { return o.name == ev.name && o.buf == ev.buf }); merge != nil && i >= 0 {
		l.pending[i] = merge(ev, l.pending[i])
	} else {
		l.pending = append(l.pending, ev)
	}
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default: // it is already awake
	}
}

// changed tells every plugin subscribed to buffer.changed of edits to b,
// with the range they cover in the text after them. An older change the
// plugin has yet to hear of moves through edits, and the range covers both
// (B183).
func (ed *Editor) changed(b *buffer, edits []kernel.Edit) {
	if len(edits) == 0 {
		return
	}
	start, end := edits[0].Start, moved(edits, edits[len(edits)-1].End)
	for _, l := range ed.listeners {
		if !slices.Contains(l.p.Subscribed(), "buffer.changed") {
			continue
		}
		l.put(event{name: "buffer.changed", buf: b, start: start, end: end}, func(ev, old event) event {
			ev.start, ev.end = min(ev.start, moved(edits, old.start)), max(ev.end, moved(edits, old.end))
			return ev
		})
	}
}

// moved returns where pos, in the text before edits, is after them. A
// position inside an edit's range goes to the end of its new text.
func moved(edits []kernel.Edit, pos int) int {
	d := 0
	for _, e := range edits {
		switch {
		case pos < e.Start:
			return pos + d
		case pos <= e.End:
			return e.Start + d + len(e.Text)
		}
		d += len(e.Text) - (e.End - e.Start)
	}
	return pos + d
}

// listen returns p's listener, starting it the first time, which hands p
// the events it subscribed to and the callbacks it asked for.
func (e *Session) listen(p api.Module) *listener {
	if l := e.listeners[p]; l != nil {
		return l
	}
	l := &listener{p: p, wake: make(chan struct{}, 1)}
	l.ctx, l.stop = context.WithCancel(context.Background())
	e.listeners[p] = l
	// on runs f on the editor loop and waits for it, or reports false once
	// the plugin is unloaded.
	on := func(f func()) bool {
		ran := make(chan struct{})
		select {
		case e.queue <- func() { f(); close(ran) }:
		case <-l.ctx.Done():
			return false
		}
		select {
		case <-ran:
			return true
		case <-l.ctx.Done():
			return false
		}
	}
	go func() {
		for {
			select {
			case <-l.wake:
			case <-l.ctx.Done():
				return
			}
			for {
				// The loop hands over the next event and its text when the
				// plugin is free to hear it, so both are the newest.
				var ev event
				var t kernel.Snapshot
				var scope api.Scope
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
						t = ev.buf.text.Snapshot()
						scope = e.scope(p, ev.buf, t)
						maps.Copy(scope.About, ev.extra)
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
				if ev.name == "buffer.changed" {
					name += fmt.Sprintf(" %d %d", ev.start, ev.end)
				}
				at := time.Now()
				var res api.Result
				var err error
				if ev.name == "" {
					res, err = p.Callback(context.Background(), ev.id, scope)
					trace.Record("plugin", p.Name()+" ran a callback", time.Since(at))
				} else {
					res, err = p.Event(context.Background(), name, scope)
					trace.Record("plugin", p.Name()+" heard "+name, time.Since(at))
				}
				scope.Done()
				// A callback runs as the session that asked for it (B247).
				s := e
				if ev.from != nil && slices.Contains(e.sessions, ev.from) {
					s = ev.from
				}
				if !on(func() { s.handled(p, ev, err, t, res) }) {
					return
				}
			}
		}
	}()
	return l
}

// handled does what p asked for when it heard ev at t. Its edits and
// decorations apply only to the text it read, and an event about the newer
// text follows when it changed since. Nothing applies to a closed buffer.
// A result for a write that ran out of waiting is dropped (B282).
func (e *Session) handled(p api.Module, ev event, err error, t kernel.Snapshot, res api.Result) {
	b := ev.buf
	if s := ev.save; s != nil {
		if s.done {
			return
		}
		e.inSave = s
		defer func() {
			e.inSave = nil
			s.handledOne(e)
		}()
	}
	switch {
	case !slices.Contains(e.modules, p):
	case err != nil:
		e.unload(p, err)
	case !slices.Contains(e.bufs, b):
	case b.text.Snapshot().Version() == t.Version():
		e.result(p, b, t, res)
		// The keys it queued, such as the finder's, keep its message.
		msg := e.message
		e.feedLater()
		e.message = cmp.Or(e.message, msg)
	default:
		e.result(p, b, t, stale(res))
	}
}

// stale returns what of res applies once the text it read has changed:
// everything but its edits, what it selected, and its decorations and a
// jump on that text (B291).
func stale(res api.Result) api.Result {
	res.Edits, res.Selected = nil, nil
	res.Decorate = slices.DeleteFunc(slices.Clone(res.Decorate), func(d api.Decoration) bool { return d.Buffer == 0 })
	if res.Jump != nil && res.Jump.Path == "" {
		res.Jump = nil
	}
	return res
}
