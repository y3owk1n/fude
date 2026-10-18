// Package editor hosts the kernel and the vim machine for a set of files
// shown in a layout of windows.
package editor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Editor is the open files and what works on them: plugins, language
// servers and decorations. Every client's session shares it.
type Editor struct {
	bufs    []*buffer
	next    int // the id the next buffer gets
	cfg     config
	theme   string     // the theme clients draw in, which :theme changes
	base16  *[16]int32 // the colours of the base16 scheme it names
	queue   chan func()
	queries string       // the directory of query files that replace shipped ones
	modules []api.Module // the plugins that loaded, in the order they did
	// What plugins add, for help and the key hints: what each command and
	// key does, by :name and <Plug>(name), and their operators and objects.
	pluginHelp     map[string]string
	pluginBindings []vim.Binding
	plugged        int                               // how many of setup come before the plugins' own
	commands       map[string]func(*Session, call)   // ex commands plugins registered
	sources        map[string]func(*Session, string) // :pick sources modules registered, by name (B287)
	completers     map[string]func(*Session, string) // by command, what asks a module to complete its argument (B290)
	decos          map[any][]decoration              // by owner: a plugin, or a buffer for its diagnostics
	servers        map[string]*server                // by command and root
	down           map[string]string                 // by name, why a configured language server is not running
	building       map[string]bool                   // the filetypes whose grammar builds now
	failed         map[string]string                 // by filetype, why its grammar did not build this session
	started        bool                              // Load ran, so new buffers attach to servers at once
	project        string                            // the directory fude started in, symlinks resolved (B243)
	share          *share                            // the guests' listener, while fude shares (B248)
	clients        atomic.Int64                      // the attached clients yet to have their last frame (B242)
	began          int                               // how many sessions began, for the next one's colour (B252)
	sessions       []*Session                        // the first is the one that opened the editor
	setup          []func(*Session)                  // what every session's machine gets, such as keymaps
	proposed       int                               // the id of the newest proposal
	listeners      map[api.Module]*listener
	segments       map[api.Module]map[*buffer]string // what each plugin shows on the statusline of each buffer
	leftSegments   map[api.Module]map[*buffer]string // what each shows on its left, after the buffer's name (B147)

	now      func() time.Time       // the clock edits are stamped with
	snippets map[string]snippetFile // each snippets file read, by path (B207)
	pictures pictures               // the images decorations show (B303)
}

