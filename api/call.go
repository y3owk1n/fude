package api

import "time"

// Decoration is a style a call puts on [Start, End) of the text, such as a
// highlight name. ID names it to its owner, for Undecorate.
type Decoration struct {
	ID         int
	Start, End int
	Style      string
	Sign, Note string // what shows on Start's line, in the sign column or after its end (B184)
	Under      bool   // any other sign on the line shows over Sign (B184)
}

// Result is what one call asked fude to do.
type Result struct {
	Edits      []Edit
	Decorate   []Decoration
	Undecorate []int     // the ids of decorations to remove
	Emit       []string  // the events to emit, named owner.event
	Status     *string   // the owner's statusline segment for the text it read, "" for none
	Message    string    // a line for the message area, "" for none
	Float      *string   // markdown to show at the cursor, "" to close it
	Show       *Panel    // text to show in a window of its own
	Jump       *Jump     // where to move the cursor
	Pick       *Pick     // a list to pick from, or a question to answer
	Timers     []Timer   // the callbacks to run later
	Jobs       []Job     // the work to do in the background
	Requests   []Request // the requests to send the buffer's language servers
	Selected   *[2]int   // what an object's call selects, nil for none
}

// Job is a program to run or a URL to get in the background. Once it ends,
// fude runs the owner's callback with its id, with the output and any
// error in its context (B181).
type Job struct {
	ID    uint32
	Argv  []string
	Input string // what the program reads on its stdin
	URL   string
}

// Request is a request a call sends the language servers of the buffer it
// was about. fude fills in its textDocument, and its position at
// Pos unless Pos is negative, and adds Params, a JSON object. The owner's
// callback with ID gets the first answer that is not null (B190).
type Request struct {
	ID     uint32
	Method string
	Pos    int
	Params string
}

// Pick is a list of items a call asks the user to pick from with Prompt,
// or with none a question whose answer is what they type. fude runs the
// owner's callback with ID once they answer or cancel (B185).
type Pick struct {
	ID     uint32
	Prompt string
	Items  []string
}

// Jump is where a call moves the cursor: Pos in the buffer the call was
// about, or in the file at Path when it is not "", which fude opens
// (B187).
type Jump struct {
	Path string
	Pos  int
}

// Panel is text a call shows in a read-only window titled Title, coloured
// as Filetype. It replaces the text of a window open under that title
// (B186).
type Panel struct {
	Title, Filetype, Text string
}

// Timer is a callback a call asks fude to run after a while, by the id it
// gave (B180).
type Timer struct {
	ID    uint32
	After time.Duration
}

// Scope is what one call reads: the text of the buffer it is
// about, what it is about, and every open buffer, by id (B188).
type Scope struct {
	Text    Text
	About   Context
	Buffers map[int]Buffer
	// Language returns the filetype of the text at pos, the buffer's or that
	// of an injected region holding it, such as a code block (B273).
	Language func(pos int) string
	// Spans returns the highlight captures over [start, end) of Text, in
	// paint order, or none without a grammar (B273).
	Spans func(start, end int) []Span
	// Comment returns how the language at pos comments a line, such as
	// "// %s", or false when fude knows none (B275).
	Comment func(pos int) (string, bool)
	// Option returns the option or config.toml setting called name there, as
	// text, such as spell or spell_words, or false for none (B274).
	Option func(name string) (string, bool)
	// Node finds a syntax node in Text, as syntax.Buffer.Nodes does. Free
	// frees what it reads once the call is done (B189).
	Node func(pos, up int) (kind string, start, end int, ok bool)
	Free func()
}

// Span is a highlight capture over [Start, End), named as the query names
// it, such as comment or string.
type Span struct {
	Start, End int
	Name       string
}

// Buffer is an open buffer as a call reads it.
type Buffer struct {
	Path, Filetype string
	Text           Text
}

// Context is what a call is about, by name. Every call has buffer, path and filetype, of the buffer it is
// about, and mode. In a command's call, args is the text typed after its
// name, and bang is true when ! followed the name. first and last are where
// its range's first and last lines start, and ranged is true when it was
// given a range.
type Context map[string]string

// Done frees what s holds. fude calls it once the call that read s
// returns, so a module never does.
func (s Scope) Done() {
	if s.Free != nil {
		s.Free()
	}
}
