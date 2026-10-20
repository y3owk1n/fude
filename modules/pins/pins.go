// Package pins keeps a short list of files for each working directory, in
// the order the user chooses: :pin and <Leader>a pin the current file,
// <Leader>1 to <Leader>9 go to one, and :pin list shows them as lines :w
// writes. Its name is pin, so its plugs are <Plug>(pin.next) and the others
// (B356 to B361).
package pins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/fsys"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/paths"
	"github.com/y3owk1n/fude/internal/trace"
)

// What pins registers, by the ids Call runs them by. slot+n-1 is
// <Leader>n.
const (
	command uint32 = iota + 1
	add
	list
	next
	prev
	open
	quit
	write
	slot
)

// most is how many pins a list holds.
const most = 9

// pin is a pinned file, by its absolute path, and where its cursor was,
// from 1.
type pin struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Module is pins. It keeps the host's pins of each working directory and
// each guest's own.
type Module struct {
	mu     sync.Mutex
	kept   map[string][]pin // the host's, by working directory
	guests map[string][]pin // by session
}

// New returns pins.
func New() *Module { return &Module{kept: map[string][]pin{}, guests: map[string][]pin{}} }

func (m *Module) Name() string { return "pin" }

func (m *Module) Subscribed() []string {
	return []string{"window.moved", "explorer.moved", "editor.quitting"}
}

func (m *Module) Registered() []api.Registration {
	regs := []api.Registration{
		{Kind: api.Command, Name: "pin", Args: "[remove|list|n]", Help: "pins the current file, takes it off, lists the pins or goes to pin n", ID: command},
		{Kind: api.Plug, Name: "add", Keys: "<Leader>a", Help: "pins the current file", ID: add},
		{Kind: api.Plug, Name: "list", Keys: "<Leader>h", Help: "lists the pins to edit", ID: list},
		{Kind: api.Plug, Name: "next", Help: "goes to the pin after the current file's", ID: next},
		{Kind: api.Plug, Name: "prev", Help: "goes to the pin before the current file's", ID: prev},
		{Kind: api.Plug, Name: "open", Keys: "<CR>", In: "pins", Help: "opens the pin on the cursor's line", ID: open},
		{Kind: api.Plug, Name: "close", Keys: "q", In: "pins", Help: "closes the pins unless they have changes not written", ID: quit},
		{Kind: api.Write, Name: "pins", Help: "makes the pins what the lines say", ID: write},
	}
	for n := range most {
		regs = append(regs, api.Registration{Kind: api.Plug, Name: strconv.Itoa(n + 1), Keys: fmt.Sprintf("<Leader>%d", n+1), Help: fmt.Sprintf("goes to pin %d", n+1), ID: slot + uint32(n)})
	}
	return regs
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call pins, unpins, lists, writes and goes to pins.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pins, err := m.pins(s)
	if err != nil {
		return api.Result{Error: err.Error()}, nil
	}
	if id >= slot {
		return goTo(s, pins, int(id-slot)+1), nil
	}
	switch id {
	case command:
		switch arg := strings.TrimSpace(s.About["args"]); arg {
		case "":
			return m.add(s, pins), nil
		case "remove":
			return m.remove(s, pins), nil
		case "list":
			return listed(s, pins), nil
		default:
			n, err := strconv.Atoi(arg)
			if err != nil || n < 1 || n > most {
				return api.Result{Error: "pin takes remove, list or a number from 1 to 9"}, nil
			}
			return goTo(s, pins, n), nil
		}
	case add:
		return m.add(s, pins), nil
	case list:
		return listed(s, pins), nil
	case next, prev:
		if len(pins) == 0 {
			return api.Result{Error: "no pins"}, nil
		}
		i := index(pins, s.About["path"])
		switch {
		case i < 0 && id == next:
			i = 0
		case i < 0:
			i = len(pins) - 1
		case id == next:
			i = (i + 1) % len(pins)
		default:
			i = (i + len(pins) - 1) % len(pins)
		}
		return goTo(s, pins, i+1), nil
	case open:
		t := s.Text
		line := t.LineOf(atoi(s.About["cursor"]))
		name := strings.TrimSpace(t.Slice(t.LineStart(line), lineEnd(t, line)))
		if name == "" {
			return api.Result{}, nil
		}
		to := pin{Path: resolve(s, name)}
		if i := index(pins, to.Path); i >= 0 {
			to = pins[i]
		}
		res := api.Result{Jump: &api.Jump{Path: shown(s, to.Path), Line: to.Line, Column: to.Column}}
		if b := atoi(s.About["buffer"]); !s.Buffers[b].Modified {
			res.Close = []int{b}
		}
		return res, nil
	case quit:
		b := atoi(s.About["buffer"])
		if s.Buffers[b].Modified {
			return api.Result{Error: "the pins have changes not written, :w writes them"}, nil
		}
		return api.Result{Close: []int{b}}, nil
	case write:
		return m.write(s, pins), nil
	}
	return api.Result{}, nil
}

