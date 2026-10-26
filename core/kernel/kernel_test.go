package kernel_test

import (
	"errors"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
)

type nodes []kernel.Node

func (s *nodes) Append(n kernel.Node) { *s = append(*s, n) }

func apply(t *testing.T, b *kernel.Buffer, edits ...kernel.Edit) {
	t.Helper()
	tx := kernel.Transaction{Base: b.Snapshot().Version(), Origin: "user", Label: "edit", Edits: edits}
	if err := b.Apply(tx); err != nil {
		t.Fatal(err)
	}
}

func text(b *kernel.Buffer) string {
	s := b.Snapshot()
	return s.Slice(0, s.Len())
}

// B100. Checks the text against a plain string model.
func TestB100ApplyAndSnapshot(t *testing.T) {
	want := strings.Repeat("the quick brown fox\n", 500)
	b := kernel.New(want, nil)
	first := b.Snapshot()
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		start := r.IntN(len(want) + 1)
		end := start + r.IntN(min(len(want)-start, 300)+1)
		ins := strings.Repeat("x", r.IntN(200))
		apply(t, b, kernel.Edit{Start: start, End: end, Text: ins})
		want = want[:start] + ins + want[end:]
	}
	if got := text(b); got != want {
		t.Fatalf("text diverged from the model: len %d, want %d", len(got), len(want))
	}
	if got := first.Slice(0, first.Len()); got != strings.Repeat("the quick brown fox\n", 500) {
		t.Fatal("first snapshot changed")
	}

	apply(t, b, kernel.Edit{Start: 0, End: 1, Text: "A"}, kernel.Edit{Start: 2, End: 2, Text: "B"})
	if got := text(b)[:4]; got != "A"+want[1:2]+"B"+want[2:3] {
		t.Fatalf("multi-edit transaction: got %q", got)
	}
}

// B106. Checks the line index against a scan of the text after random edits.
func TestB106LineIndex(t *testing.T) {
	want := strings.Repeat("the quick brown fox\n\n", 300)
	b := kernel.New(want, nil)
	r := rand.New(rand.NewPCG(3, 4))
	for range 500 {
		start := r.IntN(len(want) + 1)
		end := start + r.IntN(min(len(want)-start, 300)+1)
		ins := strings.Repeat("ab\n", r.IntN(100))
		apply(t, b, kernel.Edit{Start: start, End: end, Text: ins})
		want = want[:start] + ins + want[end:]
	}
	s := b.Snapshot()
	if s.Lines() != strings.Count(want, "\n")+1 {
		t.Fatalf("%d lines, want %d", s.Lines(), strings.Count(want, "\n")+1)
	}
	line := 0
	for pos := 0; pos <= len(want); pos++ {
		if s.LineOf(pos) != line {
			t.Fatalf("LineOf(%d) is %d, want %d", pos, s.LineOf(pos), line)
		}
		if pos == 0 || want[pos-1] == '\n' {
			if s.LineStart(line) != pos {
				t.Fatalf("LineStart(%d) is %d, want %d", line, s.LineStart(line), pos)
			}
		}
		if pos < len(want) && want[pos] == '\n' {
			line++
		}
	}
}

// heard keeps the text a listener would rebuild from the changes it hears.
type heard struct{ text string }

func (h *heard) Changed(before kernel.Snapshot, edits []kernel.Edit) {
	if before.Slice(0, before.Len()) != h.text {
		panic("listener: before is not the text it last heard")
	}
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		h.text = h.text[:e.Start] + e.Text + h.text[e.End:]
	}
}

// B107
func TestB107ListenerHearsEveryChange(t *testing.T) {
	b := kernel.New("one two", nil)
	h := &heard{text: "one two"}
	b.Listen(h)
	apply(t, b, kernel.Edit{Start: 0, End: 3, Text: "1"}, kernel.Edit{Start: 4, End: 7, Text: "2"})
	apply(t, b, kernel.Edit{Start: 1, End: 1, Text: "!"})
	first := b.Head()
	b.Undo()
	b.Undo()
	b.Redo()
	apply(t, b, kernel.Edit{Start: 0, End: 0, Text: "x"})
	b.Goto(first)
	if h.text != text(b) {
		t.Errorf("the listener rebuilt %q, want %q", h.text, text(b))
	}
}

