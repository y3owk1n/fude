// Package editor hosts the kernel and the vim machine for a set of files
// shown in a layout of windows.
package editor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/lsp"
	"github.com/y3owk1n/fude/plugin"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// Editor is the open files and what works on them: plugins, language
// servers and decorations. Every client's session shares it.
type Editor struct {
	bufs     []*buffer
	next     int // the id the next buffer gets
	cfg      config
	queue    chan func()
	queries  string // the directory of query files that replace shipped ones
	plugins  []*plugin.Plugin
	commands map[string]func(*Session) // ex commands plugins registered
	decos    map[any][]decoration      // by owner: a plugin, or a buffer for its diagnostics
	servers  map[string]*server        // by command and root
	started  bool                      // Load ran, so new buffers attach to servers at once
	sessions []*Session                // the first is the one that opened the editor
	setup    []func(*Session)          // what every session's machine gets, such as keymaps
	proposed int                       // the id of the newest proposal

	now func() time.Time // the clock edits are stamped with
}

// Session is one client's windows and vim machine on the shared editor. Its
// edits carry its origin, so its undo reverts only them (B77).
type Session struct {
	*Editor
	origin     kernel.Origin
	root       *node
	win        *window // the current window
	vim        *vim.Machine
	message    string
	done       bool
	ask        func(yes bool) // takes the answer to the question in the message
	float      string         // text shown over the windows at the cursor, such as hover
	floatSpans []syntax.Span  // the styles of the float's text
	menu       *menu          // the completions shown while typing
	ctrlX      bool           // <C-x> began a completion key in insert mode
	lspKeys    bool           // the attached keys are mapped
	later      []vim.Key      // keys to feed once the key being fed is done
	renameAt   int            // where grn was pressed
	pick       *pick          // the code actions shown to choose from
	under      *window        // the window a focused float was opened from
	depth      int            // the undo groups the machine has open
	pending    int            // the cursor when the group being made began

	width, height int // the space the windows share
}

// place is a position in a buffer, kept as a mark so it follows edits.
type place struct {
	buf  *buffer
	mark kernel.MarkID
}

// Open opens each path as a buffer and returns the first session, which
// shows the first. A path that does not exist opens an empty buffer. With no
// paths it opens one empty buffer with no file.
func Open(paths ...string) (*Session, error) { return open(defaults, paths) }

func open(cfg config, paths []string) (*Session, error) {
	if len(paths) == 0 {
		paths = []string{""}
	}
	ed := &Editor{next: 1, now: time.Now, cfg: cfg, queue: make(chan func(), 16), commands: map[string]func(*Session){}, decos: map[any][]decoration{}, servers: map[string]*server{}}
	// Without a config directory there are no overrides, and loadConfig
	// reports why.
	if dir, err := xdgDir("XDG_CONFIG_HOME", ".config"); err == nil {
		ed.queries = filepath.Join(dir, "fude", "queries")
	}
	ed.setup = []func(*Session){builtins}
	e := &Session{Editor: ed, origin: "user"}
	for i, p := range paths {
		msg, err := e.add(p)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			e.message = msg
		}
	}
	e.join()
	return e, nil
}

// Attach starts a session of origin o on the editor, showing the first
// session's buffer.
func (ed *Editor) Attach(o kernel.Origin) *Session {
	s := &Session{Editor: ed, origin: o}
	s.join()
	return s
}

// join gives the session a machine with everything sessions get, and one
// window on the first session's buffer, or the first buffer.
func (e *Session) join() {
	e.vim = vim.New(host{e})
	for _, f := range e.setup {
		f(e)
	}
	b := e.bufs[0]
	if len(e.sessions) > 0 {
		b = e.sessions[0].win.buf
	}
	e.win = newWindow(b, 0, 0)
	e.root = &node{win: e.win}
	e.sessions = append(e.sessions, e)
	e.layout()
}

// each gives every session, and every later one, f.
func (ed *Editor) each(f func(*Session)) {
	ed.setup = append(ed.setup, f)
	for _, s := range ed.sessions {
		f(s)
	}
}

