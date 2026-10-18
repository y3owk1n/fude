package api

import "time"

// Decoration is a style a call puts on [Start, End) of the text, such as a
// highlight name. ID names it to its owner, for Undecorate.
type Decoration struct {
	ID         int
	Start, End int
	Style      string
	Sign, Note string // what shows on Start's line, in the sign column or after its end (B184)
	Fold       bool   // a closed fold: the lines after Start's through End's hide behind the first and a count (B288)
	// Replace shows Note in Style in place of the text from Start to End,
	// which must be on one line, and an empty Note hides it. The cursor's
	// line and a selection's show their text as it is (B295).
	Replace bool
	// Insert shows Note in Style among the text at Start, on the cursor's
	// line too, as an inlay hint does (B216).
	Insert bool
	// Cover shows Note in Style over the text from Start to End, which
	// must be on one line. The note is padded to the text's width and
	// shows on the cursor's line too, as a jump's labels do (B315).
	Cover bool
	// Line shades the whole rows of the lines from Start's to End's in
	// Style's ground, to the window's edge, under the text's colours (B296).
	Line bool
	// Lines draw as rows of their own below Start's line, or above it when
	// Above, in Style. They hold no text, so the cursor passes over them,
	// and a row too long for the window is cut at its edge (B299).
	Lines []string
	Above bool
	// Block makes the lines from Start's to End's one block. While the
	// cursor or a selection is on any of them, they all show their text as
	// it is, without replacements or rows of their own (B300).
	Block bool
	// Image shows the image at the path, a PNG, JPEG or GIF file or an http
	// or https address, in rows of its own below Start's line, as wide as
	// its pixels or the window, in a terminal that shows images. It shows on
	// the cursor's line too (B303, B308).
	Image string
	// Layer orders the signs on one line, and the highest shows. git's signs
	// are at -1, most at 0, diagnostics at 1 and a breakpoint at 2 (B184,
	// B262).
	Layer int
	// Buffer is the buffer it is on, one of Scope.Buffers, or 0 for the
	// buffer the call is about (B291).
	Buffer int
}

// Result is what one call asked fude to do.
type Result struct {
	Edits      []Edit
	Decorate   []Decoration
	Undecorate []int    // the ids of decorations to remove
	Emit       []string // the events to emit, named owner.event
	Status     *string  // the owner's statusline segment for the text it read, "" for none
	// Capture sends the next keys to the owner's Call of the same id, each
	// as key in About, before the vim machine or a menu takes them, until
	// a result without it (B316).
	Capture    bool
	StatusLeft *string        // its segment on the statusline's left, after the buffer's name, as git's branch is (B147)
	Message    string         // a line for the message area, "" for none
	Float      *Float         // text to show at the cursor, empty to close it
	Show       *Panel         // text to show in a window of its own
	Jump       *Jump          // where to move the cursor
	Pick       *Pick          // a list to pick from, or a question to answer
	Menu       *Menu          // a menu at the cursor, numbered, with what each item does beside it
	Timers     []Timer        // the callbacks to run later
	Jobs       []Job          // the work to do in the background
	Requests   []Request      // the requests to send the buffer's language servers
	Selected   *[2]int        // what an object's call selects, nil for none
	Terminal   *Terminal      // a program to run in a terminal window below
	Places     *Places        // the last list of places, which ]q steps through
	Complete   []string       // what the argument being typed completes with
	Log        []string       // lines for fude's log, where a plugin's own go (B50)
	Apply      *WorkspaceEdit // a language server's edit of any files, applied with its version checks
	Cmdline    string         // a command line to start typing, without its colon, such as rename name
	// Set holds options to set on the buffer the call is about, by their
	// config.toml names: expand_tab, indent_width and tab_width. fude uses
	// them in place of config.toml's until the buffer closes (B283).
	Set map[string]string
}

// Job is a program to run or a URL to get in the background. Once it ends,
// fude runs the owner's callback with its id, with the output and any
// error in its context (B181).
//
// A Stream job is the owner's own work, which Work does until it ends or
// ctx does. Each line Work hands Emit runs the callback with the line as
// output, about the buffer current then, and Work's return runs it once
// more with done true (B291).
type Job struct {
	ID     uint32
	Argv   []string
	Input  string // what the program reads on its stdin
	URL    string
	Stream bool
	Emit   func(line string) // what fude gives a Stream job before Work
}

// Request is a request a call sends the language servers of the buffer it
// was about, or to the one named Server. For a textDocument method fude
// fills in its textDocument, and with Pos and End the range between them,
// with Pos alone the position there, and with neither no position. Params,
// a JSON object, adds to them. The owner's callback with ID gets the first
// answer that is not null as output, and the server's position encoding as
// encoding (B190, B281, B293).
type Request struct {
	ID       uint32
	Method   string
	Pos, End int
	Params   string
	Server   string
}

