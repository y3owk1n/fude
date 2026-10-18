package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/y3owk1n/fude/api"
)

// sessionJSON is the windows on files and listings, their cursors and
// options, and which is current, as JSON, or "" with no such window (B226,
// B325).
func (e *Session) sessionJSON() string {
	s := e.session()
	if !s.Windows {
		return ""
	}
	data, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(data)
}

// restoreFrom opens the files of the session data holds and builds its
// windows, each with its cursor and options. A file that no longer opens is
// left out, and the message says why (B226, B325).
func (e *Session) restoreFrom(data string) {
	var s saved
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		e.message = fmt.Sprintf("session: %v", err)
		return
	}
	var problems []error
	wd, _ := os.Getwd()
	for _, sb := range s.Buffers {
		if !slices.ContainsFunc(e.bufs, func(b *buffer) bool { return b.is(sb.Path) }) {
			// A file under the working directory opens by its relative path,
			// as typed.
			path := sb.Path
			if rel, ok := under(wd, path); ok {
				path = rel
			}
			if _, err := e.add(path); err != nil {
				problems = append(problems, err)
			}
		}
	}
	e.restoreLayout(s)
	if err := errors.Join(problems...); err != nil {
		e.message = err.Error()
	}
}

// onLoop gives scope what a module reads only while its call runs on the
// loop: the registry, the session and the states of the text (B324, B325,
// B328).
func (e *Session) onLoop(scope *api.Scope) {
	scope.Keys, scope.Commands, scope.HelpTags, scope.Session = e.keyList, e.commandList, e.helpTags, e.sessionJSON
	scope.States = func(id int) []api.State {
		if b, err := e.buffer(id); err == nil {
			return b.stateList()
		}
		return nil
	}
	e.lists(scope)
	scope.Sessions = func() []api.Peer {
		var out []api.Peer
		for _, s := range e.sessions {
			out = append(out, api.Peer{Name: s.label(), Power: powers[s.power], You: s == e})
		}
		return out
	}
	scope.Window = func(dir string) bool { return len(dir) == 1 && e.beside(dir[0]) != nil }
	scope.StateText = func(id, i int) (string, bool) {
		if b, err := e.buffer(id); err == nil {
			return b.stateText(i)
		}
		return "", false
	}
}

// quitting tells the modules that fude quits. It runs on the loop, before
// their listeners stop, so a module can keep the session then. What they
// log goes to the log (B325).
func (e *Session) quitting() {
	for _, p := range e.modules {
		if !slices.Contains(p.Subscribed(), "editor.quitting") {
			continue
		}
		scope := e.scope(p, e.win.buf, e.Text())
		scope.Session = e.sessionJSON
		res, err := p.Event(context.Background(), "editor.quitting", scope)
		scope.Done()
		if err != nil {
			logLine(fmt.Sprintf("%s: %v", p.Name(), err))
		}
		for _, line := range res.Log {
			logLine(line)
		}
	}
}

// lists gives scope the current buffer's marks and changes, the window's
// jumps, the messages and the registers, on the loop (B329).
func (e *Session) lists(scope *api.Scope) {
	b, w := e.win.buf, e.win
	scope.Marks = func() []api.Mark {
		var out []api.Mark
		for _, name := range slices.Sorted(maps.Keys(b.marks)) {
			out = append(out, api.Mark{Name: string(name), Buffer: b.id, Pos: b.text.Mark(b.marks[name])})
		}
		return out
	}
	scope.Jumps = func() []api.Mark {
		var out []api.Mark
		for _, j := range w.jumps {
			out = append(out, api.Mark{Buffer: j.buf.id, Pos: j.buf.text.Mark(j.mark)})
		}
		return out
	}
	scope.Changes = func() []api.Mark {
		var out []api.Mark
		for _, id := range b.changes {
			out = append(out, api.Mark{Buffer: b.id, Pos: b.text.Mark(id)})
		}
		return out
	}
	scope.Messages = func() []string { return slices.Clone(e.history) }
	scope.Register = e.vim.Register
}
