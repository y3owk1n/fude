package editor

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/plugin"
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

// addLanguage adds the grammar r names for p, a built-in module or a
// plugin granted grammar, and gives it to the open buffers of its filetype.
// It keeps why it adds none, for :grammar (B590, D71).
func (e *Session) addLanguage(p api.Module, r api.Registration) {
	w, isPlugin := p.(*plugin.Plugin)
	switch {
	case isPlugin && !slices.Contains(w.Capabilities, "grammar"):
		e.unbuilt[r.Name] = "plugin " + p.Name() + " has no grant for it"
		return
	case r.Source == nil || r.Source.Repo == "" || r.Source.Commit == "":
		e.unbuilt[r.Name] = p.Name() + " names no repo or commit for it"
		return
	}
	if err := syntax.Add(r.Name, p.Name(), syntax.Source(*r.Source)); err != nil {
		e.unbuilt[r.Name] = err.Error()
		return
	}
	delete(e.unbuilt, r.Name)
	for _, b := range e.bufs {
		if b.syntax == nil && e.filetype(b) == r.Name {
			if msg := e.syntaxFor(b); msg != "" {
				e.message = msg
			}
		}
	}
}

// grammarList is the grammars fude builds, each with its state, building
// or failed this session too, and those of modules it builds none for
// (B165, B333, B590).
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
		if why, ok := e.unbuilt[g.Filetype]; ok {
			state += ", " + why
		}
		out = append(out, api.Grammar{Filetype: g.Filetype, State: state, Owner: g.Owner})
	}
	for _, ft := range slices.Sorted(maps.Keys(e.unbuilt)) {
		if !slices.ContainsFunc(out, func(g api.Grammar) bool { return g.Filetype == ft }) {
			out = append(out, api.Grammar{Filetype: ft, State: "not built, " + e.unbuilt[ft]})
		}
	}
	slices.SortFunc(out, func(a, b api.Grammar) int { return cmp.Compare(a.Filetype, b.Filetype) })
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
