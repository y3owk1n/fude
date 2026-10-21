package editor

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// The protocol's kinds of change to a file.
const (
	fileCreated = 1
	fileEdited  = 2
	fileDeleted = 3
)

// fileChange is a file fude changed, or found changed, and how.
type fileChange struct {
	path string
	kind int
}

// watcher is a glob a server registered to hear of changes to the files
// it matches, under base when it is relative to a folder, and the kinds of
// change it hears, a bit each (B475).
type watcher struct {
	glob *regexp.Regexp
	base string
	kind int
}

// register keeps the file watchers of each registration in params, by its
// id.
func (s *server) register(params json.RawMessage) {
	var p struct {
		Registrations []struct {
			ID, Method      string
			RegisterOptions struct {
				Watchers []struct {
					GlobPattern json.RawMessage `json:"globPattern"`
					Kind        int             `json:"kind"`
				} `json:"watchers"`
			} `json:"registerOptions"`
		} `json:"registrations"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		logLine(fmt.Sprintf("registration from %s: %v", s.name, err))
		return
	}
	for _, r := range p.Registrations {
		if r.Method != "workspace/didChangeWatchedFiles" {
			continue
		}
		var ws []watcher
		for _, w := range r.RegisterOptions.Watchers {
			// A pattern is a glob, or one relative to a folder or its URI.
			var glob, base string
			var rel struct {
				BaseURI json.RawMessage `json:"baseUri"`
				Pattern string          `json:"pattern"`
			}
			if json.Unmarshal(w.GlobPattern, &glob) != nil && json.Unmarshal(w.GlobPattern, &rel) == nil {
				var uri string
				var folder struct{ URI string }
				if json.Unmarshal(rel.BaseURI, &uri) != nil && json.Unmarshal(rel.BaseURI, &folder) == nil {
					uri = folder.URI
				}
				path, _ := lsp.Path(uri)
				glob, base = rel.Pattern, filepath.ToSlash(path)
			}
			re, err := globRegexp(glob, false)
			if err != nil {
				logLine(fmt.Sprintf("watcher from %s: %v", s.name, err))
				continue
			}
			// With no kind, it hears of every kind.
			ws = append(ws, watcher{re, base, cmp.Or(w.Kind, 7)})
		}
		s.watchers[r.ID] = ws
	}
}

// unregister drops the file watchers of each registration params names.
func (s *server) unregister(params json.RawMessage) {
	// The protocol spells it so.
	var p struct {
		Unregisterations []struct{ ID string } `json:"unregisterations"`
	}
	json.Unmarshal(params, &p)
	for _, u := range p.Unregisterations {
		delete(s.watchers, u.ID)
	}
}

// filesChanged tells each server of the changes to files a watcher it
// registered matches (B475).
func (ed *Editor) filesChanged(changes ...fileChange) {
	for _, s := range ed.servers {
		var out []map[string]any
		for _, c := range changes {
			if s.watches(c) {
				out = append(out, map[string]any{"uri": lsp.URI(c.path), "type": c.kind})
			}
		}
		if len(out) > 0 {
			s.client.Notify("workspace/didChangeWatchedFiles", map[string]any{"changes": out})
		}
	}
}

// watches reports whether a watcher s registered hears of c.
func (s *server) watches(c fileChange) bool {
	abs, err := filepath.Abs(c.path)
	if err != nil {
		return false
	}
	path := filepath.ToSlash(abs)
	for _, ws := range s.watchers {
		for _, w := range ws {
			p := path
			if w.base != "" {
				rel, ok := strings.CutPrefix(path, strings.TrimSuffix(w.base, "/")+"/")
				if !ok {
					continue
				}
				p = rel
			}
			if w.kind&(1<<(c.kind-1)) != 0 && w.glob.MatchString(p) {
				return true
			}
		}
	}
	return false
}

// wants reports whether s asked to hear of op, such as didRename, for the
// file at path.
func (s *server) wants(op, path string) bool {
	var p struct {
		FileOperations map[string]struct {
			Filters []struct {
				Pattern struct {
					Glob    string
					Options struct{ IgnoreCase bool }
				}
			}
		}
	}
	json.Unmarshal(s.client.Capability("workspace"), &p)
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, f := range p.FileOperations[op].Filters {
		if re, err := globRegexp(f.Pattern.Glob, f.Pattern.Options.IgnoreCase); err == nil && re.MatchString(filepath.ToSlash(abs)) {
			return true
		}
	}
	return false
}

// fileOps tells each server of the files of op, such as didCreate, it
// asked to hear of, each a path or a rename's two (B475).
func (ed *Editor) fileOps(op string, files [][]string) {
	for _, s := range ed.servers {
		var out []map[string]string
		for _, f := range files {
			if !s.wants(op, f[0]) {
				continue
			}
			if len(f) == 2 {
				out = append(out, map[string]string{"oldUri": lsp.URI(f[0]), "newUri": lsp.URI(f[1])})
			} else {
				out = append(out, map[string]string{"uri": lsp.URI(f[0])})
			}
		}
		if len(out) > 0 {
			s.client.Notify("workspace/"+op+"Files", map[string]any{"files": out})
		}
	}
}

// willRename asks each server that edits files before a rename for its
// edits to the renames, applies them, then runs then once every one has
// answered (B475).
func (e *Session) willRename(renames [][2]string, then func()) {
	files := map[*server][]map[string]string{}
	for _, s := range e.servers {
		for _, r := range renames {
			if s.wants("willRename", r[0]) {
				files[s] = append(files[s], map[string]string{"oldUri": lsp.URI(r[0]), "newUri": lsp.URI(r[1])})
			}
		}
	}
	left := len(files)
	if left == 0 {
		then()
		return
	}
	for s, fs := range files {
		params := map[string]any{"files": fs}
		s.client.Request("workspace/willRenameFiles", func(string) any { return params }, func(res json.RawMessage) {
			if len(res) > 0 && string(res) != "null" {
				if _, problems := e.workspaceEdit(s, nil, kernel.Snapshot{}, res, "rename"); len(problems) > 0 {
					e.message = strings.Join(problems, "  ")
				}
			}
			if left--; left == 0 {
				then()
			}
		})
	}
}

// wrote tells the servers of what a confirmed write of c renamed, created
// and deleted (B475).
func (ed *Editor) wrote(c *api.Confirm) {
	var changes []fileChange
	var renames, creates, deletes [][]string
	for _, r := range c.Renames {
		changes = append(changes, fileChange{r[0], fileDeleted}, fileChange{r[1], fileCreated})
		renames = append(renames, []string{r[0], r[1]})
	}
	for _, p := range c.Creates {
		changes = append(changes, fileChange{p, fileCreated})
		creates = append(creates, []string{p})
	}
	for _, p := range c.Deletes {
		changes = append(changes, fileChange{p, fileDeleted})
		deletes = append(deletes, []string{p})
	}
	ed.fileOps("didRename", renames)
	ed.fileOps("didCreate", creates)
	ed.fileOps("didDelete", deletes)
	ed.filesChanged(changes...)
}

// globRegexp compiles a glob as the protocol writes them: * and ? within a
// part of a path, ** across parts, {a,b} for either and [...] for one of a
// set of characters, ! starting a set that leaves them out.
func globRegexp(glob string, ignoreCase bool) (*regexp.Regexp, error) {
	var b strings.Builder
	if ignoreCase {
		b.WriteString("(?i)")
	}
	b.WriteString("^")
	braces := 0
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			braces++
			b.WriteString("(?:")
		case c == '}' && braces > 0:
			braces--
			b.WriteString(")")
		case c == ',' && braces > 0:
			b.WriteString("|")
		case c == '[' && strings.IndexByte(glob[i:], ']') > 1:
			end := i + strings.IndexByte(glob[i:], ']')
			set := glob[i+1 : end]
			if rest, ok := strings.CutPrefix(set, "!"); ok {
				set = "^" + rest
			}
			b.WriteString("[" + set + "]")
			i = end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