// Session is one client's windows and vim machine on the shared editor. Its
// edits carry its origin, so its undo reverts only them (B77).
type Session struct {
	*Editor
	origin     kernel.Origin
	power      Power               // how much the session may do (B243)
	name       string              // who uses the session, "" for a client that sent none (B252)
	leader     string              // what <Leader> in a plugin's keys stands for, a guest's own (B254)
	following  *Session            // the session whose windows :follow copies (B256)
	mirrors    map[*window]*window // by the other's window, the window that copies it
	colour     int                 // which of six colours shows the session to others (B252)
	root       *node
	win        *window // the current window
	vim        *vim.Machine
	message    string
	done       bool
	cell       [2]int                  // the client's cell in pixels, 0 when it shows no images (B303)
	sentImages map[int]bool            // the images an attached client has (B303)
	freed      []int                   // the images dropped, for the client to free (B306)
	restarting string                  // the session file :restart saved, for main (B170)
	hints      []vim.Hint              // the keys that may follow the command being typed, while they show (B172)
	ask        question                // takes the answer to the question in the message
	jumpTo     *jumpTo                 // where a plugin's call asked to move the cursor, after the key
	lastList   *places                 // the last list of places a window showed
	snip       *snippet                // the snippet being filled in (B207)
	float      floating                // text shown over the windows at the cursor, such as hover
	completion completion              // the completions shown while typing, and the signature help
	lspKeys    bool                    // the attached keys are mapped
	later      []vim.Key               // keys to feed once the key being fed is done
	pick       *pick                   // the code actions shown to choose from
	finder     *finder                 // the file finder, while it shows
	jumping    *jump                   // the jump being typed after s (B218)
	cmdline    cmdline                 // the completions of the command line being typed
	searching  *[2]int                 // the top and cursor of the window a search is typed in, to put back (B21)
	locals     map[string]*vim.Keymaps // the keymaps of kinds of buffer, by kind, over the machine's (B142)
	lspCards   []lspCard               // the cards of the :lsp panel, in order
	history    []string                // the messages shown, oldest first, at most 200 (B157)
	logged     string                  // the message last put in history
	under      *window                 // the window a focused float was opened from
	opts       options                 // what the session's new windows start with
	changed    chan change             // told when the shown text changes, for a client over the socket
	clip       string                  // the text last copied, when no clipboard command runs
	toTerminal bool                    // the client has yet to put clip on its terminal's clipboard
	mode       vim.Mode                // the mode as plugins last heard it
	seen       view                    // the current window as plugins last heard it
	saves      map[*buffer]*saving     // the writes waiting for modules (B282)
	inSave     *saving                 // the write the result being applied is for, or nil
	depth      int                     // the undo groups the machine has open
	pending    int                     // the cursor when the group being made began

	width, height int  // the space the windows share
	lined         bool // the buffer line took the top row at the last layout (B258)
	centered      bool // the windows sit in the middle of the screen (B199)
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
	ed := &Editor{next: 1, now: time.Now, cfg: cfg, queue: make(chan func(), 16), commands: map[string]func(*Session, call){}, sources: map[string]func(*Session, string){}, completers: map[string]func(*Session, string){}, decos: map[any][]decoration{}, servers: map[string]*server{}, down: map[string]string{}, building: map[string]bool{}, failed: map[string]string{}, listeners: map[api.Module]*listener{}, segments: map[api.Module]map[*buffer]string{}, leftSegments: map[api.Module]map[*buffer]string{}}
	// Without a config directory there are no overrides, and loadConfig
	// reports why.
	if dir, err := keep.Dir("XDG_CONFIG_HOME", ".config"); err == nil {
		ed.queries = filepath.Join(dir, "fude", "queries")
	}
	// Without a data directory no grammar builds or loads, and a build says
	// why.
	if dir, err := keep.Dir("XDG_DATA_HOME", filepath.Join(".local", "share")); err == nil {
		// config.toml does not change while fude runs, so workers may resolve
		// through it.
		syntax.Use(filepath.Join(dir, "fude", "grammars"), func(name string) string { return ed.filetypeOf("code." + name) })
	}
	wd, err := os.Getwd()
	if err == nil {
		wd, err = filepath.EvalSymlinks(wd)
	}
	if err != nil {
		return nil, fmt.Errorf("find the project: %w", err)
	}
	ed.project = wd
	ed.setup = []func(*Session){builtins}
	e := &Session{Editor: ed, origin: "user", name: os.Getenv("USER"), saves: map[*buffer]*saving{}}
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

// Attach starts a session of origin o and power p on the editor, showing
// the first session's buffer.
func (ed *Editor) Attach(o kernel.Origin, p Power) *Session {
	s := &Session{Editor: ed, origin: o, power: p, saves: map[*buffer]*saving{}}
	s.join()
	return s
}

// join gives the session a machine with everything sessions get, and one
// window on the first session's buffer, or the first buffer.
func (e *Session) join() {
	e.leader = e.cfg.Leader
	e.vim = vim.New(host{e})
	e.vim.AutoPairs(e.cfg.Autopairs)
	e.vim.Surround(true)
	for _, f := range e.setup {
		f(e)
	}
	b := e.bufs[0]
	if len(e.sessions) > 0 {
		b = e.sessions[0].win.buf
	}
	e.opts, e.centered = e.cfg.options, e.cfg.Center
	e.win = newWindow(b, 0, 0)
	e.win.opts = e.opts
	e.root = &node{win: e.win}
	e.colour, e.began = e.began%6, e.began+1
	e.sessions = append(e.sessions, e)
	e.layout()
	// A guest starts on a buffer it may show, a new empty one if no other
	// (B245).
	if shown := e.mayShow(b); shown != b {
		e.win.show(shown)
		e.win.alt = nil
	}
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
	for _, op := range hostOperators {
		e.vim.Operator(op.keys, func(t vim.Text, start, end int, lines bool) []vim.Edit {
			return op.run(e, t, start, end, lines)
		})
	}
	for _, p := range plugs {
		e.vim.Command(p.name, func(_ vim.Text, _, count int) []vim.Edit {
			if e.power != Host && refusedPlugs[p.name] {
				e.message = "a guest may not run <Plug>(" + p.name + ")"
				return nil
			}
			p.run(e, count)
			return nil
		})
	}
	// The host's leader stands for <Leader> in every session's defaults.
	for keys, to := range defaultMaps {
		e.vim.Map(vim.Normal, withLeader(keys, e.cfg.Leader), to)
	}
	e.vim.Map(vim.Visual, "s", "<Plug>(jump)")
	e.locals = map[string]*vim.Keymaps{}
	for kind, keys := range kindKeys() {
		mode := vim.Normal
		if kind == "finder" {
			mode = vim.Insert
		}
		e.locals[kind] = &vim.Keymaps{}
		for from, to := range keys {
			e.locals[kind].Map(mode, from, to)
		}
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
	// A restart opens the saved session's files instead (B170).
	restored, restoreErr := restartSession()
	if restored != nil {
		paths, line = nil, 0
		for _, b := range restored.Buffers {
			paths = append(paths, b.Path)
		}
	}
	// fude started on a directory works in it (B201).
	if len(paths) == 1 && restored == nil {
		if info, err := os.Stat(paths[0]); err == nil && info.IsDir() {
			if err := os.Chdir(paths[0]); err != nil {
				return nil, fmt.Errorf("cd %s: %w", paths[0], err)
			}
			paths[0] = "."
		}
	}
	cfg, cfgErr := loadConfig()
	e, err := open(cfg, paths)
	if err != nil {
		return nil, err
	}
	if restored != nil {
		e.restoreSession(*restored)
	}
	if restoreErr != nil {
		e.message = restoreErr.Error()
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
	if err := e.setTheme(cfg.Theme); err != nil {
		e.theme = "dark"
		e.message = cmp.Or(e.message, err.Error())
	}
	// A guest takes its own keys (B254).
	e.each(func(s *Session) {
		if s.power == Host {
			s.useKeys(cfg.Keymap, cfg.Leader)
		}
	})
	return e, nil
}

// useKeys maps km's keys on e's machine and kinds of buffer, with leader as
// <Leader>.
func (e *Session) useKeys(km keymap, leader string) {
	for mode, maps := range map[vim.Mode]map[string]string{vim.Normal: km.Normal, vim.Visual: km.Visual, vim.Insert: km.Insert} {
		mapAll(e.vim, mode, maps, leader)
	}
	for kind, maps := range map[string]map[string]string{"listing": km.Listing, "list": km.List, "timeline": km.Timeline, "float": km.Float, "lsp": km.Lsp, "help": km.Help, "scratch": km.Scratch, "start": km.Start, "terminal": km.Terminal, "debug": km.Debug} {
		mapAll(e.locals[kind], vim.Normal, maps, leader)
	}
	mapAll(e.locals["finder"], vim.Insert, km.Finder, leader)
}

// add opens path as a new buffer at the end of the list.
func (e *Session) add(path string) (string, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("list %s: %w", path, err)
		}
		b, err := listingBuffer(e.next, abs)
		if err != nil {
			return "", err
		}
		e.next++
		e.bufs = append(e.bufs, b)
		e.emit("listing.read", "", b)
		return "", nil
	}
	b, msg, err := openBuffer(e.next, path)
	if err != nil {
		return "", err
	}
	e.next++
	e.bufs = append(e.bufs, b)
	if b.picture != "" {
		e.decos[b] = append(e.decos[b], decoration{buf: b, start: b.text.SetMark(0, kernel.Left), end: b.text.SetMark(0, kernel.Left), image: b.picture})
		b.inlines++
	}
	b.text.Listen(heard{e.Editor, b})
	e.emit("buffer.opened", "", b)
	if limit := e.cfg.LargeFile; b.text.Snapshot().Len() > int(limit)<<20 {
		if msg == "" {
			msg = fmt.Sprintf("\"%s\" is over %dMB, syntax and LSP are off", path, limit)
		}
		return msg, nil
	}
	if ok, err := e.attachSyntax(b); err != nil {
		msg = err.Error()
	} else if ft := e.filetype(b); !ok && syntax.Buildable(ft) && e.cfg.AutoInstall && e.buildGrammar(ft, false) {
		msg = cmp.Or(msg, "building the "+ft+" grammar")
	}
	if e.started {
		if err := e.attach(b); err != nil {
			msg = strings.TrimSpace(msg + "  " + err.Error())
		}
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
	l.b.touched = touched(edits)
	l.ed.changed(l.b, edits)
	for _, s := range l.ed.sessions {
		if s.changed != nil && s.win.buf == l.b {
			// This change replaces one the client has yet to hear of. The
			// loop is the only sender, so the send never waits.
			select {
			case <-s.changed:
			default:
			}
			s.changed <- change{l.b.id, before.Version() + 1}
		}
	}
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
	if l.b.syntax != nil {
		changed := make([]syntax.Edit, len(edits))
		for i, e := range edits {
			changed[i] = syntax.Edit(e)
		}
		l.b.syntax.Changed(before, changed)
	}
	out := make([]lsp.Edit, len(edits))
	for i, e := range edits {
		out[i] = lsp.Edit(e)
	}
	for _, s := range l.b.servers {
		s.versions[l.b]++
		s.client.Change(lsp.URI(l.b.file.path), s.versions[l.b], before, out)
	}
}

// feedLater feeds the keys queued while a key or a module's result was
// handled, such as the A that starts the finder's query.
func (e *Session) feedLater() {
	for len(e.later) > 0 {
		k := e.later[0]
		e.later = e.later[1:]
		e.feed(k)
	}
}

// Queue returns the work other goroutines hand the editor loop, such as new
// highlights. The loop runs each func it receives, then draws.
func (e *Session) Queue() <-chan func() { return e.queue }

// Feed handles one key. An error becomes the message.
func (e *Session) Feed(k vim.Key) {
	defer e.unfollow()()
	at, mode := time.Now(), e.vim.Mode()
	defer func() { trace.Record("key", fmt.Sprintf("%q in %s", k, modes[mode]), time.Since(at)) }()
	// While hints show, <Esc> drops the command being typed, held keys too,
	// and <BS> its last key.
	if e.hints != nil && (k == "<Esc>" || k == "<BS>") {
		if k == "<Esc>" {
			e.vim.Cancel()
		} else {
			e.vim.Back()
		}
		e.ShowHints()
		return
	}
	e.feed(k)
	e.feedLater()
	// A cursor that lands inside a closed fold, past its first line, opens
	// it, as search, G and marks do (B224).
	if t, b := e.Text(), e.win.buf; len(b.folds) > 0 {
		line := t.LineOf(e.Cursor())
		if first, _ := e.win.fold(t, line); first != line {
			e.openFoldAt(b, t, line)
		}
	}
	if e.hints != nil {
		e.ShowHints()
	}
}

// feed handles one key, typed or queued by a command for after it.
func (e *Session) feed(k vim.Key) {
	e.fit(e.win)
	e.message = ""
	if e.vim.Mode() == vim.Cmdline && k == "<CR>" {
		at, line := time.Now(), e.vim.Line()
		defer func() { trace.Record("cmd", line, time.Since(at)) }()
	}
	for _, o := range e.overlays() {
		if o.key(e, k) {
			return
		}
	}
	e.vim.IgnoreCase(e.win.opts.IgnoreCase, e.win.opts.SmartCase)
	e.vim.ExpandTab(e.win.opts.ExpandTab)
	e.vim.UseClipboard(e.win.opts.Clipboard)
	e.vim.UseLocal(e.localKeys(e.win.buf))
	at, top := e.Cursor(), e.topOf(e.win)
	e.settle(e.vim.Feed(k))
	e.incSearch(top, at)
	if start, end, put, ok := e.vim.Flash(); ok {
		kind := "yank"
		if put {
			kind = "put"
		}
		e.touch(e.win.buf, kind, start, end)
	}
	e.snippetAfter()
	e.autocomplete(k)
	e.signatureHelp(k)
	e.autocommand()
}

// Wait reports whether keys are held for a longer mapping, and how long to
// wait for the next key before calling Timeout.
func (e *Session) Wait() (time.Duration, bool) {
	if t := e.win.buf.term; t != nil && t.esc {
		return escTwice, true
	}
	return time.Duration(e.cfg.KeyTimeout) * time.Millisecond, e.vim.Waiting() && e.hints == nil
}

// Timeout runs the keys held for a longer mapping when no key came in time,
// or sends a terminal's program the <Esc> no second one followed.
func (e *Session) Timeout() {
	e.message = ""
	if t := e.win.buf.term; t != nil && t.esc {
		t.esc = false
		t.em.SendKey(uv.KeyPressEvent{Code: uv.KeyEscape})
		return
	}
	e.settle(e.vim.Timeout())
}

// settle shows the error or message from the keys just run, and scrolls to
// the cursor.
func (e *Session) settle(err error) {
	e.went()
	if err != nil {
		e.message = err.Error()
	} else if msg := e.vim.Message(); msg != "" && e.message == "" {
		e.message = msg
	}
	e.syncKeys()
	if log := e.win.buf.log; log != nil {
		if err := log.Failed(); err != nil {
			e.message = err.Error()
		}
	}
	e.reveal()
	if m := e.vim.Mode(); m != e.mode {
		e.mode = m
		e.emit("mode.changed", modes[m], e.win.buf)
	}
	if v := (view{e.win.buf, e.Cursor(), e.Top()}); v != e.seen {
		e.seen = v
		e.emit("view.moved", fmt.Sprintf("%d %d", v.cursor, v.top), v.buf)
	}
}

// view is where the current window is, for view.moved.
type view struct {
	buf         *buffer
	cursor, top int
}

// SetSize sets the columns and rows the windows share, status lines
// included.
func (e *Session) SetSize(w, h int) {
	e.width, e.height = w, h
	e.layout()
	e.reveal()
}

// Shown returns the keys of the command being typed, or "".
func (e *Session) Shown() string {
	if e.jumping != nil {
		return "s" + e.jumping.query
	}
	return e.vim.Shown()
}

// Top returns the start of the current window's first line shown.
func (e *Session) Top() int { return e.topOf(e.win) }

func (e *Session) topOf(w *window) int {
	return vim.LineStart(w.buf.text.Snapshot(), w.buf.text.Mark(w.top))
}

// rows is how many lines of text the current window shows.
func (e *Session) rows() int { return max(e.win.h-1, 1) }

// reveal scrolls the current window the least it must to show the cursor's
// line, which takes as many rows as it wraps to (B37). While a search is
// typed, it shows the match the search goes to instead.
func (e *Session) reveal() {
	w := e.win
	t, c := w.buf.text.Snapshot(), w.pos()
	// While a search is typed, the match it goes to shows instead (B21).
	if s, _, ok := e.vim.Incremental(); ok && w.opts.IncSearch {
		c = s
	}
	line := vim.LineStart(t, c)
	// scroll_off lines stay in view above and below the cursor's, at most
	// half the window (B116).
	above, below := line, line
	for range min(w.opts.ScrollOff, (e.rows()-1)/2) {
		above = w.prevLine(t, above)
		below, _ = w.nextLine(t, below)
	}
	// low is the earliest top that still shows below, found walking back
	// from it, so a far jump looks at one window of lines.
	tab := e.tabWidth(w.buf)
	d := e.shown(w, t.LineStart(max(t.LineOf(below)-e.rows(), 0)), vim.LineEnd(t, below))
	low, rows := below, w.rows(t, below, tab, d)
	for low > 0 {
		prev := w.prevLine(t, low)
		if rows += w.rows(t, prev, tab, d); rows > e.rows() {
			break
		}
		low = prev
	}
	// A top inside a closed fold is its first line (B224).
	first, _ := w.fold(t, t.LineOf(e.Top()))
	w.buf.text.MoveMark(w.top, min(max(t.LineStart(first), low), above))
	// Without wrap the view scrolls sideways the least it must to show the
	// cursor's column (B37).
	if col := vim.Column(t, line, c, tab, d.inlines); !w.opts.Wrap {
		w.left = min(max(w.left, col-w.cols()+1), col)
	}
}

// Paste inserts text as typed, with no key mapping applied (B8).
func (e *Session) Paste(text string) {
	defer e.unfollow()()
	e.message = ""
	if t := e.win.buf.term; t != nil && t.typing {
		t.em.Paste(text)
		return
	}
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

// Signature returns the signature help to draw above the cursor's line, or
// false when none shows (B203).
func (e *Session) Signature() (wire.Float, bool) {
	if e.completion.sig == nil {
		return wire.Float{}, false
	}
	return *e.completion.sig, true
}

// Terminal reports whether the current window is a terminal in terminal
// mode, where every key goes to its program (B205).
func (e *Session) Terminal() bool {
	t := e.win.buf.term
	return t != nil && t.typing
}

// Float returns the float to draw, or false when there is none.
func (e *Session) Float() (wire.Float, bool) {
	if e.hints != nil {
		return e.hintsFloat(), true
	}
	if m := e.cmdline.menu; m != nil {
		return m.float(), true
	}
	if f := e.finder; f != nil {
		e.seek(f)
		t := e.Text()
		fl := f.float(t.Slice(0, t.Len()), e.Cursor(), e.width, e.height)
		// An image previewed shows below the line that names it, under the
		// path and its rule (B305).
		if p := e.previewed(f); p != nil && fl.DocW > 4 && fl.H > 6 {
			rows, cols := e.fitImage(p, fl.DocW-4, fl.H-5)
			fl.DocImage = &wire.Picture{ID: p.id, Row: 3, Rows: rows, Cols: cols}
		}
		return fl, true
	}
	// A command line typed in a float shows alone, as the client draws one
	// box at the cursor, which would take the float for the line's menu.
	if e.vim.Mode() == vim.Cmdline {
		return wire.Float{}, false
	}
	if e.under != nil {
		if e.win.buf.title == "[lsp]" {
			return e.lspFloat(), true
		}
		t := e.Text()
		return wire.Float{Text: t.Slice(0, t.Len()), Spans: e.float.spans, Top: e.Top(), Cursor: e.Cursor()}, true
	}
	fl := wire.Float{Text: e.float.text, Spans: e.float.spans, Cursor: -1, Doc: e.float.doc, DocSpans: e.float.docSpans}
	if e.float.image != "" && e.cell[0] > 0 && e.cell[1] > 0 {
		if p := e.picture(e.float.image); p != nil {
			// A float at the cursor is at most 80 columns and 20 rows, its
			// border and a row of text included.
			rows, cols := e.fitImage(p, 76, 12)
			fl.Image = &wire.Picture{ID: p.id, Rows: rows, Cols: cols}
		}
	}
	return fl, e.float.text != ""
}

// floatKeys are a focused float's own keys, which [keymap.float] changes
// (B142).
var floatKeys = map[string]string{"q": "<Plug>(float.close)"}

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
	e.win, e.under, e.float.spans = e.under, nil, nil
}

// Cursors returns the cursors after the first while a change on several
// regions types into all of them.
func (e *Session) Cursors() []int { return e.vim.Cursors() }

// Selection returns the visual selection, or false outside visual mode.
func (e *Session) Selection() (vim.Selection, bool) { return e.vim.Selection() }

// Line returns the command line being typed.
func (e *Session) Line() string { return e.vim.Line() }

// Substituting reports whether a :s is being typed with its pattern, whose
// preview the windows show.
func (e *Session) Substituting() bool { return e.vim.Substituting() }

// setTheme makes name the theme every client draws in: dark, light or a
// base16 scheme file, read again each time (B146, B158).
func (e *Editor) setTheme(name string) error {
	var colours *[16]int32
	if name != "dark" && name != "light" {
		c, err := readBase16(name)
		if err != nil {
			return err
		}
		colours = &c
	}
	e.theme, e.base16 = name, colours
	return nil
}

// Theme names the colours config.toml or :theme asks the client for, dark
// or light, or returns the sixteen colours of the base16 scheme it names
// (B146, B158). It also returns the colours [colors] puts in place of the
// theme's, or nil (B294).
func (e *Session) Theme() (string, *[16]int32, *wire.Colors) {
	if e.cfg.Colors == (wire.Colors{}) {
		return e.theme, e.base16, nil
	}
	over := e.cfg.Colors
	return e.theme, e.base16, &over
}

// LineCursor returns where the cursor is in Line, in bytes.
func (e *Session) LineCursor() int { return e.vim.LineCursor() }

// Message returns the last message for the user, or "".
// It puts each new message into the history that :messages lists.
func (e *Session) Message() string {
	if e.message != e.logged && e.message != "" {
		trace.Record("msg", e.message, 0)
		e.history = append(e.history, e.message)
		e.history = e.history[max(len(e.history)-200, 0):]
	}
	e.logged = e.message
	return e.message
}

// Done reports whether the user has quit.
func (e *Session) Done() bool { return e.done }

// Recording returns the register a macro is being recorded into, or 0.
func (e *Session) Recording() rune { return e.vim.Recording() }

// show switches the current window to b. Leaving a buffer is a jump.
func (e *Session) show(b *buffer) {
	if err := e.barred(b); err != nil {
		e.message = err.Error()
		return
	}
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
		} else {
			j.buf.text.DeleteMark(j.mark)
		}
	}
	kept = append(kept, place{b, b.text.SetMark(pos, kernel.Right)})
	if over := len(kept) - placesKept; over > 0 {
		for _, j := range kept[:over] {
			j.buf.text.DeleteMark(j.mark)
		}
		kept = kept[over:]
	}
	w.jumps, w.jumpAt = kept, len(kept)
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
			e.message = fmt.Sprintf("\"%s\" has unsaved changes, ! drops them", b.name())
			return
		}
	}
	if err := e.saveProject(); err != nil {
		logLine(err.Error())
	}
	for _, b := range e.bufs {
		b.close()
	}
	for p, l := range e.listeners {
		l.stop()
		delete(e.listeners, p)
	}
	for _, p := range e.modules {
		p.Close(context.Background())
	}
	for _, s := range e.servers {
		s.closing = true
		s.client.Close()
	}
	recent().Flush()
	for _, s := range e.sessions {
		s.done = true
	}
}