// Pick is a list of items a call asks the user to pick from with Prompt,
// or with none a question whose answer is what they type. fude runs the
// owner's callback with ID once they answer or cancel (B185).
//
// Title names the list in its border, Hint shows before a query is typed,
// and Query is typed at the start. With Live, each change to the query
// runs the callback with the query as query and the finder's all flag as
// all. A Pick of the same ID in the answer gives the list's items for
// that query. With Places, an item is path:line:column:text, shown under its
// file with its line previewed, and picking one opens the place (B285,
// B286).
type Pick struct {
	ID           uint32
	Prompt       string
	Items        []string
	Title, Hint  string
	Query        string
	Live, Places bool
	// Files makes the items file paths, which show, preview and open as
	// :pick files's do. The owner still hears which was picked (B314).
	Files    bool
	Encoding string // what the places' columns count in, utf-8 by default
}

// Terminal is a program a call runs through the shell in a terminal window
// below, as :terminal does, while the cursor stays where it was. The
// window's title is the owner's name and Name, and the status line names it
// while it runs. If a Terminal of that Name still runs, its window shows
// again instead. The window of one that ended closes first. When the
// program ends, the owner hears terminal.exited with Name. The event is
// about the terminal's buffer, with status and took in About (B228, B231).
type Terminal struct {
	Name, Command string
}

// Places is the last list of places, so ]q goes to the first, in a window
// of its own with Show. Each item is path:line:column:text, or
// line:column:text in File when it is set. An item with no text shows its
// line. Columns count in Encoding, utf-8 or utf-16, utf-8 by default
// (B230, B292).
type Places struct {
	Title    string
	Items    []string
	Show     bool
	File     string
	Encoding string
}

// Float is text a call shows at the cursor. Without Spans it is markdown,
// rendered. With them it shows as it is, styled by them (B292).
type Float struct {
	Text  string
	Spans []Span
}

// WorkspaceEdit is a language server's edit of any files, as the server
// named Server sent it, which fude applies as one change of each file
// under Label, when each file is at the version the edit names (B292).
type WorkspaceEdit struct {
	Server string
	Edit   string
	Label  string
}

// Menu is a numbered menu at the cursor, as code actions show: each item's
// title, its kind dim after it, why it is disabled, and Doc, what it
// does, shown beside the menu while it is selected. A Menu of the same ID
// while one shows replaces its items and keeps the selection. fude runs
// the owner's callback with ID once an item is picked, with picked and
// ok as a Pick does (B227, B293).
type Menu struct {
	ID    uint32
	Items []MenuItem
}

// MenuItem is one row of a Menu.
type MenuItem struct {
	Title, Kind, Disabled string
	Doc                   *Float
}

// Jump is where a call moves the cursor: Pos in the buffer the call was
// about, or in the file at Path when it is not "", which fude opens in a
// window that shows a file. With Line set, the cursor goes to Line and
// Column, counted from 1, instead of Pos (B187, B291).
type Jump struct {
	Path         string
	Pos          int
	Line, Column int
	Encoding     string // what Column counts in, utf-8 or utf-16, utf-8 by default
}

// Panel is text a call shows in a read-only window titled Title, coloured
// as Filetype. It replaces the text of a window open under that title,
// which keeps the cursor's line and the focus where they are (B186).
type Panel struct {
	Title, Filetype, Text string
	Spans                 []Span            // styles over Text, such as diff.plus on a line (B276)
	Keys                  map[string]string // its own keys in normal mode, such as <CR> to a plug of the owner's (B291)
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
	// Query runs one tree-sitter query for each language, by filetype in
	// queries. The buffer's language's query runs on the buffer's tree, and
	// another language's on the trees of the regions injected in it. It
	// returns the captures over Text in order of their starts, the longer
	// first, or none without a grammar. A query that does not compile is an
	// error (B297).
	Query func(queries map[string]string) ([]Span, error)
	// Comment returns how the language at pos comments a line, such as
	// "// %s", or false when fude knows none (B275).
	Comment func(pos int) (string, bool)
	// Option returns the option or config.toml setting called name there, as
	// text, such as spell or spell_words, or false for none (B274).
	Option func(name string) (string, bool)
	// Config returns the value at a dotted path of config.toml, such as
	// filetype.go.format or formatter.gofmt.command, as JSON, or false for
	// none. A table's key follows its name in the path (B279).
	Config func(path string) (string, bool)
	// Decorations returns the owner's decorations on every open buffer, by
	// Buffer, where they were when the call began (B289, B291).
	Decorations func() []Decoration
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
// given a range. A buffer with a title, such as a panel, has title.
type Context map[string]string

// Done frees what s holds. fude calls it once the call that read s
// returns, so a module never does.
func (s Scope) Done() {
	if s.Free != nil {
		s.Free()
	}
}
