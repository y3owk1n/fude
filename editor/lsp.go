package editor

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/lsp"
)

// server is a running language server and the buffers attached to it.
type server struct {
	client    *lsp.Client
	command   []string
	root      string
	restarted bool // it crashed once, and the next crash leaves it off
	closing   bool // fude asked it to exit
	bufs      []*buffer
}

// attach connects b to the language server its filetype names, starting one
// for b's directory when none runs (B60).
func (e *Editor) attach(b *buffer) {
	command := e.cfg.Filetype[b.filetype()].LSP
	if len(command) == 0 || b.file.path == "" || b.title != "" {
		return
	}
	abs, err := filepath.Abs(b.file.path)
	if err != nil {
		e.message = err.Error()
		return
	}
	root := filepath.Dir(abs)
	key := strings.Join(append(slices.Clone(command), root), "\x00")
	s := e.servers[key]
	if s == nil {
		s = &server{command: command, root: root}
		if err := e.start(s); err != nil {
			e.message = err.Error()
			return
		}
		e.servers[key] = s
	}
	s.bufs = append(s.bufs, b)
	b.server, b.version = s, 0
	s.client.Open(lsp.URI(b.file.path), b.filetype(), b.text.Snapshot())
}

// detach disconnects b from its language server.
func (e *Editor) detach(b *buffer) {
	if s := b.server; s != nil {
		s.client.CloseDocument(lsp.URI(b.file.path))
		s.bufs = slices.DeleteFunc(s.bufs, func(o *buffer) bool { return o == b })
		b.server = nil
	}
}

func (e *Editor) start(s *server) error {
	c, err := lsp.Start(s.command, s.root, e.queue, logLine, func(err error) { e.exited(s, err) })
	if err != nil {
		return err
	}
	s.client = c
	return nil
}

// exited restarts a server that stopped without being asked to, once. Its
// buffers open in the new one. After a second crash it stays off (B63).
func (e *Editor) exited(s *server, err error) {
	if s.closing {
		return
	}
	name := filepath.Base(s.command[0])
	if s.restarted {
		e.message = fmt.Sprintf("language server %s exited again (%v), so it stays off", name, err)
		e.stop(s)
		return
	}
	s.restarted = true
	if err := e.start(s); err != nil {
		e.message = err.Error()
		e.stop(s)
		return
	}
	for _, b := range s.bufs {
		b.version = 0
		s.client.Open(lsp.URI(b.file.path), b.filetype(), b.text.Snapshot())
	}
	e.message = fmt.Sprintf("language server %s exited (%v) and was restarted", name, err)
}

// stop detaches every buffer from s and forgets it.
func (e *Editor) stop(s *server) {
	for _, b := range s.bufs {
		b.server = nil
	}
	for k, o := range e.servers {
		if o == s {
			delete(e.servers, k)
		}
	}
}
