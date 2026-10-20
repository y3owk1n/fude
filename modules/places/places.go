// Package places shows the last list of places in a window, where <CR>
// goes to the place on the cursor's line, and steps through it: ]q, [q,
// [Q, ]Q, :cnext, :cprevious, :cfirst, :clast, :cc, :copen, :cclose, :cdo
// and :cfdo (B194, B257, B347). Its name is list, so its plugs are
// <Plug>(list.next) and the others config.toml maps.
package places

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/paths"
	"github.com/y3owk1n/fude/internal/trace"
)

// What places registers, by the ids Call runs them by.
const (
	next uint32 = iota + 1
	prev
	first
	last
	nth
	run
	runFiles
	open
	shut
	visit
	nextKey
	prevKey
)

// keys are a list window's own keys, which [keymap.list] changes (B142).
var keys = map[string]string{"<CR>": "<Plug>(list.open)", "q": ":close<CR>"}

// Module is places. It keeps each list it showed, by its window's title,
// so <CR> in an older list goes to its own places.
type Module struct {
	mu    sync.Mutex
	lists map[string][]entry
}

// entry is one place of a list: its file, line and column from 1, 0 for
// a file's start, and what its line in the list shows.
type entry struct {
	path      string
	line, col int
	shown     string
}

// New returns places.
func New() *Module { return &Module{lists: map[string][]entry{}} }

func (m *Module) Name() string { return "list" }

func (m *Module) Subscribed() []string { return []string{"places.set"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "cnext", Short: []string{"cn"}, Help: "goes to the next place of the last list of places", ID: next},
		{Kind: api.Command, Name: "cprevious", Short: []string{"cp", "cN", "cNext"}, Help: "goes to the place before in the last list of places", ID: prev},
		{Kind: api.Command, Name: "cfirst", Short: []string{"cfir", "crewind", "cr"}, Help: "goes to the first place of the last list of places", ID: first},
		{Kind: api.Command, Name: "clast", Short: []string{"cla"}, Help: "goes to the last place of the last list of places", ID: last},
		{Kind: api.Command, Name: "cc", Args: "[n]", Help: "goes to place n of the last list of places, or the one last gone to", ID: nth},
		{Kind: api.Command, Name: "cdo", Args: "{cmd}", Help: "runs cmd at each place of the last list of places", ID: run},
		{Kind: api.Command, Name: "cfdo", Args: "{cmd}", Help: "runs cmd once in each file of the last list of places", ID: runFiles},
		{Kind: api.Command, Name: "copen", Short: []string{"cope"}, Help: "shows the last list of places in a window", ID: open},
		{Kind: api.Command, Name: "cclose", Short: []string{"ccl"}, Help: "closes the window of the last list of places", ID: shut},
		{Kind: api.Plug, Name: "open", Help: "goes to the place under the cursor", ID: visit},
		{Kind: api.Plug, Name: "next", Keys: "]q", Help: "goes to the next place of the last list", ID: nextKey},
		{Kind: api.Plug, Name: "prev", Keys: "[q", Help: "goes to the place before in the last list", ID: prevKey},
		{Kind: api.Plug, Name: "first", Keys: "[Q", Help: "goes to the first place of the last list", ID: first},
		{Kind: api.Plug, Name: "last", Keys: "]Q", Help: "goes to the last place of the last list", ID: last},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call goes to a place, shows or closes the list's window, or runs a
// command at its places.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == visit {
		return m.visit(s, args[0]), nil
	}
	pl, ok := s.Places()
	if !ok {
		return api.Result{Error: "no list of places"}, nil
	}
	arg := strings.TrimSpace(s.About["args"])
	count := 1
	if len(args) > 1 {
		count = max(args[1], 1)
	}
	switch id {
	case next:
		return step(s, pl, func(at, _ int) int { return at + 1 }), nil
	case nextKey:
		return step(s, pl, func(at, _ int) int { return at + count }), nil
	case prev:
		return step(s, pl, func(at, _ int) int { return max(at, 0) - 1 }), nil
	case prevKey:
		return step(s, pl, func(at, _ int) int { return max(at, 0) - count }), nil
	case first:
		return step(s, pl, func(int, int) int { return 0 }), nil
	case last:
		return step(s, pl, func(_, n int) int { return n - 1 }), nil
	case nth:
		n, err := strconv.Atoi(arg)
		if arg != "" && err != nil {
			return api.Result{Error: "not a number: " + arg}, nil
		}
		return step(s, pl, func(at, _ int) int {
			if arg == "" {
				return max(at, 0)
			}
			return n - 1
		}), nil
	case run, runFiles:
		if arg == "" {
			name := "cdo"
			if id == runFiles {
				name = "cfdo"
			}
			return api.Result{Error: "run what? :" + name + " cmd"}, nil
		}
		each := &api.Each{Encoding: pl.Encoding, Ex: arg, PerFile: id == runFiles}
		for _, e := range entries(s, pl) {
			each.Items = append(each.Items, fmt.Sprintf("%s:%d:%d", e.path, e.line, e.col))
		}
		return api.Result{Each: each}, nil
	case open:
		return m.show(s, pl), nil
	}
	return shutAll(s, "["+pl.Title+"]"), nil
}

// Event shows the last list of places in a window, when it asks, and keeps
// its places for <CR> there.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pl, ok := s.Places()
	if !ok {
		return api.Result{}, nil
	}
	es := entries(s, pl)
	m.lists["["+pl.Title+"]"] = es
	if !strings.HasSuffix(name, " show") {
		return api.Result{}, nil
	}
	if len(es) == 0 {
		return api.Result{Error: "no " + pl.Title}, nil
	}
	return api.Result{Show: panel(pl.Title, es, 0)}, nil
}

