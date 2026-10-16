package vim

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode"
)

// pattern is a Vim pattern translated to Go's regexp (N1).
type pattern struct {
	re *regexp.Regexp
	// lit is the text a pattern of plain characters matches, which a search
	// finds without the regexp. fold means it ignores ASCII case, and words
	// means it matches only between word edges, as \<word\> does.
	lit         string
	fold, words bool
	// spans are the Go groups that \zs and \ze mark, one per branch that
	// uses them. A match without one is the whole match.
	spans []int
	// groups maps Vim's \1 to \9 to Go group numbers.
	groups [10]int
}

// classes are the backslash classes and their Go equivalents.
var classes = map[byte]string{
	's': `[ \t]`, 'S': `[^ \t\n]`, 'd': `[0-9]`, 'D': `[^0-9\n]`,
	'w': `[0-9A-Za-z_]`, 'W': `[^0-9A-Za-z_\n]`, 'a': `[A-Za-z]`, 'A': `[^A-Za-z\n]`,
	'l': `[a-z]`, 'L': `[^a-z\n]`, 'u': `[A-Z]`, 'U': `[^A-Z\n]`,
	'x': `[0-9A-Fa-f]`, 'X': `[^0-9A-Fa-f\n]`, 'h': `[A-Za-z_]`, 'H': `[^A-Za-z_\n]`,
}

// escapes are the backslash characters that stand for one character.
var escapes = map[byte]string{'n': `\n`, 't': `\t`, 'e': `\x1b`, 'r': `\r`}

// token is one atom of a Vim pattern: a character, or a backslash and one.
type token struct {
	s       string
	escaped bool
}

// compile translates a Vim pattern to Go's regexp. ~ stands for last, the
// previous replacement. It ignores case when ignore is set, unless \C asks
// for case, and always when \c asks for it. Atoms that need backtracking
// are refused.
func compile(src, last string, ignore bool) (*pattern, error) {
	var toks []token
	for i := 0; i < len(src); i++ {
		if src[i] == '\\' && i+1 < len(src) {
			toks = append(toks, token{src[i+1 : i+2], true})
			i++
			continue
		}
		toks = append(toks, token{src[i : i+1], false})
	}
	c := &compiler{toks: toks, mode: 'm', last: last}
	body, err := c.branches()
	if err != nil {
		return nil, err
	}
	flags := "(?m)"
	if c.caseFlag == 'c' || ignore && c.caseFlag != 'C' {
		flags = "(?mi)"
	}
	re, err := regexp.Compile(flags + body)
	if err != nil {
		return nil, fmt.Errorf("bad pattern %s: %w", src, err)
	}
	p := &pattern{re: re}
	p.lit, p.fold, p.words = literal(flags + body)
	for i, name := range re.SubexpNames() {
		if strings.HasPrefix(name, "zs") {
			p.spans = append(p.spans, i)
		} else if k, err := strconv.Atoi(strings.TrimPrefix(name, "g")); err == nil && k < len(p.groups) {
			p.groups[k] = i
		}
	}
	return p, nil
}

type compiler struct {
	toks     []token
	i        int
	mode     byte // v, m, M or V: very magic, magic, nomagic, very nomagic
	last     string
	caseFlag byte // c or C from \c or \C, \c winning, or 0
	groups   int  // Vim capturing groups opened so far
	spans    int  // \zs and \ze groups so far
}

// special reports whether t is an operator in the current magic mode.
func (c *compiler) special(t token) bool {
	switch {
	case strings.Contains(".*[~", t.s):
		return t.escaped == (c.mode == 'M' || c.mode == 'V')
	case strings.Contains("+?={()|<>@%", t.s):
		return t.escaped == (c.mode != 'v')
	}
	return false
}

