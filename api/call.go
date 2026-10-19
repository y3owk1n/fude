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
	// Icon shows Sign in Style in a listing's icon column, left of Start's
	// line, while icons is on. It does not show in the sign column (B337).
	Icon bool
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
	Servers    []ServerAction // what to do to language servers, which only the host may ask (B317)
	Keys       string         // keys to run once the call is done, as typed, such as f to run what a panel maps it to (B320)
	Restore    string         // a session Scope.Session gave, whose files open and whose windows build again (B325)
	Confirm    *Confirm       // a question a Write's call asks before it writes (B327)
	Moved      [][2]string    // paths renamed, from and to, whose open buffers follow them (B327)
	Travel     *Travel        // a move of a buffer's text to one of its states (B328)
	Completion *Completion    // what a Completer offers (B336)
	Copy       string         // text to put on the clipboard (B329)
	Window     string         // h, j, k or l: the window next to the current one that way becomes current (B330)
	Tabs       *[]Tab         // what the buffer line lists, none for no row (B331)
	Follow     string         // the session whose windows the call's session follows, by name (B256, B332)
	Build      []string       // the filetypes whose grammars build again, which only the host may ask (B165, B333)
	Kick       string         // the guest whose session ends, by name, which only the host may ask (B250, B332)
	// Close closes the buffers by id, as :bd does, and keeps a modified
	// one open. Write writes the buffers by id, as :w does. Windows does
	// each action to its window, and Scratch opens a buffer with no file
	// to edit. fude runs them after the call's edits, in that order (B343).
	Close   []int
	Drop    []int // the buffers to close by id, their changes dropped, as :bd! does (B89)
	Write   []int
	Windows []WindowAction
	Scratch *Scratch
	// Marks sets each mark by its letter at its place, Registers puts the
	// text in each register, and Search makes a pattern the last search,
	// which n finds. Ex runs each command line, without its colon, as
	// typed. fude runs them after Scratch, in that order (B344).
	Marks     []Mark
	Registers []RegisterText
	Search    *Search
	Ex        []string
	Each      *Each
	// Styles defines styles by name, which a decoration's or a span's Style
	// names, over the theme's own and those defined before (B345).
	Styles map[string]Style
	// Set holds options to set, by their config.toml names. expand_tab,
	// indent_width and tab_width are the buffer's the call is about, which
	// fude uses in place of config.toml's until the buffer closes (B283).
	// fude sets any other option as :set does (B344).
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
	Emit   func(line string) `json:"-"` // what fude gives a Stream job before Work
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
	// Update replaces the items of the owner's open pick of this ID and
	// keeps its query. It shows nothing once that pick is closed. A list
	// read off the loop fills the finder this way. <C-g> in a pick that is
	// not Live tells its callback all, true or false, and the owner sends
	// the items again (B321).
	Update bool
}

// Terminal is a program a call runs through the shell in a terminal window
// below, as :terminal does, while the cursor stays where it was. The
// window's title is the owner's name and Name, and the status line names it
// while it runs. If a Terminal of that Name still runs, its window shows
// again instead. The window of one that ended closes first. When the
// program ends, the owner hears terminal.exited with Name. The event is
// about the terminal's buffer, with status and took in About (B228, B231).
// With no Name it is a terminal of its own, as :terminal opens: a new one
// each time, which takes the focus to type into and the status line does
// not name. With no Command it runs the shell (B335).
type Terminal struct {
	Name, Command string
}

// Places is the last list of places, so ]q goes to the first, in a window
// of its own with Show. Each item is path:line:column:text, or
// line:column:text in File when it is set, or a path alone for its start.
// An item with no text shows its line. Columns count in Encoding, utf-8 or
// utf-16, utf-8 by default. The editor keeps it and emits places.set,
// with show when it asks for a window, and modules/places shows and steps
// through it (B230, B292, B347).
type Places struct {
	Title    string
	Items    []string
	Lines    []string // what each item's line shows, in place of its place and text (B329)
	Show     bool
	File     string
	Encoding string
	At       int // the item last gone to, from 1, or 0 before the first (B341)
}