// B101
func TestB101StaleBaseRejected(t *testing.T) {
	b := kernel.New("abc", nil)
	apply(t, b, kernel.Edit{Start: 0, End: 0, Text: "x"})
	err := b.Apply(kernel.Transaction{Base: 0, Edits: []kernel.Edit{{Start: 0, End: 1, Text: "y"}}})
	if !errors.Is(err, kernel.ErrStale) {
		t.Fatalf("err = %v, want ErrStale", err)
	}
	if got := text(b); got != "xabc" || len(b.Nodes()) != 1 {
		t.Fatalf("buffer changed: %q, %d nodes", got, len(b.Nodes()))
	}
}

// B102. Run it with -race to catch data races.
func TestB102SnapshotsAcrossGoroutines(t *testing.T) {
	b := kernel.New(strings.Repeat("a", 4096), nil)
	var wg sync.WaitGroup
	for range 100 {
		s := b.Snapshot()
		wg.Go(func() {
			if strings.Trim(s.Slice(0, s.Len()), "a") != "" || s.Len() != 4096+int(s.Version()) {
				t.Error("snapshot is inconsistent")
			}
		})
		apply(t, b, kernel.Edit{Start: 2048, End: 2048, Text: "a"})
	}
	wg.Wait()
}

// B103
func TestB103MarksMoveWithEdits(t *testing.T) {
	cases := []struct {
		name string
		pos  int
		g    kernel.Gravity
		edit kernel.Edit
		want int
	}{
		{"edit after", 2, kernel.Left, kernel.Edit{Start: 3, End: 4, Text: "xyz"}, 2},
		{"insert before", 2, kernel.Left, kernel.Edit{Start: 0, End: 0, Text: "xyz"}, 5},
		{"delete before", 4, kernel.Left, kernel.Edit{Start: 0, End: 2}, 2},
		{"insert at, left", 2, kernel.Left, kernel.Edit{Start: 2, End: 2, Text: "xyz"}, 2},
		{"insert at, right", 2, kernel.Right, kernel.Edit{Start: 2, End: 2, Text: "xyz"}, 5},
		{"inside deletion", 3, kernel.Right, kernel.Edit{Start: 1, End: 5}, 1},
	}
	for _, c := range cases {
		b := kernel.New("0123456789", nil)
		m := b.SetMark(c.pos, c.g)
		apply(t, b, c.edit)
		if got := b.Mark(m); got != c.want {
			t.Errorf("%s: mark at %d, want %d", c.name, got, c.want)
		}
	}
}

// B104
func TestB104GroupIsOneNode(t *testing.T) {
	b := kernel.New("", nil)
	b.BeginGroup("user")
	for _, s := range []string{"h", "i", "!"} {
		apply(t, b, kernel.Edit{Start: b.Snapshot().Len(), End: b.Snapshot().Len(), Text: s})
	}
	if len(b.Nodes()) != 0 {
		t.Fatal("node made before the group ended")
	}
	b.EndGroup("user")
	if n := b.Nodes(); len(n) != 1 || len(n[0].Changes) != 3 || text(b) != "hi!" {
		t.Fatalf("got %d nodes and text %q, want one node of 3 changes and %q", len(n), text(b), "hi!")
	}

	// Another origin's change splits the group around it.
	b.BeginGroup("user")
	apply(t, b, kernel.Edit{Start: 0, End: 0, Text: "a"})
	if err := b.Apply(kernel.Transaction{Base: b.Snapshot().Version(), Origin: "agent", Edits: []kernel.Edit{{Start: 0, End: 0, Text: "b"}}}); err != nil {
		t.Fatal(err)
	}
	apply(t, b, kernel.Edit{Start: 0, End: 0, Text: "c"})
	b.EndGroup("user")
	var origins []kernel.Origin
	for _, n := range b.Nodes()[1:] {
		origins = append(origins, n.ID.Origin)
	}
	if !slices.Equal(origins, []kernel.Origin{"user", "agent", "user"}) {
		t.Fatalf("a group split by another origin made nodes of %v", origins)
	}

	// While a group is open, the head's text leaves out its edits, and an
	// undo ends its node first.
	g := kernel.New("one", nil)
	if err := g.Apply(kernel.Transaction{Origin: "agent", Edits: []kernel.Edit{{Start: 3, End: 3, Text: " two"}}}); err != nil {
		t.Fatal(err)
	}
	g.BeginGroup("user")
	apply(t, g, kernel.Edit{Start: 0, End: 0, Text: "X"})
	if s, _ := g.At(g.Head()); s.Slice(0, s.Len()) != "one two" {
		t.Errorf("the head's text inside a group is %q, want %q", s.Slice(0, s.Len()), "one two")
	}
	if _, err := g.UndoOrigin("agent", "undo", time.Time{}); err != nil || text(g) != "Xone" {
		t.Errorf("undoing agent inside user's group left %q: %v", text(g), err)
	}
}

