package wire

import (
	"image/color"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// Frame is what an attached session's client draws. The server sends one
// whenever it changes (D39). Its views leave out their text, which Texts
// brings up to date.
type Frame struct {
	Views        []View
	Buffers      []Tab          `json:",omitempty"` // what the buffer line shows (B258)
	Texts        []Text         `json:",omitempty"`
	Float        *Float         `json:",omitempty"`
	Signature    *Float         `json:",omitempty"`
	Line         string         `json:",omitempty"`
	LineCursor   int            `json:",omitempty"`
	Mode         vim.Mode       `json:",omitempty"`
	Message      string         `json:",omitempty"`
	Selection    *vim.Selection `json:",omitempty"`
	Cursors      []int          `json:",omitempty"`
	Recording    rune           `json:",omitempty"`
	Substituting bool           `json:",omitempty"`
	Terminal     bool           `json:",omitempty"`
	Shown        string         `json:",omitempty"`
	Theme        string
	Base16       *[16]int32     `json:",omitempty"`
	Colors       *Colors        `json:",omitempty"`
	Images       map[int][]byte `json:",omitempty"` // by id, the PNG of each image a view shows that the client lacks (B303)
	Freed        []int          `json:",omitempty"` // the images dropped since the last frame, for the client to free (B306)
	Clipboard    *string        `json:",omitempty"` // to put on the client's clipboard (B241)
	Wait         *time.Duration `json:",omitempty"` // until a key typed now times out
	Hints        *time.Duration `json:",omitempty"` // until the key hints show
	Done         bool           `json:",omitempty"` // the session ended, as Message says when fude quit
	Input        int            // the client's notices fude has handled, so a frame follows each
}

// Text is a buffer's text as a frame brings it: the edits since the version
// the client has, or else the whole text.
type Text struct {
	Buffer  int
	Version kernel.Version
	Edits   [][]kernel.Edit `json:",omitempty"`
	Whole   *string         `json:",omitempty"`
}

// View is one window as a client draws it.
type View struct {
	Buffer            int             // the buffer's number, which a frame's Texts name
	Text              kernel.Snapshot `json:"-"`
	Top, Cursor       int
	X, Y, W, H        int // H counts the status line on the bottom row
	Name              string
	Modified, Current bool
	Highlights        []syntax.Span // in paint order, later ones drawing over earlier ones
	Decorations       []syntax.Span // plugins' styles, drawn over highlights
	Ghosts            []Ghost
	Matches           [][2]int          // of the search, shown in every window
	Searched          [2]int            // the match the search being typed goes to, lit apart from the rest (B21)
	Segments          []string          // plugins' statusline segments, in the order they loaded
	Ruler             string            // the cursor's line and column, and where the view is (B38)
	Branch            string            // the git branch of the buffer's repository, "" outside one (B147)
	Errors, Warnings  int               // the buffer's diagnostics of each severity (B147)
	Filetype          string            // the buffer's filetype, "" for none (B164)
	Server            string            // the buffer's language servers, "" for none (B161, B169)
	Progress          string            // what the server is doing, "" once it is ready (B161)
	Gutter            int               // the columns the sign column and line numbers take on the left
	SignCols          int               // the columns of the sign column, at the gutter's left
	Signs             map[int]Sign      // by line, the sign each one shows (B119)
	Notes             map[int]Note      // by line, text after its end (B127, B184)
	Icons             map[int]Sign      // by line, a listing's icon at the gutter's right (B128)
	Number, Relative  bool              // show line numbers, and relative ones (B39)
	Wrap              bool              // long lines go on the rows below
	CursorLine        bool              // shade the cursor's line (B117)
	Pair              []int             // the bracket at the cursor and its match, shown bold (B121)
	List              bool              // show tabs, trailing spaces and no-break spaces (B118)
	Left              int               // the first column shown without wrap
	Indent            int               // the columns of an indent level, where guides go
	Tab               int               // the columns a tab shows as
	Guides            bool              // draw indent guides (B153)
	ColorColumn       int               // the column of the text to shade, from 1, or 0 (B200)
	Colours           []Colour          // text that names a colour, shown on it (B208, B209)
	Labels            []Label           // a jump's labels, drawn over the text (B218)
	Folds             map[int]int       // by the first line of each closed fold shown, the lines it hides (B224)
	Grounds           map[int]string    // by line, the style whose ground shades its rows (B296)
	Virtual           map[int][]Virtual // by line, the rows drawn above and below it (B299)
	Tasks             []string          // the tasks running, on the current window's status line (B231)
	Others            []Other           // the other sessions whose current window shows the buffer (B253)
	Sharing           string            // on the current window, whether fude shares or the session is a guest (B269)
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

// Ghost is text a window draws that the text does not hold, before the
// byte at Pos: a proposal's, an inlay hint or a replacement (B71, B216,
// B295).
type Ghost struct {
	Pos   int
	End   int // past Pos, the text from Pos to End shows as Text in its place (B295)
	Text  string
	Stale bool
	Style string // how it shows: "" for a proposal's text, sub.new for :s's (B154) or inlay for an inlay hint (B216)
}

// Inlines returns gs as the inlines the window's rows and columns count.
func Inlines(gs []Ghost) []vim.Inline {
	out := make([]vim.Inline, len(gs))
	for i, g := range gs {
		out[i] = vim.Inline{Start: g.Pos, End: max(g.End, g.Pos), Text: g.Text}
	}
	return out
}

// Picture is an image drawn in a box. It takes Rows by Cols cells, from
// the box's row Row (B305).
type Picture struct {
	ID, Row, Rows, Cols int
}

// Virtual is a row a window draws below a line, or above it, that holds no
// text (B299).
type Virtual struct {
	Text, Style string
	Above       bool
	// Image is the id of the image the row shows row Row of, Cols columns
	// wide, or 0 (B303).
	Image, Row, Cols int
}

// Note is text after the end of a line. Name is its style, a diagnostic's
// severity, and a note with none shows dim (B61, B127, B184).
type Note struct {
	Text, Name string
}

// Sign is a letter in the sign column and the style it takes its colour
// from, such as diagnostic.error.
type Sign struct {
	Text, Name string
}

// Other is another session as a view shows it (B252, B253).
type Other struct {
	Name      string
	Colour    int // which of six colours
	Cursor    int
	Selection *vim.Selection `json:",omitempty"`
}

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
	Lit         bool     // the windows under it stay lit, as under the key hints (B151)
	DocImage    *Picture // an image in the documentation's box, as the finder previews one (B305)
	Image       *Picture // an image below the text of a float at the cursor, as hover's markdown shows one (B309)
	// Foot shows under a rule at the bottom of a box in a place of its own,
	// styled by FootSpans, and stays there as the text scrolls, as a
	// panel's keys do (B159).
	Foot      string
	FootSpans []syntax.Span
	// Where a box in a place of its own goes, its border included. W is 0
	// for a box at the cursor. DocW is the width of the documentation's box
	// right of it.
	X, Y, W, H, DocW int
}

// Tab is a buffer as the buffer line shows it (B258).
type Tab struct {
	Number            int
	Name              string
	Current, Modified bool
}

// Cell is a character of a terminal window's screen with its look. A nil
// colour is the theme's.
type Cell struct {
	Text                                            string
	Width                                           int
	Fg, Bg                                          color.Color
	Bold, Faint, Italic, Underline, Reverse, Strike bool
}

// Label is the label of a jump's match, drawn over the character at Pos,
// the one after the match (B218).
type Label struct {
	Pos  int
	Text string
}

// Colour is text a window shows on the colour it names (B208, B209).
type Colour struct {
	Start, End int
	RGB        int32 // 0xRRGGBB
}