// add pins the current file after the others.
func (m *Module) add(s api.Scope, pins []pin) api.Result {
	b := s.Buffers[atoi(s.About["buffer"])]
	switch {
	case b.Path == "" || b.Kind != "":
		return api.Result{Error: "this buffer has no file to pin"}
	case b.Barred:
		return api.Result{Error: "a guest may not pin a file outside the project"}
	}
	path := abs(b.Path)
	if i := index(pins, path); i >= 0 {
		return api.Result{Error: fmt.Sprintf("already pin %d", i+1)}
	}
	if len(pins) == most {
		return api.Result{Error: "9 pins already, :pin list takes one off"}
	}
	line, col := lineColumn(s.Text, atoi(s.About["cursor"]))
	pins = append(pins, pin{Path: path, Line: line, Column: col})
	return m.changed(s, pins, fmt.Sprintf("pin %d", len(pins)))
}

// remove takes the current file off.
func (m *Module) remove(s api.Scope, pins []pin) api.Result {
	i := index(pins, s.About["path"])
	if i < 0 {
		return api.Result{Error: "this file is not pinned"}
	}
	return m.changed(s, slices.Delete(slices.Clone(pins), i, i+1), "unpinned")
}

// write makes the pins what the list's lines say, in their order. A line
// naming no file, a file named twice or a tenth line stops it (B358).
func (m *Module) write(s api.Scope, pins []pin) api.Result {
	t := s.Text
	var out []pin
	for line := range t.Lines() {
		name := strings.TrimSpace(t.Slice(t.LineStart(line), lineEnd(t, line)))
		if name == "" {
			continue
		}
		path := resolve(s, name)
		if err := mayPin(s, path); err != nil {
			return api.Result{Error: fmt.Sprintf("line %d: %v", line+1, err)}
		}
		if index(out, path) >= 0 {
			return api.Result{Error: fmt.Sprintf("line %d: %s is pinned already", line+1, name)}
		}
		if len(out) == most {
			return api.Result{Error: fmt.Sprintf("line %d: 9 pins at most", line+1)}
		}
		p := pin{Path: path}
		if i := index(pins, path); i >= 0 {
			p = pins[i]
		}
		out = append(out, p)
	}
	return m.changed(s, out, "")
}

// changed makes pins the session's list, then says msg, and shows each
// open file's pin on the session's status lines (B359).
func (m *Module) changed(s api.Scope, pins []pin, msg string) api.Result {
	if err := m.set(s, pins); err != nil {
		return api.Result{Error: err.Error()}
	}
	return api.Result{Message: msg, SessionLeft: segments(s, pins)}
}

// Event keeps where a pinned file's cursor is, follows an explorer's moves
// and keeps the host's pins as fude quits.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	event, detail, _ := strings.Cut(name, " ")
	switch event {
	case "window.moved":
		pins, err := m.pins(s)
		if err != nil {
			return api.Result{Log: []string{err.Error()}}, nil
		}
		if len(pins) == 0 {
			return api.Result{}, nil
		}
		b := atoi(s.About["buffer"])
		if i := index(pins, s.About["path"]); i >= 0 {
			cursor, _, _ := strings.Cut(detail, " ")
			pins[i].Line, pins[i].Column = lineColumn(s.Text, atoi(cursor))
		}
		return api.Result{SessionLeft: map[int]string{b: segment(pins, s.Buffers[b])}}, nil
	case "explorer.moved":
		// This loads the host's pins of the directory, so the move reaches them.
		if _, err := m.pins(s); err != nil {
			return api.Result{Log: []string{err.Error()}}, nil
		}
		return api.Result{Log: m.moved(s.About["from"], s.About["to"])}, nil
	case "editor.quitting":
		var failed []string
		for wd, pins := range m.kept {
			if err := save(wd, pins); err != nil {
				failed = append(failed, err.Error())
			}
		}
		return api.Result{Log: failed}, nil
	}
	return api.Result{}, nil
}

// moved points the pins at or under from to where they are now, under to,
// and keeps the host's lists it changed. It returns what failed to write.
func (m *Module) moved(from, to string) []string {
	var failed []string
	follow := func(pins []pin) bool {
		changed := false
		for i, p := range pins {
			if rel, ok := under(from, p.Path); ok {
				pins[i].Path, changed = filepath.Join(to, rel), true
			}
		}
		return changed
	}
	for wd, pins := range m.kept {
		if follow(pins) {
			if err := save(wd, pins); err != nil {
				failed = append(failed, err.Error())
			}
		}
	}
	for _, pins := range m.guests {
		follow(pins)
	}
	return failed
}