// builtins registers fude's own objects, operators and commands on a
// session's machine.
func builtins(e *Session) {
	for name := range syntax.Objects {
		e.vim.Object(name, func(t vim.Text, start, end int) (int, int, bool) {
			if s := e.win.buf.syntax; s != nil {
				return s.Object(t, name, start, end)
			}
			return 0, 0, false
		})
	}
	e.vim.Operator("gc", e.comment)
	e.vim.Operator("gq", func(_ vim.Text, start, end int, _ bool) []vim.Edit {
		e.format(e.win.buf, start, end)
		return nil
	})
	for name := range requests {
		e.vim.Command(name, func(vim.Text, int, int) []vim.Edit {
			e.request(name)
			return nil
		})
	}
	e.vim.Map(vim.Normal, "<C-]>", "<Plug>(lsp.definition)")
	e.vim.Map(vim.Normal, "K", "<Plug>(lsp.hover)")
	for keys, n := range map[string]int{"]d": 1, "[d": -1} {
		name := "diagnostic" + keys
		e.vim.Command(name, func(_ vim.Text, _, count int) []vim.Edit {
			e.diagnostic(n * max(count, 1))
			return nil
		})
		e.vim.Map(vim.Normal, keys, "<Plug>("+name+")")
	}
	e.vim.Selector(func(t vim.Text, query string, start, end int) ([][2]int, error) {
		if s := e.win.buf.syntax; s != nil {
			return s.Select(t, query, start, end)
		}
		return nil, errors.New("no syntax for this file")
	})
}

// Start reads config.toml and opens the files a command line names. +N puts
// the cursor on the first non-blank of line N of the first file.
func Start(args []string) (*Session, error) {
	line := 0
	var paths []string
	for _, a := range args {
		if n, ok := strings.CutPrefix(a, "+"); ok {
			if v, err := strconv.Atoi(n); err == nil {
				line = v
				continue
			}
		}
		paths = append(paths, a)
	}
	cfg, cfgErr := loadConfig()
	e, err := open(cfg, paths)
	if err != nil {
		return nil, err
	}
	if line > 0 {
		t := e.Text()
		p := t.LineStart(min(line, t.Lines()) - 1)
		l := t.Slice(p, vim.LineEnd(t, p))
		e.setCursor(p + len(l) - len(strings.TrimLeft(l, " \t")))
	}
	if cfgErr != nil {
		e.message = cfgErr.Error()
	}
	e.each(func(s *Session) {
		for mode, maps := range map[vim.Mode]map[string]string{vim.Normal: cfg.Keymap.Normal, vim.Visual: cfg.Keymap.Visual, vim.Insert: cfg.Keymap.Insert} {
			for from, to := range maps {
				s.vim.Map(mode, from, to)
			}
		}
	})
	return e, nil
}

// add opens path as a new buffer at the end of the list.
func (e *Session) add(path string) (string, error) {
	b, msg, err := openBuffer(e.next, path)
	if err != nil {
		return "", err
	}
	e.next++
	e.bufs = append(e.bufs, b)
	b.text.Listen(heard{e.Editor, b})
	if limit := e.cfg.LargeFile; b.text.Snapshot().Len() > int(limit)<<20 {
		if msg == "" {
			msg = fmt.Sprintf("\"%s\" is over %dMB, syntax and LSP are off", path, limit)
		}
		return msg, nil
	}
	h, ok, err := syntax.New(b.filetype(), e.queries, e.queue)
	if err != nil {
		msg = err.Error()
	} else if ok {
		b.syntax = h
	}
	if e.started {
		e.attach(b)
	}
	return msg, nil
}

// heard passes a buffer's changes to its syntax and its language server,
// and marks the proposals they overlap stale (B74).
type heard struct {
	ed *Editor
	b  *buffer
}

