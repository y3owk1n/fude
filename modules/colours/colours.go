// Package colours shows text that names a colour on that colour, and the
// colours a language server reports (B208, B209).
package colours

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// rows is more lines than a window shows. The text's colours show for that
// many from the top of the view.
const rows = 200

// Module is colours.
type Module struct {
	mu      sync.Mutex
	tops    map[string]int    // by buffer, where the view starts, as last heard
	named   map[string][]int  // by buffer, the ids of the colours the text names
	given   map[string][]int  // by buffer, the ids of the colours the server gave
	asked   map[string]uint32 // by buffer, the request waiting for the server
	again   map[string]bool   // by buffer, the text changed while a request waited
	without map[string]bool   // by buffer, no server gives colours, until one attaches
	bufs    map[uint32]string // by request, the buffer it is about
	ids     int
	reqs    uint32
}

// New returns colours.
func New() *Module {
	return &Module{tops: map[string]int{}, named: map[string][]int{}, given: map[string][]int{}, asked: map[string]uint32{}, again: map[string]bool{}, without: map[string]bool{}, bufs: map[uint32]string{}}
}

func (m *Module) Name() string { return "colours" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "buffer.closed", "view.moved", "lsp.attached", "option.set", "config.reloaded"}
}

func (m *Module) Registered() []api.Registration { return nil }

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event colours the text the view shows when it moves or the text changes,
// and asks the server again after each change and once one attaches.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	event, detail, _ := strings.Cut(name, " ")
	switch {
	case event == "option.set" && detail != "highlight_colors":
		return api.Result{}, nil
	case event == "lsp.attached":
		delete(m.without, buf)
	case event == "buffer.closed":
		delete(m.bufs, m.asked[buf])
		delete(m.tops, buf)
		delete(m.named, buf)
		delete(m.given, buf)
		delete(m.asked, buf)
		delete(m.again, buf)
		delete(m.without, buf)
		return api.Result{}, nil
	}
	if on, _ := s.Option("highlight_colors"); on != "true" {
		res := api.Result{Undecorate: append(m.named[buf], m.given[buf]...)}
		delete(m.named, buf)
		delete(m.given, buf)
		return res, nil
	}
	if event == "view.moved" {
		_, top, _ := strings.Cut(detail, " ")
		at, _ := strconv.Atoi(top)
		if at == m.tops[buf] && m.named[buf] != nil {
			return api.Result{}, nil
		}
		m.tops[buf] = at
		return m.name(s), nil
	}
	res := m.name(s)
	if _, waiting := m.asked[buf]; waiting {
		m.again[buf] = true
	} else if !m.without[buf] {
		res.Requests = m.ask(s).Requests
	}
	return res, nil
}

// Callback shows the colours the server sent, in place of those showing,
// and asks again when the text changed meanwhile.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := m.bufs[id]
	delete(m.bufs, id)
	if m.asked[buf] != id {
		return api.Result{}, nil
	}
	delete(m.asked, buf)
	res := api.Result{Undecorate: m.given[buf]}
	m.given[buf] = nil
	if m.again[buf] {
		delete(m.again, buf)
		res.Requests = m.ask(s).Requests
	}
	switch err := s.About["error"]; {
	case strings.HasPrefix(err, "no language server offers"):
		m.without[buf] = true
		return res, nil
	case strings.HasPrefix(err, "no language server answered"):
		// A server answers null for a text with no colours.
		return res, nil
	case err != "":
		res.Message = err
		return res, nil
	}
	var found []struct {
		Range lsp.Range `json:"range"`
		Color struct {
			Red, Green, Blue float64
		} `json:"color"`
	}
	if err := json.Unmarshal([]byte(s.About["output"]), &found); err != nil {
		res.Message = "colours from " + s.About["server"] + ": " + err.Error()
		return res, nil
	}
	enc := s.About["encoding"]
	for _, f := range found {
		rgb := int32(math.Round(f.Color.Red*255))<<16 | int32(math.Round(f.Color.Green*255))<<8 | int32(math.Round(f.Color.Blue*255))
		m.ids++
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: lsp.Offset(s.Text, f.Range.Start, enc), End: lsp.Offset(s.Text, f.Range.End, enc), Style: style(rgb)})
		m.given[buf] = append(m.given[buf], m.ids)
	}
	return res, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }

// name colours the text that names colours in the lines of the view, from
// its top, in place of the colours it had (B208).
func (m *Module) name(s api.Scope) api.Result {
	buf, t := s.About["buffer"], s.Text
	res := api.Result{Undecorate: m.named[buf]}
	m.named[buf] = []int{}
	from := min(m.tops[buf], t.Len())
	last := min(t.LineOf(from)+rows, t.Lines()-1)
	to := t.Len()
	if last+1 < t.Lines() {
		to = t.LineStart(last + 1)
	}
	for _, c := range textColours(t.Slice(from, to), from) {
		m.ids++
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: c.start, End: c.end, Style: style(c.rgb)})
		m.named[buf] = append(m.named[buf], m.ids)
	}
	return res
}

// ask asks the servers of the buffer s is about for its colours.
func (m *Module) ask(s api.Scope) api.Result {
	buf := s.About["buffer"]
	m.reqs++
	m.asked[buf], m.bufs[m.reqs] = m.reqs, buf
	return api.Result{Requests: []api.Request{{ID: m.reqs, Method: "textDocument/documentColor", Pos: -1, End: -1}}}
}

// style is the decoration style that shows rgb.
func style(rgb int32) string { return fmt.Sprintf("colour #%06x", rgb) }

// colour is a colour the text names, and where.
type colour struct {
	start, end int
	rgb        int32
}

// textColours returns the colours text names as #rgb, #rgba, #rrggbb or
// #rrggbbaa, or with rgb(), rgba(), hsl() or hsla(), text starting at base
// (B208).
func textColours(text string, base int) []colour {
	var out []colour
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
				out = append(out, colour{base + i, base + j, hexRGB(text[i+1 : j])})
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
				out = append(out, colour{base + i, base + i + end + 1, rgb})
				i += end
			}
			break
		}
	}
	return out
}

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
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
