//go:build tinygo.wasm || wasm

// Package sdk writes fude plugins in Go. A plugin registers its commands,
// keys, operators, objects and events from its init functions, and
// `fude plugin build` builds it with TinyGo into the .wasm fude loads.
package sdk

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/y3owk1n/fude/api"
)

// Call is where a command or key ran: the cursor's byte offset and the
// count typed before it, 0 for none. A command's call also has the text
// typed after its name, whether ! followed the name, and where the first
// and last lines of its range start, which are the cursor's line when
// Ranged is false.
type Call struct {
	Cursor, Count int
	Args          string
	Bang          bool
	First, Last   int
	Ranged        bool
}

// Range is the text [Start, End) an operator acts on. Lines is set when it
// covers whole lines.
type Range struct {
	Start, End int
	Lines      bool
}

// Event is an event the plugin subscribed to with On. Detail is the new
// mode for mode.changed. For view.moved it is the cursor and the top of the
// view, as "cursor top", which Cursor and Top hold too (B182). For
// buffer.changed it is the range the change covers, as "start end", which
// Start and End hold too (B183).
type Event struct {
	Name, Detail string
	Cursor, Top  int
	Start, End   int
}

// The errors fude returns for a call it refuses.
var (
	ErrDenied = errors.New("the plugin did not declare the capability it needs")
	ErrRange  = errors.New("a range outside the text")
	ErrMemory = errors.New("the plugin's memory could not take the result")
	ErrFile   = errors.New("a file outside the project, or one that cannot be read")
	ErrRun    = errors.New("a program the plugin did not declare, or one that failed")
	ErrNet    = errors.New("a host the plugin did not declare, or a request that failed")
)

// fail returns the error a negative result from fude means, or nil.
func fail(code int64) error {
	if code >= 0 {
		return nil
	}
	return [...]error{ErrDenied, ErrRange, ErrMemory, ErrFile, ErrRun, ErrNet}[min(-code, 6)-1]
}

// The functions fude gives a plugin.

//go:wasmimport fude log
func hostLog(ptr *byte, n uint32)

//go:wasmimport fude context
func hostContext(ptr *byte, n uint32) int64

//go:wasmimport fude setting
func hostSetting(ptr *byte, n uint32) int64

//go:wasmimport fude text_len
func hostTextLen() int32

//go:wasmimport fude lines
func hostLines() int32

//go:wasmimport fude len_of
func hostLenOf(id uint32) int32

//go:wasmimport fude text_of
func hostTextOf(id, start, end uint32) int64

//go:wasmimport fude line_of
func hostLineOf(pos uint32) int32

//go:wasmimport fude line_start
func hostLineStart(line uint32) int32

//go:wasmimport fude text
func hostText(start, end uint32) int64

//go:wasmimport fude subscribe
func hostSubscribe(ptr *byte, n uint32) int32

//go:wasmimport fude register
func hostRegister(ptr *byte, n uint32) int32

//go:wasmimport fude read
func hostRead(ptr *byte, n uint32) int64

//go:wasmimport fude result
func hostResult(ptr *byte, n uint32) int32

//go:wasmimport fude read_file
func hostReadFile(ptr *byte, n uint32) int64

//go:wasmimport fude run
func hostRun(ptr *byte, n uint32) int64

//go:wasmimport fude http_get
func hostHTTPGet(ptr *byte, n uint32) int64

// held is the buffer alloc made for fude's last answer, kept here so the
// collector does not free it before the plugin reads it.
var held []byte

//go:wasmexport alloc
func alloc(n uint32) *byte {
	held = make([]byte, max(n, 1))
	return &held[0]
}

// answer returns the text fude wrote into held, as a result of
// ptr<<32|len, or the error a negative result means.
func answer(r int64) (string, error) {
	if err := fail(r); err != nil {
		return "", err
	}
	return string(held[:uint32(r)]), nil
}

// ptr returns s as the pointer and length fude reads it at.
func ptr(s string) (*byte, uint32) {
	return unsafe.StringData(s), uint32(len(s))
}

