package editor

import "slices"

// startFollow makes the windows follow the session called name, as
// :follow does (B256).
func (e *Session) startFollow(name string) {
	i := slices.IndexFunc(e.sessions, func(s *Session) bool { return s != e && s.label() == name })
	switch {
	case name == "":
		e.message = "follow whom? :follow name"
		return
	case i < 0:
		e.message = "no session is called " + name
		return
	}
	e.following, e.mirrors = e.sessions[i], nil
	e.message = "following " + name
	e.follow()
}

// follow makes the windows a copy of those of the session they follow:
// the same layout, the same window current, and each one on its
// counterpart's buffer, top, left column and cursor (B256).
func (e *Session) follow() {
	s := e.following
	switch {
	case s == nil:
		return
	case !slices.Contains(e.sessions, s):
		e.following = nil
		e.message = s.label() + " left, so following stopped"
		return
	}
	mirrors := map[*window]*window{}
	e.root = e.mirror(s.root, nil, mirrors)
	e.mirrors = mirrors
	// While the other has a float or the finder open, the window under it
	// is the current one.
	current := s.win
	if !slices.Contains(s.root.windows(), current) {
		current = s.under
	}
	if w := mirrors[current]; w != nil {
		e.win = w
	} else {
		e.win = e.root.windows()[0]
	}
	e.layout()
	// Each window keeps its cursor in view on its own rows, as reveal does
	// for the current window.
	win := e.win
	for _, w := range e.root.windows() {
		e.win = w
		e.reveal()
	}
	e.win = win
}

// mirror returns a copy of n under parent, whose windows copy n's. A
// window that copied one of n's last time is kept, and mirrors gets each.
func (e *Session) mirror(n, parent *node, mirrors map[*window]*window) *node {
	m := &node{across: n.across, size: n.size, parent: parent}
	for _, k := range n.kids {
		m.kids = append(m.kids, e.mirror(k, m, mirrors))
	}
	if l := n.win; l != nil {
		w := e.mirrors[l]
		if w == nil {
			w = newWindow(e.mayShow(l.buf), 0, 0)
			w.opts = e.opts
		}
		// A float or the finder has a buffer of its own, and a guest's
		// window keeps its buffer for one it may not show (B244, B245).
		if b := l.buf; slices.Contains(e.bufs, b) && e.barred(b) == nil {
			if w.buf != b {
				w.show(b)
			}
			b.text.MoveMark(w.top, b.text.Mark(l.top))
			b.text.MoveMark(w.cursor, l.pos())
			w.left = l.left
		}
		mirrors[l], m.win = w, w
	}
	return m
}

// unfollow stops following, as a key typed or a paste does (B256). It
// returns a func to run after the key, which says following stopped
// unless the key showed a message of its own.
func (e *Session) unfollow() func() {
	s := e.following
	e.following = nil
	return func() {
		if s != nil && e.message == "" {
			e.message = "stopped following " + s.label()
		}
	}
}
