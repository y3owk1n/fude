package wire

import (
	"fmt"
	"strconv"
	"strings"
)

// Style is a style a module defines by name: its text's and its ground's
// colours, either unset to keep the theme's, and how its text draws (B345).
type Style struct {
	Fg, Bg                          Hex
	Bold, Italic, Underline, Strike bool
}

// Colors are the colours config.toml's [colors] puts in place of the
// theme's, by role. A role it leaves out keeps the theme's colour (B294).
type Colors struct {
	Bg          Hex `toml:"bg"`
	Surface     Hex `toml:"surface"`
	Raised      Hex `toml:"raised"`
	Border      Hex `toml:"border"`
	Text        Hex `toml:"text"`
	Muted       Hex `toml:"muted"`
	Faint       Hex `toml:"faint"`
	CursorLine  Hex `toml:"cursor_line"`
	Selection   Hex `toml:"selection"`
	Search      Hex `toml:"search"`
	Normal      Hex `toml:"normal"`
	Insert      Hex `toml:"insert"`
	Visual      Hex `toml:"visual"`
	Command     Hex `toml:"command"`
	Error       Hex `toml:"error"`
	Warn        Hex `toml:"warn"`
	Info        Hex `toml:"info"`
	Hint        Hex `toml:"hint"`
	Keyword     Hex `toml:"keyword"`
	String      Hex `toml:"string"`
	Function    Hex `toml:"function"`
	Type        Hex `toml:"type"`
	Number      Hex `toml:"number"`
	Comment     Hex `toml:"comment"`
	Punctuation Hex `toml:"punctuation"`
}

// Hex is a colour config.toml gives as #rrggbb. The zero Hex is none, and
// an empty string reads as none.
type Hex struct {
	rgb int32
	set bool
}

// RGB returns the colour, or false for none.
func (h Hex) RGB() (int32, bool) { return h.rgb, h.set }

func (h *Hex) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*h = Hex{}
		return nil
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(string(b), "#"), 16, 24)
	if len(b) != 7 || b[0] != '#' || err != nil {
		return fmt.Errorf("a colour is #rrggbb, not %q", b)
	}
	*h = Hex{int32(v), true}
	return nil
}

func (h Hex) MarshalText() ([]byte, error) {
	if !h.set {
		return nil, nil
	}
	return fmt.Appendf(nil, "#%06x", h.rgb), nil
}