// Each runs Ex, a command line without its colon, at each of Items, each
// path:line:column in Encoding, or with PerFile once in each file, as :cdo
// and :cfdo do. Marks keep the places on their text as Ex edits around
// them (B257, B347).
type Each struct {
	Items        []string
	Encoding, Ex string
	PerFile      bool
}

// Float is text a call shows at the cursor. Without Spans it is markdown,
// rendered. With them it shows as it is, styled by them (B292).
type Float struct {
	Text  string
	Spans []Span
	// Keys makes it the key hints of the command being typed. It shows
	// across the bottom of the windows, titled with the keys typed, and
	// keeps the windows lit. While it shows, the command waits for its next key
	// instead of key_timeout, <Esc> drops it and <BS> its last key. It
	// closes once no command is being typed, and an empty Text closes it
	// sooner (B172, B326).
	Keys bool
}

// Hint is a key that may follow the command being typed, as the key hints
// show it (B326).
type Hint struct {
	Key   string // the key that follows what was typed
	Help  string // what it does, or the keys a mapping types when Typed
	Typed bool
	Group string // the name of the keys a group starts, [keymap.groups]'s or fude's
	More  int    // how many keys start with it, or 0 for one that runs
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
	Buffer       int    // the buffer to show by id, in place of Path, its cursor where it was unless Line is set (B343)
	Split        string // below or right opens Path in a new window there (B327)
	Pos          int
	Line, Column int
	Encoding     string // what Column counts in, utf-8 or utf-16, utf-8 by default
}

// WindowAction does Do to the window by its ID, as Scope.Windows lists it,
// or to the current one for 0. close closes it, only closes every other,
// focus makes it current, height and width make it Size rows or columns,
// or as many as it can take for 0, taller and wider make it Size more, or
// fewer for a negative Size, cursor moves its cursor to Pos and top makes
// the line holding Pos its top line (B343).
type WindowAction struct {
	ID        int
	Do        string
	Size, Pos int
}

// Style is a style a module defines: its text's colour and its ground's,
// as #rrggbb or "" to keep the theme's, and how its text draws (B345).
type Style struct {
	Fg, Bg                          string
	Bold, Italic, Underline, Strike bool
}

// Search is a search's pattern, and whether n finds it forward (B344).
type Search struct {
	Pattern string
	Forward bool
}

// Scratch is a new buffer with no file, holding Text and coloured as
// Filetype, for the user to edit. It shows in the current window, or in a
// new one Split below or right. When a module's Write writes its Kind, :w
// writes it through that module (B327, B343).
type Scratch struct {
	Text, Filetype, Split, Kind string
}

// Panel is text a call shows in a read-only window titled Title, coloured
// as Filetype. It replaces the text of a window open under that title,
// which keeps the cursor's line and the focus where they are (B186).
type Panel struct {
	// Kind names its kind of buffer, such as list, whose [keymap.<kind>]
	// keys go over Keys (B347).
	Kind                  string
	Title, Filetype, Text string
	Spans                 []Span            // styles over Text, such as diff.plus on a line (B276)
	Keys                  map[string]string // its own keys in normal mode, such as <CR> to a plug of the owner's (B291)
	// Line, from 1, is the line that shows at the top of the panel's window
	// each time it shows, with the cursor on it and the focus there, as
	// help's entry does (B334).
	Line int
	// Float shows it in a box of its own in the middle of the screen, which
	// takes the focus and names it by Title without its brackets (B318).
	Float bool
	// Screen shows it in the current window in place of the buffer the call
	// is about, while the window still shows that buffer. The window shows
	// no line numbers, guides or wrap until it shows another buffer, as
	// the start screen does (B320).
	Screen bool
	// Cursor is where the cursor goes when the panel opens.
	Cursor int
	// Update only replaces the text of the panel open under Title, and
	// opens none when it is closed, as an event's result does (B318).
	Update bool
	// Cards are the lines of the first rows of the parts of a Float panel.
	// The cursor stays on a card's first row, which shows raised, and the
	// box scrolls to show all of the card. Such a panel takes its own keys
	// and : alone (B318).
	Cards []int
	// Foot shows under a rule at the foot of a Float panel, styled by
	// FootSpans, and stays there as the text scrolls.
	Foot      string
	FootSpans []Span
}