func (l heard) Changed(before kernel.Snapshot, edits []kernel.Edit) {
	for owner, ds := range l.ed.decos {
		if _, ok := owner.(*Session); !ok {
			continue
		}
		for i, d := range ds {
			if d.buf != l.b {
				continue
			}
			start, end := l.b.text.Mark(d.start), l.b.text.Mark(d.end)
			for _, e := range edits {
				if e.Start < end && start < e.End || e.Start == e.End && start < e.Start && e.Start < end {
					ds[i].style = "proposal.stale"
				}
			}
		}
	}
	out := make([]api.Edit, len(edits))
	for i, e := range edits {
		out[i] = api.Edit(e)
	}
	if l.b.syntax != nil {
		l.b.syntax.Changed(before, out)
	}
	if s := l.b.server; s != nil {
		l.b.version++
		s.client.Change(lsp.URI(l.b.file.path), l.b.version, before, out)
	}
}

// Queue returns the work other goroutines hand the editor loop, such as new
// highlights. The loop runs each func it receives, then draws.
func (e *Session) Queue() <-chan func() { return e.queue }

// Feed handles one key. An error becomes the message.
func (e *Session) Feed(k vim.Key) {
	e.feed(k)
	for len(e.later) > 0 {
		k := e.later[0]
		e.later = e.later[1:]
		e.feed(k)
	}
}

// feed handles one key, typed or queued by a command for after it.
func (e *Session) feed(k vim.Key) {
	e.message = ""
	if e.completing(k) {
		e.reveal()
		return
	}
	if e.pick != nil {
		e.picking(k)
		e.reveal()
		return
	}
	if text := e.float; text != "" {
		// Any key closes the float. K again moves the cursor into it.
		e.float = ""
		if k == "K" && e.vim.Idle() {
			e.focus(text)
			return
		}
	}
	if e.under != nil && k == "q" && e.vim.Idle() {
		e.unfocus()
		return
	}
	if ask := e.ask; ask != nil {
		e.ask = nil
		ask(k == "y")
		e.reveal()
		return
	}
	if e.win.buf.spots != nil && e.vim.Idle() && e.listKey(k) {
		e.settle(nil)
		return
	}
	if e.win.buf.lines != nil && e.vim.Idle() && e.timelineKey(k) {
		e.reveal()
		return
	}
	e.settle(e.vim.Feed(k))
}

// Wait reports whether keys are held for a longer mapping, and how long to
// wait for the next key before calling Timeout.
func (e *Session) Wait() (time.Duration, bool) {
	return time.Duration(e.cfg.KeyTimeout) * time.Millisecond, e.vim.Waiting()
}

// Timeout runs the keys held for a longer mapping when no key came in time.
func (e *Session) Timeout() {
	e.message = ""
	e.settle(e.vim.Timeout())
}

// settle shows the error or message from the keys just run, and scrolls to
// the cursor.
func (e *Session) settle(err error) {
	if err != nil {
		e.message = err.Error()
	} else if msg := e.vim.Message(); msg != "" && e.message == "" {
		e.message = msg
	}
	e.syncKeys()
	if log := e.win.buf.log; log != nil {
		if err := log.failed(); err != nil {
			e.message = err.Error()
		}
	}
	e.reveal()
}

// SetSize sets the columns and rows the windows share, status lines
// included.
func (e *Session) SetSize(w, h int) {
	e.width, e.height = w, h
	e.layout()
	e.reveal()
}

// View is one window as a client draws it.
type View struct {
	Text              kernel.Snapshot
	Top, Cursor       int
	X, Y, W, H        int // H counts the status line on the bottom row
	Name              string
	Modified, Current bool
	Highlights        []syntax.Span // in paint order, later ones drawing over earlier ones
	Decorations       []syntax.Span // plugins' styles, drawn over highlights
	Ghosts            []Ghost
}

// Ghost is a proposal's text, drawn before the byte at Pos but not in the
// text (B71).
type Ghost struct {
	Pos   int
	Text  string
	Stale bool
}

