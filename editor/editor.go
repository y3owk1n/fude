// Package editor hosts the kernel and the vim machine for a set of files
// shown in a layout of windows.
package editor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

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
	bufs    []*buffer
	next    int // the id the next buffer gets
	cfg     config
	theme   string     // the theme clients draw in, which :theme changes
	base16  *[16]int32 // the colours of the base16 scheme it names
	queue   chan func()
	queries string // the directory of query files that replace shipped ones
	plugins []*plugin.Plugin
	// What plugins add, for help and the key hints: what each command and
	// key does, by :name and <Plug>(name), and their operators and objects.
	pluginHelp     map[string]string
	pluginBindings []vim.Binding
	plugged        int                             // how many of setup come before the plugins' own
	commands       map[string]func(*Session, call) // ex commands plugins registered
	decos          map[any][]decoration            // by owner: a plugin, or a buffer for its diagnostics
	servers        map[string]*server              // by command and root
	down           map[string]string               // by name, why a configured language server is not running
	building       map[string]bool                 // the filetypes whose grammar builds now
	failed         map[string]string               // by filetype, why its grammar did not build this session
	started        bool                            // Load ran, so new buffers attach to servers at once
	sessions       []*Session                      // the first is the one that opened the editor
	setup          []func(*Session)                // what every session's machine gets, such as keymaps
	proposed       int                             // the id of the newest proposal
	listeners      map[*plugin.Plugin]*listener
	segments       map[*plugin.Plugin]map[*buffer]string // what each plugin shows on the statusline of each buffer

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
	restarting string         // the session file :restart saved, for main (B170)
	hints      []vim.Hint     // the keys that may follow the command being typed, while they show (B172)
	ask        func(yes bool) // takes the answer to the question in the message
	jumpTo     *jumpTo        // where a plugin's call asked to move the cursor, after the key
	lastList   *places        // the last list of places a window showed
	sig        *Float         // the signature help showing in insert mode (B203)
	snip       *snippet       // the snippet being filled in (B207)
	float      string         // text shown over the windows at the cursor, such as hover
	floatSpans []syntax.Span  // the styles of the float's text
	doc        string         // the selected completion's documentation, beside the menu
	docSpans   []syntax.Span
	menu       *menu                   // the completions shown while typing
	ctrlX      bool                    // <C-x> began a completion key in insert mode
	lspKeys    bool                    // the attached keys are mapped
	later      []vim.Key               // keys to feed once the key being fed is done
	renameAt   int                     // where grn was pressed
	pick       *pick                   // the code actions shown to choose from
	finder     *finder                 // the file finder, while it shows
	cmenu      *cmenu                  // the completions of the command line being typed
	dirs       map[string][]string     // entries of directories the command line has completed in
	flash      *flash                  // text shown for a moment after a yank, put, undo or redo
	locals     map[string]*vim.Keymaps // the keymaps of kinds of buffer, by kind, over the machine's (B142)
	left       string                  // the float the key being handled closed, which hover enters
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
	depth      int                     // the undo groups the machine has open
	pending    int                     // the cursor when the group being made began

	width, height int  // the space the windows share
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
	ed := &Editor{next: 1, now: time.Now, cfg: cfg, queue: make(chan func(), 16), commands: map[string]func(*Session, call){}, decos: map[any][]decoration{}, servers: map[string]*server{}, down: map[string]string{}, building: map[string]bool{}, failed: map[string]string{}, listeners: map[*plugin.Plugin]*listener{}, segments: map[*plugin.Plugin]map[*buffer]string{}}
	// Without a config directory there are no overrides, and loadConfig
	// reports why.
	if dir, err := xdgDir("XDG_CONFIG_HOME", ".config"); err == nil {
		ed.queries = filepath.Join(dir, "fude", "queries")
	}
	// Without a data directory no grammar builds or loads, and a build says
	// why.
	if dir, err := xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share")); err == nil {
		// config.toml does not change while fude runs, so workers may resolve
		// through it.
		syntax.Use(filepath.Join(dir, "fude", "grammars"), func(name string) string { return ed.filetypeOf("code." + name) })
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
	e.vim.AutoPairs(e.cfg.Autopairs)
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
	for _, op := range hostOperators {
		e.vim.Operator(op.keys, func(t vim.Text, start, end int, lines bool) []vim.Edit {
			return op.run(e, t, start, end, lines)
		})
	}
	for _, p := range plugs {
		e.vim.Command(p.name, func(_ vim.Text, _, count int) []vim.Edit {
			p.run(e, count)
			return nil
		})
	}
	for keys, to := range defaultMaps {
		e.vim.Map(vim.Normal, keys, to)
	}
	e.locals = map[string]*vim.Keymaps{}
	for kind, keys := range map[string]map[string]string{"listing": listingKeys, "list": listKeys, "timeline": timelineKeys, "finder": finderKeys, "float": floatKeys, "lsp": lspKeys, "help": helpKeys, "scratch": scratchKeys} {
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
	e.each(func(s *Session) {
		for mode, maps := range map[vim.Mode]map[string]string{vim.Normal: cfg.Keymap.Normal, vim.Visual: cfg.Keymap.Visual, vim.Insert: cfg.Keymap.Insert} {
			mapAll(s.vim, mode, maps, cfg.Leader)
		}
		for kind, maps := range map[string]map[string]string{"listing": cfg.Keymap.Listing, "list": cfg.Keymap.List, "timeline": cfg.Keymap.Timeline, "float": cfg.Keymap.Float, "lsp": cfg.Keymap.Lsp, "help": cfg.Keymap.Help, "scratch": cfg.Keymap.Scratch} {
			mapAll(s.locals[kind], vim.Normal, maps, cfg.Leader)
		}
		mapAll(s.locals["finder"], vim.Insert, cfg.Keymap.Finder, cfg.Leader)
	})
	return e, nil
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
		e.loadGit(b)
		return "", nil
	}
	b, msg, err := openBuffer(e.next, path)
	if err != nil {
		return "", err
	}
	e.next++
	e.bufs = append(e.bufs, b)
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
	e.loadGit(b)
	return msg, nil
}