// handlers are what call runs, by the id register gave each.
var handlers []func(a, b, c int64) int64

// add registers r, which call runs with run.
func add(r api.Registration, run func(a, b, c int64) int64) {
	r.ID = uint32(len(handlers))
	b, err := json.Marshal(r)
	if err == nil {
		err = fail(int64(hostRegister(&b[0], uint32(len(b)))))
	}
	if err != nil {
		Log("register " + r.Name + ": " + err.Error())
		return
	}
	handlers = append(handlers, run)
}

// Register registers r, of any kind and with any field a module's
// registration has, which f runs with the arguments its kind takes, 0 for
// the others. What f returns is an Object's selection, as start<<32|end,
// or a negative number for none. Call it from an init function.
func Register(r api.Registration, f func(a, b, c int) int) {
	add(r, func(a, b, c int64) int64 { return int64(f(int(a), int(b), int(c))) })
}

// Do asks for what r holds, as a module's call returns it. fude applies it
// once the call returns, and none of it when a part needs a capability the
// plugin did not declare. Emit names the plugin's events without its own
// name, which fude puts before them.
func Do(r api.Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return fail(int64(hostResult(&b[0], uint32(len(b)))))
}

// ids is the id ID gave last.
var ids int

// ID returns a new id for a decoration Do puts on the text.
func ID() int {
	ids++
	return ids
}

// decorate puts d on the text under a new id, which it returns.
func decorate(d api.Decoration) (int, error) {
	d.ID = ID()
	if err := Do(api.Result{Decorate: []api.Decoration{d}}); err != nil {
		return 0, err
	}
	return d.ID, nil
}

//go:wasmexport call
func call(id uint32, a, b, c int64) int64 {
	return handlers[id](a, b, c)
}

// later are the funcs callbacks run, by the id fude calls them back with,
// each kept until it returns false, and last is the id given last.
var (
	later = map[uint32]func() bool{}
	last  uint32
)

//go:wasmexport callback
func callback(id uint32) {
	if f := later[id]; f != nil && !f() {
		delete(later, id)
	}
}

// Callback returns a new id for a callback a Result asks for, such as a
// timer's, a job's, a request's, a pick's or a menu's. Each callback with
// that id runs f, as an event runs, until f returns false. f reads the
// callback's context, such as a job's output, with About.
func Callback(f func() bool) uint32 {
	last++
	later[last] = f
	return last
}

// After runs f once, ms milliseconds after the call that asks for it
// returns, off the input path, as an event runs. f reads the text of the
// buffer that call was about. A call that fails asks for nothing.
func After(ms int, f func()) error {
	return schedule(f, func(id uint32) error {
		return Do(api.Result{Timers: []api.Timer{{ID: id, After: time.Duration(ms) * time.Millisecond}}})
	})
}

// Start runs the program name with args in the background, in the project,
// with no budget, and then f with what it printed, off the input path, as
// an event runs. It needs process, and name in the plugin's programs.
func Start(f func(output string, err error), name string, args ...string) error {
	return Pipe("", f, name, args...)
}

// Pipe runs the program name with args as Start does, with input on its
// stdin.
func Pipe(input string, f func(output string, err error), name string, args ...string) error {
	return schedule(done(f), func(id uint32) error {
		return Do(api.Result{Jobs: []api.Job{{ID: id, Argv: append([]string{name}, args...), Input: input}}})
	})
}

// Fetch gets the http or https URL in the background, with no budget, and
// then runs f with the body, as Start does. It needs net, and the URL's
// host in the plugin's hosts.
func Fetch(url string, f func(body string, err error)) error {
	return schedule(done(f), func(id uint32) error { return Do(api.Result{Jobs: []api.Job{{ID: id, URL: url}}}) })
}

// Request sends method to the language servers of the buffer the call is
// about, with its textDocument, its position at pos unless pos is -1, and
// params, a JSON object or "". Then f runs, as Start's does, with the first
// answer that is not null, as JSON. It needs lsp.
func Request(method string, pos int, params string, f func(result string, err error)) error {
	return RequestRange(method, pos, -1, params, f)
}