// Views returns every window from the top left.
func (e *Session) Views() []View {
	var out []View
	for _, w := range e.root.windows() {
		t, top := w.buf.text.Snapshot(), e.topOf(w)
		v := View{
			Text: t, Top: top, Cursor: w.pos(),
			X: w.x, Y: w.y, W: w.w, H: w.h,
			Name: w.buf.name(), Modified: w.buf.modified(), Current: w == e.win || w == e.under,
		}
		last := vim.LineEnd(t, t.LineStart(min(t.LineOf(top)+max(w.h-2, 0), t.Lines()-1)))
		if h := w.buf.syntax; h != nil {
			v.Highlights = h.Show(t, top, last)
		}
		for _, ds := range e.decos {
			for _, d := range ds {
				if d.buf != w.buf {
					continue
				}
				s, end := w.buf.text.Mark(d.start), w.buf.text.Mark(d.end)
				if s < end && end > top && s < last {
					v.Decorations = append(v.Decorations, syntax.Span{Start: s, End: end, Name: d.style})
				}
				if strings.HasPrefix(d.style, "proposal") && d.note != "" && end >= top && end <= last {
					v.Ghosts = append(v.Ghosts, Ghost{end, d.note, d.style == "proposal.stale"})
				}
			}
		}
		out = append(out, v)
	}
	return out
}

// Top returns the start of the current window's first line shown.
func (e *Session) Top() int { return e.topOf(e.win) }

func (e *Session) topOf(w *window) int {
	return vim.LineStart(w.buf.text.Snapshot(), w.buf.text.Mark(w.top))
}

// rows is how many lines of text the current window shows.
func (e *Session) rows() int { return max(e.win.h-1, 1) }

// reveal scrolls the current window the least it must to show the cursor's
// line.
func (e *Session) reveal() {
	w := e.win
	t, c := w.buf.text.Snapshot(), w.pos()
	line := vim.LineStart(t, c)
	// low is the earliest top that still shows the cursor's line.
	low := line
	for range e.rows() - 1 {
		if low == 0 {
			break
		}
		low = vim.LineStart(t, low-1)
	}
	w.buf.text.MoveMark(w.top, min(max(e.Top(), low), line))
}

// Matches returns the search matches between from and to, for highlighting.
func (e *Session) Matches(from, to int) [][2]int { return e.vim.Matches(from, to) }

// Paste inserts text as typed, with no key mapping applied (B8).
func (e *Session) Paste(text string) {
	e.message = ""
	if err := e.vim.Paste(text); err != nil {
		e.message = err.Error()
	}
	e.reveal()
}

// Text returns the current window's text.
func (e *Session) Text() kernel.Snapshot { return e.win.buf.text.Snapshot() }

// Cursor returns the cursor's byte offset in the current window.
func (e *Session) Cursor() int { return e.win.pos() }

func (e *Session) setCursor(pos int) { e.win.buf.text.MoveMark(e.win.cursor, pos) }

// Mode returns the vim mode.
func (e *Session) Mode() vim.Mode { return e.vim.Mode() }

// Float is text a client draws over the windows at the cursor, styled by
// Spans. While it has the cursor, Top is the start of the first line it
// shows and Cursor is where the cursor is in it, else -1.
type Float struct {
	Text        string
	Spans       []syntax.Span
	Top, Cursor int
}

// Float returns the float to draw, or false when there is none.
func (e *Session) Float() (Float, bool) {
	if e.under != nil {
		t := e.Text()
		return Float{t.Slice(0, t.Len()), e.floatSpans, e.Top(), e.Cursor()}, true
	}
	return Float{e.float, e.floatSpans, 0, -1}, e.float != ""
}

// focus moves the cursor into a float showing text, a read-only window
// outside the layout, where keys move and scroll as in any window.
func (e *Session) focus(text string) {
	b := &buffer{text: kernel.New(text, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}, title: "[float]"}
	b.file.readonly = true
	w := newWindow(b, 0, 0)
	w.w, w.h = 80, 21
	e.under, e.win = e.win, w
}

// unfocus closes the float that has the cursor.
func (e *Session) unfocus() {
	e.win, e.under, e.floatSpans = e.under, nil, nil
}

// Cursors returns the cursors after the first while a change on several
// regions types into all of them.
func (e *Session) Cursors() []int { return e.vim.Cursors() }

// Selection returns the visual selection, or false outside visual mode.
func (e *Session) Selection() (vim.Selection, bool) { return e.vim.Selection() }

// Line returns the command line being typed.
func (e *Session) Line() string { return e.vim.Line() }

