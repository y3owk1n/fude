// Package trace keeps what fude did, for fude tail (B174).
package trace

import (
	"sync"
	"time"
)

// Event is something fude did, as fude tail shows it (B174).
type Event struct {
	// Seq counts events from 1, so a gap is events a tail missed.
	Seq int       `json:"seq"`
	At  time.Time `json:"at"`
	// Src is what did it: key, cmd, msg, frame, image, syntax, lsp, dap,
	// module, plugin, job, term, file, rpc, share or log.
	Src  string        `json:"src"`
	Text string        `json:"text"`
	Took time.Duration `json:"took,omitempty"`
}

// traceKeep is how many events fude keeps for a tail that comes later.
const traceKeep = 4096

var trace struct {
	sync.Mutex
	seq     int
	kept    [traceKeep]Event
	watches map[chan Event]bool
}

// Record keeps an event, with how long it took or 0. It is safe on any
// goroutine. With no tail watching, it only keeps the event.
func Record(src, text string, took time.Duration) {
	trace.Lock()
	defer trace.Unlock()
	trace.seq++
	ev := Event{trace.seq, time.Now(), src, text, took}
	trace.kept[ev.Seq%traceKeep] = ev
	for w := range trace.watches {
		select {
		case w <- ev:
		default: // the tail is behind, and the gap in Seq tells it
		}
	}
}

// Since records what as an event of src that took since at, with err when
// it failed.
func Since(src, what string, at time.Time, err error) {
	if err != nil {
		what += ": " + err.Error()
	}
	Record(src, what, time.Since(at))
}

// Watch returns the events kept, oldest first, and the events after them
// until stop.
func Watch() (kept []Event, next <-chan Event, stop func()) {
	trace.Lock()
	defer trace.Unlock()
	for s := max(trace.seq-traceKeep+1, 1); s <= trace.seq; s++ {
		kept = append(kept, trace.kept[s%traceKeep])
	}
	w := make(chan Event, traceKeep)
	if trace.watches == nil {
		trace.watches = map[chan Event]bool{}
	}
	trace.watches[w] = true
	return kept, w, func() {
		trace.Lock()
		defer trace.Unlock()
		delete(trace.watches, w)
	}
}