// pins are the session's pins: the host's of its working directory, read
// the first time, or a guest's own (B361).
func (m *Module) pins(s api.Scope) ([]pin, error) {
	if s.About["power"] != "host" {
		return m.guests[s.About["session"]], nil
	}
	wd := s.About["cwd"]
	if pins, ok := m.kept[wd]; ok {
		return pins, nil
	}
	pins, err := load(wd)
	if err != nil {
		return nil, err
	}
	m.kept[wd] = pins
	return pins, nil
}

// set makes pins the session's, and writes the host's.
func (m *Module) set(s api.Scope, pins []pin) error {
	if s.About["power"] != "host" {
		m.guests[s.About["session"]] = pins
		return nil
	}
	m.kept[s.About["cwd"]] = pins
	return save(s.About["cwd"], pins)
}

// load reads the pins kept for wd, none when there are none.
func load(wd string) ([]pin, error) {
	path, err := keep.PinsPath(wd)
	if err != nil {
		return nil, fmt.Errorf("pins: %w", err)
	}
	at := time.Now()
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	trace.Since("file", fmt.Sprintf("read %q, %d bytes", path, len(data)), at, err)
	if err != nil {
		return nil, fmt.Errorf("pins: %w", err)
	}
	var pins []pin
	if err := json.Unmarshal(data, &pins); err != nil {
		return nil, fmt.Errorf("pins in %s: %w", path, err)
	}
	return pins, nil
}

// save keeps pins for wd (B356).
func save(wd string, pins []pin) error {
	path, err := keep.PinsPath(wd)
	if err != nil {
		return fmt.Errorf("pins: %w", err)
	}
	data, err := json.Marshal(pins)
	if err != nil {
		return fmt.Errorf("pins: %w", err)
	}
	if err := keep.Write(path, data); err != nil {
		return fmt.Errorf("pins: %w", err)
	}
	return nil
}

// goTo shows pin n's file with the cursor where it last was.
func goTo(s api.Scope, pins []pin, n int) api.Result {
	if n > len(pins) {
		return api.Result{Error: fmt.Sprintf("no pin %d", n)}
	}
	p := pins[n-1]
	if _, err := os.Stat(p.Path); err != nil {
		return api.Result{Error: fmt.Sprintf("pin %d, %s, is gone", n, shown(s, p.Path))}
	}
	return api.Result{Jump: &api.Jump{Path: shown(s, p.Path), Line: p.Line, Column: p.Column}}
}

// listed shows the pins in the window, pin n on line n, for :w to write
// (B358).
func listed(s api.Scope, pins []pin) api.Result {
	var text strings.Builder
	for _, p := range pins {
		text.WriteString(shown(s, p.Path))
		text.WriteByte('\n')
	}
	return api.Result{Scratch: &api.Scratch{Text: text.String(), Kind: "pins"}}
}

// mayPin says why path may not be pinned: it names no file, or one
// outside the project for a guest (B245).
func mayPin(s api.Scope, path string) error {
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return fmt.Errorf("%s is no file", shown(s, path))
	}
	if _, ok := under(s.About["project"], path); s.About["power"] != "host" && !ok {
		return errors.New("a guest may not pin a file outside the project")
	}
	return nil
}

// segments are the pin of each open file a user edits, for the session's
// status lines (B359).
func segments(s api.Scope, pins []pin) map[int]string {
	out := map[int]string{}
	for id, b := range s.Buffers {
		if b.Kind == "" && b.Path != "" {
			out[id] = segment(pins, b)
		}
	}
	return out
}

// segment is b's pin and how many there are, such as 2/4, or "".
func segment(pins []pin, b api.Buffer) string {
	if i := index(pins, b.Path); i >= 0 {
		return fmt.Sprintf("%d/%d", i+1, len(pins))
	}
	return ""
}

// index is where the file at path is among pins, or -1.
func index(pins []pin, path string) int {
	path = abs(path)
	return slices.IndexFunc(pins, func(p pin) bool { return paths.Same(p.Path, path) })
}

// resolve is the absolute path a list's line names, from the working
// directory.
func resolve(s api.Scope, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(s.About["cwd"], name)
}

// shown is path as the list and the window show it, relative to the
// working directory when under it.
func shown(s api.Scope, path string) string {
	if rel, ok := under(s.About["cwd"], path); ok {
		return rel
	}
	return path
}

// under returns path relative to dir, when it is under it.
func under(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// abs is path made absolute, or path when it cannot be.
func abs(path string) string {
	if a, err := filepath.Abs(path); err == nil {
		return a
	}
	return path
}

// lineColumn is the line and column of pos in t, from 1.
func lineColumn(t api.Text, pos int) (int, int) {
	pos = min(max(pos, 0), t.Len())
	line := t.LineOf(pos)
	return line + 1, pos - t.LineStart(line) + 1
}

// lineEnd is where line ends in t, before its newline.
func lineEnd(t api.Text, line int) int {
	if line+1 < t.Lines() {
		return t.LineStart(line+1) - 1
	}
	return t.Len()
}

// atoi is s as a number, 0 when it is none.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