// Message returns the last message for the user, or "".
func (e *Session) Message() string { return e.message }

// Done reports whether the user has quit.
func (e *Session) Done() bool { return e.done }

// Recording returns the register a macro is being recorded into, or 0.
func (e *Session) Recording() rune { return e.vim.Recording() }

// show switches the current window to b. Leaving a buffer is a jump.
func (e *Session) show(b *buffer) {
	if b != e.win.buf {
		e.pushJump(e.Cursor())
		e.win.show(b)
	}
}

// pushJump records pos in the current window's buffer as a jump, dropping
// any older one on the same line of the same buffer.
func (e *Session) pushJump(pos int) {
	w := e.win
	b, t := w.buf, w.buf.text.Snapshot()
	var kept []place
	for _, j := range w.jumps {
		if j.buf != b || !sameLine(t, b.text.Mark(j.mark), pos) {
			kept = append(kept, j)
		}
	}
	w.jumps = append(kept, place{b, b.text.SetMark(pos, kernel.Right)})
	w.jumpAt = len(w.jumps)
}

// jump moves n jumps back, or forward when n is negative. The first step
// back records where the cursor is, so <C-i> can return to it.
func (e *Session) jump(n int) bool {
	w := e.win
	if n > 0 && w.jumpAt == len(w.jumps) {
		e.pushJump(e.Cursor())
		w.jumpAt = len(w.jumps) - 1
	}
	i := w.jumpAt - n
	if i < 0 || i >= len(w.jumps) {
		return false
	}
	j := w.jumps[i]
	if j.buf != w.buf {
		w.show(j.buf)
	}
	w.jumpAt = i
	e.setCursor(j.buf.text.Mark(j.mark))
	return true
}

// window runs a <C-w> command.
func (e *Session) window(k string) {
	var err error
	switch k {
	case "s", "S", "<C-s>":
		e.split(false)
	case "v", "<C-v>":
		e.split(true)
	case "h", "j", "k", "l":
		e.moveTo(k[0])
	case "<C-h>", "<C-j>", "<C-k>", "<C-l>":
		e.moveTo(k[3])
	case "c", "<C-c>":
		err = e.closeWindow(e.win)
	case "q", "<C-q>":
		e.quit(false)
	case "o", "<C-o>":
		e.only()
	case "=":
		// Splits always share their space equally, so there is nothing to
		// even out.
	}
	if err != nil {
		e.message = err.Error()
	}
}

// quit closes the current window, or quits once it is the last one. Without
// force it refuses to quit while a buffer has unsaved changes.
func (e *Session) quit(force bool) {
	if e.root.win == nil {
		if err := e.closeWindow(e.win); err != nil {
			e.message = err.Error()
		}
		return
	}
	e.quitAll(force)
}

// quitAll quits fude. Without force it refuses while a buffer has unsaved
// changes. A session other than the first only leaves, and fude keeps running.
func (e *Session) quitAll(force bool) {
	if e != e.sessions[0] {
		e.leave()
		return
	}
	for _, b := range e.bufs {
		if b.modified() && !force {
			e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", b.name())
			return
		}
	}
	for _, b := range e.bufs {
		b.close()
	}
	for _, p := range e.plugins {
		p.Close(context.Background())
	}
	for _, s := range e.servers {
		s.closing = true
		s.client.Close()
	}
	for _, s := range e.sessions {
		s.done = true
	}
}

// leave ends a session other than the first. Its proposals stay until a
// user decides them.
func (e *Session) leave() {
	e.sessions = slices.DeleteFunc(e.sessions, func(s *Session) bool { return s == e })
	e.done = true
}