// RequestRange is Request with the range from start to end in place of a
// position, as textDocument/rangeFormatting takes (B281).
func RequestRange(method string, start, end int, params string, f func(result string, err error)) error {
	return schedule(done(f), func(id uint32) error {
		return Do(api.Result{Requests: []api.Request{{ID: id, Method: method, Pos: start, End: end, Params: params}}})
	})
}

// done is the callback that runs f with a job's output and error.
func done(f func(string, error)) func() {
	return func() {
		var err error
		if e := About("error"); e != "" {
			err = errors.New(e)
		}
		f(About("output"), err)
	}
}

// schedule keeps f under a new id and asks fude for it with ask, or forgets
// it when fude refuses.
func schedule(f func(), ask func(id uint32) error) error {
	return keep(func() bool { f(); return false }, ask)
}

// keep is schedule for f that runs until it returns false.
func keep(f func() bool, ask func(id uint32) error) error {
	id := Callback(f)
	if err := ask(id); err != nil {
		delete(later, id)
		return err
	}
	return nil
}

// Stream runs the program name with args as Start does, and runs f, as an
// event runs, with each line it prints as it prints it. When it ends, f
// runs once more with done and the error it ended with. It needs process,
// and name in the plugin's programs (B339).
func Stream(f func(line string, done bool, err error), name string, args ...string) error {
	return keep(func() bool {
		if About("done") != "true" {
			f(About("output"), false, nil)
			return true
		}
		var err error
		if e := About("error"); e != "" {
			err = errors.New(e)
		}
		f("", true, err)
		return false
	}, func(id uint32) error {
		return Do(api.Result{Jobs: []api.Job{{ID: id, Argv: append([]string{name}, args...), Stream: true}}})
	})
}

// listeners are the functions On added, by event.
var listeners = map[string][]func(Event){}

//go:wasmexport on_event
func onEvent(p *byte, n uint32) {
	name, detail, _ := strings.Cut(string(unsafe.Slice(p, n)), " ")
	ev := Event{Name: name, Detail: detail}
	a, b, _ := strings.Cut(detail, " ")
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	switch name {
	case "view.moved":
		ev.Cursor, ev.Top = x, y
	case "buffer.changed":
		ev.Start, ev.End = x, y
	}
	for _, f := range listeners[name] {
		f(ev)
	}
}

// Command adds the ex command :name, which runs f. help says what it does,
// for :h. Call it from an init function.
func Command(name, help string, f func(Call)) {
	add(api.Registration{Kind: api.Command, Name: name, Help: help}, func(a, b, _ int64) int64 {
		first, _ := strconv.Atoi(About("first"))
		last, _ := strconv.Atoi(About("last"))
		f(Call{int(a), int(b), About("args"), About("bang") == "true", first, last, About("ranged") == "true"})
		return 0
	})
}

// About returns what the call running is about by name, or "", such as
// path, or a callback's output.
func About(name string) string {
	p, n := ptr(name)
	v, _ := answer(hostContext(p, n))
	return v
}

// Key maps keys in normal mode to f, as a mapping in config.toml does.
// help says what it does, for :h and the key hints.
func Key(keys, help string, f func(Call)) {
	add(api.Registration{Kind: api.Key, Name: keys, Help: help}, func(a, b, _ int64) int64 {
		f(Call{Cursor: int(a), Count: int(b)})
		return 0
	})
}

// Insert maps keys in insert mode to f, as a mapping in config.toml does.
// The cursor goes after the text f's first edit puts in, as typing it
// would leave it. help says what it does, for :h and the key hints.
func Insert(keys, help string, f func(Call)) {
	add(api.Registration{Kind: api.Insert, Name: keys, Help: help}, func(a, _, _ int64) int64 {
		f(Call{Cursor: int(a)})
		return 0
	})
}