// Binding is a key fude knows, as :pick keymaps lists it. Kind is a mode's
// mapping, such as normal mapping, the mode a key works in, or motion,
// operator or object (B324).
type Binding struct {
	Keys, Help, Kind string
}

// ExCommand is an ex command, as :pick commands lists it. Args are the
// arguments it takes, in brackets when it may go without (B324).
type ExCommand struct {
	Name, Args, Help string
}

// Grammar is a grammar fude builds, by its filetype, as :grammar lists it
// (B333).
type Grammar struct {
	Filetype, State string
}

// Peer is a session on this fude, as :who lists it. Power is host, guest
// or read-only, and You is the session the call is about (B332).
type Peer struct {
	Name, Power string
	You         bool
}

// Window is a window of the session, as Scope.Windows lists it: its id,
// the buffer it shows, its cursor and the start of its top line in that
// buffer, and where it is on the screen, its status line included (B340).
type Window struct {
	ID, Buffer    int
	Alt           int // the buffer it showed before, which # names, or 0
	Cursor, Top   int
	Row, Column   int
	Width, Height int
	Current       bool
}

// Selection is what visual mode selects: its Kind, v, V or <C-v>, the
// bytes from Start to End, or every line a block spans, a block's display
// columns from Left to Right, and each region :select chose when it chose
// several (B340).
type Selection struct {
	Kind        string
	Start, End  int
	Block       bool
	Left, Right int
	Regions     [][2]int
}

// Tab is a buffer the buffer line lists, by its id, with the name it shows.
// The line marks a modified one itself (B258, B331).
type Tab struct {
	Buffer int
	Name   string
}

// Completion is what a Completer offers for the text before the cursor:
// items that replace the text from Start. Only makes them the menu's
// alone, as a path's entries are. With Later they come in a callback, and
// the menu waits for them (B336).
type Completion struct {
	Start       int
	Items       []CompletionItem
	Only, Later bool
}

// CompletionItem is an item of a completion menu. With Snippet, Text is a
// snippet's body, which the editor expands as it goes in (B207, B336).
type CompletionItem struct {
	Label, Text, Doc string
	Snippet          bool
}

// RegisterText is what the register Name holds. Linewise text is whole
// lines, and Block text is one line for each line of a block (B341).
type RegisterText struct {
	Name, Text      string
	Linewise, Block bool
}

// Mark is a place in the buffer by its id, as :marks, :jumps and :changes
// list them. Name is a mark's letter (B329).
type Mark struct {
	Name        string
	Buffer, Pos int
}

// State is a state of a buffer's text, as :timeline lists it. Parent is
// the state it follows, by its place in States (B328).
type State struct {
	Time    time.Time // zero for the text before any change
	Label   string    // the command that made it
	Parent  int
	Current bool // the text is at it
	Saved   bool // a write wrote it
	Unsaved bool // the newest change from a session that did not close, which :recover brings back (B34)
}

// Travel moves the text of the buffer by its id, or of the one the call is
// about for 0, to the state To, by its place in States. Each window on the
// buffer moves its cursor to where the state's last change starts. New
// makes that state's text the newest state, as a new state on top of the
// newest, so no state is lost (B31, B328).
type Travel struct {
	Buffer, To int
	New        bool
}

// Confirm is a yes or no question a Write's call asks. Paths are the
// files and directories the write will change, which a guest may change
// only under the project (B245, B327).
type Confirm struct {
	Question string
	Paths    []string
}

// Entry is a name a listing showed when it was read, and where its line's
// text is now. Text inserted at Start goes before the entry, and at End
// after it, so lines added around it stay outside. An entry whose ends
// met was deleted or changed (B327).
type Entry struct {
	Name       string
	Start, End int
}

// Server is a language server config.toml sets, as :lsp shows it (B317).
type Server struct {
	Name string
	// State is running, starting, stopped, disabled, not installed, failed,
	// or no files when none of the files it serves is open.
	State     string
	Why       string // why a failed server is down
	Version   string
	Filetypes []string
	Roots     []Root // where it runs
}

// Root is a project root a language server runs in, with the paths of the
// files open in it.
type Root struct {
	Path  string
	Files []string
}

