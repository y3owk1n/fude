// Package diff is diff mode: windows that show how their buffers differ,
// line beside line, through :diffthis, :diffsplit, :diffoff, :diffupdate,
// ]c, [c, do, dp, :diffget and :diffput (B520 to B525).
package diff

import (
	"cmp"
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/api/diff"
	splits "github.com/y3owk1n/fude/internal/split"
)

// What diff registers, by the ids its calls run them by.
const (
	this uint32 = iota + 1
	off
	split
	update
	get
	put
	next
	prev
)

// fill is a row that stands for a line another buffer has, cut at the
// window's edge.
var fill = strings.Repeat("-", 512)

// Module is diff. It keeps the windows in diff mode.
type Module struct {
	mu   sync.Mutex
	wins []int // by id, in the order they joined
	ids  int
}

// New returns diff.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "diff" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.changed", "buffer.shown", "window.closed"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "diffthis", Short: []string{"difft"}, Help: "puts the window in diff mode", ID: this},
		{Kind: api.Command, Name: "diffoff", Short: []string{"diffo"}, Help: "takes the window out of diff mode, or every window with !", ID: off},
		{Kind: api.Command, Name: "diffsplit", Short: []string{"diffs"}, Args: "{file}", Paths: true, Help: "opens file in a window below, both windows in diff mode", ID: split},
		{Kind: api.Command, Name: "diffupdate", Short: []string{"diffu"}, Help: "compares the buffers in diff mode again", ID: update},
		{Kind: api.Command, Name: "diffget", Short: []string{"diffg"}, Args: "[buffer]", Help: "takes the other buffer's lines of the changes in the range", ID: get},
		{Kind: api.Command, Name: "diffput", Short: []string{"diffpu"}, Args: "[buffer]", Help: "gives the other buffer the lines of the changes in the range", ID: put},
		{Kind: api.Plug, Name: "next", Keys: "]c", Help: "goes to the start of the next change", ID: next},
		{Kind: api.Plug, Name: "prev", Keys: "[c", Help: "goes to the start of the change before", ID: prev},
		{Kind: api.Plug, Name: "get", Keys: "do", Help: "takes the other buffer's lines of the change at the cursor, or buffer n's", ID: get, Bound: true},
		{Kind: api.Plug, Name: "put", Keys: "dp", Help: "gives the other buffer the lines of the change at the cursor, or buffer n", ID: put, Bound: true},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs a command or key of diff mode in the current window.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	win := 0
	for _, w := range s.Windows() {
		if w.Current {
			win = w.ID
		}
	}
	arg := strings.TrimSpace(s.About["args"])
	switch id {
	case this, split:
		if id == split && arg == "" {
			return api.Result{Error: "usage: :diffsplit {file}"}, nil
		}
		if !slices.Contains(m.wins, win) {
			m.wins = append(m.wins, win)
		}
		res := m.show(s)
		res.Windows = []api.WindowAction{{ID: win, Do: "bind"}}
		if id == split {
			buf, _ := strconv.Atoi(s.About["buffer"])
			res.Jump = &api.Jump{Buffer: buf, Where: splits.Way(s, false)}
			res.Ex = []string{"edit " + arg, "diffthis"}
		}
		return res, nil
	case off:
		gone := []int{win}
		if s.About["bang"] == "true" {
			gone = m.wins
		}
		var unbind []api.WindowAction
		for _, w := range gone {
			unbind = append(unbind, api.WindowAction{ID: w, Do: "unbind"})
		}
		m.wins = slices.DeleteFunc(slices.Clone(m.wins), func(w int) bool { return slices.Contains(gone, w) })
		res := m.show(s)
		res.Windows = unbind
		return res, nil
	case update:
		return m.show(s), nil
	}
	bufs := m.buffers(s)
	cur, _ := strconv.Atoi(s.About["buffer"])
	j := slices.Index(bufs, cur)
	if !slices.Contains(m.wins, win) || len(bufs) < 2 {
		return api.Result{Error: "not in diff mode"}, nil
	}
	texts := lines(s, bufs)
	blocks := align(texts)
	t := s.Text
	line := t.LineOf(args[0])
	if id == next || id == prev {
		return jump(t, blocks, j, line, max(args[1], 1), id == next), nil
	}
	// A count names the other buffer, as an argument does.
	if args[1] > 0 {
		arg = strconv.Itoa(args[1])
	}
	k, err := other(s, bufs, j, arg)
	if err != "" {
		return api.Result{Error: err}, nil
	}
	first, last := line, line
	if s.About["ranged"] == "true" {
		f, _ := strconv.Atoi(s.About["first"])
		l, _ := strconv.Atoi(s.About["last"])
		first, last = t.LineOf(f), t.LineOf(l)
	}
	from, to := k, j
	if id == put {
		from, to = j, k
	}
	now := slices.Clone(texts[to])
	took := false
	for _, b := range slices.Backward(blocks) {
		// A change with no lines here is on the line after its fill.
		if r := b[j]; min(r[0], t.Lines()-1) <= last && max(r[1], r[0]+1) > first {
			now = slices.Replace(now, b[to][0], b[to][1], texts[from][b[from][0]:b[from][1]]...)
			took = true
		}
	}
	if !took {
		return api.Result{Error: "no change here"}, nil
	}
	edits := diff.Edits(strings.Join(texts[to], "\n"), strings.Join(now, "\n"))
	for i := range edits {
		edits[i].Buffer = bufs[to]
	}
	return api.Result{Edits: edits}, nil
}

