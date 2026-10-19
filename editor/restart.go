package editor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/keep"
)

// RestartEnv names the file of the session a restarting fude saved, which
// the new process opens (B170).
const RestartEnv = "FUDE_RESTART"

// saved is the session :restart keeps for the new process.
type saved struct {
	Buffers  []savedBuffer
	Layout   savedNode
	Windows  bool `json:"-"` // Layout has a window on a file or explorer
	Current  int  // the current window, counted from the top left
	Centered bool // :center is on (B199)
}

// savedBuffer is a buffer's file and the timeline node its text was at.
type savedBuffer struct {
	Path string
	Head kernel.NodeID
}

// savedNode is a window, with its file or explorer's directory, cursor,
// top line and options, or a split of them.
type savedNode struct {
	Path        string
	Explorer    bool // Path is an explorer's directory (B226)
	Cursor, Top int
	Options     options
	Across      bool
	Kids        []savedNode
}

// restorable reports whether b is a file that a new process can open.
func restorable(b *buffer) bool {
	return b.file.path != "" && b.title == "" && b.listed == ""
}

// save returns n's windows on files and explorers, or false when it has
// none.
func (n *node) save() (savedNode, bool) {
	if w := n.win; w != nil {
		s := savedNode{Path: w.buf.listed, Explorer: w.buf.listed != "", Cursor: w.pos(), Top: w.buf.text.Mark(w.top), Options: w.ownOptions()}
		if s.Explorer {
			return s, true
		}
		abs, err := filepath.Abs(w.buf.file.path)
		if !restorable(w.buf) || err != nil {
			return savedNode{}, false
		}
		s.Path = abs
		return s, true
	}
	s := savedNode{Across: n.across}
	for _, k := range n.kids {
		if ks, ok := k.save(); ok {
			s.Kids = append(s.Kids, ks)
		}
	}
	switch len(s.Kids) {
	case 0:
		return s, false
	case 1:
		return s.Kids[0], true
	}
	return s, true
}

// restart saves the session and quits, keeping unsaved changes, so that
// main starts fude again on it (B170, N23).
func (e *Session) restart() {
	if e != e.sessions[0] {
		e.message = "only the terminal fude started in can restart it"
		return
	}
	path, err := writeSession(e.session())
	if err != nil {
		e.message = err.Error()
		return
	}
	e.restarting = path
	e.quitAll(true)
}

// session returns the session to keep: the buffers on files, the windows
// and which is current.
func (e *Session) session() saved {
	s := saved{Centered: e.centered}
	for _, b := range e.bufs {
		if abs, err := filepath.Abs(b.file.path); restorable(b) && err == nil {
			s.Buffers = append(s.Buffers, savedBuffer{abs, b.text.Head()})
		}
	}
	s.Layout, s.Windows = e.root.save()
	for _, w := range e.root.windows() {
		if w == e.win {
			break
		}
		if restorable(w.buf) || w.buf.listed != "" {
			s.Current++
		}
	}
	return s
}

// writeSession writes s to a file of its own in fude's state directory and
// returns its path.
func writeSession(s saved) (string, error) {
	state, err := keep.Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", fmt.Errorf("restart: %w", err)
	}
	dir := filepath.Join(state, "fude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("restart: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("restart: %w", err)
	}
	f, err := os.CreateTemp(dir, "restart-*.json")
	if err != nil {
		return "", fmt.Errorf("restart: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("restart: %w", err)
	}
	return f.Name(), f.Close()
}

// Restart returns the file of the session :restart saved, or "" when fude
// quit without it.
func (e *Session) Restart() string { return e.restarting }

// restartSession reads and removes the session file RestartEnv names, or
// returns nil when fude did not start from :restart.
func restartSession() (*saved, error) {
	path := os.Getenv(RestartEnv)
	if path == "" {
		return nil, nil
	}
	// Language servers and the next restart must not see it.
	os.Unsetenv(RestartEnv)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("restore the session: %w", err)
	}
	os.Remove(path)
	var s saved
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("restore the session: %w", err)
	}
	return &s, nil
}

// restoreSession puts each saved buffer's text back at its node and builds the
// saved windows. It leaves out the windows of a file that did not open.
func (e *Session) restoreSession(s saved) {
	for _, sb := range s.Buffers {
		if i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(sb.Path) }); i >= 0 && e.bufs[i].text.Head() != sb.Head {
			goTo(e.bufs[i], sb.Head)
		}
	}
	e.restoreLayout(s)
}

// restoreLayout builds s's windows on the open buffers, leaving out those
// of a file that is not open.
func (e *Session) restoreLayout(s saved) {
	e.centered = s.Centered
	var build func(sn savedNode) *node
	build = func(sn savedNode) *node {
		if sn.Kids == nil {
			b := e.savedBuffer(sn)
			if b == nil {
				return nil
			}
			t := b.text.Snapshot()
			w := newWindow(b, min(sn.Cursor, t.Len()), min(sn.Top, t.Len()))
			w.opts = sn.Options
			return &node{win: w}
		}
		n := &node{across: sn.Across}
		for _, k := range sn.Kids {
			if kn := build(k); kn != nil {
				kn.parent = n
				n.kids = append(n.kids, kn)
			}
		}
		switch len(n.kids) {
		case 0:
			return nil
		case 1:
			only := n.kids[0]
			only.parent = nil
			return only
		}
		return n
	}
	root := build(s.Layout)
	if root == nil {
		return
	}
	e.root = root
	ws := root.windows()
	e.win = ws[min(s.Current, len(ws)-1)]
	// New windows start with the current one's options, as after :set.
	e.opts = e.win.opts
	e.layout()
}

// savedBuffer is the open buffer sn's window shows: its file's, or its
// directory's explorer, which it lists again when none is open (B226).
func (e *Session) savedBuffer(sn savedNode) *buffer {
	if !sn.Explorer {
		if i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(sn.Path) }); i >= 0 {
			return e.bufs[i]
		}
		return nil
	}
	if i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.listed == sn.Path }); i >= 0 {
		return e.bufs[i]
	}
	b, err := explorerBuffer(e.next, sn.Path)
	if err != nil {
		e.message = err.Error()
		return nil
	}
	e.next++
	e.bufs = append(e.bufs, b)
	e.emit("explorer.read", "", b)
	return b
}