// Source adds the source name to :pick, which runs f with the text typed
// after the name as its args, for f to show a list with Picker or Pick.
// help says what it does, for :h.
func Source(name, help string, f func(Call)) {
	add(api.Registration{Kind: api.Source, Name: name, Help: help}, func(a, b, _ int64) int64 {
		f(Call{Cursor: int(a), Count: int(b), Args: About("args")})
		return 0
	})
}

// Plug adds a command keys reach only as <Plug>(plugin.name), which runs
// f, as :plug does. help says what it does, for :h.
func Plug(name, help string, f func(Call)) {
	add(api.Registration{Kind: api.Plug, Name: name, Help: help}, func(a, b, _ int64) int64 {
		f(Call{Cursor: int(a), Count: int(b)})
		return 0
	})
}

// Operator adds the operator keys, which runs f on the text a motion or
// object covers, in every way fude's own operators run.
func Operator(keys, help string, f func(Range)) {
	add(api.Registration{Kind: api.Operator, Name: keys, Help: help}, func(a, b, c int64) int64 {
		f(Range{int(a), int(b), c != 0})
		return 0
	})
}

// Object adds the text object keys. f gets the selection or the cursor as
// [start, end) and returns the text it selects, or false for none.
func Object(keys, help string, f func(start, end int) (int, int, bool)) {
	add(api.Registration{Kind: api.Object, Name: keys, Help: help}, func(a, b, _ int64) int64 {
		s, e, ok := f(int(a), int(b))
		if !ok {
			return -1
		}
		return int64(s)<<32 | int64(e)
	})
}

// On runs f for each event named, such as buffer.saved. Call it from an
// init function.
func On(event string, f func(Event)) {
	if listeners[event] == nil {
		p, n := ptr(event)
		if err := fail(int64(hostSubscribe(p, n))); err != nil {
			Log("subscribe " + event + ": " + err.Error())
			return
		}
	}
	listeners[event] = append(listeners[event], f)
}

// Log adds a line to fude's log.
func Log(text string) {
	p, n := ptr(text)
	hostLog(p, n)
}

// Len returns the length of the text in bytes. It needs buffer.read.
func Len() (int, error) {
	n := hostTextLen()
	return int(n), fail(int64(n))
}

// Buffer is the buffer a call or event is about: its id, its file's path,
// "" for none, and its filetype, "" for none. Buffers also fills in Name,
// Modified and Kind.
type Buffer struct {
	ID             int
	Path, Filetype string
	Name           string
	Modified       bool
	Kind           string
}

// Current returns the buffer the call running is about.
func Current() Buffer {
	id, _ := strconv.Atoi(About("buffer"))
	return Buffer{ID: id, Path: About("path"), Filetype: About("filetype")}
}

// Setting returns the value of the setting name in the plugin's settings
// table in config.toml, as text, or false when it has none.
func Setting(name string) (string, bool) {
	p, n := ptr(name)
	v, err := answer(hostSetting(p, n))
	return v, err == nil
}

// Option returns the option or config.toml setting called name there, as
// text, such as spell or spell_words, or false for none. An option is the
// window's, as a filetype table may set it.
func Option(name string) (string, bool) {
	var v string
	var ok bool
	err := Read("Option", []any{name}, &v, &ok)
	return v, err == nil && ok
}

// Read reads the field name of the call's api.Scope, as a module's call
// reads it. It needs the capability that field needs. For a func, Read
// calls it with args and puts each value it returns into one of out, in
// order, an error as its text. Any other field goes into out[0].
func Read(name string, args []any, out ...any) error {
	b, err := json.Marshal(struct {
		Name string
		Args []any
	}{name, args})
	if err != nil {
		return err
	}
	s, err := answer(hostRead(&b[0], uint32(len(b))))
	if err != nil {
		return err
	}
	var vals []json.RawMessage
	if err := json.Unmarshal([]byte(s), &vals); err != nil {
		return err
	}
	for i := range min(len(vals), len(out)) {
		if err := json.Unmarshal(vals[i], out[i]); err != nil {
			return err
		}
	}
	return nil
}

