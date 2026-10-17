package editor

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/wire"
)

// coloured names the colours a server reported for a buffer.
type coloured struct {
	s *server
	b *buffer
}

// textColours returns the colours text names as #rgb, #rgba, #rrggbb or
// #rrggbbaa, or with rgb(), rgba(), hsl() or hsla(), text starting at base
// (B208).
func textColours(text string, base int) []wire.Colour {
	var out []wire.Colour
	for i := 0; i < len(text); i++ {
		if i > 0 && isWord(text[i-1]) {
			continue
		}
		if text[i] == '#' {
			j := i + 1
			for j < len(text) && isHex(text[j]) {
				j++
			}
			if n := j - i - 1; (n == 3 || n == 4 || n == 6 || n == 8) && (j == len(text) || !isWord(text[j])) {
				out = append(out, wire.Colour{Start: base + i, End: base + j, RGB: hexRGB(text[i+1 : j])})
				i = j - 1
			}
			continue
		}
		for _, fn := range []string{"rgba(", "rgb(", "hsla(", "hsl("} {
			if !strings.HasPrefix(text[i:], fn) {
				continue
			}
			end := strings.IndexByte(text[i:min(i+48, len(text))], ')')
			if end < 0 {
				break
			}
			if rgb, ok := funcRGB(fn[:3], text[i+len(fn):i+end]); ok {
				out = append(out, wire.Colour{Start: base + i, End: base + i + end + 1, RGB: rgb})
				i += end
			}
			break
		}
	}
	return out
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// hexRGB returns the colour of hex, 3, 4, 6 or 8 digits, without its alpha.
func hexRGB(hex string) int32 {
	if len(hex) <= 4 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	v, _ := strconv.ParseInt(hex[:6], 16, 32)
	return int32(v)
}

// funcRGB returns the colour of the arguments of a CSS rgb or hsl
// function, apart by commas, blanks or a slash before the alpha.
func funcRGB(fn, args string) (int32, bool) {
	parts := strings.FieldsFunc(args, func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(parts) < 3 {
		return 0, false
	}
	var v [3]float64
	for k, p := range parts[:3] {
		n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(p, "%"), "deg"), 64)
		if err != nil {
			return 0, false
		}
		switch {
		case fn == "rgb" && strings.HasSuffix(p, "%"):
			v[k] = n / 100
		case fn == "rgb":
			v[k] = n / 255
		case k == 0:
			v[k] = math.Mod(n, 360) / 360
		default:
			v[k] = n / 100
		}
	}
	if fn == "hsl" {
		v = hslRGB(v[0], v[1], v[2])
	}
	var rgb int32
	for _, c := range v {
		rgb = rgb<<8 | int32(math.Round(math.Max(0, math.Min(1, c))*255))
	}
	return rgb, true
}

// hslRGB turns hue, saturation and lightness, each 0 to 1, into red, green
// and blue.
func hslRGB(h, s, l float64) [3]float64 {
	q := l + s - l*s
	if l < 0.5 {
		q = l * (1 + s)
	}
	p := 2*l - q
	channel := func(t float64) float64 {
		t -= math.Floor(t)
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return [3]float64{channel(h + 1.0/3), channel(h), channel(h - 1.0/3)}
}

// askColours asks s for the colours in b, when highlight_colors is on and s
// reports colours. An answer for text since changed is dropped, as the
// change asks again (B209).
func (e *Editor) askColours(s *server, b *buffer) {
	if !e.cfg.HighlightColors || !s.client.Supports("colorProvider") {
		return
	}
	t := b.text.Snapshot()
	uri := lsp.URI(b.file.path)
	s.client.Request("textDocument/documentColor", func(string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}}
	}, func(res json.RawMessage) {
		if b.text.Snapshot().Version() != t.Version() {
			return
		}
		var found []struct {
			Range lsp.Range `json:"range"`
			Color struct {
				Red, Green, Blue float64
			} `json:"color"`
		}
		if err := json.Unmarshal(res, &found); err != nil {
			logLine(fmt.Sprintf("colours from %s: %v", s.name, err))
			return
		}
		for _, d := range e.decos[coloured{s, b}] {
			d.remove()
		}
		enc := s.client.Encoding()
		var ds []decoration
		for _, f := range found {
			rgb := int32(math.Round(f.Color.Red*255))<<16 | int32(math.Round(f.Color.Green*255))<<8 | int32(math.Round(f.Color.Blue*255))
			start, end := lsp.Offset(t, f.Range.Start, enc), lsp.Offset(t, f.Range.End, enc)
			ds = append(ds, decoration{buf: b, start: b.text.SetMark(start, kernel.Right), end: b.text.SetMark(end, kernel.Left), style: fmt.Sprintf("colour #%06x", rgb)})
		}
		e.decos[coloured{s, b}] = ds
	})
}