// branches translates the whole pattern, splitting it at the top-level
// alternatives so each can carry its own \zs and \ze.
func (c *compiler) branches() (string, error) {
	var out []string
	for {
		b, err := c.sequence(true)
		if err != nil {
			return "", err
		}
		out = append(out, b)
		if c.i >= len(c.toks) {
			return strings.Join(out, "|"), nil
		}
		if c.toks[c.i].s == ")" {
			return "", errors.New(`unmatched \)`)
		}
		c.i++ // the |
	}
}

// sequence translates atoms up to an alternative or a closing group, or the
// end. At the top level it also places \zs and \ze.
func (c *compiler) sequence(top bool) (string, error) {
	var b strings.Builder
	zs, ze := -1, -1 // where \zs and \ze fall in b
	start := c.i
	for c.i < len(c.toks) {
		t := c.toks[c.i]
		if c.special(t) && (t.s == "|" || t.s == ")") {
			break
		}
		c.i++
		if t.escaped && t.s == "z" && c.i < len(c.toks) && top {
			switch c.toks[c.i].s {
			case "s":
				c.i++
				zs = b.Len()
				continue
			case "e":
				c.i++
				ze = b.Len()
				continue
			}
		}
		s, err := c.atom(t, c.i-1 == start)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	out := b.String()
	if zs < 0 && ze < 0 {
		return out, nil
	}
	if zs < 0 {
		zs = 0
	}
	if ze < 0 {
		ze = len(out)
	}
	c.spans++
	return fmt.Sprintf("%s(?P<zs%d>%s)%s", out[:zs], c.spans, out[zs:ze], out[ze:]), nil
}

// atom translates one token, reading more for groups, braces and brackets.
func (c *compiler) atom(t token, first bool) (string, error) {
	if !t.escaped || c.special(t) {
		return c.operator(t, first)
	}
	switch x := t.s[0]; {
	case strings.IndexByte("vmMV", x) >= 0:
		c.mode = x
		return "", nil
	case x == 'c', x == 'C':
		if c.caseFlag != 'c' {
			c.caseFlag = x
		}
		return "", nil
	case x == '_':
		return c.newlineClass()
	case x >= '1' && x <= '9', x == '&', x == 'z':
		return "", fmt.Errorf(`\%c needs backtracking, which Go's regexp cannot do`, x)
	case classes[x] != "":
		return classes[x], nil
	case escapes[x] != "":
		return escapes[x], nil
	}
	return regexp.QuoteMeta(t.s), nil
}

// operator translates a token that is special in the current mode, or a
// literal that is not.
func (c *compiler) operator(t token, first bool) (string, error) {
	switch t.s {
	case "^":
		if first {
			return "^", nil
		}
	case "$":
		if c.i == len(c.toks) || c.special(c.toks[c.i]) && (c.toks[c.i].s == "|" || c.toks[c.i].s == ")") {
			return "$", nil
		}
	}
	if !c.special(t) {
		return regexp.QuoteMeta(t.s), nil
	}
	switch t.s {
	case ".", "*", "+", "?":
		return t.s, nil
	case "=":
		return "?", nil
	case "~":
		return regexp.QuoteMeta(c.last), nil
	case "<", ">":
		return `\b`, nil
	case "{":
		return c.brace()
	case "[":
		return c.bracket(), nil
	case "(":
		return c.group("(")
	case "%":
		if c.i < len(c.toks) && c.toks[c.i].s == "(" {
			c.i++
			return c.group("(?:")
		}
	}
	return "", fmt.Errorf(`\%s needs backtracking, which Go's regexp cannot do`, t.s)
}

// group translates a group up to its closing paren.
func (c *compiler) group(open string) (string, error) {
	if open == "(" {
		c.groups++
		open = fmt.Sprintf("(?P<g%d>", c.groups)
	}
	var alts []string
	for {
		s, err := c.sequence(false)
		if err != nil {
			return "", err
		}
		alts = append(alts, s)
		if c.i >= len(c.toks) {
			return "", errors.New(`unmatched \(`)
		}
		closing := c.toks[c.i].s == ")"
		c.i++
		if closing {
			return open + strings.Join(alts, "|") + ")", nil
		}
	}
}

// brace translates a {n,m} count, where a leading - makes it lazy.
func (c *compiler) brace() (string, error) {
	var b strings.Builder
	for c.i < len(c.toks) && c.toks[c.i].s != "}" {
		b.WriteString(c.toks[c.i].s)
		c.i++
	}
	if c.i == len(c.toks) {
		return "", errors.New(`unclosed \{`)
	}
	c.i++
	body, lazy := strings.CutPrefix(b.String(), "-")
	q := "{" + body + "}"
	switch {
	case body == "" || body == ",":
		q = "*"
	case strings.HasPrefix(body, ","):
		q = "{0" + body + "}"
	}
	if lazy {
		q += "?"
	}
	return q, nil
}

// bracket copies a [] class, or a lone [ as a literal.
func (c *compiler) bracket() string {
	var b strings.Builder
	b.WriteString("[")
	j := c.i
	if j < len(c.toks) && c.toks[j].s == "^" {
		b.WriteString("^")
		j++
	}
	if j < len(c.toks) && c.toks[j].s == "]" && !c.toks[j].escaped {
		b.WriteString(`\]`)
		j++
	}
	for ; j < len(c.toks); j++ {
		t := c.toks[j]
		switch {
		case t.s == "]" && !t.escaped:
			c.i = j + 1
			return b.String() + "]"
		case t.escaped && escapes[t.s[0]] != "":
			b.WriteString(escapes[t.s[0]])
		case t.escaped:
			b.WriteString(`\` + t.s)
		case t.s == "[" && j+1 < len(c.toks) && c.toks[j+1].s == ":":
			// A POSIX class such as [:alpha:] runs to its own :].
			for j < len(c.toks) && (c.toks[j].s != "]" || c.toks[j-1].s != ":") {
				b.WriteString(c.toks[j].s)
				j++
			}
			b.WriteString("]")
		case t.s == "[" || t.s == `\`:
			b.WriteString(`\` + t.s)
		default:
			b.WriteString(t.s)
		}
	}
	return `\[`
}

// newlineClass translates \_ followed by a class, which also matches a line
// break.
func (c *compiler) newlineClass() (string, error) {
	if c.i >= len(c.toks) {
		return "", errors.New(`\_ needs a class after it`)
	}
	x := c.toks[c.i].s[0]
	c.i++
	switch {
	case x == '.':
		return `(?s:.)`, nil
	case x == '^':
		return "^", nil
	case x == '$':
		return "$", nil
	case classes[x] != "":
		cls := classes[x]
		return cls[:len(cls)-1] + `\n]`, nil
	}
	return "", fmt.Errorf(`unknown class \_%c`, x)
}

// literal returns the text the Go pattern expr matches when expr is plain
// characters, alone or between word edges, and whether it folds case. It
// returns "" for any other pattern, and when expr folds the case of a letter
// outside ASCII.
func literal(expr string) (lit string, fold, words bool) {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return "", false, false
	}
	if re.Op == syntax.OpConcat && len(re.Sub) == 3 && re.Sub[0].Op == syntax.OpWordBoundary && re.Sub[2].Op == syntax.OpWordBoundary {
		re, words = re.Sub[1], true
	}
	if re.Op != syntax.OpLiteral {
		return "", false, false
	}
	lit, fold = string(re.Rune), re.Flags&syntax.FoldCase != 0
	for _, r := range lit {
		if fold && r > unicode.MaxASCII {
			return "", false, false
		}
	}
	if fold {
		lit = strings.ToLower(lit)
	}
	return lit, fold, words
}

// all returns every match in text, each as Go submatch indexes.
func (p *pattern) all(text string) [][]int {
	return p.re.FindAllStringSubmatchIndex(text, -1)
}

// span returns where match m starts and ends after \zs and \ze.
func (p *pattern) span(m []int) (int, int) {
	for _, g := range p.spans {
		if m[2*g] >= 0 {
			return m[2*g], m[2*g+1]
		}
	}
	return m[0], m[1]
}
