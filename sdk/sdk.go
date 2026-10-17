//go:build tinygo.wasm || wasm

// Package sdk writes fude plugins in Go. A plugin registers its commands,
// keys, operators, objects and events from its init functions, and
// `fude plugin build` builds it with TinyGo into the .wasm fude loads.
package sdk

import (
	"errors"
	"strconv"
	"strings"
	"unsafe"
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

// What register adds, as fude numbers them.
const (
	kindCommand = iota
	kindOperator
	kindObject
	kindKey
	kindInsert
)

// The functions fude gives a plugin.

//go:wasmimport fude log
func hostLog(ptr *byte, n uint32)

//go:wasmimport fude context
func hostContext(ptr *byte, n uint32) int64

//go:wasmimport fude after
func hostAfter(ms, id uint32) int32

//go:wasmimport fude start
func hostStart(ptr *byte, n uint32, in *byte, inN uint32, id uint32) int32

//go:wasmimport fude fetch
func hostFetch(ptr *byte, n uint32, id uint32) int32

//go:wasmimport fude request
func hostRequest(ptr *byte, n uint32, pos, id uint32) int32

//go:wasmimport fude setting
func hostSetting(ptr *byte, n uint32) int64

//go:wasmimport fude option
func hostOption(ptr *byte, n uint32) int64

//go:wasmimport fude text_len
func hostTextLen() int32

//go:wasmimport fude lines
func hostLines() int32

//go:wasmimport fude node
func hostNode(pos, up uint32) int64

//go:wasmimport fude language
func hostLanguage(pos uint32) int64

//go:wasmimport fude spans
func hostSpans(start, end uint32) int64

//go:wasmimport fude comment
func hostComment(pos uint32) int64

//go:wasmimport fude buffers
func hostBuffers() int64

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

//go:wasmimport fude edit
func hostEdit(start, end uint32, ptr *byte, n uint32) int32

//go:wasmimport fude register
func hostRegister(kind uint32, nptr *byte, nn uint32, hptr *byte, hn uint32, id uint32) int32

//go:wasmimport fude subscribe
func hostSubscribe(ptr *byte, n uint32) int32

//go:wasmimport fude emit
func hostEmit(ptr *byte, n uint32) int32

//go:wasmimport fude decorate
func hostDecorate(start, end uint32, ptr *byte, n uint32) int32

//go:wasmimport fude sign
func hostSign(pos uint32, ptr *byte, n uint32, under uint32) int32

//go:wasmimport fude note
func hostNote(pos uint32, ptr *byte, n uint32) int32

//go:wasmimport fude show
func hostShow(ptr *byte, n uint32, sptr *byte, sn uint32) int32

//go:wasmimport fude jump
func hostJump(pos uint32) int32

//go:wasmimport fude pick
func hostPick(ptr *byte, n uint32, id uint32) int32

//go:wasmimport fude open
func hostOpen(pos uint32, ptr *byte, n uint32) int32

//go:wasmimport fude undecorate
func hostUndecorate(id uint32) int32

//go:wasmimport fude status
func hostStatus(ptr *byte, n uint32) int32

//go:wasmimport fude message
func hostMessage(ptr *byte, n uint32) int32

//go:wasmimport fude float
func hostFloat(ptr *byte, n uint32) int32

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

// add registers kind under name with help, run by run.
func add(kind uint32, name, help string, run func(a, b, c int64) int64) {
	np, nn := ptr(name)
	hp, hn := ptr(help)
	if err := fail(int64(hostRegister(kind, np, nn, hp, hn, uint32(len(handlers))))); err != nil {
		Log("register " + name + ": " + err.Error())
		return
	}
	handlers = append(handlers, run)
}

//go:wasmexport call
func call(id uint32, a, b, c int64) int64 {
	return handlers[id](a, b, c)
}

// later are the functions After, Start and Fetch run once, by the id fude calls them back
// with, and last is the id given last.
var (
	later = map[uint32]func(){}
	last  uint32
)

//go:wasmexport callback
func callback(id uint32) {
	f := later[id]
	delete(later, id)
	if f != nil {
		f()
	}
}

// After runs f once, ms milliseconds after the call that asks for it
// returns, off the input path, as an event runs. f reads the text of the
// buffer that call was about. A call that fails asks for nothing.
func After(ms int, f func()) error {
	return schedule(f, func(id uint32) int32 { return hostAfter(uint32(ms), id) })
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
	p, n := ptr(strings.Join(append([]string{name}, args...), "\x00"))
	in, inN := ptr(input)
	return schedule(done(f), func(id uint32) int32 { return hostStart(p, n, in, inN, id) })
}

// Fetch gets the http or https URL in the background, with no budget, and
// then runs f with the body, as Start does. It needs net, and the URL's
// host in the plugin's hosts.
func Fetch(url string, f func(body string, err error)) error {
	p, n := ptr(url)
	return schedule(done(f), func(id uint32) int32 { return hostFetch(p, n, id) })
}

// Request sends method to the language servers of the buffer the call is
// about, with its textDocument, its position at pos unless pos is -1, and
// params, a JSON object or "". Then f runs, as Start's does, with the first
// answer that is not null, as JSON. It needs lsp.
func Request(method string, pos int, params string, f func(result string, err error)) error {
	p, n := ptr(method + "\x00" + params)
	return schedule(done(f), func(id uint32) int32 { return hostRequest(p, n, uint32(pos), id) })
}

// done is the callback that runs f with a job's output and error.
func done(f func(string, error)) func() {
	return func() {
		var err error
		if e := about("error"); e != "" {
			err = errors.New(e)
		}
		f(about("output"), err)
	}
}

// schedule keeps f under a new id and asks fude for it with ask, or forgets
// it when fude refuses.
func schedule(f func(), ask func(id uint32) int32) error {
	last++
	later[last] = f
	if err := fail(int64(ask(last))); err != nil {
		delete(later, last)
		return err
	}
	return nil
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
	add(kindCommand, name, help, func(a, b, _ int64) int64 {
		first, _ := strconv.Atoi(about("first"))
		last, _ := strconv.Atoi(about("last"))
		f(Call{int(a), int(b), about("args"), about("bang") == "true", first, last, about("ranged") == "true"})
		return 0
	})
}

// about returns what the call running is about by name, or "".
func about(name string) string {
	p, n := ptr(name)
	v, _ := answer(hostContext(p, n))
	return v
}

// Key maps keys in normal mode to f, as a mapping in config.toml does.
// help says what it does, for :h and the key hints.
func Key(keys, help string, f func(Call)) {
	add(kindKey, keys, help, func(a, b, _ int64) int64 {
		f(Call{Cursor: int(a), Count: int(b)})
		return 0
	})
}

// Insert maps keys in insert mode to f, as a mapping in config.toml does.
// The cursor goes after the text f's first edit puts in, as typing it
// would leave it. help says what it does, for :h and the key hints.
func Insert(keys, help string, f func(Call)) {
	add(kindInsert, keys, help, func(a, _, _ int64) int64 {
		f(Call{Cursor: int(a)})
		return 0
	})
}

// Operator adds the operator keys, which runs f on the text a motion or
// object covers, in every way fude's own operators run.
func Operator(keys, help string, f func(Range)) {
	add(kindOperator, keys, help, func(a, b, c int64) int64 {
		f(Range{int(a), int(b), c != 0})
		return 0
	})
}

// Object adds the text object keys. f gets the selection or the cursor as
// [start, end) and returns the text it selects, or false for none.
func Object(keys, help string, f func(start, end int) (int, int, bool)) {
	add(kindObject, keys, help, func(a, b, _ int64) int64 {
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
// "" for none, and its filetype, "" for none.
type Buffer struct {
	ID             int
	Path, Filetype string
}

// Current returns the buffer the call running is about.
func Current() Buffer {
	id, _ := strconv.Atoi(about("buffer"))
	return Buffer{id, about("path"), about("filetype")}
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
	p, n := ptr(name)
	v, err := answer(hostOption(p, n))
	return v, err == nil
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
	s, err := answer(hostNode(uint32(pos), uint32(up)))
	if err != nil {
		return Node{}, false
	}
	f := strings.Split(s, "\x00")
	start, _ := strconv.Atoi(f[1])
	end, _ := strconv.Atoi(f[2])
	return Node{f[0], start, end}, true
}

// Language returns the filetype of the text at pos, the buffer's or that
// of an injected region holding it, such as a code block. It needs
// buffer.read.
func Language(pos int) (string, error) { return answer(hostLanguage(uint32(pos))) }

// Comment returns how the language at pos comments a line, such as
// "// %s", or false when fude knows none. It needs buffer.read.
func Comment(pos int) (string, bool) {
	cs, err := answer(hostComment(uint32(pos)))
	return cs, err == nil
}

// Span is a highlight capture over [Start, End), named as the query names
// it, such as comment or string.
type Span struct {
	Start, End int
	Name       string
}

// Spans returns the highlight captures over [start, end) of the text, in
// paint order, or none without a grammar. It needs buffer.read.
func Spans(start, end int) ([]Span, error) {
	s, err := answer(hostSpans(uint32(start), uint32(end)))
	if err != nil {
		return nil, err
	}
	f := strings.Split(s, "\x00")
	var out []Span
	for i := 0; i+2 < len(f); i += 3 {
		start, _ := strconv.Atoi(f[i])
		end, _ := strconv.Atoi(f[i+1])
		out = append(out, Span{start, end, f[i+2]})
	}
	return out, nil
}

// Buffers returns every open buffer, by id. It needs buffer.read.
func Buffers() ([]Buffer, error) {
	s, err := answer(hostBuffers())
	if err != nil {
		return nil, err
	}
	var out []Buffer
	f := strings.Split(s, "\x00")
	for i := 0; i+2 < len(f); i += 3 {
		id, _ := strconv.Atoi(f[i])
		out = append(out, Buffer{id, f[i+1], f[i+2]})
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
func Mode() string { return about("mode") }

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
	p, n := ptr(text)
	return fail(int64(hostEdit(uint32(start), uint32(end), p, n)))
}

// Emit asks fude to emit the event plugin.name about the text the call
// read.
func Emit(name string) error {
	p, n := ptr(name)
	return fail(int64(hostEmit(p, n)))
}

// Decorate puts a style, such as keyword, on [start, end), and returns its
// id for Undecorate. It needs ui.
func Decorate(start, end int, style string) (int, error) {
	p, n := ptr(style)
	id := hostDecorate(uint32(start), uint32(end), p, n)
	return int(id), fail(int64(id))
}

// Sign shows text in the sign column on the line that holds pos, in the
// colour of style, such as diagnostic.warn. The column shows its first two
// characters. It returns an id for Undecorate, and moves with edits. A
// diagnostic's sign wins on its line. It needs ui.
func Sign(pos int, text, style string) (int, error) { return sign(pos, text, style, 0) }

// SignUnder is Sign for a sign any other sign on the line shows over, as
// git's signs are.
func SignUnder(pos int, text, style string) (int, error) { return sign(pos, text, style, 1) }

func sign(pos int, text, style string, under uint32) (int, error) {
	p, n := ptr(text + "\x00" + style)
	id := hostSign(uint32(pos), p, n, under)
	return int(id), fail(int64(id))
}

// Note shows text dim after the end of the line that holds pos. It returns
// an id for Undecorate, and moves with edits. It needs ui.
func Note(pos int, text string) (int, error) {
	p, n := ptr(text)
	id := hostNote(uint32(pos), p, n)
	return int(id), fail(int64(id))
}

// Pick shows items in a list, after prompt, once the call returns. The
// user types to narrow it and picks one with enter. Then f runs, as an
// event does, with the index of the item picked, or false when they
// cancelled or no item matched. A list asked for while one shows is
// cancelled. It needs ui.
func Pick(prompt string, items []string, f func(i int, ok bool)) error {
	p, n := ptr(strings.Join(append([]string{prompt}, items...), "\x00"))
	return schedule(func() {
		i, _ := strconv.Atoi(about("picked"))
		f(i, about("ok") == "true" && i >= 0)
	}, func(id uint32) int32 { return hostPick(p, n, id) })
}

// Ask asks the user prompt once the call returns, and runs f, as an event
// does, with what they typed, or false when they cancelled. It needs ui.
func Ask(prompt string, f func(text string, ok bool)) error {
	p, n := ptr(prompt)
	return schedule(func() { f(about("text"), about("ok") == "true") }, func(id uint32) int32 { return hostPick(p, n, id) })
}

// Jump moves the cursor to pos once the call returns, after its edits, when
// the current window shows the buffer the call was about. It needs ui.
func Jump(pos int) error {
	return fail(int64(hostJump(uint32(pos))))
}

// Open opens the file at path, relative to the project, in the current
// window once the call returns, with the cursor at pos. It refuses a path
// that leads outside the project. It needs ui.
func Open(path string, pos int) error {
	p, n := ptr(path)
	return fail(int64(hostOpen(uint32(pos), p, n)))
}

// Show shows text in a read-only window titled title, coloured as
// filetype, "" for none. A window of that title already open shows text in
// place of what it showed. It needs ui.
func Show(title, filetype, text string) error { return ShowStyled(title, filetype, text, nil) }

// ShowStyled is Show with spans styling the text, such as diff.plus on a
// line, over what the filetype colours.
func ShowStyled(title, filetype, text string, spans []Span) error {
	p, n := ptr(title + "\x00" + filetype + "\x00" + text)
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(strconv.Itoa(s.Start) + "\x00" + strconv.Itoa(s.End) + "\x00" + s.Name + "\x00")
	}
	sp, sn := ptr(b.String())
	return fail(int64(hostShow(p, n, sp, sn)))
}

// Undecorate removes the decoration id. It needs ui.
func Undecorate(id int) error {
	return fail(int64(hostUndecorate(uint32(id))))
}

// Status shows text on the statusline of the buffer the call read, or none
// for "". It needs ui.
func Status(text string) error {
	p, n := ptr(text)
	return fail(int64(hostStatus(p, n)))
}

// Message shows text in the message area once the call returns. It needs
// ui.
func Message(text string) error {
	p, n := ptr(text)
	return fail(int64(hostMessage(p, n)))
}

// Float shows markdown in a float at the cursor, or closes it for "". It
// needs ui.
func Float(markdown string) error {
	p, n := ptr(markdown)
	return fail(int64(hostFloat(p, n)))
}

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
