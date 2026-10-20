package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/paths"
	"github.com/y3owk1n/fude/internal/timeline"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// highlights keeps the highlight captures of one version of a buffer's
// whole text, for calls that read them off the loop.
type highlights struct {
	mu      sync.Mutex
	version kernel.Version
	spans   []syntax.Span
	ok      bool
}

// of returns the captures of t in filetype ft, parsing t once per version.
func (h *highlights) of(t kernel.Snapshot, ft, queries string) []syntax.Span {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.ok || h.version != t.Version() {
		spans, err := syntax.Highlight(ft, queries, t.Slice(0, t.Len()))
		if err != nil {
			logLine(err.Error())
		}
		h.version, h.spans, h.ok = t.Version(), spans, true
	}
	return h.spans
}

// buffer is one open file: its text, and what the vim machine needs to undo
// and mark it.
type buffer struct {
	id    int
	file  file
	text  *kernel.Buffer
	saved kernel.NodeID
	hl    highlights // for calls that read its captures (B273)

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

	log     *timeline.Log     // the timeline on disk, nil for a buffer with no file
	syntax  *syntax.Buffer    // nil without a grammar for the filetype
	servers []*server         // the language servers attached to it, in its filetype's order
	signs   int               // plugins' signs on it, which show a sign column too
	picture string            // the image file it shows, when it is one (B304)
	inlines int               // decorations that show text in its lines
	listed  string            // the directory an explorer lists, "" for any other buffer
	entries []entry           // an explorer's names as it read them
	touched [2]int            // the text the last change made, as it is after it
	unsaved kernel.NodeID     // the newest unsaved change from a session that did not close
	set     map[string]string // the options modules set on it, by name (B283)
	folds   []decoration      // the closed folds on it, modules' decorations (B224)
	keys    *vim.Keymaps      // a panel's own keys, nil for its kind's (B291)
	// foot is text a panel keeps under a rule at its window's foot as it
	// scrolls, styled by footSpans (B159).
	foot      string
	footSpans []syntax.Span
	screen    bool      // a panel in place of a window's buffer, which shows it bare (B320)
	term      *terminal // the program a terminal buffer shows, nil for any other (B205)
	// A scratch buffer shows something about another buffer, such as its
	// timeline, under a title instead of a file name.
	title     string
	ft        string // the filetype of a buffer with no file, such as help's markdown
	shown     bool   // fude wrote it to show something, such as :messages or a diff
	panelKind string // its kind as a module's panel names it, such as list (B347)
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
	trace.Since("file", fmt.Sprintf("read %q, %d bytes", path, len(text)), at, err)
	if err != nil {
		return nil, "", err
	}
	b := &buffer{id: id, file: f, before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}}
	if f.image {
		b.picture = path
	}
	msg := ""
	switch {
	case b.picture != "":
	case f.readonly:
		msg = fmt.Sprintf("\"%s\" is not valid UTF-8, opened read-only", path)
	case f.isNew && path != "":
		msg = fmt.Sprintf("\"%s\" [New]", path)
	}
	if path == "" || f.readonly {
		b.text = kernel.New(text, nil)
		return b, msg, nil
	}
	logPath, err := historyPath(path)
	if err != nil {
		return nil, "", err
	}
	log, l, err := timeline.Open(logPath, text)
	if err != nil {
		return nil, "", err
	}
	if b.text, err = kernel.Load(text, l.Head, l.Nodes, log); err != nil {
		return nil, "", fmt.Errorf("timeline of %s: %w", path, err)
	}
	b.log, b.saved, b.head, b.saves, b.unsaved = log, l.Head, l.Head, l.Saves, l.Unsaved
	if l.Unsaved != (kernel.NodeID{}) {
		msg = fmt.Sprintf("\"%s\" has changes a session did not save, :recover brings them back", path)
	}
	return b, msg, nil
}

// historyPath returns where the timeline log of the file at path lives.
func historyPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("timeline of %s: %w", path, err)
	}
	dir, err := keep.Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", fmt.Errorf("timeline of %s: %w", path, err)
	}
	sum := sha256.Sum256([]byte(paths.Key(abs)))
	return filepath.Join(dir, "fude", "timeline", hex.EncodeToString(sum[:16])+".log"), nil
}

// written reports whether fude writes b's text, as it does a float, help, a
// list or a panel, so nothing types in it. A read-only file is not one.
func (b *buffer) written() bool { return b.file.readonly && b.title != "" }

func (b *buffer) modified() bool { return b.text.Head() != b.saved }

// numbered reports whether b has a number of those :b and :ls count (B373).
func (b *buffer) numbered() bool { return b.id < firstAside }

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
	y, err := filepath.Abs(path)
	if b.listed != "" {
		return err == nil && paths.Same(y, b.listed)
	}
	x, err1 := filepath.Abs(b.file.path)
	return b.file.path != "" && err1 == nil && err == nil && paths.Same(x, y)
}

// markSaved notes that the file on disk holds text, the buffer's text.
func (b *buffer) markSaved(text string) {
	b.saved = b.text.Head()
	b.saves = append(b.saves, b.saved)
	if b.log != nil {
		b.log.Saved(b.saved, text)
	}
}

// close notes a clean close in the timeline log, so the next open offers no
// recovery.
func (b *buffer) close() {
	if b.log != nil {
		// The log keeps a crash's changes while :recover offers them.
		b.log.Close(b.text, b.saves, b.unsaved == kernel.NodeID{})
	}
	// A closed buffer's syntax is gone, so what reads b after, such as the
	// scope of buffer.closed, reads none (B331).
	if b.syntax != nil {
		b.syntax.Close()
		b.syntax = nil
	}
	if b.term != nil {
		b.term.stop()
	}
}

// blank reports whether b is an empty buffer with no file, as fude starts
// with, which :bd passes while another is open (B89).
func (b *buffer) blank() bool {
	return b.file.path == "" && b.title == "" && !b.modified() && b.text.Snapshot().Len() == 0
}

// kind names the buffer's own keymap, or is "" for a buffer with none.
func (b *buffer) kind() string {
	switch {
	case b.term != nil:
		return "terminal"
	case b.listed != "":
		return "explorer"
	case b.panelKind != "":
		return b.panelKind
	case b.title == "[timeline]":
		return "timeline"
	case b.title == "[float]":
		return "float"
	case b.title == "[language servers]":
		return "lsp"
	case b.title == "[help]":
		return "help"
	case b.title == "[start]":
		return "start"
	case b.title == "[debug]":
		return "debug"
	case b.shown:
		return "scratch"
	}
	return ""
}
