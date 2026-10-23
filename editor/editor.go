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
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// Editor is the open files and what works on them: plugins, language
// servers and decorations. Every client's session shares it.
type Editor struct {
	bufs     []*buffer
	next     int // the id the next buffer gets
	aside    int // the id the next buffer with no number gets, past every number (B373)
	joined   int // how many sessions joined, which numbers the next (B332)
	cfg      config
	styles   map[string]wire.Style // the styles modules defined, by name (B345)
	restyled int                   // how many times they changed
	theme    string                // the theme clients draw in, which :theme changes
	base16   *[16]int32            // the colours of the base16 scheme it names
	queue    chan func()
	queries  string              // the directory of query files that replace shipped ones
	modules  []api.Module        // the plugins that loaded, in the order they did
	declared map[string]declared // the options modules registered, by owner.name (B377)
	working  int                 // the plugin loads, jobs, timers and requests modules started that have yet to come back to the loop
	values   map[string]string   // their values, a window option's as it starts
	// What plugins add, for help and the key hints: what each command and
	// key does, by :name and <Plug>(name), and their operators and objects.
	pluginHelp        map[string]string
	pluginArgs        map[string]string // the arguments of a module's command, by its name (B324)
	pathArgs          map[string]bool   // the modules' commands whose argument completes as a path (B290)
	pluginBindings    []vim.Binding
	plugged           int                                        // how many of setup come before the plugins' own
	commands          map[string]func(*Session, call) error      // ex commands plugins registered
	sources           map[string]func(*Session, string)          // :pick sources modules registered, by name (B287)
	writers           map[string]writer                          // what writes each kind of buffer modules write, by kind (B327)
	completionSources map[string]completer                       // the modules' sources of completion, by name (B336)
	shortOf           map[string]string                          // the modules' command each short name stands for (B329)
	tabs              []api.Tab                                  // what a module says the buffer line lists (B331)
	completers        map[string]func(*Session, string)          // by command, what asks a module to complete its argument (B290)
	decos             map[any][]decoration                       // by owner: a plugin, or a buffer for its diagnostics
	servers           map[string]*server                         // by command and root
	down              map[string]string                          // by name, why a configured language server is not running
	waiting           map[string]func(any)                       // by the id lsp.requested gave, what answers a server's request a module answers (B532)
	clientCommands    map[string]func(*Session, *server, string) // by name, what runs a command a server names for a module to run (B539)
	asked             int                                        // the last id lsp.requested gave
	building          map[string]bool                            // the filetypes whose grammar builds now
	failed            map[string]string                          // by filetype, why its grammar did not build this session
	started           bool                                       // Load ran, so new buffers attach to servers at once
	project           string                                     // the directory fude started in, symlinks resolved (B243)
	wd                string                                     // the working directory, which :cd changes (B340)
	share             *share                                     // the guests' listener, while fude shares (B248)
	socket            string                                     // where Serve listens, for a :terminal's FUDE_SOCKET (B355)
	clients           atomic.Int64                               // the attached clients yet to have their last frame (B242)
	began             int                                        // how many sessions began, for the next one's colour (B252)
	sessions          []*Session                                 // the first is the one that opened the editor
	setup             []func(*Session)                           // what every session's machine gets, such as keymaps
	proposed          int                                        // the id of the newest proposal
	listeners         map[api.Module]*listener
	segments          map[api.Module]map[*buffer]string // what each plugin shows on the statusline of each buffer
	leftSegments      map[api.Module]map[*buffer]string // what each shows on its left, after the buffer's name (B147)

	now      func() time.Time // the clock edits are stamped with
	pictures pictures         // the images decorations show (B303)
}