// ServerAction asks to restart, stop, disable or enable the language
// server Name, as Do says. A disabled server stays off for every file
// until enabled (B317).
type ServerAction struct {
	Name, Do string
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
	// Object returns the range one of the vim machine's own text objects,
	// such as iw, a( or 2i", selects at pos, or false when nothing matches
	// (B322).
	Object func(keys string, pos int) (start, end int, ok bool)
	// Keys returns every key fude knows: the mappings of normal, visual and
	// insert mode, each with what it runs, then the vim machine's keys,
	// fude's and the modules', each with its help and kind (B324).
	Keys func() []Binding
	// Commands returns every ex command, the modules' too, and :plug of each
	// <Plug>, with the arguments each takes and its help (B324).
	Commands func() []ExCommand
	// HelpTags returns what :h opens help at (B324).
	HelpTags func() []string
	// States returns the states of the text of the buffer by its id, the
	// text before any change first, then each in the order it was made.
	// StateText returns the text of one of them, by its place in States.
	// Only a call has them (B328).
	States    func(buffer int) []State
	StateText func(buffer, state int) (string, bool)
	// Marks returns the current buffer's marks, Jumps the current window's
	// jumps, oldest first, and Changes where the current buffer changed,
	// oldest first. Messages returns the messages shown, oldest first, and
	// Register a register's text, or false when it is empty. Only a call has
	// them (B329).
	Marks, Jumps, Changes func() []Mark
	Messages              func() []string
	Register              func(name rune) (string, bool, error)
	// Registers returns what every register holds but the clipboard's, which
	// Register reads. Search returns the last search's pattern, "" before
	// any, and whether it went forward. Recording returns the register a
	// macro is recorded into, or "". History returns the lines entered with
	// : or /, oldest first. Places returns the last list of places, which ]q
	// steps through, or false with none, in an event too. Only a call has
	// the others (B341).
	Registers func() []RegisterText
	Search    func() (pattern string, forward bool)
	Recording func() string
	History   func(prompt string) []string
	Places    func() (Places, bool)
	// HelpAt returns help's text and the line, from 0, of the entry topic
	// names, or of the start for "", or false when it names none. Only a
	// call has it (B334).
	HelpAt func(topic string) (text string, line int, ok bool)
	// Grammars returns the grammars fude builds, each with its state: built,
	// missing, outdated, building, or failed with why. Only a call has them
	// (B333).
	Grammars func() []Grammar
	// Sessions returns the sessions on this fude: the host's, each guest's
	// and each agent's. Only a call has them (B332).
	Sessions func() []Peer
	// Window reports whether a window is next to the current one that way,
	// h, j, k or l, on the cursor's row or column (B330).
	Window func(dir string) bool
	// Entries returns the entries of the listing a Write's call writes
	// (B327).
	Entries func() []Entry
	// Hints returns the keys that may follow the command being typed, for a
	// module that hears keys.pending (B326).
	Hints func() []Hint
	// Session returns the windows on files, their cursors, top lines and
	// options, and which is current, for Result.Restore to build again, or
	// "" with no window on a file. Only a call and editor.quitting have it
	// (B325).
	Session func() string
	// Servers returns the language servers config.toml sets, by name (B317).
	Servers func() []Server
	// Windows returns the session's windows, in the order of the layout
	// (B340).
	Windows func() []Window
	// Selection returns what visual mode selects in the current window, or
	// false outside it, and Cursors the other cursors there while a change
	// types into several regions (B340).
	Selection func() (Selection, bool)
	Cursors   func() []int
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
	Name           string // what :buffers calls it, its path or its title (B329)
	Modified       bool
	Kind           string // its kind of buffer, such as listing or help, or "" for one a user edits (B331)
	Barred         bool   // the session may not show it, as a guest may not a file outside the project (B245)
	Text           Text   `json:"-"`
}

// Context is what a call is about, by name. Every call has buffer, path and filetype, of the buffer it is
// about, and mode. It also has cwd, the working directory, and project,
// the directory fude started in. cursor is where the cursor is in that
// buffer, in the current window or else in the first window that shows
// it. A call about a buffer no window shows has no cursor (B340). In a command's call, args is the text typed after its
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