// Config returns the value at a dotted path of config.toml, such as
// filetype.go.format or formatter.gofmt.command, as JSON, or false for
// none. A table's key follows its name in the path.
func Config(path string) (string, bool) {
	var v string
	var ok bool
	err := Read("Config", []any{path}, &v, &ok)
	return v, err == nil && ok
}

// Node is a syntax node: its kind, such as function_declaration, and where
// it starts and ends.
type Node struct {
	Kind       string
	Start, End int
}

// NodeAt returns the named syntax node that holds pos, or with up above 0
// its up'th named parent, in the buffer the call is about. It is false
// without a grammar or a parse, or above the root. It needs buffer.read.
func NodeAt(pos, up int) (Node, bool) {
	var n Node
	var ok bool
	err := Read("Node", []any{pos, up}, &n.Kind, &n.Start, &n.End, &ok)
	return n, err == nil && ok
}

// Language returns the filetype of the text at pos, the buffer's or that
// of an injected region holding it, such as a code block. It needs
// buffer.read.
func Language(pos int) (string, error) {
	var lang string
	err := Read("Language", []any{pos}, &lang)
	return lang, err
}

// Comment returns how the language at pos comments a line, such as
// "// %s", or false when fude knows none. It needs buffer.read.
func Comment(pos int) (string, bool) {
	var cs string
	var ok bool
	err := Read("Comment", []any{pos}, &cs, &ok)
	return cs, err == nil && ok
}

// Span is a highlight capture over [Start, End), named as the query names
// it, such as comment or string.
type Span = api.Span

// Spans returns the highlight captures over [start, end) of the text, in
// paint order, or none without a grammar. It needs buffer.read.
func Spans(start, end int) ([]Span, error) {
	var out []Span
	err := Read("Spans", []any{start, end}, &out)
	return out, err
}

// Buffers returns every open buffer, by id. It needs buffer.read.
func Buffers() ([]Buffer, error) {
	var all map[int]api.Buffer
	if err := Read("Buffers", nil, &all); err != nil {
		return nil, err
	}
	var out []Buffer
	for _, id := range slices.Sorted(maps.Keys(all)) {
		b := all[id]
		out = append(out, Buffer{ID: id, Path: b.Path, Filetype: b.Filetype, Name: b.Name, Modified: b.Modified, Kind: b.Kind})
	}
	return out, nil
}

// LenOf returns the length of the text of the buffer id, as it was when the
// call began. It needs buffer.read.
func LenOf(id int) (int, error) {
	n := hostLenOf(uint32(id))
	return int(n), fail(int64(n))
}

// TextOf returns [start, end) of the text of the buffer id, as it was when
// the call began. It needs buffer.read.
func TextOf(id, start, end int) (string, error) {
	return answer(hostTextOf(uint32(id), uint32(start), uint32(end)))
}

// Mode returns the mode, such as normal or insert.
func Mode() string { return About("mode") }

// IndentWidth returns the columns of an indent level in the buffer the call
// is about, as its .editorconfig, filetype and config.toml settle it, and
// TabWidth the columns a tab shows as (B280).
func IndentWidth() int { n, _ := strconv.Atoi(About("indent")); return n }

func TabWidth() int { n, _ := strconv.Atoi(About("tab")); return n }

// Lines returns how many lines the text has. It needs buffer.read.
func Lines() (int, error) {
	n := hostLines()
	return int(n), fail(int64(n))
}

// LineOf returns the line that holds the byte at pos, from 0. It needs
// buffer.read.
func LineOf(pos int) (int, error) {
	n := hostLineOf(uint32(pos))
	return int(n), fail(int64(n))
}

// LineStart returns where line starts, counting lines from 0. It needs
// buffer.read.
func LineStart(line int) (int, error) {
	n := hostLineStart(uint32(line))
	return int(n), fail(int64(n))
}

// Text returns the text in [start, end). It needs buffer.read.
func Text(start, end int) (string, error) {
	return answer(hostText(uint32(start), uint32(end)))
}

// Edit asks to replace [start, end) with text. The edits a call asks for
// apply together once it returns. It needs buffer.write.
func Edit(start, end int, text string) error {
	return Do(api.Result{Edits: []api.Edit{{Start: start, End: end, Text: text}}})
}

