package term

import (
	"io"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/y3owk1n/fude/vim"
)

// typed is a terminal the user typed in, as bytes, and whose output goes
// nowhere.
type typed struct{ in chan []byte }

func (t typed) Start() error                { return nil }
func (t typed) Stop() error                 { return nil }
func (t typed) Drain() error                { return nil }
func (t typed) NotifyResize(chan<- bool)    {}
func (t typed) Close() error                { return nil }
func (t typed) Write(p []byte) (int, error) { return len(p), nil }
func (t typed) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: 80, Height: 24}, nil
}

func (t typed) Read(p []byte) (int, error) {
	b, ok := <-t.in
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}

// B141
func TestB141CtrlH(t *testing.T) {
	in := make(chan []byte, 1)
	s, err := screen(typed{in})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	for _, c := range []struct {
		in   string
		want vim.Key
	}{{"\x08", "<C-h>"}, {"\x7f", "<BS>"}} {
		in <- []byte(c.in)
		for {
			select {
			case ev := <-s.EventQ():
				k, ok := ev.(*tcell.EventKey)
				if !ok {
					continue
				}
				if got := keys(k); len(got) != 1 || got[0] != c.want {
					t.Fatalf("%q read as %v, want %v", c.in, got, c.want)
				}
			case <-time.After(time.Second):
				t.Fatalf("%q read as nothing", c.in)
			}
			break
		}
	}
}