// heard passes a buffer's changes to its syntax and its language server,
// and marks the proposals they overlap stale (B74).
type heard struct {
	ed *Editor
	b  *buffer
}

func (l heard) Changed(before kernel.Snapshot, edits []kernel.Edit) {
	l.b.gitStale = true
	l.b.touched = touched(edits)
	l.ed.changed(l.b, edits)
	for _, s := range l.ed.sessions {
		if s.flash != nil && s.flash.b == l.b {
			// What it showed has moved.
			s.flash = nil
		}
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
	out := make([]api.Edit, len(edits))
	for i, e := range edits {
		out[i] = api.Edit(e)
	}
	if l.b.syntax != nil {
		l.b.syntax.Changed(before, out)
	}
	for _, s := range l.b.servers {
		s.versions[l.b]++
		s.client.Change(lsp.URI(l.b.file.path), s.versions[l.b], before, out)
		l.ed.askColours(s, l.b)
		l.ed.askHints(s, l.b)
	}
}

// Queue returns the work other goroutines hand the editor loop, such as new
// highlights. The loop runs each func it receives, then draws.
func (e *Session) Queue() <-chan func() { return e.queue }

// Feed handles one key. An error becomes the message.
func (e *Session) Feed(k vim.Key) {
	at, mode := time.Now(), e.vim.Mode()
	defer func() { api.Trace("key", fmt.Sprintf("%q in %s", k, modes[mode]), time.Since(at)) }()
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
	for len(e.later) > 0 {
		k := e.later[0]
		e.later = e.later[1:]
		e.feed(k)
	}
	if e.hints != nil {
		e.ShowHints()
	}
}

// feed handles one key, typed or queued by a command for after it.
func (e *Session) feed(k vim.Key) {
	e.message = ""
	if e.finder != nil {
		e.finding(k)
		e.reveal()
		return
	}
	// In terminal mode every key goes to the program, and in normal mode in
	// a terminal i and a go back to terminal mode (B205).
	if t := e.win.buf.term; t != nil {
		switch {
		case t.typing:
			e.terminalKey(k)
			return
		case (k == "i" || k == "a") && e.vim.Idle():
			e.enterTerminal()
			return
		}
	}
	if e.completing(k) || e.snippetKey(k) {
		e.reveal()
		return
	}
	if e.pick != nil {
		e.picking(k)
		e.reveal()
		return
	}
	// Any key closes the float, and hover again moves the cursor into it.
	e.left, e.float = e.float, ""
	if ask := e.ask; ask != nil {
		e.ask = nil
		ask(k == "y")
		e.reveal()
		return
	}
	if e.vim.Mode() == vim.Cmdline && k == "<CR>" {
		at, line := time.Now(), e.vim.Line()
		defer func() { api.Trace("cmd", line, time.Since(at)) }()
	}
	if e.commandKey(k) {
		return
	}
	e.vim.IgnoreCase(e.win.opts.IgnoreCase, e.win.opts.SmartCase)
	e.vim.ExpandTab(e.win.opts.ExpandTab)
	e.vim.UseClipboard(e.win.opts.Clipboard)
	e.vim.UseLocal(e.locals[e.win.buf.kind()])
	e.settle(e.vim.Feed(k))
	if start, end, ok := e.vim.Flash(); ok {
		e.flashOn(e.win.buf, start, end)
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
		if err := log.failed(); err != nil {
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
	Matches           [][2]int       // of the search, shown in every window
	Segments          []string       // plugins' statusline segments, in the order they loaded
	Ruler             string         // the cursor's line and column, and where the view is (B38)
	Branch            string         // the git branch of the buffer's repository, "" outside one (B147)
	Errors, Warnings  int            // the buffer's diagnostics of each severity (B147)
	Filetype          string         // the buffer's filetype, "" for none (B164)
	Server            string         // the buffer's language servers, "" for none (B161, B169)
	Progress          string         // what the server is doing, "" once it is ready (B161)
	Gutter            int            // the columns the sign column and line numbers take on the left
	SignCols          int            // the columns of the sign column, at the gutter's left
	Signs             map[int]Sign   // by line, the sign each one shows (B119)
	Notes             map[int]string // by line, dim text after its end (B127)
	Icons             map[int]Sign   // by line, a listing's icon at the gutter's right (B128)
	Number, Relative  bool           // show line numbers, and relative ones (B39)
	Wrap              bool           // long lines go on the rows below
	CursorLine        bool           // shade the cursor's line (B117)
	Pair              []int          // the bracket at the cursor and its match, shown bold (B121)
	List              bool           // show tabs, trailing spaces and no-break spaces (B118)
	Left              int            // the first column shown without wrap
	Indent            int            // the columns of an indent level, where guides go
	Guides            bool           // draw indent guides (B153)
	ColorColumn       int            // the column of the text to shade, from 1, or 0 (B200)
	Colours           []Colour       // text that names a colour, shown on it (B208, B209)
	// Context is the start of each line pinned over the window's first rows,
	// the functions and classes the text under them is in, with their
	// highlights (B217).
	Context           []int
	ContextHighlights []syntax.Span
	// A terminal window in terminal mode shows its program's screen, with
	// the program's cursor at CellX and CellY, or -1 when it hides it (B205).
	Cells        [][]Cell
	CellX, CellY int
}

// Ghost is a proposal's text, drawn before the byte at Pos but not in the
// text (B71).
type Ghost struct {
	Pos   int
	Text  string
	Stale bool
	Style string // how it shows: "" for a proposal's text, sub.new for :s's (B154) or inlay for an inlay hint (B216)
}

// Views returns every window from the top left.
func (e *Session) Views() []View {
	shown := map[*buffer][]any{}
	for _, s := range e.sessions {
		for _, w := range s.root.windows() {
			shown[w.buf] = append(shown[w.buf], w)
		}
	}
	for b, ws := range shown {
		if b.syntax != nil {
			for _, w := range slices.Clone(ws) {
				for i := range maxContext {
					ws = append(ws, contextRow{w.(*window), i})
				}
			}
			b.syntax.Keep(ws)
		}
	}
	var out []View
	_, _, centered := e.area()
	for _, w := range e.root.windows() {
		t, top := w.buf.text.Snapshot(), e.topOf(w)
		v := View{
			Text: t, Top: top, Cursor: w.pos(),
			X: w.x, Y: w.y, W: w.w, H: w.h,
			Name: w.buf.name(), Modified: w.buf.modified(), Current: w == e.win || w == e.under,
			Gutter: w.gutter(), SignCols: w.signCols(), Number: w.numberCols() > 0 && w.opts.Number, Relative: w.numberCols() > 0 && w.opts.RelativeNumber,
			Wrap: w.opts.Wrap, Left: w.left, CursorLine: w.opts.CursorLine, List: w.opts.List,
			Indent: e.indentWidth(w.buf), Guides: w.opts.IndentGuides, ColorColumn: w.opts.ColorColumn,
		}
		// Centered, the colour column is the windows' edge, so it shows none.
		if centered {
			v.ColorColumn = 0
		}
		if term := w.buf.term; term != nil && term.typing {
			v.Cells, v.CellX, v.CellY = term.screen()
		}
		last := vim.LineEnd(t, t.LineStart(min(t.LineOf(top)+max(w.h-2, 0), t.Lines()-1)))
		if h := w.buf.syntax; h != nil {
			v.Highlights = h.Show(t, w, top, last)
			if e.cfg.StickyContext {
				v.Context = e.pinned(w, t, top)
				for i, start := range v.Context {
					v.ContextHighlights = append(v.ContextHighlights, h.Show(t, contextRow{w, i}, start, vim.LineEnd(t, start))...)
				}
			}
		}
		v.Matches = e.vim.Matches(t, top, last)
		if e.cfg.HighlightColors {
			v.Colours = textColours(t.Slice(top, last), top)
		}
		e.diffGit(w.buf)
		v.Signs = e.signs(w.buf, top, last)
		v.Notes = w.buf.notes(t, top, last)
		for _, p := range e.plugins {
			for _, d := range e.decos[p] {
				if d.buf != w.buf || d.note == "" {
					continue
				}
				if at := w.buf.text.Mark(d.start); at >= top && at <= last {
					if v.Notes == nil {
						v.Notes = map[int]string{}
					}
					v.Notes[t.LineOf(at)] = d.note
				}
			}
		}
		if w.opts.Icons {
			v.Icons = w.buf.icons(t, top, last)
			for line, s := range v.Signs {
				if icon, ok := diagnosticIcons[s.Name]; ok {
					v.Signs[line] = Sign{icon, s.Name}
				}
			}
		}
		v.Pair = e.pair(w, t, top, last)
		v.Ruler = ruler(t, w.pos(), top, max(w.h-1, 1), func(p int) int { return w.rows(t, p) })
		for _, p := range e.plugins {
			if s := e.segments[p][w.buf]; s != "" {
				v.Segments = append(v.Segments, s)
			}
		}
		v.Branch, v.Filetype = w.buf.branch, e.filetype(w.buf)
		var names []string
		for _, s := range w.buf.servers {
			names = append(names, s.name)
			v.Progress = cmp.Or(v.Progress, s.doing())
		}
		v.Server = strings.Join(names, ", ")
		for _, d := range e.diagnostics(w.buf) {
			switch d.style {
			case "diagnostic.error":
				v.Errors++
			case "diagnostic.warn":
				v.Warnings++
			}
		}
		if w == e.win {
			// The :s being typed shows its matches, then each match struck
			// out before its replacement (B154).
			edits, replacing := e.vim.Preview(top, last)
			for _, ed := range edits {
				if !replacing {
					v.Matches = append(v.Matches, [2]int{ed.Start, ed.End})
					continue
				}
				v.Decorations = append(v.Decorations, syntax.Span{Start: ed.Start, End: ed.End, Name: "sub.old"})
				v.Ghosts = append(v.Ghosts, Ghost{Pos: ed.End, Text: ed.Text, Style: "sub.new"})
			}
		}
		if f := e.flash; f != nil && f.b == w.buf && f.end > top && f.start < last {
			// Only what shows, as an undo can touch the whole text.
			v.Decorations = append(v.Decorations, syntax.Span{Start: max(f.start, top), End: min(f.end, last), Name: "flash"})
		}
		if w == e.win {
			v.Decorations = append(v.Decorations, e.snippetSpan(w.buf)...)
		}
		for _, ds := range e.decos {
			for _, d := range ds {
				if d.buf != w.buf {
					continue
				}
				s, end := w.buf.text.Mark(d.start), w.buf.text.Mark(d.end)
				if hex, ok := strings.CutPrefix(d.style, "colour #"); ok {
					if rgb, err := strconv.ParseInt(hex, 16, 32); err == nil && s < end && end > top && s < last {
						v.Colours = append(v.Colours, Colour{s, end, int32(rgb)})
					}
					continue
				}
				if d.style == "inlay" {
					if s >= top && s <= last {
						v.Ghosts = append(v.Ghosts, Ghost{Pos: s, Text: d.note, Style: "inlay"})
					}
					continue
				}
				if s < end && end > top && s < last {
					v.Decorations = append(v.Decorations, syntax.Span{Start: s, End: end, Name: d.style})
				}
				if strings.HasPrefix(d.style, "proposal") && d.note != "" && end >= top && end <= last {
					v.Ghosts = append(v.Ghosts, Ghost{Pos: end, Text: d.note, Stale: d.style == "proposal.stale"})
				}
			}
		}
		out = append(out, v)
	}
	return out
}

// maxContext is the most lines sticky context pins (B217).
const maxContext = 3

// contextRow names a line sticky context pins over w's rows, for the
// highlights of a line above what w shows.
type contextRow struct {
	w   *window
	row int
}

// pinned returns the starts of the lines pinned over w's first rows: those
// of the functions and classes that hold the first line they leave in view
// and start above it, the outermost first. They never cover the cursor's
// line (B217).
func (e *Session) pinned(w *window, t kernel.Snapshot, top int) []int {
	p, h := &w.pinned, w.buf.syntax
	if p.version == t.Version() && p.top == top && p.cursor == w.pos() && p.parses == h.Parses() {
		return p.starts
	}
	var starts []int
	first := t.LineOf(top)
	room := min(maxContext, t.LineOf(w.pos())-first, max(w.h-3, 0))
	// Each pinned line covers one more line of text, so the loop asks again
	// about the line under them until the answer stops changing.
	for range maxContext {
		under := first + len(starts)
		var found []int
		for _, s := range h.Context(t.LineStart(min(under, t.Lines()-1))) {
			if ls := t.LineStart(t.LineOf(s)); t.LineOf(s) < under && !slices.Contains(found, ls) {
				found = append(found, ls)
			}
		}
		found = found[max(len(found)-room, 0):]
		if slices.Equal(found, starts) {
			break
		}
		starts = found
	}
	p.version, p.top, p.cursor, p.parses, p.starts = t.Version(), top, w.pos(), h.Parses(), starts
	return starts
}

// pair returns the bracket at w's cursor and its match within the lines
// from top to last, or nil. In insert mode the bracket may be the one
// before the cursor.
func (e *Session) pair(w *window, t kernel.Snapshot, top, last int) []int {
	at := []int{w.pos()}
	if w == e.win && e.vim.Mode() == vim.Insert && at[0] > 0 {
		at = append(at, at[0]-1)
	}
	for _, p := range at {
		if q, ok := vim.Pair(t, p, top, last); ok {
			return []int{p, q}
		}
	}
	return nil
}

// Sign is a letter in the sign column and the style it takes its colour
// from, such as diagnostic.error.
type Sign struct {
	Text, Name string
}

// diagnosticSigns are the diagnostic letters by severity, the worst first.
var diagnosticSigns = []Sign{{"E", "diagnostic.error"}, {"W", "diagnostic.warn"}, {"I", "diagnostic.info"}, {"H", "diagnostic.hint"}}

// signs returns each line of b's sign: the letter of its worst diagnostic,
// or else its git sign. A
// listing's lines from top to last show the git status of their names.
func (e *Session) signs(b *buffer, top, last int) map[int]Sign {
	if !b.signed && b.signs == 0 && len(b.git) == 0 && len(b.status) == 0 {
		return nil
	}
	t := b.text.Snapshot()
	out := maps.Clone(b.git)
	if out == nil {
		out = map[int]Sign{}
	}
	// A plugin's sign wins over git's, and a diagnostic over a plugin's
	// (B184).
	for _, p := range e.plugins {
		for _, d := range e.decos[p] {
			if d.buf == b && d.sign != "" {
				out[t.LineOf(b.text.Mark(d.start))] = Sign{d.sign, d.style}
			}
		}
	}
	for _, d := range e.diagnostics(b) {
		line := t.LineOf(b.text.Mark(d.start))
		i := slices.IndexFunc(diagnosticSigns, func(s Sign) bool { return s.Name == d.style })
		if old, ok := out[line]; i >= 0 && (!ok || !slices.Contains(diagnosticSigns, old) || i < slices.Index(diagnosticSigns, old)) {
			out[line] = diagnosticSigns[i]
		}
	}
	for line := t.LineOf(top); len(b.status) > 0 && line <= t.LineOf(last); line++ {
		start := t.LineStart(line)
		if s := b.status[t.Slice(start, vim.LineEnd(t, start))]; s.Text != "" {
			out[line] = s
		}
	}
	return out
}

// ruler returns the cursor's place for a window that shows rows rows from
// top, whose lines take rowsOf rows each. It is line,col, then -screen
// column when that differs, then All, Top, Bot or how far down the view is
// (B38).
func ruler(t kernel.Snapshot, c, top, rows int, rowsOf func(int) int) string {
	line, ls := t.LineOf(c), vim.LineStart(t, c)
	col, vcol := c-ls+1, vim.Column(t, ls, c)+1
	if ls == vim.LineEnd(t, ls) {
		col = 0
	}
	at := fmt.Sprintf("%d,%d", line+1, col)
	if vcol != col {
		at += fmt.Sprintf("-%d", vcol)
	}
	// last is the last line the view shows.
	last := t.LineOf(top)
	for p, n := top, 0; ; p = vim.LineEnd(t, p) + 1 {
		if n += rowsOf(p); n > rows {
			break
		}
		last = t.LineOf(p)
		if vim.LineEnd(t, p) == t.Len() {
			break
		}
	}
	above, below := t.LineOf(top), t.Lines()-1-last
	where := fmt.Sprintf("%d%%", above*100/max(above+below, 1))
	switch {
	case above == 0 && below == 0:
		where = "All"
	case above == 0:
		where = "Top"
	case below == 0:
		where = "Bot"
	}
	return fmt.Sprintf("%-*s %s", 17-len(where), at, where)
}

// Shown returns the keys of the command being typed, or "".
func (e *Session) Shown() string { return e.vim.Shown() }

// Top returns the start of the current window's first line shown.
func (e *Session) Top() int { return e.topOf(e.win) }

func (e *Session) topOf(w *window) int {
	return vim.LineStart(w.buf.text.Snapshot(), w.buf.text.Mark(w.top))
}

// rows is how many lines of text the current window shows.
func (e *Session) rows() int { return max(e.win.h-1, 1) }

// reveal scrolls the current window the least it must to show the cursor's
// line, which takes as many rows as it wraps to (B37).
func (e *Session) reveal() {
	w := e.win
	t, c := w.buf.text.Snapshot(), w.pos()
	line := vim.LineStart(t, c)
	// scroll_off lines stay in view above and below the cursor's, at most
	// half the window (B116).
	above, below := line, line
	for range min(w.opts.ScrollOff, (e.rows()-1)/2) {
		if above > 0 {
			above = vim.LineStart(t, above-1)
		}
		if end := vim.LineEnd(t, below); end < t.Len() {
			below = end + 1
		}
	}
	// low is the earliest top that still shows below, found walking back
	// from it, so a far jump looks at one window of lines.
	low, rows := below, w.rows(t, below)
	for low > 0 {
		prev := vim.LineStart(t, low-1)
		if rows += w.rows(t, prev); rows > e.rows() {
			break
		}
		low = prev
	}
	w.buf.text.MoveMark(w.top, min(max(e.Top(), low), above))
	// Without wrap the view scrolls sideways the least it must to show the
	// cursor's column (B37).
	if col := vim.Column(t, line, c); !w.opts.Wrap {
		w.left = min(max(w.left, col-w.cols()+1), col)
	}
}

// Paste inserts text as typed, with no key mapping applied (B8).
func (e *Session) Paste(text string) {
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

// Float is text a client draws over the windows at the cursor, styled by
// Spans. While it has the cursor, Top is the start of the first line it
// shows and Cursor is where the cursor is in it, else -1.
type Float struct {
	Text        string
	Spans       []syntax.Span
	Top, Cursor int
	Title       string // drawn in the top of its border
	Doc         string // drawn beside it, styled by DocSpans
	DocSpans    []syntax.Span
	Lit         bool // the windows under it stay lit, as under the key hints (B151)
	// Where a box in a place of its own goes, its border included. W is 0
	// for a box at the cursor. DocW is the width of the documentation's box
	// right of it.
	X, Y, W, H, DocW int
}

// Signature returns the signature help to draw above the cursor's line, or
// false when none shows (B203).
func (e *Session) Signature() (Float, bool) {
	if e.sig == nil {
		return Float{}, false
	}
	return *e.sig, true
}

// Terminal reports whether the current window is a terminal in terminal
// mode, where every key goes to its program (B205).
func (e *Session) Terminal() bool {
	t := e.win.buf.term
	return t != nil && t.typing
}

// Float returns the float to draw, or false when there is none.
func (e *Session) Float() (Float, bool) {
	if e.hints != nil {
		return e.hintsFloat(), true
	}
	if m := e.cmenu; m != nil {
		return m.float(), true
	}
	if f := e.finder; f != nil {
		e.seek(f)
		t := e.Text()
		return f.float(t.Slice(0, t.Len()), e.Cursor(), e.width, e.height), true
	}
	if e.under != nil {
		if e.win.buf.title == "[lsp]" {
			return e.lspFloat(), true
		}
		t := e.Text()
		return Float{Text: t.Slice(0, t.Len()), Spans: e.floatSpans, Top: e.Top(), Cursor: e.Cursor()}, true
	}
	return Float{Text: e.float, Spans: e.floatSpans, Cursor: -1, Doc: e.doc, DocSpans: e.docSpans}, e.float != ""
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
	e.win, e.under, e.floatSpans = e.under, nil, nil
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
// (B146, B158).
func (e *Session) Theme() (string, *[16]int32) { return e.theme, e.base16 }

// LineCursor returns where the cursor is in Line, in bytes.
func (e *Session) LineCursor() int { return e.vim.LineCursor() }

// Message returns the last message for the user, or "".
// It puts each new message into the history that :messages lists.
func (e *Session) Message() string {
	if e.message != e.logged && e.message != "" {
		api.Trace("msg", e.message, 0)
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

// windowKey is a <C-w> command: its keys after <C-w>, what it does, and
// how.
type windowKey struct {
	keys []string
	help string
	run  func(e *Session, count int) error // count is 0 when none was typed
}

// windowKeys are the <C-w> commands (B91).
var windowKeys = []windowKey{
	{[]string{"s", "S", "<C-s>"}, "splits the window, the new one below", func(e *Session, _ int) error { e.split(false); return nil }},
	{[]string{"v", "<C-v>"}, "splits the window, the new one right", func(e *Session, _ int) error { e.split(true); return nil }},
	{[]string{"h", "<C-h>"}, "moves to the window left", func(e *Session, _ int) error { e.moveTo('h'); return nil }},
	{[]string{"j", "<C-j>"}, "moves to the window below", func(e *Session, _ int) error { e.moveTo('j'); return nil }},
	{[]string{"k", "<C-k>"}, "moves to the window above", func(e *Session, _ int) error { e.moveTo('k'); return nil }},
	{[]string{"l", "<C-l>"}, "moves to the window right", func(e *Session, _ int) error { e.moveTo('l'); return nil }},
	{[]string{"c", "<C-c>"}, "closes the window", func(e *Session, _ int) error { return e.closeWindow(e.win) }},
	{[]string{"q", "<C-q>"}, "closes the window, and quits with the last", func(e *Session, _ int) error { e.quit(false); return nil }},
	{[]string{"o", "<C-o>"}, "closes every other window", func(e *Session, _ int) error { e.only(); return nil }},
	{[]string{"="}, "makes the windows the same size", func(e *Session, _ int) error { e.root.even(); e.layout(); return nil }},
	{[]string{"+"}, "makes the window taller, by the count", func(e *Session, n int) error {
		e.resize(false, func(now, _ int) int { return now + max(n, 1) })
		return nil
	}},
	{[]string{"-"}, "makes the window shorter, by the count", func(e *Session, n int) error {
		e.resize(false, func(now, _ int) int { return now - max(n, 1) })
		return nil
	}},
	{[]string{">"}, "makes the window wider, by the count", func(e *Session, n int) error {
		e.resize(true, func(now, _ int) int { return now + max(n, 1) })
		return nil
	}},
	{[]string{"<"}, "makes the window narrower, by the count", func(e *Session, n int) error {
		e.resize(true, func(now, _ int) int { return now - max(n, 1) })
		return nil
	}},
	{[]string{"_", "<C-_>"}, "makes the window the count tall, or as tall as it can be", func(e *Session, n int) error {
		e.resize(false, func(_, room int) int { return cmp.Or(n, room) })
		return nil
	}},
	{[]string{"|"}, "makes the window the count wide, or as wide as it can be", func(e *Session, n int) error {
		e.resize(true, func(_, room int) int { return cmp.Or(n, room) })
		return nil
	}},
}

// window runs a <C-w> command, typed after count.
func (e *Session) window(k string, count int) {
	i := slices.IndexFunc(windowKeys, func(w windowKey) bool { return slices.Contains(w.keys, k) })
	if i < 0 {
		return
	}
	if err := windowKeys[i].run(e, count); err != nil {
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
	for p, l := range e.listeners {
		l.stop()
		delete(e.listeners, p)
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
		if takesPath(c) {
			if arg, err = e.expand(arg); err != nil {
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
		e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", b.name())
		return
	}
	e.detach(b)
	b.close()
	e.emit("buffer.closed", "", b)
	for _, segs := range e.segments {
		delete(segs, b)
	}
	for owner, ds := range e.decos {
		e.decos[owner] = slices.DeleteFunc(ds, func(d decoration) bool { return d.buf == b })
	}
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
	if b.listed != "" {
		e.save(b, then)
		return
	}
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
	if e.cfg.FormatOnSave {
		e.formatOnSave(b, func() { e.writeOut(b, then) })
		return
	}
	e.writeOut(b, then)
}

// writeOut writes b's text to its file, then runs then.
func (e *Session) writeOut(b *buffer, then func()) {
	s, at := b.text.Snapshot(), time.Now()
	text := s.Slice(0, s.Len())
	n, err := b.file.write(text)
	traced("file", fmt.Sprintf("wrote %q, %d bytes", b.file.path, n), at, err)
	if err != nil {
		e.message = err.Error()
		return
	}
	tag := ""
	if b.file.isNew {
		tag = "[New] "
	}
	b.file.isNew = false
	b.markSaved(text)
	e.emit("buffer.saved", "", b)
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
		// git's index may have changed while fude was away.
		e.loadGit(b)
		if b.title != "" || b.file.warned || !b.file.changed() {
			continue
		}
		if b.modified() {
			e.message = fmt.Sprintf("\"%s\" changed on disk and in fude, keeping fude's text", b.file.path)
			b.file.warned = true
			continue
		}
		if err := e.revert(b); err != nil {
			e.message = err.Error()
			continue
		}
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

// Viewport gives a width no line reaches without wrap, so gj is j.
func (h host) Viewport() (int, int, int) {
	if !h.e.win.opts.Wrap {
		return h.e.Top(), h.e.rows(), math.MaxInt
	}
	return h.e.Top(), h.e.rows(), h.e.win.cols()
}
func (h host) SetTop(top int)   { h.e.win.buf.text.MoveMark(h.e.win.top, top) }
func (h host) PushJump(pos int) { h.e.pushJump(pos) }
func (h host) Jump(n int) bool  { return h.e.jump(n) }

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
func (h host) IndentWidth() int { return h.e.indentWidth(h.e.win.buf) }

// indentWidth is the columns an indent takes in b: its filetype's
// indent_width, or else the global one, or else the tab width.
func (ed *Editor) indentWidth(b *buffer) int {
	w := ed.cfg.IndentWidth
	if ft, ok := ed.cfg.Filetype[ed.filetype(b)]; ok && ft.IndentWidth > 0 {
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
	h.e.flashOn(b, b.touched[0], b.touched[1])
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
	h.e.flashOn(b, b.touched[0], b.touched[1])
	return start, b.before[b.head], nil
}

func (h host) Unhandled(k string) {
	// A count before <C-w> sizes a window (B193).
	digits := len(k) - len(strings.TrimLeft(k, "0123456789"))
	count, _ := strconv.Atoi(k[:digits])
	k = k[digits:]
	if h.e.under != nil {
		// In a float, :q, :close and <C-w> only leave it. Any other ex line
		// leaves it and acts on the window under it.
		h.e.unfocus()
		if name, _, _ := strings.Cut(strings.TrimPrefix(k, ":"), " "); strings.HasPrefix(k, "<C-w>") || slices.Contains([]string{"q", "q!", "quit", "quit!", "clo", "close"}, name) {
			return
		}
	}
	if w, ok := strings.CutPrefix(k, "<C-w>"); ok {
		h.e.window(w, count)
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