// Event compares the buffers again when one changes, a window in diff mode
// shows another buffer or one closes. The last window left in diff mode
// leaves it.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.wins)
	buf, _ := strconv.Atoi(s.About["buffer"])
	if n == 0 || strings.HasPrefix(name, "buffer.changed") && !slices.Contains(m.buffers(s), buf) {
		return api.Result{}, nil
	}
	res := m.show(s)
	if n > 1 && len(m.wins) == 1 {
		res.Windows = []api.WindowAction{{ID: m.wins[0], Do: "unbind"}}
		m.wins = nil
	}
	return res, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }

// buffers returns the buffers the windows in diff mode show, in the order
// the windows joined, once it forgets the windows that closed.
func (m *Module) buffers(s api.Scope) []int {
	shows := map[int]int{}
	for _, w := range s.Windows() {
		shows[w.ID] = w.Buffer
	}
	m.wins = slices.DeleteFunc(m.wins, func(w int) bool { _, ok := shows[w]; return !ok })
	var bufs []int
	for _, w := range m.wins {
		if !slices.Contains(bufs, shows[w]) {
			bufs = append(bufs, shows[w])
		}
	}
	return bufs
}

// show decorates the buffers in diff mode with how they differ, in place
// of what it showed before. Lines every buffer has at a change show as
// changed, with the text that differs from the next buffer's line lit, and
// the others as added. Rows of fill stand for the lines another buffer has
// there, so lines that pair stay level (B520).
func (m *Module) show(s api.Scope) api.Result {
	var res api.Result
	for _, d := range s.Decorations() {
		res.Undecorate = append(res.Undecorate, d.ID)
	}
	bufs := m.buffers(s)
	if len(bufs) < 2 {
		return res
	}
	texts := lines(s, bufs)
	add := func(d api.Decoration) {
		m.ids++
		d.ID = m.ids
		res.Decorate = append(res.Decorate, d)
	}
	for _, b := range align(texts) {
		rows, both := 0, b[0][1]-b[0][0]
		for _, r := range b {
			rows, both = max(rows, r[1]-r[0]), min(both, r[1]-r[0])
		}
		for j, r := range b {
			t, buf := s.Buffers[bufs[j]].Text, bufs[j]
			if both > 0 {
				add(api.Decoration{Start: t.LineStart(r[0]), End: t.LineStart(r[0] + both - 1), Style: "diff.change", Line: true, Buffer: buf})
			}
			if r[1]-r[0] > both {
				add(api.Decoration{Start: t.LineStart(r[0] + both), End: t.LineStart(r[1] - 1), Style: "diff.add", Line: true, Buffer: buf})
			}
			o := b[(j+1)%len(b)]
			for i := range both {
				line, with := texts[j][r[0]+i], texts[(j+1)%len(b)][o[0]+i]
				if from, to := changed(line, with); from < to {
					start := t.LineStart(r[0] + i)
					add(api.Decoration{Start: start + from, End: start + to, Style: "diff.text", Buffer: buf})
				}
			}
			if n := rows - (r[1] - r[0]); n > 0 {
				// The rows go above the line after the change, or below the
				// last line at the end.
				at, above := r[1], true
				if at == t.Lines() {
					at, above = at-1, false
				}
				add(api.Decoration{Start: t.LineStart(at), End: t.LineStart(at), Style: "diff.delete", Insert: true, Lines: slices.Repeat([]string{fill}, n), Above: above, Buffer: buf})
			}
		}
	}
	return res
}