func (e *Session) ex(line string) {
	name, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	arg = strings.TrimSpace(arg)
	name, force := strings.CutSuffix(name, "!")
	switch name {
	case "w":
		e.write(e.win.buf, force, nil)
	case "wq", "x", "xit", "exit":
		if name == "wq" || e.win.buf.modified() {
			e.write(e.win.buf, force, func() { e.quit(force) })
		} else {
			e.quit(force)
		}
	case "wa", "wall":
		e.writeAll(e.bufs, force, nil)
	case "qa", "qall", "quita", "quitall":
		e.quitAll(force)
	case "wqa", "wqall", "xa", "xall":
		e.writeAll(e.bufs, force, func() { e.quitAll(force) })
	case "q", "quit":
		e.quit(force)
	case "reg", "registers":
		e.registers(arg)
	case "e", "edit":
		e.edit(arg)
	case "sp", "split", "vs", "vsplit":
		e.split(name[0] == 'v')
		if arg != "" {
			e.edit(arg)
		}
	case "rename":
		e.rename(arg)
	case "accept", "reject":
		e.decide(arg, name == "accept")
	case "timeline":
		e.timeline()
	case "restore":
		e.restore(arg)
	case "diff":
		e.diff(arg)
	case "earlier", "later":
		e.travel(arg, name == "later")
	case "recover":
		e.recover()
	case "on", "only":
		e.only()
	case "clo", "close":
		if err := e.closeWindow(e.win); err != nil {
			e.message = err.Error()
		}
	case "ls", "buffers", "files":
		e.list()
	case "b", "buffer":
		if b := e.find(arg); b != nil {
			e.show(b)
		}
	case "bn", "bnext", "bp", "bprevious", "bN", "bNext":
		step := 1
		if name != "bn" && name != "bnext" {
			step = len(e.bufs) - 1
		}
		e.show(e.bufs[(index(e.bufs, e.win.buf)+step)%len(e.bufs)])
	case "bd", "bdelete":
		b := e.win.buf
		if arg != "" {
			b = e.find(arg)
		}
		if b != nil {
			e.close(b, force)
		}
	default:
		if run, ok := e.commands[name]; ok {
			run(e)
			return
		}
		e.message = "not a command: " + line
	}
}

// edit shows the file at path, opening it if no buffer holds it.
func (e *Session) edit(path string) {
	switch path {
	case "":
		e.message = "no file name"
		return
	case "#":
		if b := e.find(path); b != nil {
			e.show(b)
		}
		return
	}
	for _, b := range e.bufs {
		if b.is(path) {
			e.show(b)
			return
		}
	}
	msg, err := e.add(path)
	if err != nil {
		e.message = err.Error()
		return
	}
	e.show(e.bufs[len(e.bufs)-1])
	e.message = msg
}

// list shows every buffer with its number, % for the current window's and +
// for a modified one, as :ls does.
func (e *Session) list() {
	var shown []string
	for _, b := range e.bufs {
		flags := ""
		if b == e.win.buf {
			flags += "%"
		}
		if b.modified() {
			flags += "+"
		}
		shown = append(shown, fmt.Sprintf("%d%s \"%s\"", b.id, flags, b.name()))
	}
	e.message = strings.Join(shown, "  ")
}

// find returns the buffer numbered s or the one whose name contains s. #
// names the buffer the window showed before.
func (e *Session) find(s string) *buffer {
	if s == "#" {
		if e.win.alt == nil {
			e.message = "no alternate buffer"
		}
		return e.win.alt
	}
	var found *buffer
	for _, b := range e.bufs {
		switch {
		case strconv.Itoa(b.id) == s:
			return b
		case s != "" && strings.Contains(b.name(), s):
			if found != nil {
				e.message = "more than one buffer matches " + s
				return nil
			}
			found = b
		}
	}
	if found == nil {
		e.message = "no buffer matches " + s
	}
	return found
}

// close removes b, refusing a modified buffer unless forced. Windows showing
// it move to the next buffer, or to a new empty buffer if none is left.
func (e *Session) close(b *buffer, force bool) {
	if b.modified() && !force {
		e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", b.name())
		return
	}
	e.detach(b)
	b.close()
	i := index(e.bufs, b)
	e.bufs = append(e.bufs[:i], e.bufs[i+1:]...)
	if len(e.bufs) == 0 {
		if _, err := e.add(""); err != nil {
			e.message = err.Error()
			return
		}
	}
	for _, s := range e.sessions {
		for _, w := range s.root.windows() {
			var kept []place
			for _, j := range w.jumps {
				if j.buf != b {
					kept = append(kept, j)
				}
			}
			w.jumps, w.jumpAt = kept, len(kept)
			if w.alt == b {
				w.alt = nil
			}
			if w.buf == b {
				w.show(e.bufs[min(i, len(e.bufs)-1)])
			}
		}
	}
}

