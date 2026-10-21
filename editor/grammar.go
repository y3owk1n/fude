package editor

import (
	"strings"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/syntax"
)

// attachSyntax gives b the syntax of its filetype's grammar, and reports
// whether there is one (B46).
func (e *Session) attachSyntax(b *buffer) (bool, error) {
	h, ok, err := syntax.New(e.filetype(b), e.queries, e.queue, func() { e.emit("syntax.updated", "", b) }, func(ft string, err error) {
		switch {
		case err != nil:
			e.message = err.Error()
		case e.cfg.AutoInstall:
			e.buildGrammar(ft, false)
		}
	}, func(text string, took time.Duration) { trace.Record("syntax", text, took) })
	if ok {
		b.syntax = h
	}
	return ok, err
}

// syntaxFor gives b its filetype's syntax, or starts building the grammar,
// and returns what to say of it.
func (e *Session) syntaxFor(b *buffer) string {
	ok, err := e.attachSyntax(b)
	if err != nil {
		return err.Error()
	}
	if ft := e.filetype(b); !ok && syntax.Buildable(ft) && e.cfg.AutoInstall && e.buildGrammar(ft, false) {
		return "building the " + ft + " grammar"
	}
	return ""
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
		at := time.Now()
		trace.Record("syntax", "building the "+ft+" grammar", 0)
		err := syntax.Build(ft)
		done := "built the " + ft + " grammar"
		if err != nil {
			done = err.Error()
		}
		trace.Record("syntax", done, time.Since(at))
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
				// A buffer that injects the language colours it from its next
				// request.
				if b.syntax != nil {
					b.syntax.Refresh()
				}
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

// grammarList is the grammars fude builds, each with its state, building
// or failed this session too (B165, B333).
func (e *Session) grammarList() []api.Grammar {
	var out []api.Grammar
	for _, g := range syntax.Grammars() {
		state := g.State
		switch {
		case e.building[g.Filetype]:
			state = "building"
		case e.failed[g.Filetype] != "":
			state = "failed: " + e.failed[g.Filetype]
		}
		out = append(out, api.Grammar{Filetype: g.Filetype, State: state})
	}
	return out
}

// build builds the grammars of fts again and says which started. A guest
// may not (B165, B244, B333).
func (e *Session) build(fts []string) {
	if e.power != Host {
		e.message = "a guest may not build grammars"
		return
	}
	var started []string
	for _, ft := range fts {
		switch {
		case !syntax.Buildable(ft):
			e.message = "fude cannot build a grammar for " + ft
			return
		case e.buildGrammar(ft, true):
			started = append(started, ft)
		}
	}
	switch len(started) {
	case 0:
		e.message = "the " + strings.Join(fts, ", ") + " grammar is already building"
	case 1:
		e.message = "building the " + started[0] + " grammar"
	default:
		e.message = "building the " + strings.Join(started, ", ") + " grammars"
	}
}
