package term

import (
	"bytes"
	"runtime"
	"time"

	"github.com/gdamore/tcell/v3"
)

// screen returns a started screen on tty. Negotiation waits on a terminal
// round trip, so fude turns it off and asks for the kitty keyboard protocol
// without querying (D26).
func screen(tty tcell.Tty) (tcell.Screen, error) {
	// After a lone Esc from a terminal without the kitty protocol, fude waits
	// 10ms for the rest of a sequence. Windows keeps tcell's 125ms, as its
	// terminal input comes in delayed batches (N42).
	esc := 10 * time.Millisecond
	if runtime.GOOS == "windows" {
		esc = 0
	}
	s, err := tcell.NewTerminfoScreenFromTty(&ctrlH{Tty: tty},
		tcell.OptNegotiation(false),
		tcell.OptKeyboardProtocol(tcell.KittyKeyboard),
		tcell.OptAdvancedKeys(true),
		tcell.OptEscapeTimeout(esc),
	)
	if err != nil {
		return nil, err
	}
	return s, s.Init()
}

// ctrlHKey is Ctrl+h in the kitty keyboard protocol.
var ctrlHKey = []byte("\x1b[104;5u")

// ctrlH is a tty that reads the byte 0x08 as Ctrl+h, where
// tcell reads it as backspace. A terminal sends 0x7f for backspace and 0x08
// for Ctrl+h, and tmux passes both on as they are, so <C-h> can be mapped
// apart from <BS> (B141).
type ctrlH struct {
	tcell.Tty
	rest []byte // what the last read left over
	err  error  // the last read's error, for after rest
}

func (t *ctrlH) Read(p []byte) (int, error) {
	if len(t.rest) == 0 {
		if t.err != nil {
			err := t.err
			t.err = nil
			return 0, err
		}
		n, err := t.Tty.Read(p)
		if bytes.IndexByte(p[:n], 0x08) < 0 {
			return n, err
		}
		t.rest, t.err = bytes.ReplaceAll(p[:n], []byte{0x08}, ctrlHKey), err
	}
	n := copy(p, t.rest)
	t.rest = t.rest[n:]
	return n, nil
}
