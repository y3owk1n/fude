package vim_test

import (
	"fmt"
	"strings"

	"github.com/y3owk1n/fude/vim"
)

// line is a host's text held in a string.
type line string

func (l line) Len() int                    { return len(l) }
func (l line) Slice(start, end int) string { return string(l[start:end]) }
func (l line) Lines() int                  { return strings.Count(string(l), "\n") + 1 }
func (l line) LineOf(pos int) int          { return strings.Count(string(l[:pos]), "\n") }

func (l line) LineStart(n int) int {
	pos := 0
	for range n {
		pos += strings.IndexByte(string(l[pos:]), '\n') + 1
	}
	return pos
}

// field is a host for a one-line text field. It moves its marks with each
// edit and leaves undo, jumps and the clipboard to vim.Defaults.
type field struct {
	vim.Defaults
	text   string
	cursor int
	marks  map[rune]int
}

func (f *field) Text() vim.Text             { return line(f.text) }
func (f *field) Cursor() int                { return f.cursor }
func (f *field) SetCursor(pos int)          { f.cursor = pos }
func (f *field) Viewport() (int, int, int)  { return 0, 1, 80 }
func (f *field) SetTop(int)                 {}
func (f *field) SetMark(name rune, pos int) { f.marks[name] = pos }

func (f *field) Mark(name rune) (int, bool) {
	pos, ok := f.marks[name]
	return pos, ok
}

// Apply makes the edits last to first, so each edit's offsets are still
// right when it is made.
func (f *field) Apply(edits []vim.Edit, _ string) error {
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		f.text = f.text[:e.Start] + e.Text + f.text[e.End:]
		for name, pos := range f.marks {
			switch {
			case pos >= e.End:
				f.marks[name] = pos + len(e.Text) - (e.End - e.Start)
			case pos > e.Start:
				f.marks[name] = e.Start
			}
		}
	}
	return nil
}

func Example() {
	f := &field{text: "hello big world", marks: map[rune]int{}}
	m := vim.New(f)
	for _, keys := range []string{"wdw", "$bcwthere<Esc>", "0yeP", "$x."} {
		for _, k := range vim.Keys(keys) {
			if err := m.Feed(k); err != nil {
				fmt.Println(err)
			}
		}
		fmt.Printf("%-16s %q\n", keys, f.text)
	}
	// Output:
	// wdw              "hello world"
	// $bcwthere<Esc>   "hello there"
	// 0yeP             "hellohello there"
	// $x.              "hellohello the"
}