// B105
func TestB105StoreRebuildsTimeline(t *testing.T) {
	var store nodes
	b := kernel.New("hello", &store)
	apply(t, b, kernel.Edit{Start: 5, End: 5, Text: " world"})
	b.BeginGroup("user")
	apply(t, b, kernel.Edit{Start: 0, End: 1, Text: "H"})
	apply(t, b, kernel.Edit{Start: 6, End: 7, Text: "W"})
	b.EndGroup("user")

	if !reflect.DeepEqual([]kernel.Node(store), b.Nodes()) {
		t.Fatalf("store got %d nodes, buffer has %d", len(store), len(b.Nodes()))
	}
	r, err := kernel.Load(text(b), b.Head(), store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Nodes(), b.Nodes()) || r.Head() != b.Head() || text(r) != "Hello World" {
		t.Fatalf("rebuilt buffer differs: head %v, text %q", r.Head(), text(r))
	}
	apply(t, r, kernel.Edit{Start: 0, End: 0, Text: ">"})
	if id := r.Head(); id.Seq != 3 || r.Nodes()[2].Parent != b.Head() {
		t.Fatalf("new node after load is %v, want seq 3 on the loaded head", id)
	}
}

// B108
func TestB108Map(t *testing.T) {
	b := kernel.New("abcdef", nil)
	v := b.Snapshot().Version()
	apply(t, b, kernel.Edit{Start: 0, End: 0, Text: "xx"})
	apply(t, b, kernel.Edit{Start: 4, End: 6, Text: ""})
	// Text inserted at a position goes after it with left gravity.
	for _, c := range []struct {
		pos  int
		g    kernel.Gravity
		want int
		ok   bool
	}{{0, kernel.Right, 2, true}, {0, kernel.Left, 0, true}, {1, kernel.Right, 3, true}, {2, kernel.Right, 4, true}, {3, kernel.Right, 0, false}, {5, kernel.Right, 5, true}} {
		if got, ok := b.Map(c.pos, v, c.g); got != c.want || ok != c.ok {
			t.Errorf("Map(%d, %v) is %d, %v, want %d, %v", c.pos, c.g, got, ok, c.want, c.ok)
		}
	}
	if _, ok := b.Map(0, b.Snapshot().Version()+1, kernel.Right); ok {
		t.Error("Map from a version still to come gave a position")
	}
}

// B115
func TestB115Since(t *testing.T) {
	b := kernel.New("ab", nil)
	apply(t, b, kernel.Edit{Start: 2, End: 2, Text: "c"})
	from := b.Snapshot().Version()
	apply(t, b, kernel.Edit{Start: 0, End: 1, Text: "A"})
	b.Undo()
	got, ok := b.Since(from)
	want := [][]kernel.Edit{{{Start: 0, End: 1, Text: "A"}}, {{Start: 0, End: 1, Text: "a"}}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("since %d got %v, %v, want %v", from, got, ok, want)
	}
	if got, ok := b.Since(b.Snapshot().Version()); !ok || len(got) != 0 {
		t.Errorf("since now got %v, %v, want nothing", got, ok)
	}
	// Once the edits after a version are gone, it says so.
	for range 5000 {
		apply(t, b, kernel.Edit{Start: 0, End: 0, Text: "x"})
	}
	if _, ok := b.Since(from); ok {
		t.Error("since a version 5000 edits back reported the edits")
	}
}

