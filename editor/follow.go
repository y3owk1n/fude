package editor

import (
	"cmp"
	"slices"
)

// startFollow makes the current window follow the session called name, as
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
	e.following, e.follower = e.sessions[i], e.win
	e.message = "following " + name
	e.follow()
}

// follow moves the following window to the buffer, top and cursor of the
// current window of the session it follows (B256).
func (e *Session) follow() {
	s, w := e.following, e.follower
	switch {
	case s == nil:
		return
	case !slices.Contains(e.sessions, s):
		e.following = nil
		e.message = s.label() + " left, so following stopped"
		return
	case !slices.Contains(e.root.windows(), w):
		e.following = nil
		return
	}
	// A float or the finder has a buffer of its own, and a guest shows only
	// what it may (B245).
	b := s.win.buf
	if !slices.Contains(e.bufs, b) || e.outside(cmp.Or(b.file.path, b.listed)) != nil {
		return
	}
	if w.buf != b {
		w.show(b)
	}
	b.text.MoveMark(w.top, b.text.Mark(s.win.top))
	b.text.MoveMark(w.cursor, s.win.pos())
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
