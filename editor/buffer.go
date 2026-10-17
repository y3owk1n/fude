package editor

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// buffer is one open file: its text, and what the vim machine needs to undo
// and mark it.
type buffer struct {
	id    int
	file  file
	text  *kernel.Buffer
	saved kernel.NodeID

	// Where the cursor and the top line were when the buffer was last left,
	// for the next window to show it.
	last, lastTop int

	// The cursor from before each undo step, for the vim machine's undo.
	before map[kernel.NodeID]int
	head   kernel.NodeID

	marks map[rune]kernel.MarkID

	changes  []kernel.MarkID // where edits happened, oldest first
	changeAt int             // the change g; last moved to, or len(changes)

	saves []kernel.NodeID // the node at each write, oldest first

	log      *history          // the timeline on disk, nil for a buffer with no file
	syntax   *syntax.Buffer    // nil without a grammar for the filetype
	servers  []*server         // the language servers attached to it, in its filetype's order
	signed   bool              // it has diagnostics, so windows show a sign column
	signs    int               // plugins' signs on it, which show a sign column too
	listed   string            // the directory a listing lists, "" for any other buffer
	entries  []entry           // a listing's names as it read them
	noted    map[string]string // a listing's note of each name it has shown
	base     *string           // the text git's index has, nil outside a repository
	git      map[int]Sign      // the git sign of each changed line
	touched  [2]int            // the text the last change made, as it is after it
	status   map[string]Sign   // a listing's git status sign of each name
	branch   string            // the git branch of its repository, "" outside one
	gitStale bool              // the text changed since the last diff
	gitBusy  bool              // a diff runs
	ask      bool              // its text changed, so the next frame asks servers again for colours and hints
	blamed   blamed            // git blame of the cursor's line, for :blame (B220)
	unsaved  kernel.NodeID     // the newest unsaved change from a session that did not close
	ec       editorconfig      // what its .editorconfig files set (B238)
	term     *terminal         // the program a terminal buffer shows, nil for any other (B205)
	// A scratch buffer shows something about another buffer, such as its
	// timeline, under a title instead of a file name.
	title   string
	ft      string // the filetype of a buffer with no file, such as help's markdown
	source  *buffer
	lines   []kernel.NodeID // the timeline node on each line, for :restore
	shown   bool            // fude wrote it to show something, such as :messages or a diff
	recent  []string        // the recent files the start screen lists (B225)
	spots   []spot          // the place each line of a list names, for <CR>
	breaks  []breakpoint    // where a debug session stops (B262)
	stopped bool            // the line a debug session stopped on is in it
}

// pushChange records an edit at pos, replacing the newest entry when it is
// on the same line.
func (b *buffer) pushChange(pos int) {
	t := b.text.Snapshot()
	if n := len(b.changes); n > 0 && sameLine(t, b.text.Mark(b.changes[n-1]), pos) {
		b.text.MoveMark(b.changes[n-1], pos)
	} else {
		b.changes = append(b.changes, b.text.SetMark(pos, kernel.Right))
	}
	if over := len(b.changes) - placesKept; over > 0 {
		for _, id := range b.changes[:over] {
			b.text.DeleteMark(id)
		}
		b.changes = b.changes[over:]
	}
	b.changeAt = len(b.changes)
}

// placesKept is how many places a jumplist or changelist keeps (B90).
const placesKept = 100

func sameLine(t kernel.Snapshot, p, q int) bool {
	return vim.LineStart(t, p) == vim.LineStart(t, q)
}

// openBuffer reads path into a buffer, and returns what to tell the user.
func openBuffer(id int, path string) (*buffer, string, error) {
	at := time.Now()
	f, text, err := read(path)
	traced("file", fmt.Sprintf("read %q, %d bytes", path, len(text)), at, err)
	if err != nil {
		return nil, "", err
	}
	b := &buffer{id: id, file: f, before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}}
	msg := ""
	switch {
	case f.readonly:
		msg = fmt.Sprintf("\"%s\" is not valid UTF-8, opened read-only", path)
	case f.isNew && path != "":
		msg = fmt.Sprintf("\"%s\" [New]", path)
	}
	if path == "" || f.readonly {
		b.text = kernel.New(text, nil)
		return b, msg, nil
	}
	log, l, err := openHistory(path, text)
	if err != nil {
		return nil, "", err
	}
	if b.text, err = kernel.Load(text, l.head, l.nodes, log); err != nil {
		return nil, "", fmt.Errorf("timeline of %s: %w", path, err)
	}
	b.log, b.saved, b.head, b.saves, b.unsaved = log, l.head, l.head, l.saves, l.unsaved
	if l.unsaved != (kernel.NodeID{}) {
		msg = fmt.Sprintf("\"%s\" has changes a session did not save, :recover brings them back", path)
	}
	return b, msg, nil
}

func (b *buffer) modified() bool { return b.text.Head() != b.saved }

// name is the buffer's path, or [No Name] for a buffer with no file.
func (b *buffer) name() string {
	switch {
	case b.title != "":
		return b.title
	case b.file.path == "":
		return "[No Name]"
	}
	return b.file.path
}

// is reports whether the buffer holds the file at path.
func (b *buffer) is(path string) bool {
	x, err1 := filepath.Abs(b.file.path)
	y, err2 := filepath.Abs(path)
	return b.file.path != "" && err1 == nil && err2 == nil && x == y
}

// markSaved notes that the file on disk holds text, the buffer's text.
func (b *buffer) markSaved(text string) {
	b.saved = b.text.Head()
	b.saves = append(b.saves, b.saved)
	if b.log != nil {
		b.log.write(record{Kind: "save", Head: b.saved, Hash: hash(text)})
	}
}

// close notes a clean close in the timeline log, so the next open offers no
// recovery.
func (b *buffer) close() {
	if b.log != nil {
		// The log keeps a crash's changes while :recover offers them.
		b.log.close(b.text, b.saves, b.unsaved == kernel.NodeID{})
	}
	if b.syntax != nil {
		b.syntax.Close()
	}
	if b.term != nil {
		b.term.stop()
	}
}

// kind names the buffer's own keymap, or is "" for a buffer with none.
func (b *buffer) kind() string {
	switch {
	case b.term != nil:
		return "terminal"
	case b.listed != "":
		return "listing"
	case b.spots != nil:
		return "list"
	case b.lines != nil:
		return "timeline"
	case b.title == "[float]":
		return "float"
	case b.title == "[lsp]":
		return "lsp"
	case b.title == "[help]":
		return "help"
	case b.title == "[start]":
		return "start"
	case b.shown:
		return "scratch"
	}
	return ""
}