// SetName names the session, as --name does (B252).
func (e *Session) SetName(name string) { e.name = name }

// label is the name others see the session by: its name, or else its
// origin (B252).
func (e *Session) label() string { return cmp.Or(e.name, string(e.origin)) }

// leave ends a session other than the first. Its proposals stay until a
// user decides them.
func (e *Session) leave() {
	e.sessions = slices.DeleteFunc(e.sessions, func(s *Session) bool { return s == e })
	e.done = true
}

func (e *Session) ex(line string) {
	first, last, rest, ranged, err := e.vim.LineRange(strings.TrimSpace(line))
	if err != nil {
		e.message = err.Error()
		return
	}
	name, arg, _ := strings.Cut(rest, " ")
	name, force := strings.CutSuffix(name, "!")
	if cmd, ok := strings.CutPrefix(rest, "!"); ok {
		name, arg, force = "!", cmd, false
	}
	arg = strings.TrimSpace(arg)
	if c, ok := commandsByName[name]; ok {
		if e.power != Host && refuses(c.Name, arg) {
			e.message = "a guest may not run :" + c.Name
			return
		}
		if takesPath(c) {
			if arg, err = e.expand(arg); err != nil {
				e.message = err.Error()
				return
			}
			if err := e.outside(arg); err != nil {
				e.message = err.Error()
				return
			}
		}
		c.run(e, call{name, arg, force, first, last, ranged})
		return
	}
	if run, ok := e.commands[name]; ok {
		run(e, call{name, arg, force, first, last, ranged})
		return
	}
	e.message = "not a command: " + line
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
	if err := e.outside(path); err != nil {
		e.message = err.Error()
		return
	}
	for _, b := range e.bufs {
		if b.is(path) {
			e.show(b)
			return
		}
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		e.openListing(path, "")
		return
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
		e.message = fmt.Sprintf("\"%s\" has unsaved changes, :bd! drops them", b.name())
		return
	}
	e.detach(b)
	b.close()
	e.emit("buffer.closed", "", b)
	for _, segs := range e.segments {
		delete(segs, b)
	}
	for _, segs := range e.leftSegments {
		delete(segs, b)
	}
	var images []string
	for owner, ds := range e.decos {
		for _, d := range ds {
			if d.buf == b && d.image != "" {
				images = append(images, d.image)
			}
		}
		// An owner with no decorations left, such as a closed diff's colours,
		// is deleted.
		if e.decos[owner] = slices.DeleteFunc(ds, func(d decoration) bool { return d.buf == b }); len(e.decos[owner]) == 0 {
			delete(e.decos, owner)
		}
	}
	e.release(images)
	i := slices.Index(e.bufs, b)
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
				w.show(s.mayShow(s.after(i)))
			}
		}
	}
}