// step goes to the place of the list that to gives, from the place last
// gone to and the count of places, and says which it is.
func step(s api.Scope, pl api.Places, to func(at, n int) int) api.Result {
	es := entries(s, pl)
	at := to(pl.At-1, len(es))
	if at < 0 || at >= len(es) {
		return api.Result{Error: "no more places"}
	}
	// Going to a place does not show the list again.
	pl.At, pl.Show = at+1, false
	return api.Result{Places: &pl, Jump: jump(es[at], pl.Encoding), Message: fmt.Sprintf("(%d of %d) %s", at+1, len(es), strings.TrimSpace(es[at].shown))}
}

// visit goes to the place on the cursor's line of a list window, which is
// then the place last gone to of the last list.
func (m *Module) visit(s api.Scope, cursor int) api.Result {
	title := s.About["title"]
	es, ok := m.lists[title]
	line := s.Text.LineOf(cursor)
	if !ok || line >= len(es) {
		return api.Result{Error: "not in a list"}
	}
	res := api.Result{Jump: jump(es[line], "")}
	if pl, ok := s.Places(); ok && "["+pl.Title+"]" == title {
		pl.At, pl.Show = line+1, false
		res.Places, res.Jump.Encoding = &pl, pl.Encoding
	}
	return res
}

// show shows the last list in a window of its own, the one that shows it
// already or a new one, with the cursor on the place last gone to (B194).
func (m *Module) show(s api.Scope, pl api.Places) api.Result {
	title, line := "["+pl.Title+"]", max(pl.At-1, 0)
	for _, w := range s.Windows() {
		if b := s.Buffers[w.Buffer]; b.Name == title {
			return api.Result{Windows: []api.WindowAction{{ID: w.ID, Do: "focus"}, {ID: w.ID, Do: "cursor", Pos: b.Text.LineStart(min(line, b.Text.Lines()-1))}}}
		}
	}
	es := entries(s, pl)
	m.lists[title] = es
	return api.Result{Show: panel(pl.Title, es, line)}
}

// shutAll closes the windows that show the list titled title.
func shutAll(s api.Scope, title string) api.Result {
	var res api.Result
	for _, w := range s.Windows() {
		if s.Buffers[w.Buffer].Name == title {
			res.Windows = append(res.Windows, api.WindowAction{ID: w.ID, Do: "close"})
		}
	}
	return res
}

// panel is the window that shows a list's places, a line each, with the
// cursor at the start of line.
func panel(title string, es []entry, line int) *api.Panel {
	lines := make([]string, len(es))
	cursor := 0
	for i, e := range es {
		lines[i] = e.shown
		if i < line {
			cursor += len(e.shown) + 1
		}
	}
	return &api.Panel{Unnumbered: true, Title: "[" + title + "]", Text: strings.Join(lines, "\n"), Keys: keys, Kind: "list", Cursor: cursor}
}

// jump goes to e, its column in enc.
func jump(e entry, enc string) *api.Jump {
	return &api.Jump{Path: e.path, Line: e.line, Column: e.col, Encoding: enc}
}

// entries are the places of pl. An item with no text shows its line, and
// one with a line of its own in Lines shows that (B230, B292, B329).
func entries(s api.Scope, pl api.Places) []entry {
	var out []entry
	for i, item := range pl.Items {
		var e entry
		var text, place string
		if pl.File != "" {
			parts := strings.SplitN(item, ":", 3)
			if len(parts) < 3 {
				continue
			}
			e.line, _ = strconv.Atoi(parts[0])
			e.col, _ = strconv.Atoi(parts[1])
			e.path, text, place = pl.File, parts[2], parts[0]+":"+parts[1]+":"
		} else {
			// The colon of a Windows path's drive, as in C:, splits nothing.
			vol := filepath.VolumeName(item)
			parts := strings.SplitN(item[len(vol):], ":", 4)
			parts[0] = vol + parts[0]
			line, err1 := strconv.Atoi(part(parts, 1))
			col, err2 := strconv.Atoi(part(parts, 2))
			if len(parts) < 4 || err1 != nil || err2 != nil {
				// A path alone is its file's start.
				out = append(out, entry{path: item, shown: item})
				continue
			}
			e.path, e.line, e.col, text = parts[0], line, col, parts[3]
			place = fmt.Sprintf("%s:%d:%d:", e.path, line, col)
		}
		if strings.TrimSpace(text) == "" {
			text = lineOf(s, e.path, e.line)
		}
		e.shown = place + " " + text
		if i < len(pl.Lines) {
			e.shown = pl.Lines[i]
		}
		out = append(out, e)
	}
	return out
}

// part is parts[i], or "" past its end.
func part(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return ""
}

// lineOf is the text of line n, from 1, of the file at path, from its
// buffer or else the file, trimmed. A guest reads no file outside the
// project (B245).
func lineOf(s api.Scope, path string, n int) string {
	abs, err := filepath.Abs(path)
	if err != nil || n < 1 {
		return ""
	}
	text := ""
	found := false
	for _, b := range s.Buffers {
		if p, err := filepath.Abs(b.Path); err == nil && b.Path != "" && paths.Same(p, abs) {
			text, found = b.Text.Slice(0, b.Text.Len()), true
			break
		}
	}
	if !found {
		if rel, err := filepath.Rel(s.About["project"], abs); s.About["power"] != "host" && (err != nil || strings.HasPrefix(rel, "..")) {
			return ""
		}
		at := time.Now()
		data, err := os.ReadFile(abs)
		trace.Since("file", fmt.Sprintf("read %q, %d bytes", abs, len(data)), at, err)
		if err != nil {
			return ""
		}
		text = string(data)
	}
	if lines := strings.Split(text, "\n"); n <= len(lines) {
		return strings.TrimSpace(lines[n-1])
	}
	return ""
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
