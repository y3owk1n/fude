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
// mode for mode.changed, and "" for the others.
type Event struct {
	Name, Detail string
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

//go:wasmimport fude setting
func hostSetting(ptr *byte, n uint32) int64

//go:wasmimport fude text_len
func hostTextLen() int32

//go:wasmimport fude lines
func hostLines() int32

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

//go:wasmimport fude undecorate
func hostUndecorate(id uint32) int32

//go:wasmimport fude status
func hostStatus(ptr *byte, n uint32) int32

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

// listeners are the functions On added, by event.
var listeners = map[string][]func(Event){}

//go:wasmexport on_event
func onEvent(p *byte, n uint32) {
	name, detail, _ := strings.Cut(string(unsafe.Slice(p, n)), " ")
	for _, f := range listeners[name] {
		f(Event{name, detail})
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