// registers shows each named register, as :reg does.
func (e *Session) registers(names string) {
	var shown []string
	for _, r := range names {
		text, ok, err := e.vim.Register(r)
		switch {
		case err != nil:
			shown = append(shown, fmt.Sprintf("\"%c   %v", r, err))
		case ok:
			shown = append(shown, fmt.Sprintf("\"%c   %s", r, strings.ReplaceAll(text, "\n", "^J")))
		}
	}
	e.message = strings.Join(shown, "  ")
}

// overlay is what may take a key before the vim machine sees it: the jump
// labels, the finder, a terminal, the completion menu, a snippet, a code
// action pick, the float, a question, the :lsp panel and the command
// line's menu. One that is not up takes nothing.
type overlay interface {
	key(e *Session, k vim.Key) bool
}

// overlays are what may take a key, in the order they get it.
func (e *Session) overlays() []overlay {
	return []overlay{e.jumping, e.finder, terminalMode{}, &e.completion, e.snip, e.pick, &e.float, e.ask, lspPanel{}, &e.cmdline}
}

// floating is text shown over the windows at the cursor, such as hover,
// and the documentation shown beside a menu.
type floating struct {
	text     string
	spans    []syntax.Span
	doc      string // the selected completion's documentation, beside the menu
	docSpans []syntax.Span
	left     string // the float the key being handled closed, which hover enters
	image    string // the image its markdown shows, below its text (B309)
}

// key closes the float, which any key does, and keeps its text so that
// hover again moves the cursor into it.
func (f *floating) key(*Session, vim.Key) bool {
	f.left, f.text = f.text, ""
	return false
}

// question is what takes the answer to a question in the message area.
type question func(yes bool)

// key answers the question with k, y for yes.
func (q question) key(e *Session, k vim.Key) bool {
	if q == nil {
		return false
	}
	e.ask = nil
	q(k == "y")
	e.reveal()
	return true
}
