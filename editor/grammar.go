package editor

import (
	"fmt"
	"strings"

	"github.com/y3owk1n/fude/syntax"
)

// attachSyntax gives b the syntax of its filetype's grammar, and reports
// whether there is one (B46).
func (e *Session) attachSyntax(b *buffer) (bool, error) {
	h, ok, err := syntax.New(e.filetype(b), e.queries, e.queue, func() { e.emit("syntax.updated", "", b) })
	if ok {
		b.syntax = h
	}
	return ok, err
}

// buildGrammar builds filetype's grammar off the loop, and gives it to the
// open buffers of that filetype once it is built. A build that failed this
// session runs again only when forced. It reports whether a build started
// (B165).
func (e *Session) buildGrammar(ft string, force bool) bool {
	if e.building[ft] || !force && e.failed[ft] != "" {
		return false
	}
	e.building[ft] = true
	go func() {
		err := syntax.Build(ft)
		e.queue <- func() {
			delete(e.building, ft)
			if err != nil {
				e.failed[ft] = err.Error()
				e.message = err.Error()
				return
			}
			delete(e.failed, ft)
			e.message = "built the " + ft + " grammar"
			for _, b := range e.bufs {
				if b.syntax != nil || e.filetype(b) != ft || b.text.Snapshot().Len() > int(e.cfg.LargeFile)<<20 {
					continue
				}
				if _, err := e.attachSyntax(b); err != nil {
					e.message = err.Error()
				}
			}
		}
	}()
	return true
}

// grammar runs :grammar, which lists the grammars and their states, and
// :grammar install, which builds one again (B165).
func (e *Session) grammar(arg string) {
	if ft, ok := strings.CutPrefix(arg, "install "); ok {
		ft = strings.TrimSpace(ft)
		switch {
		case !syntax.Buildable(ft):
			e.message = "fude cannot build a grammar for " + ft
		case e.buildGrammar(ft, true):
			e.message = "building the " + ft + " grammar"
		default:
			e.message = "the " + ft + " grammar is building"
		}
		return
	}
	if arg != "" {
		e.message = "not a grammar command: " + arg
		return
	}
	var lines []string
	for _, g := range syntax.Grammars() {
		state := g.State
		switch {
		case e.building[g.Filetype]:
			state = "building"
		case e.failed[g.Filetype] != "":
			state = "failed: " + e.failed[g.Filetype]
		}
		lines = append(lines, fmt.Sprintf("%-12s %s", g.Filetype, state))
	}
	e.scratch("[grammars]", strings.Join(lines, "\n"), nil, nil)
}
