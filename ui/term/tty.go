package term

import (
	"bytes"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v3"
)

// screen returns a started screen on tty. Negotiation waits on a terminal
// round trip, so fude turns it off and asks for the kitty keyboard protocol
// without querying (D26). It asks whether the terminal draws cursors of its
// own and sets extra when the answer comes, without waiting for it.
func screen(tty tcell.Tty, extra *atomic.Bool) (tcell.Screen, error) {
	// After a lone Esc from a terminal without the kitty protocol, fude waits
	// 10ms for the rest of a sequence. Windows keeps tcell's 125ms, as its
	// terminal input comes in delayed batches (N42).
	esc := 10 * time.Millisecond
	if runtime.GOOS == "windows" {
		esc = 0
	}
	s, err := tcell.NewTerminfoScreenFromTty(&ctrlH{Tty: tty, extra: extra},
		tcell.OptNegotiation(false),
		tcell.OptKeyboardProtocol(tcell.KittyKeyboard),
		tcell.OptAdvancedKeys(true),
		tcell.OptEscapeTimeout(esc),
	)
	if err != nil {
		return nil, err
	}
	if err := s.Init(); err != nil {
		return nil, err
	}
	_, err = tty.Write([]byte(askCursors))
	return s, err
}

// askCursors asks a terminal which shapes of kitty's multiple cursors
// protocol it draws. A terminal without it says nothing.
const askCursors = "\x1b[> q"

// ctrlHKey is Ctrl+h in the kitty keyboard protocol.
var ctrlHKey = []byte("\x1b[104;5u")

// ctrlH is a tty that reads the byte 0x08 as Ctrl+h, where
// tcell reads it as backspace. A terminal sends 0x7f for backspace and 0x08
// for Ctrl+h, and tmux passes both on as they are, so <C-h> can be mapped
// apart from <BS> (B141).
type ctrlH struct {
	tcell.Tty
	rest  []byte       // what the last read left over
	err   error        // the last read's error, for after rest
	extra *atomic.Bool // set by the answer to askCursors, which it takes out
	held  []byte       // the start of an answer the last read cut off
}

func (t *ctrlH) Read(p []byte) (int, error) {
	if len(t.rest) == 0 {
		if t.err != nil {
			err := t.err
			t.err = nil
			return 0, err
		}
		n, err := t.read(p)
		if bytes.IndexByte(p[:n], 0x08) < 0 {
			return n, err
		}
		t.rest, t.err = bytes.ReplaceAll(p[:n], []byte{0x08}, ctrlHKey), err
	}
	n := copy(p, t.rest)
	t.rest = t.rest[n:]
	return n, nil
}

// read reads into p after what the last read held back, takes the answer
// to askCursors out, and reads again when that was all it read.
func (t *ctrlH) read(p []byte) (int, error) {
	for {
		h := copy(p, t.held)
		t.held = t.held[:0]
		n, err := t.Tty.Read(p[h:])
		if n = t.answer(p[:h+n]); n > 0 || err != nil || len(p) == 0 {
			return n, err
		}
	}
}

// answer takes the answer to askCursors out of the bytes read into b and
// returns how many are left. An answer the read cuts off waits in held for
// the rest. The terminal draws cursors in the main
// cursor's shape when the shapes it lists hold 29.
func (t *ctrlH) answer(b []byte) int {
	if t.extra == nil {
		return len(b)
	}
	at := bytes.Index(b, []byte("\x1b[>"))
	if at < 0 {
		return len(b)
	}
	end := at + 3
	for end < len(b) && (b[end] >= '0' && b[end] <= '9' || b[end] == ';') {
		end++
	}
	if rest := b[end:]; len(rest) < 2 && bytes.HasPrefix([]byte(" q"), rest) && len(b)-at < 64 {
		t.held = append(t.held, b[at:]...)
		return at
	}
	if !bytes.HasPrefix(b[end:], []byte(" q")) {
		return len(b)
	}
	if slices.Contains(strings.Split(string(b[at+3:end]), ";"), "29") {
		t.extra.Store(true)
	}
	return at + copy(b[at:], b[end+2:])
}