// B109
func TestB109UndoOrigin(t *testing.T) {
	b := kernel.New("abc", nil)
	edit := func(o kernel.Origin, e kernel.Edit) {
		t.Helper()
		if err := b.Apply(kernel.Transaction{Base: b.Snapshot().Version(), Origin: o, Edits: []kernel.Edit{e}}); err != nil {
			t.Fatal(err)
		}
	}
	edit("a", kernel.Edit{Start: 0, End: 0, Text: "1"})
	edit("b", kernel.Edit{Start: 1, End: 1, Text: "2"})
	// a's change is under b's, so undoing a reverts only it, as a new node.
	if _, err := b.UndoOrigin("a", "undo", time.Time{}); err != nil || text(b) != "2abc" || len(b.Nodes()) != 3 {
		t.Fatalf("undoing a left %q in %d nodes: %v", text(b), len(b.Nodes()), err)
	}
	// Undoing a again finds nothing, as its change and the undo of it are
	// both reverted, and so it is after the timeline loads again.
	if _, err := b.UndoOrigin("a", "undo", time.Time{}); !errors.Is(err, kernel.ErrNothing) {
		t.Errorf("undoing a twice gave %v, want ErrNothing", err)
	}
	loaded, err := kernel.Load(text(b), b.Head(), b.Nodes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.UndoOrigin("a", "undo", time.Time{}); !errors.Is(err, kernel.ErrNothing) || text(loaded) != "2abc" {
		t.Errorf("undoing a after a load gave %v and %q, want ErrNothing", err, text(loaded))
	}
	if _, err := b.UndoOrigin("b", "undo", time.Time{}); err != nil || text(b) != "abc" {
		t.Errorf("undoing b left %q: %v", text(b), err)
	}

	// The head's own change undoes as Undo does, so it can be redone.
	c := kernel.New("x", nil)
	if err := c.Apply(kernel.Transaction{Origin: "a", Edits: []kernel.Edit{{Start: 1, End: 1, Text: "y"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UndoOrigin("a", "undo", time.Time{}); err != nil || text(c) != "x" || len(c.Nodes()) != 1 {
		t.Errorf("undoing the head left %q in %d nodes: %v", text(c), len(c.Nodes()), err)
	}
	if _, ok := c.Redo(); !ok || text(c) != "xy" {
		t.Errorf("redo after undoing the head left %q", text(c))
	}

	// A later change inside it stops the undo.
	d := kernel.New("", nil)
	for _, e := range []struct {
		o kernel.Origin
		e kernel.Edit
	}{{"a", kernel.Edit{Text: "abc"}}, {"b", kernel.Edit{Start: 1, End: 2, Text: "B"}}} {
		if err := d.Apply(kernel.Transaction{Base: d.Snapshot().Version(), Origin: e.o, Edits: []kernel.Edit{e.e}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.UndoOrigin("a", "undo", time.Time{}); !errors.Is(err, kernel.ErrOverlap) || text(d) != "aBc" {
		t.Errorf("undoing under an overlapping change left %q: %v", text(d), err)
	}
}

// B381
func TestB381Forget(t *testing.T) {
	b := kernel.New("one", nil)
	apply(t, b, kernel.Edit{Start: 3, End: 3, Text: " two"})
	m := b.SetMark(4, kernel.Right)
	tx := kernel.Transaction{Base: b.Snapshot().Version(), Origin: "view", Edits: []kernel.Edit{{Start: 0, End: 3, Text: "three"}}, Forget: true}
	if err := b.Apply(tx); err != nil {
		t.Fatal(err)
	}
	if text(b) != "three two" || b.Mark(m) != 6 {
		t.Fatalf("forgetting left %q with the mark at %d, want three two with it at 6", text(b), b.Mark(m))
	}
	if _, ok := b.Undo(); ok || len(b.Nodes()) != 0 || b.Head() != (kernel.NodeID{}) {
		t.Fatalf("after forgetting, %d nodes stay and undo went %v", len(b.Nodes()), ok)
	}
	// A later change is the root's first child.
	apply(t, b, kernel.Edit{Start: 0, End: 0, Text: ">"})
	if b.Undo(); text(b) != "three two" {
		t.Errorf("undoing the change after forgetting left %q", text(b))
	}
	// A buffer whose timeline a store keeps refuses to forget it.
	var store nodes
	s := kernel.New("a", &store)
	tx = kernel.Transaction{Base: s.Snapshot().Version(), Edits: []kernel.Edit{{Text: "b"}}, Forget: true}
	if err := s.Apply(tx); err == nil || text(s) != "a" {
		t.Errorf("forgetting with a store gave %v and left %q", err, text(s))
	}
}