// registers shows each named register, as :reg does.
func (e *Session) registers(names string) {
	var shown []string
	for _, r := range names {
		if text, ok := e.vim.Register(r); ok {
			shown = append(shown, fmt.Sprintf("\"%c   %s", r, strings.ReplaceAll(text, "\n", "^J")))
		}
	}
	e.message = strings.Join(shown, "  ")
}

// write writes b to its file, then runs then. Without force it asks first
// when the file changed on disk since fude read it.
func (e *Session) write(b *buffer, force bool, then func()) {
	switch {
	case b.file.path == "":
		e.message = "no file name"
		return
	case b.file.readonly:
		e.message = fmt.Sprintf("\"%s\" is read-only", b.file.path)
		return
	case !force && b.file.changed():
		e.message = fmt.Sprintf("\"%s\" changed on disk since fude read it, write anyway? (y/n)", b.file.path)
		e.ask = func(yes bool) {
			if yes {
				e.write(b, true, then)
			}
		}
		return
	}
	s := b.text.Snapshot()
	n, err := b.file.write(s.Slice(0, s.Len()))
	if err != nil {
		e.message = err.Error()
		return
	}
	tag := ""
	if b.file.isNew {
		tag = "[New] "
	}
	b.file.isNew = false
	b.markSaved()
	e.message = fmt.Sprintf("\"%s\" %s%dB written", b.file.path, tag, n)
	if then != nil {
		then()
	}
}

// writeAll writes each modified buffer in bs, then runs then. It stops at
// the first buffer that fails to write.
func (e *Session) writeAll(bs []*buffer, force bool, then func()) {
	for i, b := range bs {
		if b.modified() {
			e.write(b, force, func() { e.writeAll(bs[i+1:], force, then) })
			return
		}
	}
	if then != nil {
		then()
	}
}

// Focus reloads each unmodified buffer whose file changed on disk, as one
// undo step. A modified one keeps its text, and fude warns once.
func (e *Session) Focus() {
	for _, b := range e.bufs {
		if b.title != "" || b.file.warned || !b.file.changed() {
			continue
		}
		if b.modified() {
			e.message = fmt.Sprintf("\"%s\" changed on disk and in fude, keeping fude's text", b.file.path)
			b.file.warned = true
			continue
		}
		f, text, err := read(b.file.path)
		if err != nil {
			e.message = err.Error()
			continue
		}
		now := b.text.Snapshot()
		if old := now.Slice(0, now.Len()); old != text {
			t := kernel.Transaction{Base: now.Version(), Origin: e.origin, Label: "reload", Time: e.now(), Edits: []kernel.Edit{replace(old, text)}}
			if err := b.text.Apply(t); err != nil {
				e.message = err.Error()
				continue
			}
			b.head = b.text.Head()
		}
		b.file = f
		b.markSaved()
		e.message = fmt.Sprintf("\"%s\" changed on disk, reloaded", b.file.path)
	}
	e.reveal()
}

// host implements vim.Host for an Editor, acting on the current window.
type host struct{ e *Session }

func (h host) Text() vim.Text    { return h.e.Text() }
func (h host) Cursor() int       { return h.e.Cursor() }
func (h host) SetCursor(pos int) { h.e.setCursor(pos) }
func (h host) BeginGroup() {
	h.begin()
	h.e.depth++
	h.e.win.buf.text.BeginGroup(h.e.origin)
}

func (h host) EndGroup() {
	h.e.depth--
	h.e.win.buf.text.EndGroup(h.e.origin)
	h.end()
}

// begin notes the cursor when a new undo step may begin.
func (h host) begin() {
	if h.e.depth == 0 {
		h.e.pending = h.e.Cursor()
	}
}

