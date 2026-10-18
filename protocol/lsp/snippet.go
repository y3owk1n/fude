package lsp

import (
	"strconv"
	"strings"
)

// Stop is where a snippet's tab stop N is in its expanded text.
type Stop struct{ N, Start, End int }

// ExpandSnippet returns body, a snippet in the LSP syntax, with each
// placeholder's text, the first of each choice, and each variable's value
// or default in place, and where its stops are. A stop that repeats shows
// its placeholder's text everywhere. Lines after the first start with
// indent, and a tab is unit (B207).
func ExpandSnippet(body, indent, unit string, vars map[string]string) (string, []Stop) {
	first := snippetParser{body: body, indent: indent, unit: unit, vars: vars, texts: map[int]string{}}
	first.parse(false)
	p := snippetParser{body: body, indent: indent, unit: unit, vars: vars, texts: first.texts}
	p.parse(false)
	return string(p.out), p.stops
}

// snippetParser expands a snippet's body.
type snippetParser struct {
	body, indent, unit string
	vars               map[string]string
	texts              map[int]string // each stop's placeholder text, for where it repeats bare
	at                 int
	out                []byte
	stops              []Stop
}

// parse expands the body up to its end, or up to the } that ends a
// placeholder when nested.
func (p *snippetParser) parse(nested bool) {
	for p.at < len(p.body) {
		c := p.body[p.at]
		switch {
		case c == '\\' && p.at+1 < len(p.body) && strings.IndexByte(`$}\,|`, p.body[p.at+1]) >= 0:
			p.out = append(p.out, p.body[p.at+1])
			p.at += 2
		case c == '}' && nested:
			return
		case c == '$':
			p.at++
			p.dollar()
		case c == '\n':
			p.out = append(p.out, "\n"+p.indent...)
			p.at++
		case c == '\t':
			p.out = append(p.out, p.unit...)
			p.at++
		default:
			p.out = append(p.out, c)
			p.at++
		}
	}
}

// dollar expands what follows a $: a stop, a placeholder, a choice or a
// variable, or else the $ itself.
func (p *snippetParser) dollar() {
	braced := p.at < len(p.body) && p.body[p.at] == '{'
	if braced {
		p.at++
	}
	start := len(p.out)
	if n, ok := p.number(); ok {
		switch {
		case braced && p.peek(':'):
			p.at++
			p.parse(true)
			if _, ok := p.texts[n]; !ok {
				p.texts[n] = string(p.out[start:])
			}
		case braced && p.peek('|'):
			p.at++
			end := strings.Index(p.body[p.at:], "|}")
			if end < 0 {
				end = len(p.body) - p.at
			}
			choice, _, _ := strings.Cut(p.body[p.at:p.at+end], ",")
			p.out = append(p.out, choice...)
			p.at += end + 1
		default:
			p.out = append(p.out, p.texts[n]...)
		}
		p.stops = append(p.stops, Stop{n, start, len(p.out)})
	} else if name := p.name(); name != "" {
		stops := len(p.stops)
		if braced && p.peek(':') {
			p.at++
			p.parse(true)
		}
		// A variable in vars replaces its default.
		if value, ok := p.vars[name]; ok {
			p.out, p.stops = append(p.out[:start], value...), p.stops[:stops]
		}
	} else if !braced {
		p.out = append(p.out, '$')
		return
	}
	if braced && p.peek('}') {
		p.at++
	}
}

func (p *snippetParser) peek(c byte) bool { return p.at < len(p.body) && p.body[p.at] == c }

func (p *snippetParser) number() (int, bool) {
	end := p.at
	for end < len(p.body) && p.body[end] >= '0' && p.body[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(p.body[p.at:end])
	if err != nil {
		return 0, false
	}
	p.at = end
	return n, true
}

func (p *snippetParser) name() string {
	end := p.at
	for end < len(p.body) && (isWord(p.body[end]) && (end > p.at || p.body[end] > '9')) {
		end++
	}
	name := p.body[p.at:end]
	p.at = end
	return name
}

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
