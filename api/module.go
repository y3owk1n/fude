package api

import (
	"context"
	"errors"
)

// Module is what fude runs calls into: a module built into a distribution
// or a plugin (D21). Calls into one take turns.
type Module interface {
	// Name is what its keys, events and messages are named after.
	Name() string
	// Registered is what its init registered, and Subscribed the events it
	// hears.
	Registered() []Registration
	Subscribed() []string
	// Init runs before any other call, and may register and subscribe.
	Init(ctx context.Context, s Scope) (Result, error)
	// Call runs what it registered as id, on the input path, with the
	// arguments its kind takes. It may wait briefly for a call that runs,
	// as a plugin's waits at most half its budget, and returns ErrBusy
	// instead of waiting longer.
	Call(ctx context.Context, id uint32, s Scope, args ...int) (Result, error)
	// Event tells it of an event it subscribed to, with the event's detail
	// after its name.
	Event(ctx context.Context, name string, s Scope) (Result, error)
	// Callback runs the callback a timer, job, request or pick asked for.
	Callback(ctx context.Context, id uint32, s Scope) (Result, error)
	// Work does a job a result asked for, off the editor loop, until it
	// ends or ctx does.
	Work(ctx context.Context, j Job) (string, error)
	Close(ctx context.Context) error
}

// ErrBusy is what Call returns while another call runs, such as an event
// handler, that it cannot wait for. The input path never waits long.
var ErrBusy = errors.New("is busy")

// Kind is what a module registers.
type Kind int32

// What a module registers during init, and the arguments a call to each
// gets.
const (
	Command  Kind = iota // an ex command, called with (cursor, count)
	Operator             // called with (start, end, linewise), linewise 1 or 0
	Object               // called with (start, end), and sets Result.Selected
	Key                  // keys in normal mode, called with (cursor, count)
	Insert               // keys in insert mode, called with (cursor)
	Plug                 // a command keys reach only as <Plug>(owner.name), called with (cursor, count)
	Source               // a source of :pick, called with (cursor, count) and the text after its name as args (B287)
	// Typed is a key typed in insert mode, which the vim machine reads as
	// its own, called with (cursor) before the key does its own work. The
	// call's edits go in as typed text does, with the cursor after the first
	// one's text, and none lets the key do what it does. A finder's query
	// never calls it (B323).
	Typed
	// Completer is a source of insert mode completion by its Name, which
	// completion_sources orders, called with (cursor). Its result's
	// Completion has what it offers. Autocomplete asks it after a word
	// character, and after each character of its Keys (B336). keys in
	// About are the keys that asked, such as <C-x><C-o>, empty for
	// autocomplete. <C-n>, <C-p>, <C-x><C-n>, <C-x><C-p> and <C-x><C-l>
	// ask the source named buffer alone, and <C-x><C-f> the one named
	// path (B440).
	Completer
	// Write writes the buffers of the kind Name, such as explorer, in place
	// of a file, as :w does. It is called with (cursor), and its scope has
	// Entries. Its result's Confirm asks first, and the call comes again with
	// confirmed in About once the answer is yes. A message from that call
	// says the write failed, and what was to run after it, such as :wq's
	// quit, does not. An explorer reads its directory again after (B327).
	Write
	// Page is a page of the guide, which help lists after fude's own and
	// fude man guide prints. Name is its tag after guide-, and Doc is its
	// markdown, starting with a # title. It is never called (B376).
	Page
	// Option is a value :set changes as owner.name, which starts as Default,
	// or as config.toml gives it: a plugin's in its options table, and a
	// module's in [module.owner]. A Default of true or false makes it a
	// switch, and one of digits a number. Window makes it each window's, as
	// fude's options are. It is never called (B377).
	Option
	// ClientCommand is a command a language server's code action, code lens
	// or completion names for the editor to run, by Name, such as
	// editor.action.showReferences. fude calls it with (cursor), the
	// command's JSON arguments in About as arguments and the server as
	// server, in place of sending the command to the server (B539).
	ClientCommand
)

// Registration is one thing a module's init registered: its name, what it
// does, and the id Call runs it by.
type Registration struct {
	Kind       Kind
	Name, Help string
	// Doc is a longer explanation in markdown, which help shows under the
	// entry of a Command, Key, Operator, Object or Plug, unless fude's own
	// reference explains it (B375). A Page's Doc is the page.
	Doc  string
	ID   uint32
	Host bool // only the host's session may run it, as gh is (B244)
	// Complete makes Call complete a command's argument too. The word typed
	// is complete in About, and a Result's Complete answers (B290).
	Complete bool
	// Visual maps a Key, or a Plug's Keys, in visual mode too, where its
	// call gets the selection's start and end instead of the cursor and
	// count (B291).
	Visual bool
	// Attached maps a Key, or a Plug's Keys, only while a language server
	// is attached to the buffer, so the key is free without one (N3).
	Attached bool
	// Bound maps a Key, or a Plug's Keys, only while the current window
	// scrolls with others, as windows in diff mode do (B523).
	Bound bool
	// Filetype maps a Key, or a Plug's Keys, only in buffers of that
	// filetype, over a module's key that names none. When its call asks for
	// nothing, the key does what it does without this mapping (B588, B589).
	Filetype string
	// Enters makes a Key or a Plug run while a float shows move the cursor
	// into the float instead, as K does on hover (B62).
	Enters bool
	// Chars makes an Operator wait for that many characters typed after its
	// motion, and a Key for that many after its keys. The call gets them as
	// chars in About, and dot repeats it with them. A Key with Chars is read by
	// the vim machine as its own keys are. In normal mode its Name is an
	// operator's key and one more, as ds is, so the operator does not wait
	// for a mapping. With Visual it is a key of visual mode alone, as S is,
	// and its call gets the selection (B322).
	Chars int
	// Keys are the keys a Plug is mapped to, in normal mode, or with In
	// among the own keys of that kind of buffer, such as explorer. A
	// config.toml mapping of them in that table wins (B327, D53).
	Keys, In string
	// Short are other names a Command answers to, such as ls for buffers
	// (B329).
	Short []string
	// Paths makes a Command's argument a path, as :e's is. It completes as
	// one instead of in Call, and the call gets it with %, # and ~
	// expanded, which a guest may not name outside the project (B204,
	// B245, B290).
	Paths bool
	// Args are the arguments a Command takes, as :h and :pick commands show
	// them, in brackets when it may go without, such as [file] (B324).
	Args string
	// Default and Window are an Option's (B377).
	Default string
	Window  bool
}