// end saves the noted cursor for the step just made, if there is one.
func (h host) end() {
	b := h.e.win.buf
	if head := b.text.Head(); head != b.head {
		b.before[head] = h.e.pending
		b.head = head
	}
}

func (h host) SetMark(name rune, pos int) {
	b := h.e.win.buf
	if id, ok := b.marks[name]; ok {
		b.text.MoveMark(id, pos)
		return
	}
	b.marks[name] = b.text.SetMark(pos, kernel.Right)
}

func (h host) Mark(name rune) (int, bool) {
	b := h.e.win.buf
	id, ok := b.marks[name]
	if !ok {
		return 0, false
	}
	return b.text.Mark(id), true
}

func (h host) Viewport() (int, int) { return h.e.Top(), h.e.rows() }
func (h host) SetTop(top int)       { h.e.win.buf.text.MoveMark(h.e.win.top, top) }
func (h host) PushJump(pos int)     { h.e.pushJump(pos) }
func (h host) Jump(n int) bool      { return h.e.jump(n) }

func (h host) Change(n int) bool {
	b := h.e.win.buf
	i := b.changeAt - n
	if i < 0 || i >= len(b.changes) {
		return false
	}
	b.changeAt = i
	h.e.setCursor(b.text.Mark(b.changes[i]))
	return true
}

func (h host) Step(n int) (int, bool) { return h.e.step(n) }

// IndentWidth is the setting for the buffer's filetype, else the top-level
// one.
func (h host) IndentWidth() int {
	w := h.e.cfg.IndentWidth
	if ft, ok := h.e.cfg.Filetype[h.e.win.buf.filetype()]; ok && ft.IndentWidth > 0 {
		w = ft.IndentWidth
	}
	if w == 0 {
		return vim.Tabstop
	}
	return int(w)
}

// Undo reverts the session's newest change. When another origin changed the
// text since, it becomes a new node and the cursor goes to its start.
func (h host) Undo() (int, int, error) {
	b := h.e.win.buf
	node, n := b.text.Head(), len(b.text.Nodes())
	start, err := b.text.UndoOrigin(h.e.origin, "undo", h.e.now())
	switch {
	case errors.Is(err, kernel.ErrNothing):
		return 0, 0, vim.ErrNoStep
	case errors.Is(err, kernel.ErrOverlap):
		return 0, 0, errors.New("cannot undo, a later change overlaps it")
	case err != nil:
		return 0, 0, err
	}
	cursor := b.before[node]
	if len(b.text.Nodes()) > n {
		cursor = start
	}
	b.head = b.text.Head()
	return start, cursor, nil
}

// Redo moves to the newest node undone from the head, whoever made it.
func (h host) Redo() (int, int, error) {
	b := h.e.win.buf
	start, ok := b.text.Redo()
	b.head = b.text.Head()
	if !ok {
		return 0, 0, vim.ErrNoStep
	}
	return start, b.before[b.head], nil
}

func (h host) Unhandled(k string) {
	if h.e.under != nil {
		// In a float, :q, :close and <C-w> only leave it. Any other ex line
		// leaves it and acts on the window under it.
		h.e.unfocus()
		if name, _, _ := strings.Cut(strings.TrimPrefix(k, ":"), " "); strings.HasPrefix(k, "<C-w>") || slices.Contains([]string{"q", "q!", "quit", "quit!", "clo", "close"}, name) {
			return
		}
	}
	if w, ok := strings.CutPrefix(k, "<C-w>"); ok {
		h.e.window(w)
		return
	}
	h.e.ex(strings.TrimPrefix(k, ":"))
}

func (h host) Apply(edits []vim.Edit, label string) error {
	b := h.e.win.buf
	if b.file.readonly {
		return errors.New("cannot make changes, the file is read-only")
	}
	t := kernel.Transaction{Base: b.text.Snapshot().Version(), Origin: h.e.origin, Label: label, Time: h.e.now()}
	for _, ed := range edits {
		t.Edits = append(t.Edits, kernel.Edit(ed))
	}
	h.begin()
	err := b.text.Apply(t)
	h.end()
	if err == nil {
		b.pushChange(edits[0].Start)
	}
	return err
}