// Session is one client's windows and vim machine on the shared editor. Its
// edits carry its origin, so its undo reverts only them (B77).
type Session struct {
	*Editor
	origin      kernel.Origin
	id          int                 // what modules know it by, which no other session of this fude has (B332)
	power       Power               // how much the session may do (B243)
	name        string              // who uses the session, "" for a client that sent none (B252)
	leader      string              // what <Leader> in a plugin's keys stands for, a guest's own (B254)
	following   *Session            // the session whose windows :follow copies (B256)
	mirrors     map[*window]*window // by the other's window, the window that copies it
	colour      int                 // which of six colours shows the session to others (B252)
	root        *node
	win         *window // the current window
	vim         *vim.Machine
	cursorsIn   *buffer            // the buffer the machine's cursors are in (N43)
	fileMarks   map[rune]place     // the marks A to Z, each in the one buffer that holds it
	marksToOpen map[rune]filePlace // the file marks fude remembered whose files are not open yet
	startup     []string           // the commands after + that Load runs (B80)
	message     string
	done        bool
	cell        [2]int                  // the client's cell in pixels, 0 when it shows no images (B303)
	sentImages  map[int]bool            // the images an attached client has (B303)
	freed       []int                   // the images dropped, for the client to free (B306)
	restarting  string                  // the session file :restart saved, for main (B170)
	keyFloat    *api.Float              // the key hints a module shows, while they show (B172, B326)
	keyPage     int                     // the page of the key hints that shows
	pendingKeys string                  // the command being typed the modules last heard of (B326)
	ask         *question               // the yes or no question showing until y or n answers it (B362)
	jumpTo      *jumpTo                 // where a plugin's call asked to move the cursor, after the key
	actions     []func()                // what modules' results asked of buffers and windows, after the key (B343)
	lastList    *api.Places             // the last list of places, which modules/places shows (B347)
	snip        *snippet                // the snippet being filled in (B207)
	links       links                   // the ranges a server linked, which typing changes together (B473)
	suggest     suggestion              // a server's inline completion at the cursor (B474)
	menu        cursorMenu              // a menu at the cursor, such as code actions'
	completion  completion              // the completions shown while typing
	lspKeys     bool                    // the attached keys are mapped
	boundKeys   bool                    // the bound keys are mapped
	later       []vim.Key               // keys to feed once the key being fed is done
	pick        *pick                   // the code actions shown to choose from
	finder      *finder                 // the file finder, while it shows
	lastFinder  *finder                 // the finder that closed last, which :pick resume opens again (B374)
	capture     *captured               // the module call the keys typed next go to (B316)
	shownBuf    *buffer                 // the buffer the current window showed when the modules last heard (B314)
	cmdline     cmdline                 // the completions of the command line being typed
	searching   *[2]int                 // the top and cursor of the window a search is typed in, to put back (B21)
	locals      map[string]*vim.Keymaps // the keymaps of kinds of buffer, by kind, over the machine's (B142)
	keymap      keymap                  // the [keymap] tables the session's keys come from, a guest's own (B254)
	history     []string                // the messages shown, oldest first, at most 200 (B157)
	logged      string                  // the message last put in history
	floats      []*window               // the floats over the layout, the topmost last (D55)
	closed      *window                 // the float at the cursor the key being handled closed, which hover enters
	hidden      []*window               // the windows out of sight, each keeping where it was (B364)
	shells      map[int]*buffer         // the shells :terminal n shows and hides, by n, once each started (B205)
	placing     string                  // where a modifier puts the window its command opens, until the next key (B365)
	zoomed      *window                 // the window that shows alone across the layout's room, nil for none (B370)
	unzoomed    [4]int                  // where the zoomed window is in the layout, its x, y, w and h
	tabpages    []tabPage               // the tab pages, of which the one at tabpage shows (B544)
	tabpage     int
	tabNames    map[int]string // by page id, what the top row calls each tab page a module named (B545)
	placed      map[int][4]int // by window id, where a frontend put a window's text, its x, y, w and h (B542)
	clicked     int            // where the last click put the cursor, which a drag selects from (B552)
	grabbed     bool           // a click grabbed the selection, which its release moves (B557)
	pointing    *pointed       // the word the mouse rests on, until the next key (B562)
	lastWin     *window        // the window current when a key last ended, and prevWin the one before it, for <C-w>p (B420)
	prevWin     *window
	suspending  bool                // the client has yet to suspend to the shell, as <C-z> asked (B424)
	exitCode    int                 // what fude exits with, which :cquit sets (B423)
	opts        options             // what the session's new windows start with
	changed     chan change         // told when the shown text changes, for a client over the socket
	notices     *notices            // what its client over the socket has yet to hear of its proposals (B389)
	clip        string              // the text last copied, when no clipboard command runs
	toTerminal  bool                // the client has yet to put clip on its terminal's clipboard
	alert       *string             // a message the client has yet to notify the user of (B564)
	mode        vim.Mode            // the mode as plugins last heard it
	seen        seenAt              // the current window as plugins last heard it
	noticed     noticed             // the windows and the rest as modules last heard of them (B346)
	saves       map[*buffer]*saving // the writes waiting for modules (B282)
	inSave      *saving             // the write the result being applied is for, or nil
	depth       int                 // the undo groups the machine has open
	pending     int                 // the cursor when the group being made began

	width, height int  // the space the windows share
	lined         bool // the buffer line took the top row at the last layout (B258)
	centered      bool // the windows sit in the middle of the screen (B199)

	// ownLeft is what each module shows on the left of this session's status
	// lines alone, by buffer (B312).
	ownLeft map[api.Module]map[*buffer]string
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
	ed := &Editor{next: 1, aside: firstAside, now: time.Now, cfg: cfg, queue: make(chan func(), 16), commands: map[string]func(*Session, call) error{}, sources: map[string]func(*Session, string){}, writers: map[string]writer{}, completionSources: map[string]completer{}, shortOf: map[string]string{}, completers: map[string]func(*Session, string){}, decos: map[any][]decoration{}, servers: map[string]*server{}, down: map[string]string{}, waiting: map[string]func(any){}, clientCommands: map[string]func(*Session, *server, string){}, building: map[string]bool{}, failed: map[string]string{}, listeners: map[api.Module]*listener{}, declared: map[string]declared{}, values: map[string]string{}, segments: map[api.Module]map[*buffer]string{}, leftSegments: map[api.Module]map[*buffer]string{}}
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
	ed.wd = wd
	if err == nil {
		wd, err = filepath.EvalSymlinks(wd)
	}
	if err != nil {
		return nil, fmt.Errorf("find the project: %w", err)
	}
	ed.project = wd
	ed.setup = []func(*Session){builtins}
	// Windows names the user in USERNAME.
	e := &Session{Editor: ed, origin: "user", name: cmp.Or(os.Getenv("USER"), os.Getenv("USERNAME")), saves: map[*buffer]*saving{}}
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
	e.joined++
	e.id = e.joined
	e.leader = e.cfg.Leader
	e.vim = vim.New(host{e})
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
	e.tabpages = []tabPage{newPage(e.win)}
	e.root = e.tabpages[0].root
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
		e.vim.Operator(op.keys, 0, func(t vim.Text, start, end int, lines bool, _ []string) []vim.Edit {
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
	e.vim.Map(vim.Visual, "s", "<Plug>(jump.start)")
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

// Start reads config.toml and opens the files a command line names. A
// path:N starts that file on line N, and path:N:M on byte M of it. Load
// runs each +cmd (B80).
func Start(args []string) (*Session, error) {
	var paths, cmds []string
	var places [][2]int
	for _, a := range args {
		if cmd, ok := strings.CutPrefix(a, "+"); ok {
			cmds = append(cmds, cmp.Or(cmd, "$"))
			continue
		}
		path, line, col := fileAt(a)
		paths, places = append(paths, path), append(places, [2]int{line, col})
	}
	// A restart opens the saved session's files instead (B170).
	restored, restoreErr := restartSession()
	if restored != nil {
		paths, places, cmds = nil, nil, nil
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
	if err := e.recall(); err != nil {
		e.message = err.Error()
	}
	if restored != nil {
		e.restoreSession(*restored)
	}
	if restoreErr != nil {
		e.message = restoreErr.Error()
	}
	// Each path added one buffer, in order.
	for i, at := range places {
		if at[0] == 0 {
			continue
		}
		b := e.bufs[i]
		pos := lineByte(b.text.Snapshot(), at[0], at[1])
		b.setMark('"', pos)
		if b == e.win.buf {
			e.setCursor(pos)
		}
	}
	e.startup = cmds
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

// fileAt splits a path:N or path:N:M that names no file into the path, line
// N and byte M. A colon before anything but a number stays, as in C:\a.go.
func fileAt(arg string) (path string, line, col int) {
	if _, err := os.Lstat(arg); err == nil {
		return arg, 0, 0
	}
	path = arg
	for range 2 {
		i := strings.LastIndexByte(path, ':')
		n, err := strconv.Atoi(path[i+1:])
		if i < 1 || err != nil || n < 1 {
			break
		}
		path, line, col = path[:i], n, line
	}
	return path, line, col
}

// lineByte returns where byte col of line is in t, both from 1, or the
// line's first non-blank when col is 0.
func lineByte(t kernel.Snapshot, line, col int) int {
	p := t.LineStart(min(line, t.Lines()) - 1)
	l := t.Slice(p, vim.LineEnd(t, p))
	if col > 0 {
		return p + min(col-1, max(len(l)-1, 0))
	}
	return p + len(l) - len(strings.TrimLeft(l, " \t"))
}

// useKeys maps km's keys on e's machine and kinds of buffer, with leader as
// <Leader>.
func (e *Session) useKeys(km keymap, leader string) {
	e.keymap = km
	for name, maps := range km {
		if name != "groups" {
			m, mode := e.table(name)
			mapAll(m, mode, maps, leader)
		}
	}
}

// nextID returns the id a new buffer takes. While the empty buffer fude
// began with holds no text and no window but the current one shows it, it
// is that buffer's, which add then closes, so the first file opened is
// buffer 1, as Vim's is.
func (e *Session) nextID() (int, *buffer) {
	for _, b := range e.bufs {
		if b.id != 1 || b.file.path != "" || b.title != "" || b.listed != "" || b.term != nil || b.modified() || b.text.Snapshot().Len() > 0 {
			continue
		}
		if !slices.ContainsFunc(e.sessions, func(s *Session) bool {
			return slices.ContainsFunc(append(s.windows(), s.unseen()...), func(w *window) bool { return w.buf == b && w != e.win })
		}) {
			return b.id, b
		}
	}
	return e.newID(true), nil
}

// firstAside is the id of the first buffer with no number, past any number
// a session reaches.
const firstAside = 1 << 30

// newID returns the id of a new buffer: the next number, or without one an
// id past every number, which :b, :ls and the buffer line skip (B373).
func (e *Session) newID(numbered bool) int {
	if !numbered {
		e.aside++
		return e.aside - 1
	}
	e.next++
	return e.next - 1
}

// add opens path as a new buffer at the end of the list.
func (e *Session) add(path string) (string, error) { return e.addAs(path, true) }

// addAs is add, where an empty buffer may take no number.
func (e *Session) addAs(path string, numbered bool) (string, error) {
	// A typed path takes the system's separators, \ on Windows.
	path = filepath.FromSlash(path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("list %s: %w", path, err)
		}
		// An explorer takes no number, as :b and the buffer line pass it.
		b, err := explorerBuffer(e.newID(false), abs)
		if err != nil {
			return "", err
		}
		e.bufs = append(e.bufs, b)
		e.emit("explorer.read", "", b)
		return "", nil
	}
	// An empty buffer never takes another's place.
	id, blank := 0, (*buffer)(nil)
	if path == "" {
		id = e.newID(numbered)
	} else {
		id, blank = e.nextID()
	}
	b, msg, err := openBuffer(id, path)
	if err != nil {
		return "", err
	}
	e.bufs = append(e.bufs, b)
	// The empty buffer whose id b took goes before b is heard of.
	if blank != nil {
		e.close(blank, true)
	}
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
	if said := e.syntaxFor(b); said != "" {
		msg = cmp.Or(msg, said)
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
		s, ok := owner.(*Session)
		if !ok {
			continue
		}
		for i, d := range ds {
			if d.buf != l.b || d.style == "proposal.stale" {
				continue
			}
			start, end := l.b.text.Mark(d.start), l.b.text.Mark(d.end)
			if slices.ContainsFunc(edits, func(e kernel.Edit) bool {
				return e.Start < end && start < e.End || e.Start == e.End && start < e.Start && e.Start < end
			}) {
				ds[i].style = "proposal.stale"
				s.tell(d.id, "stale")
				l.ed.emit("proposals.changed", "", l.b)
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
		l.ed.askDiagnostics(s, l.b)
	}
	l.b.moveTokens(edits)
	l.ed.askTokens(l.b)
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
	defer func() { e.record("key", fmt.Sprintf("%q in %s", k, modes[mode]), time.Since(at)) }()
	// While hints show, <Esc> drops the command being typed, held keys too,
	// and <BS> its last key.
	if e.keyFloat != nil && (k == "<Esc>" || k == "<BS>") {
		if k == "<Esc>" {
			e.vim.Cancel()
		} else {
			e.vim.Back()
		}
		e.typing()
		return
	}
	if e.keyFloat != nil && e.turnPage(k) {
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
}

// feed handles one key, typed or queued by a command for after it.
func (e *Session) feed(k vim.Key) {
	e.fit(e.win)
	e.message = ""
	// A modifier places the window its command opens by the next key.
	e.placing = ""
	e.pointing = nil
	// Hover enters a float the last key closed only while the next key
	// runs, so the float closed before this key is freed.
	if c := e.closed; c != nil {
		e.closed = nil
		e.forget(c.buf)
	}
	if e.vim.Mode() == vim.Cmdline && k == "<CR>" {
		at, line := time.Now(), e.vim.Line()
		defer func() { e.record("cmd", line, time.Since(at)) }()
	}
	for _, o := range e.overlays() {
		if o.key(e, k) {
			return
		}
	}
	e.vim.IgnoreCase(e.win.opts.IgnoreCase, e.win.opts.SmartCase)
	e.vim.WrapScan(e.win.opts.WrapScan)
	e.vim.VirtualEdit(e.cfg.VirtualEdit)
	e.vim.ExpandTab(e.win.opts.ExpandTab)
	e.vim.TextWidth(e.win.opts.TextWidth)
	e.vim.Abbreviate(e.cfg.Abbreviations)
	e.vim.UseClipboard(e.win.opts.Clipboard)
	e.vim.UseLocal(e.localKeys(e.win.buf))
	e.ownCursors()
	at, top := e.Cursor(), e.topOf(e.win)
	e.settle(e.vim.Feed(k))
	e.incSearch(top, at)
	if spans, put := e.vim.Flash(); len(spans) > 0 {
		kind := "yank"
		if put {
			kind = "put"
		}
		e.touch(e.win.buf, kind, spans...)
	}
	e.snippetAfter()
	e.linkedAfter()
	e.autocomplete(k)
	e.signatureHelp(k)
	e.formatTyped(k)
	e.suggestAfter()
	e.autocommand()
}

// Wait reports whether keys are held for a longer mapping, and how long to
// wait for the next key before calling Timeout.
func (e *Session) Wait() (time.Duration, bool) {
	if t := e.win.buf.term; t != nil && t.esc {
		return escTwice, true
	}
	return time.Duration(e.cfg.KeyTimeout) * time.Millisecond, e.vim.Waiting() && e.keyFloat == nil
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
	e.ownCursors()
	e.settle(e.vim.Timeout())
}

// ownCursors forgets the multicursors once the window shows another buffer
// than theirs (N43).
func (e *Session) ownCursors() {
	if e.win.buf != e.cursorsIn {
		e.vim.ForgetCursors()
		e.cursorsIn = e.win.buf
	}
}

// settle shows the error or message from the keys just run, and scrolls to
// the cursor.
func (e *Session) settle(err error) {
	e.went()
	defer e.typing()
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
	// When the current window shows another buffer, the modules hear of it,
	// as recent keeps the files shown (B213, B314).
	if e.win.buf != e.shownBuf {
		e.shownBuf = e.win.buf
		e.emit("buffer.shown", "", e.win.buf)
	}
	// A view that takes no number cannot show again once no window shows
	// it, so fude closes it (B381).
	for _, b := range slices.Clone(e.bufs) {
		if b.shown && !b.numbered() && !e.showing(b) {
			e.close(b, true)
		}
	}
	if e.win != e.lastWin {
		e.prevWin, e.lastWin = e.lastWin, e.win
	}
	if v := (seenAt{e.win.buf, e.Cursor(), e.Top()}); v != e.seen {
		e.seen = v
		e.emit("window.moved", fmt.Sprintf("%d %d", v.cursor, v.top), v.buf)
	}
}

// seenAt is where the current window is, for window.moved.
type seenAt struct {
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

// Place puts the text of the window with the id at x and y, w columns
// wide, gutter included, and h rows tall, in cells of the frontend's own
// grid. The window takes that place over the layout's (B542).
func (e *Session) Place(id, x, y, w, h int) {
	if e.placed == nil {
		e.placed = map[int][4]int{}
	}
	e.placed[id] = [4]int{x, y, w, h}
	e.layout()
	e.reveal()
}

// Layout returns how the windows split the screen (B541).
func (e *Session) Layout() wire.Split { return e.root.split() }

// Shown returns the keys of the command being typed, or "".
func (e *Session) Shown() string { return e.vim.Shown() }

// Top returns the start of the current window's first line shown.
func (e *Session) Top() int { return e.topOf(e.win) }

func (e *Session) topOf(w *window) int {
	return vim.LineStart(w.buf.text.Snapshot(), w.buf.text.Mark(w.top))
}

// rows is how many lines of text the current window shows.
func (e *Session) rows() int { return max(e.win.h, 1) }

// reveal scrolls the current window the least it must to show the cursor's
// line, which takes as many rows as it wraps to (B37). While a search is
// typed, it shows the match the search goes to instead.
func (e *Session) reveal() {
	w := e.win
	t, c, first := w.buf.text.Snapshot(), w.pos(), w.firstRow()
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
	folded, _ := w.fold(t, t.LineOf(e.Top()))
	w.buf.text.MoveMark(w.top, min(max(t.LineStart(folded), low), above))
	// A line taller than the window shows from one of its rows, chosen so
	// the cursor's row shows. The row shown first stays while it does.
	if w.opts.Wrap && len(w.starts(t, line, tab, d)) > e.rows() {
		above := 0 // the rows of the lines shown above the cursor's
		for p := e.Top(); p < line; {
			above += w.rows(t, p, tab, d)
			next, ok := w.nextLine(t, p)
			if !ok {
				break
			}
			p = next
		}
		starts := w.rowsFrom(t, line, line, e.rows()+1, c, tab, d)
		r := len(starts) - 1
		for r > 0 && starts[r] > c {
			r--
		}
		if above+r >= e.rows() {
			w.buf.text.MoveMark(w.top, w.topRow(t, line, c, first, e.rows(), tab, d))
		}
	}
	// Without wrap the window scrolls sideways the least it must to show the
	// cursor's column and side_scroll_off columns each side of it, at most
	// half the window (B37, B482).
	if !w.opts.Wrap {
		col := e.vim.Past()
		if len(d.inlines) > 0 {
			col += vim.Column(t, line, c, tab, d.inlines)
		} else {
			col += w.buf.column(t, line, c, tab)
		}
		off := min(w.opts.SideScrollOff, (w.cols()-1)/2)
		w.left = max(min(max(w.left, col+off-w.cols()+1), col-off), 0)
	}
	e.level()
}

// Paste inserts text as typed, with no key mapping applied (B8).
func (e *Session) Paste(text string) {
	defer e.unfollow()()
	e.message = ""
	if t := e.win.buf.term; t != nil && t.typing {
		t.em.Paste(text)
		return
	}
	e.ownCursors()
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

// Terminal reports whether the current window is a terminal in terminal
// mode, where every key goes to its program (B205).
func (e *Session) Terminal() bool {
	t := e.win.buf.term
	return t != nil && t.typing
}

// Menu returns the menu to draw, or false when none shows.
func (e *Session) Menu() (wire.Menu, bool) {
	if e.keyFloat != nil {
		return e.keysMenu(), true
	}
	if m := e.cmdline.menu; m != nil {
		return m.box(), true
	}
	if f := e.finder; f != nil {
		e.seek(f)
		t := e.Text()
		fl := f.box(t.Slice(0, t.Len()), e.Cursor(), e.width, e.height)
		// An image previewed shows below the line that names it, under the
		// path and its rule (B305).
		if p := e.previewed(f); p != nil && fl.DocW > 4 && fl.H > 6 {
			rows, cols := e.fitImage(p, fl.DocW-4, fl.H-5)
			fl.DocImage = &wire.Picture{ID: p.id, Row: 3, Rows: rows, Cols: cols}
		}
		return fl, true
	}
	return wire.Menu{Anchor: "cursor", Text: e.menu.text, Spans: e.menu.spans, Cursor: -1, Doc: e.menu.doc, DocSpans: e.menu.docSpans, Rows: int(e.cfg.MenuHeight), Items: e.menu.items, Selected: e.menu.selected}, e.menu.text != ""
}

// floatKeys are a focused float's own keys, which [keymap.float] changes
// (B142).
var floatKeys = map[string]string{"q": "<Plug>(float.close)"}

// Cursors returns the multicursors in the current window, when it shows
// the buffer they were placed in (B392).
func (e *Session) Cursors() []int {
	if e.win.buf != e.cursorsIn {
		return nil
	}
	return e.vim.Cursors()
}

// Selection returns the visual selection, or false outside visual mode.
func (e *Session) Selection() (vim.Selection, bool) { return e.vim.Selection() }

// Line returns the command line being typed.
func (e *Session) Line() string { return e.vim.Line() }

// Substituting reports whether a :s is being typed with its pattern, whose
// preview the windows show.
func (e *Session) Substituting() bool { return e.vim.Substituting() }

// Pending reports whether an operator waits for its target or r for its
// character.
func (e *Session) Pending() bool { return e.vim.Pending() }

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

// Theme returns the theme config.toml or :theme asks the frontend to draw
// in: dark, light or a base16 scheme with its sixteen colours (B146, B158).
// It also holds the colours [colors] puts in place of the theme's (B294).
func (e *Session) Theme() wire.Theme {
	t := wire.Theme{Name: e.theme, Base16: e.base16}
	if e.cfg.Colors != (wire.Colors{}) {
		over := e.cfg.Colors
		t.Colors = &over
	}
	return t
}

// Title returns what the frontend's title shows: the name of the current
// window's file with title on, else "" (B480).
func (e *Session) Title() string {
	if !e.cfg.Title {
		return ""
	}
	return filepath.Base(e.win.buf.name())
}

// Styles returns the styles modules defined, by name, and how many times
// they changed (B345).
func (e *Session) Styles() (map[string]wire.Style, int) { return e.styles, e.restyled }

// LineCursor returns where the cursor is in Line, in bytes.
func (e *Session) LineCursor() int { return e.vim.LineCursor() }

// Message returns the last message for the user, or "".
// It puts each new message into the history that :messages lists.
func (e *Session) Message() string {
	if e.message != e.logged && e.message != "" {
		e.record("msg", e.message, 0)
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
	// '' and `` go back to where the last jump left from (B20).
	host{e}.SetMark('\'', pos)
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
func (e *Session) quit(force bool) error {
	if e.root.win == nil || e.win.anchor != "" || len(e.tabpages) > 1 {
		return e.closeWindow(e.win)
	}
	return e.quitAll(force)
}

// quitAll quits fude. Without force it refuses while a buffer has unsaved
// changes. A session other than the first only leaves, and fude keeps running.
func (e *Session) quitAll(force bool) error {
	if e != e.sessions[0] {
		e.leave()
		return nil
	}
	for _, b := range e.bufs {
		if b.modified() && !force {
			return fmt.Errorf("\"%s\" has unsaved changes, ! drops them", b.name())
		}
	}
	e.quitting()
	if err := e.remember(); err != nil {
		trace.Record("memory", err.Error(), 0)
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
	for _, s := range e.sessions {
		s.done = true
	}
	return nil
}

// cquit quits, dropping every change, so that fude exits with the code arg
// gives, or 1 (B423).
func (e *Session) cquit(arg string) error {
	code := 1
	if arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil {
			return fmt.Errorf("not an exit code: %s", arg)
		}
		code = n
	}
	if err := e.quitAll(true); err != nil {
		return err
	}
	e.exitCode = code
	return nil
}

// ExitCode returns what fude exits with once the session is done, 0 unless
// :cquit gave another (B423).
func (e *Session) ExitCode() int { return e.exitCode }

// Suspend reports, once, that the user asked to suspend fude to the shell,
// for the client to do (B424).
func (e *Session) Suspend() bool {
	ok := e.suspending
	e.suspending = false
	return ok
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

func (e *Session) ex(line string) error {
	first, last, rest, ranged, err := e.vim.LineRange(strings.TrimSpace(line))
	if err != nil {
		return err
	}
	name, arg, _ := strings.Cut(rest, " ")
	name, force := strings.CutSuffix(name, "!")
	if cmd, ok := strings.CutPrefix(rest, "!"); ok {
		name, arg, force = "!", cmd, false
	}
	arg = strings.TrimSpace(arg)
	// :w !cmd gives the lines to cmd, which the shell module runs.
	if (name == "w" || name == "write") && strings.HasPrefix(arg, "!") {
		run, ok := e.commands["!"]
		if !ok {
			return errors.New("no shell to run " + arg[1:])
		}
		cmd := strings.TrimSpace(arg[1:])
		if err := e.mayTake(&cmd); err != nil {
			return err
		}
		return run(e, call{name, cmd, false, first, last, ranged})
	}
	if c, ok := commandsByName[name]; ok {
		if e.power != Host && refuses(c.Name, arg) {
			return errors.New("a guest may not run :" + c.Name)
		}
		if takesPath(c) {
			if err := e.mayTake(&arg); err != nil {
				return err
			}
		}
		return c.run(e, call{name, arg, force, first, last, ranged})
	}
	if run, ok := e.commands[name]; ok {
		if e.pathArgs[name] {
			if err := e.mayTake(&arg); err != nil {
				return err
			}
		}
		return run(e, call{name, arg, force, first, last, ranged})
	}
	return errors.New("not a command: " + line)
}

// mayTake expands the path arg names, as %, # and ~, and reports whether
// the session may use it, saying why not (B204, B245).
func (e *Session) mayTake(arg *string) error {
	full, err := e.expand(*arg)
	if err == nil {
		err = e.outside(full)
	}
	if err != nil {
		return err
	}
	*arg = full
	return nil
}

// edit shows the file at path, opening it if no buffer holds it.
func (e *Session) edit(path string) error {
	switch path {
	case "":
		return errors.New("no file name")
	case "#":
		if b := e.find(path); b != nil {
			e.show(b)
		}
		return nil
	}
	if err := e.outside(path); err != nil {
		return err
	}
	for _, b := range e.bufs {
		if b.is(path) {
			e.show(b)
			return nil
		}
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		e.openExplorer(path, "")
		return nil
	}
	msg, err := e.add(path)
	if err != nil {
		return err
	}
	e.show(e.bufs[len(e.bufs)-1])
	e.message = msg
	return nil
}

// list shows every buffer with its number, % for the current window's and +
// find returns the buffer numbered s or the one whose name contains s. #
// names the buffer the window showed before.
func (e *Session) find(s string) *buffer {
	// A completion names a buffer by its number, then its path.
	if n, _, ok := strings.Cut(s, " "); ok {
		if _, err := strconv.Atoi(n); err == nil {
			s = n
		}
	}
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
	for _, s := range e.sessions {
		for _, segs := range s.ownLeft {
			delete(segs, b)
		}
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
		s.hidden = slices.DeleteFunc(s.hidden, func(w *window) bool { return w.buf == b })
		for _, w := range append(s.root.windows(), s.parked()...) {
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
				// The window shows the buffer it showed before, as Neovim
				// does, or else the next being edited (B89).
				next := s.after(i)
				if a := w.alt; a != nil && a.kind() == "" && s.barred(a) == nil && !a.blank() {
					next = a
				}
				w.show(s.mayShow(next))
				if w.alt == b {
					w.alt = nil
				}
			}
		}
	}
}

// overlay is what may take a key before the vim machine sees it: a module
// call that captured the keys, a question, the finder, a terminal, the
// completion menu, a snippet, a code action pick, the menu and the float
// at the cursor, and the command line's menu. One that is not up takes
// nothing.
type overlay interface {
	key(e *Session, k vim.Key) bool
}

// overlays are what may take a key, in the order they get it.
func (e *Session) overlays() []overlay {
	return []overlay{e.capture, e.ask, e.finder, terminalMode{}, &e.completion, e.snip, &e.suggest, e.pick, &e.menu, &e.cmdline}
}

// cursorMenu is a menu at the cursor, such as code actions', and the
// documentation shown beside it.
type cursorMenu struct {
	text     string
	spans    []wire.Span
	items    []wire.Item // the text's rows, for a frontend that lays them out (B560)
	selected int
	doc      string // the selected item's documentation, beside the menu
	docSpans []wire.Span
}

// key closes the menu and the float at the cursor, which any key does. It
// keeps that float, so that hover again moves the focus into it.
func (f *cursorMenu) key(e *Session, _ vim.Key) bool {
	f.text = ""
	if w := e.floatAt(atCursor); w != nil && w != e.win {
		// Hover enters the float it opened at the cursor, not the mouse's.
		if w.at == nil {
			e.closed = w
		}
		e.closeFloat(w)
	}
	return false
}

// question is a yes or no question in a box in the middle of the screen,
// which only y and n answer (B362).
type question struct {
	text   string
	answer func(yes bool)
}

// confirm asks text, a yes or no question, and gives answer the reply.
// Modules hear it asked and answered (B363).
func (e *Session) confirm(text string, answer func(yes bool)) {
	e.record("ask", text, 0)
	e.ask = &question{text, answer}
	e.emit("confirm.asked", text, e.win.buf)
}

// Confirm returns the yes or no question showing, or "" (B362).
func (e *Session) Confirm() string {
	if e.ask == nil {
		return ""
	}
	return e.ask.text
}

// key answers q with y or n, and takes every other key so that it does
// nothing.
func (q *question) key(e *Session, k vim.Key) bool {
	if q == nil {
		return false
	}
	if k == "y" || k == "n" {
		e.ask = nil
		answer := "no"
		if k == "y" {
			answer = "yes"
		}
		e.emit("confirm.answered", answer, e.win.buf)
		q.answer(k == "y")
		e.reveal()
	}
	return true
}