// SetOption sets the option name, expand_tab, indent_width or tab_width,
// to value on the buffer the call is about, over config.toml's, until the
// buffer closes.
func SetOption(name, value string) error { return Do(api.Result{Set: map[string]string{name: value}}) }

// Emit asks fude to emit the event plugin.name about the text the call
// read.
func Emit(name string) error { return Do(api.Result{Emit: []string{name}}) }

// Decorate puts a style, such as keyword, on [start, end), and returns its
// id for Undecorate. It needs ui.
func Decorate(start, end int, style string) (int, error) {
	return decorate(api.Decoration{Start: start, End: end, Style: style})
}

// Sign shows text in the sign column on the line that holds pos, in the
// colour of style, such as diagnostic.warn. The column shows its first two
// characters. It returns an id for Undecorate, and moves with edits. A
// diagnostic's sign wins on its line. It needs ui.
func Sign(pos int, text, style string) (int, error) { return sign(pos, text, style, 0) }

// SignUnder is Sign for a sign any other sign on the line shows over, as
// git's signs are.
func SignUnder(pos int, text, style string) (int, error) { return sign(pos, text, style, -1) }

func sign(pos int, text, style string, layer int) (int, error) {
	return decorate(api.Decoration{Start: pos, End: pos, Style: style, Sign: text, Layer: layer})
}

// Note shows text dim after the end of the line that holds pos. It returns
// an id for Undecorate, and moves with edits. It needs ui.
func Note(pos int, text string) (int, error) {
	return decorate(api.Decoration{Start: pos, End: pos, Note: text})
}

// Pick shows items in a list, after prompt, once the call returns. The
// user types to narrow it and picks one with enter. Then f runs, as an
// event does, with the index of the item picked, or false when they
// cancelled or no item matched. A list asked for while one shows is
// cancelled. It needs ui.
func Pick(prompt string, items []string, f func(i int, ok bool)) error {
	return schedule(func() {
		i, _ := strconv.Atoi(About("picked"))
		f(i, About("ok") == "true" && i >= 0)
	}, func(id uint32) error { return Do(api.Result{Pick: &api.Pick{ID: id, Prompt: prompt, Items: items}}) })
}

// Picker shows a live list in the finder, titled title, with hint shown
// before a query is typed and query typed at the start. find runs, as an
// event does, with each query typed and whether <C-g> asked for the files
// git ignores too, and returns the items for it. With places, an item is
// path:line:column:text, shown under its file, and picking one opens the
// place. chose then runs with the item picked, or false when they
// cancelled. It needs ui.
func Picker(title, hint, query string, places bool, find func(query string, all bool) []string, chose func(item string, ok bool)) error {
	pick := api.Pick{Title: title, Hint: hint, Query: query, Live: true, Places: places}
	return keep(func() bool {
		if picked := About("picked"); picked != "" {
			i, _ := strconv.Atoi(picked)
			chose(About("item"), About("ok") == "true" && i >= 0)
			return false
		}
		// A query, which the next callback follows.
		items := pick
		items.Items = find(About("query"), About("all") == "true")
		Do(api.Result{Pick: &items})
		return true
	}, func(id uint32) error {
		pick.ID = id
		return Do(api.Result{Pick: &pick})
	})
}

// Ask asks the user prompt once the call returns, and runs f, as an event
// does, with what they typed, or false when they cancelled. It needs ui.
func Ask(prompt string, f func(text string, ok bool)) error {
	return schedule(func() { f(About("text"), About("ok") == "true") }, func(id uint32) error {
		return Do(api.Result{Pick: &api.Pick{ID: id, Prompt: prompt}})
	})
}

// Jump moves the cursor to pos once the call returns, after its edits, when
// the current window shows the buffer the call was about. It needs ui.
func Jump(pos int) error { return Do(api.Result{Jump: &api.Jump{Pos: pos}}) }