// lines returns the lines of each buffer of bufs.
func lines(s api.Scope, bufs []int) [][]string {
	out := make([][]string, len(bufs))
	for k, b := range bufs {
		t := s.Buffers[b].Text
		out[k] = strings.Split(t.Slice(0, t.Len()), "\n")
	}
	return out
}

// changed returns where line differs from with: the bytes between their
// common start and common end.
func changed(line, with string) (int, int) {
	pre := 0
	for pre < len(line) && pre < len(with) && line[pre] == with[pre] {
		pre++
	}
	for pre > 0 && pre < len(line) && !utf8.RuneStart(line[pre]) {
		pre--
	}
	end := len(line)
	for end > pre && len(with)-(len(line)-end) > pre && line[end-1] == with[len(with)-(len(line)-end)-1] {
		end--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}
	return pre, end
}

// block is a change: by buffer, its lines from the first to the one after
// the last.
type block [][2]int

// align returns the changes between texts, in order. Each first text's
// change with another one becomes a change of all of them, with those
// that touch it, and each text has the lines in its place.
func align(texts [][]string) []block {
	type hunk struct{ a0, a1, delta int }
	per := make([][]hunk, len(texts))
	var spans [][2]int
	for j := 1; j < len(texts); j++ {
		x := 0
		ops := diff.Lines(texts[0], texts[j])
		for i := 0; i < len(ops); {
			if ops[i].Kind == ' ' {
				x, i = x+1, i+1
				continue
			}
			h := hunk{a0: x, a1: x}
			for ; i < len(ops) && ops[i].Kind != ' '; i++ {
				if ops[i].Kind == '-' {
					h.a1++
					h.delta--
				} else {
					h.delta++
				}
			}
			x = h.a1
			per[j] = append(per[j], h)
			spans = append(spans, [2]int{h.a0, h.a1})
		}
	}
	slices.SortFunc(spans, func(p, q [2]int) int { return cmp.Compare(p[0], q[0]) })
	var out []block
	for i := 0; i < len(spans); {
		a0, a1 := spans[i][0], spans[i][1]
		for i++; i < len(spans) && spans[i][0] <= a1; i++ {
			a1 = max(a1, spans[i][1])
		}
		b := block{{a0, a1}}
		for _, hs := range per[1:] {
			s, e := a0, a1
			for _, h := range hs {
				if h.a0 < a0 {
					s += h.delta
				}
				if h.a0 <= a1 {
					e += h.delta
				}
			}
			b = append(b, [2]int{s, e})
		}
		out = append(out, b)
	}
	return out
}

// jump goes count changes on from line in text j, forward or back, to the
// first line of the change there, or the line after it in a text without
// its lines (B524).
func jump(t api.Text, blocks []block, j, line, count int, forward bool) api.Result {
	var starts []int
	for _, b := range blocks {
		starts = append(starts, min(b[j][0], t.Lines()-1))
	}
	i := -1
	if forward {
		if k := slices.IndexFunc(starts, func(l int) bool { return l > line }); k >= 0 {
			i = min(k+count-1, len(starts)-1)
		}
	} else {
		k := len(starts)
		if f := slices.IndexFunc(starts, func(l int) bool { return l >= line }); f >= 0 {
			k = f
		}
		if k > 0 {
			i = max(k-count, 0)
		}
	}
	if i < 0 {
		return api.Result{Error: "no change there"}
	}
	return api.Result{Jump: &api.Jump{Pos: t.LineStart(starts[i])}}
}

// other returns the place in bufs of the buffer, not the current one at j,
// that spec names by its number or part of its name, or of the only one
// for "", or why there is none. It looks in each file's own name first, and
// in its directories only when no file's name matches.
func other(s api.Scope, bufs []int, j int, spec string) (int, string) {
	var found, dirs []int
	for k, b := range bufs {
		name := s.Buffers[b].Name
		switch {
		case k == j:
		case spec == "" || spec == strconv.Itoa(b) || strings.Contains(filepath.Base(name), spec):
			found = append(found, k)
		case strings.Contains(name, spec):
			dirs = append(dirs, k)
		}
	}
	if len(found) == 0 {
		found = dirs
	}
	switch {
	case len(found) == 1:
		return found[0], ""
	case spec == "":
		return 0, "more than two buffers in diff mode, so name one"
	}
	return 0, "no one buffer in diff mode matches " + spec
}
