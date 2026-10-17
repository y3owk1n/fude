package editor

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// tabStop is where a snippet's stop n is in its expanded text.
type tabStop struct{ n, start, end int }

// expandSnippet returns body, a snippet in the LSP syntax, with each
// placeholder's text, the first of each choice, and each variable's value
// or default in place, and where its stops are. A stop that repeats shows
// its placeholder's text everywhere. Lines after the first start with
// indent, and a tab is unit (B207).
func expandSnippet(body, indent, unit string, vars map[string]string) (string, []tabStop) {
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
	stops              []tabStop
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
		p.stops = append(p.stops, tabStop{n, start, len(p.out)})
	} else if name := p.name(); name != "" {
		stops := len(p.stops)
		if braced && p.peek(':') {
			p.at++
			p.parse(true)
		}
		// A variable fude knows replaces its default.
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

// snippetVars are the variables a snippet can use, for the file of b with
// the cursor at c.
func (e *Session) snippetVars(b *buffer, c int) map[string]string {
	t, now := b.text.Snapshot(), e.now()
	line := t.LineOf(c)
	ls := t.LineStart(line)
	path, _ := filepath.Abs(b.file.path)
	name := filepath.Base(b.file.path)
	return map[string]string{
		"TM_FILENAME": name, "TM_FILENAME_BASE": strings.TrimSuffix(name, filepath.Ext(name)),
		"TM_FILEPATH": path, "TM_DIRECTORY": filepath.Dir(path),
		"TM_LINE_INDEX": strconv.Itoa(line), "TM_LINE_NUMBER": strconv.Itoa(line + 1),
		"TM_CURRENT_LINE": t.Slice(ls, vim.LineEnd(t, ls)), "TM_SELECTED_TEXT": "",
		"CURRENT_YEAR": now.Format("2006"), "CURRENT_MONTH": now.Format("01"), "CURRENT_DATE": now.Format("02"),
		"CURRENT_HOUR": now.Format("15"), "CURRENT_MINUTE": now.Format("04"), "CURRENT_SECOND": now.Format("05"),
		"CURRENT_DAY_NAME": now.Weekday().String(), "CURRENT_MONTH_NAME": now.Month().String(),
		"CURRENT_SECONDS_UNIX": strconv.FormatInt(now.Unix(), 10),
	}
}

// snippetFile is a parsed snippets file with the modification time and
// size it had, so fude reads it again only after it changes.
type snippetFile struct {
	mod      time.Time
	size     int64
	snippets map[string]struct {
		Prefix      json.RawMessage `json:"prefix"`
		Body        json.RawMessage `json:"body"`
		Description string          `json:"description"`
	}
}

// userSnippets returns the snippets of filetype ft and of all filetypes
// in fude's snippets directory, in VS Code's format, whose prefix starts
// with typed, as completion items of their body (B207).
func (ed *Editor) userSnippets(ft, typed string) ([]item, error) {
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return nil, fmt.Errorf("find snippets: %w", err)
	}
	var out []item
	for _, name := range []string{ft, "all"} {
		if name == "" {
			continue
		}
		path := filepath.Join(dir, "fude", "snippets", name+".json")
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read snippets: %w", err)
		}
		f, ok := ed.snippets[path]
		if !ok || !f.mod.Equal(info.ModTime()) || f.size != info.Size() {
			src, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read snippets: %w", err)
			}
			f = snippetFile{mod: info.ModTime(), size: info.Size()}
			if err := json.Unmarshal(src, &f.snippets); err != nil {
				return nil, fmt.Errorf("read snippets %s: %w", path, err)
			}
			if ed.snippets == nil {
				ed.snippets = map[string]snippetFile{}
			}
			ed.snippets[path] = f
		}
		file := f.snippets
		for _, title := range slices.Sorted(maps.Keys(file)) {
			s := file[title]
			body := strings.Join(oneOrMany(s.Body), "\n")
			for _, prefix := range oneOrMany(s.Prefix) {
				if typed != "" && strings.HasPrefix(prefix, typed) {
					doc := strings.TrimSpace(s.Description + "\n```" + ft + "\n" + body + "\n```")
					out = append(out, item{label: prefix, text: body, doc: doc, snippet: true})
				}
			}
		}
	}
	return out, nil
}

// oneOrMany reads a string or a list of strings, as a snippet's prefix and
// body may be.
func oneOrMany(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	json.Unmarshal(raw, &many)
	return many
}

// snippet is a snippet being filled in: each of its stops, in the order
// <Tab> visits them, with every place it shows, the first being the one the
// cursor goes to (B207).
type snippet struct {
	b     *buffer
	stops [][][2]kernel.MarkID
	at    int  // the stop the cursor is in
	fresh bool // its placeholder is as the snippet put it, so typing replaces it
}