// Open opens the file at path, relative to the project, in the current
// window once the call returns, with the cursor at pos. It refuses a path
// that leads outside the project. It needs ui.
func Open(path string, pos int) error { return Do(api.Result{Jump: &api.Jump{Path: path, Pos: pos}}) }

// Show shows text in a read-only window titled title, coloured as
// filetype, "" for none. A window of that title already open shows text in
// place of what it showed. It needs ui.
func Show(title, filetype, text string) error { return ShowStyled(title, filetype, text, nil) }

// ShowStyled is Show with spans styling the text, such as diff.plus on a
// line, over what the filetype colours.
func ShowStyled(title, filetype, text string, spans []Span) error {
	return Do(api.Result{Show: &api.Panel{Title: title, Filetype: filetype, Text: text, Spans: spans}})
}

// Fold closes a fold over the lines from start's through end's. They hide
// behind the first line and a count. The fold moves with edits, and
// Undecorate opens it by the id Fold returns. It needs ui.
func Fold(start, end int) (int, error) {
	return decorate(api.Decoration{Start: start, End: end, Fold: true})
}

// Replace shows text in style in place of [start, end), which must be on
// one line, and empty text hides it. The text stays, and the cursor's line
// shows it as it is. It returns an id for Undecorate, and moves with edits.
// It needs ui.
func Replace(start, end int, text, style string) (int, error) {
	return decorate(api.Decoration{Start: start, End: end, Style: style, Note: text, Replace: true})
}

// Shade shades the whole rows of the lines from start's through end's, to
// the window's edge, in the ground of style, such as markdown.code. The
// text keeps its own colours on it. It returns an id for Undecorate, and
// moves with edits. It needs ui.
func Shade(start, end int, style string) (int, error) {
	return decorate(api.Decoration{Start: start, End: end, Style: style, Line: true})
}

// Rows draws rows of their own below the line that holds pos, or above
// it when above, in style. They hold no text, so the cursor passes over
// them, and the line under the cursor draws none. It returns an id for
// Undecorate, and moves with edits. It needs ui.
func Rows(pos int, rows []string, style string, above bool) (int, error) {
	return decorate(api.Decoration{Start: pos, End: pos, Style: style, Lines: rows, Above: above})
}

// Block makes the lines from start's through end's one block. While the
// cursor or a selection is on any of them, they all show their text as it
// is, without replacements or rows of their own. It returns an id for
// Undecorate, and moves with edits. It needs ui.
func Block(start, end int) (int, error) {
	return decorate(api.Decoration{Start: start, End: end, Block: true})
}

// Decorations returns the plugin's decorations on every open buffer, each
// with its Buffer, where they are now. It needs ui.
func Decorations() ([]api.Decoration, error) {
	var out []api.Decoration
	err := Read("Decorations", nil, &out)
	return out, err
}

// Undecorate removes the decoration id. It needs ui.
func Undecorate(id int) error { return Do(api.Result{Undecorate: []int{id}}) }

// Status shows text on the statusline of the buffer the call read, or none
// for "". It needs ui.
func Status(text string) error { return Do(api.Result{Status: &text}) }

// Message shows text in the message area once the call returns. It needs
// ui.
func Message(text string) error { return Do(api.Result{Message: text}) }

// Float shows markdown in a float at the cursor, or closes it for "". It
// needs ui.
func Float(markdown string) error { return Do(api.Result{Float: &api.Float{Text: markdown}}) }

// ReadFile returns the file at path, relative to the folder fude started
// in, and refuses a path outside it. It needs fs.read.
func ReadFile(path string) (string, error) {
	p, n := ptr(path)
	return answer(hostReadFile(p, n))
}

// Run runs name with args in the folder fude started in, and returns what
// it printed. name must be in the plugin's programs. It needs process.
func Run(name string, args ...string) (string, error) {
	p, n := ptr(strings.Join(append([]string{name}, args...), "\x00"))
	return answer(hostRun(p, n))
}

// Get returns the body of the http or https URL, whose host must be in the
// plugin's hosts. It needs net.
func Get(url string) (string, error) {
	p, n := ptr(url)
	return answer(hostHTTPGet(p, n))
}