// startSnippet starts filling in a snippet whose text, expanded with
// stops, starts at at in the current buffer, at its first stop. Stop 0, or
// else the snippet's end, comes last.
func (e *Session) startSnippet(at int, text string, stops []tabStop) {
	b := e.win.buf
	var ns []int
	for _, s := range stops {
		if !slices.Contains(ns, s.n) {
			ns = append(ns, s.n)
		}
	}
	slices.SortFunc(ns, func(x, y int) int {
		// 0 comes after every other stop.
		if x == 0 || y == 0 {
			return cmp.Compare(y, x)
		}
		return cmp.Compare(x, y)
	})
	if !slices.Contains(ns, 0) {
		ns = append(ns, 0)
		stops = append(stops, tabStop{0, len(text), len(text)})
	}
	sn := &snippet{b: b}
	for _, n := range ns {
		var places [][2]kernel.MarkID
		for _, s := range stops {
			if s.n == n {
				places = append(places, [2]kernel.MarkID{b.text.SetMark(at+s.start, kernel.Left), b.text.SetMark(at+s.end, kernel.Right)})
			}
		}
		sn.stops = append(sn.stops, places)
	}
	e.endSnippet()
	e.snip = sn
	e.snipJump(0)
}

// snipJump moves the cursor d stops on in the snippet, and reports whether it
// did. The last stop ends the snippet, with the cursor there.
func (e *Session) snipJump(d int) bool {
	sn := e.snip
	i := sn.at + d
	if i < 0 {
		return false
	}
	start, end := sn.place(i, 0)
	e.setCursor(start)
	sn.at, sn.fresh = i, end > start
	if i == len(sn.stops)-1 {
		e.endSnippet()
	}
	return true
}

// place returns where the j-th place of stop i is.
func (sn *snippet) place(i, j int) (int, int) {
	p := sn.stops[i][j]
	return sn.b.text.Mark(p[0]), sn.b.text.Mark(p[1])
}

// snippetKey handles k for the snippet being filled in, before the vim
// machine sees it, and reports whether it took it. <Tab> and <S-Tab> go to
// the next and the stop before, and a key that types replaces a fresh
// placeholder.
func (e *Session) snippetKey(k vim.Key) bool {
	sn := e.snip
	if sn == nil || e.vim.Mode() != vim.Insert || e.win.buf != sn.b {
		return false
	}
	switch k {
	case "\t":
		return e.snipJump(1)
	case "<S-Tab>":
		return e.snipJump(-1)
	}
	if sn.fresh && (k == "<BS>" || k == "<Del>" || !strings.HasPrefix(string(k), "<") || k == "<") {
		sn.fresh = false
		start, end := sn.place(sn.at, 0)
		if err := (host{e}).Apply([]vim.Edit{{Start: start, End: end}}, "snippet"); err != nil {
			e.message = err.Error()
		}
		e.setCursor(start)
		return k == "<BS>" || k == "<Del>"
	}
	return false
}

// snippetAfter follows a key: the snippet ends when insert mode does or
// the cursor leaves its stop, and otherwise every other place of the stop
// takes the text of the first (B207).
func (e *Session) snippetAfter() {
	sn := e.snip
	if sn == nil {
		return
	}
	start, end := sn.place(sn.at, 0)
	if c := e.Cursor(); e.vim.Mode() != vim.Insert || e.win.buf != sn.b || c < start || c > end {
		e.endSnippet()
		return
	}
	t := sn.b.text.Snapshot()
	text := t.Slice(start, end)
	var edits []vim.Edit
	for j := 1; j < len(sn.stops[sn.at]); j++ {
		if s, end := sn.place(sn.at, j); t.Slice(s, end) != text {
			edits = append(edits, vim.Edit{Start: s, End: end, Text: text})
		}
	}
	slices.SortFunc(edits, func(x, y vim.Edit) int { return x.Start - y.Start })
	if len(edits) > 0 {
		if err := (host{e}).Apply(edits, "snippet"); err != nil {
			e.message = err.Error()
		}
	}
}

// endSnippet stops filling in the snippet and lets its marks go.
func (e *Session) endSnippet() {
	sn := e.snip
	if sn == nil {
		return
	}
	for _, places := range sn.stops {
		for _, p := range places {
			sn.b.text.DeleteMark(p[0])
			sn.b.text.DeleteMark(p[1])
		}
	}
	e.snip = nil
}

// snippetSpan is the fresh placeholder the cursor is on, drawn selected.
func (e *Session) snippetSpan(b *buffer) []syntax.Span {
	sn := e.snip
	if sn == nil || !sn.fresh || sn.b != b {
		return nil
	}
	start, end := sn.place(sn.at, 0)
	return []syntax.Span{{Start: start, End: end, Name: "snippet.placeholder"}}
}
