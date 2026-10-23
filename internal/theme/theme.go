// Package theme is the colours fude draws in, for any frontend: the dark and
// light themes, a base16 scheme's, the colours config.toml sets over them,
// and the style each name of highlighted text takes (D33).
package theme

import (
	"strings"

	"github.com/y3owk1n/fude/wire"
)

// RGB is a colour as 0xRRGGBB, or None.
type RGB int32

// None is no colour: a style with it keeps the colour of the cell it draws
// over.
const None RGB = -1

// Theme is a theme's colour for each role.
type Theme struct {
	Bg, Surface, Raised, Border RGB // grounds, from the text's to a float's edge
	Text, Muted, Faint          RGB // text, from the most to the least prominent
	CursorLine, Selection       RGB
	Search                      RGB // search matches' ground
	Normal, Insert, Visual, Cmd RGB // each mode's colour
	Error, Warn, Info, Hint     RGB
	Keyword, String, Function   RGB
	Type, Number, Comment       RGB
	Punctuation                 RGB
}

// Themes are the themes by the names the theme setting takes.
var Themes = map[string]Theme{
	"dark": {
		Bg: 0x14161b, Surface: 0x1a1d24, Raised: 0x232733, Border: 0x2c313c,
		Text: 0xd5d9e0, Muted: 0x8f97a5, Faint: 0x4b5263,
		CursorLine: 0x1b1e25, Selection: 0x2b3a55, Search: 0xe3b341,
		Normal: 0x7aa7f5, Insert: 0x86c58a, Visual: 0xc49bf2, Cmd: 0xe5a56b,
		Error: 0xe8787a, Warn: 0xe3b341, Info: 0x7aa7f5, Hint: 0x6cc8bd,
		Keyword: 0xc49bf2, String: 0xa6d189, Function: 0x7ab3f0, Type: 0x6cc8bd,
		Number: 0xe5a56b, Comment: 0x6a7180, Punctuation: 0x8f97a5,
	},
	"light": {
		Bg: 0xf7f7f5, Surface: 0xffffff, Raised: 0xeceef1, Border: 0xd6d9df,
		Text: 0x1f2328, Muted: 0x5b6270, Faint: 0x9aa1ad,
		CursorLine: 0xeef0f3, Selection: 0xcfe0fb, Search: 0xa66a00,
		Normal: 0x2f6fd6, Insert: 0x2e8540, Visual: 0x8a4fd6, Cmd: 0xb85f12,
		Error: 0xc9363b, Warn: 0xa66a00, Info: 0x2f6fd6, Hint: 0x1d8a7e,
		Keyword: 0x8a4fd6, String: 0x2e7d3a, Function: 0x2f6fd6, Type: 0x1d7a70,
		Number: 0xa85a10, Comment: 0x7b8290, Punctuation: 0x5b6270,
	},
}

// FromBase16 is the theme a base16 scheme's colours, base00 to base0F, make,
// by the roles base16's styling guide gives them (B146).
func FromBase16(b [16]int32) Theme {
	c := func(i int) RGB { return RGB(b[i]) }
	return Theme{
		Bg: c(0x0), Surface: c(0x1), Raised: c(0x2), Border: c(0x2),
		Text: c(0x5), Muted: c(0x4), Faint: c(0x3),
		CursorLine: c(0x1), Selection: c(0x2), Search: c(0xA),
		Normal: c(0xD), Insert: c(0xB), Visual: c(0xE), Cmd: c(0x9),
		Error: c(0x8), Warn: c(0xA), Info: c(0xD), Hint: c(0xC),
		Keyword: c(0xE), String: c(0xB), Function: c(0xD), Type: c(0xA),
		Number: c(0x9), Comment: c(0x3), Punctuation: c(0x4),
	}
}

// With returns t with the colours over sets in place of its own (B294).
func (t Theme) With(over wire.Colors) Theme {
	for to, hex := range map[*RGB]wire.Hex{
		&t.Bg: over.Bg, &t.Surface: over.Surface, &t.Raised: over.Raised, &t.Border: over.Border,
		&t.Text: over.Text, &t.Muted: over.Muted, &t.Faint: over.Faint,
		&t.CursorLine: over.CursorLine, &t.Selection: over.Selection, &t.Search: over.Search,
		&t.Normal: over.Normal, &t.Insert: over.Insert, &t.Visual: over.Visual, &t.Cmd: over.Command,
		&t.Error: over.Error, &t.Warn: over.Warn, &t.Info: over.Info, &t.Hint: over.Hint,
		&t.Keyword: over.Keyword, &t.String: over.String, &t.Function: over.Function, &t.Type: over.Type,
		&t.Number: over.Number, &t.Comment: over.Comment, &t.Punctuation: over.Punctuation,
	} {
		if rgb, ok := hex.RGB(); ok {
			*to = RGB(rgb)
		}
	}
	return t
}

// Tint is t darkened for what shows under a modal box: its grounds darker,
// and every colour of text faded into the darker ground (B151).
func (t Theme) Tint() Theme {
	g := func(c RGB) RGB { return Mix(c, 0, 0.35) }
	bg := g(t.Bg)
	f := func(c RGB) RGB { return Mix(c, bg, 0.55) }
	return Theme{
		Bg: bg, Surface: g(t.Surface), Raised: g(t.Raised), Border: f(t.Border),
		Text: f(t.Text), Muted: f(t.Muted), Faint: f(t.Faint),
		CursorLine: g(t.CursorLine), Selection: g(t.Selection), Search: f(t.Search),
		Normal: f(t.Normal), Insert: f(t.Insert), Visual: f(t.Visual), Cmd: f(t.Cmd),
		Error: f(t.Error), Warn: f(t.Warn), Info: f(t.Info), Hint: f(t.Hint),
		Keyword: f(t.Keyword), String: f(t.String), Function: f(t.Function), Type: f(t.Type),
		Number: f(t.Number), Comment: f(t.Comment), Punctuation: f(t.Punctuation),
	}
}

// Mix returns the colour f of the way from a to b, for tints.
func Mix(a, b RGB, f float64) RGB {
	at := func(shift int) RGB {
		x, y := a>>shift&0xff, b>>shift&0xff
		return (x + RGB(float64(y-x)*f)) << shift
	}
	return at(16) | at(8) | at(0)
}

// Style is how text draws. Its underline is in Under while UnderSet, and
// else in the text's colour.
type Style struct {
	Fg, Bg                          RGB
	Bold, Italic, Underline, Strike bool
	Under                           RGB
	UnderSet                        bool
}

// Name returns the style of text called name in l: its own, or that of the
// name without its last part, so function.builtin falls back to function,
// or else one of no colours.
func (l Look) Name(name string) (Style, bool) {
	for {
		if s, ok := l.Names[name]; ok {
			return s, true
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			return Style{Fg: None, Bg: None}, false
		}
		name = name[:i]
	}
}

// Look is what drawing takes from a theme.
type Look struct {
	Theme Theme
	Base  Style // the text's colours on the ground, which every style starts from
	// Names are the styles of highlighted text by name. A name with no entry
	// takes the one for its name without its last part, so function.builtin
	// falls back to function.
	Names map[string]Style
	// Underlines are the colours diagnostics underline their text in, over its
	// other colours.
	Underlines map[string]RGB
	// Signs are the colours of signs and icons: diagnostics' as they underline,
	// git's in its add, change and delete colours, and icons' by colour name.
	Signs map[string]RGB
}

// LookOf returns the look t gives.
func LookOf(t Theme) Look {
	base := Style{Fg: t.Text, Bg: t.Bg}
	fg := func(c RGB) Style { s := base; s.Fg = c; return s }
	on := func(s Style, bg RGB) Style { s.Bg = bg; return s }
	bold := func(s Style) Style { s.Bold = true; return s }
	italic := func(s Style) Style { s.Italic = true; return s }
	under := func(s Style) Style { s.Underline = true; return s }
	strike := func(s Style) Style { s.Strike = true; return s }
	l := Look{Theme: t, Base: base}
	l.Names = map[string]Style{
		"comment": italic(fg(t.Comment)),
		"keyword": fg(t.Keyword),
		"string":  fg(t.String), "escape": fg(t.Type),
		"number": fg(t.Number), "constant": fg(t.Number),
		"function": fg(t.Function), "type": fg(t.Type),
		"punctuation": fg(t.Punctuation), "operator": fg(t.Punctuation),
		// The names other grammars give the same kinds of text.
		"boolean": fg(t.Number), "conditional": fg(t.Keyword), "repeat": fg(t.Keyword), "label": fg(t.Keyword),
		"preproc": fg(t.Keyword), "tag": fg(t.Keyword), "method": fg(t.Function), "constructor": fg(t.Type),
		"attribute": fg(t.Type), "property": fg(t.Type), "field": fg(t.Type),
		// The names CSS's grammar gives its at-rules.
		"charset": fg(t.Keyword), "import": fg(t.Keyword), "keyframes": fg(t.Keyword), "media": fg(t.Keyword),
		"namespace": fg(t.Keyword), "supports": fg(t.Keyword),
		// The names markdown's grammar uses.
		"text.title": bold(fg(t.Function)), "text.strong": bold(base), "text.emphasis": italic(base), "text.literal": fg(t.Type), "text.uri": under(base), "text.reference": fg(t.Function),
		// Markdown in floats, such as hover.
		"markup.strong": bold(base), "markup.italic": italic(base), "markup.heading": bold(base),
		"markup.raw": fg(t.Type), "markup.link": under(base), "markup.rule": fg(t.Faint),
		"diagnostic.error.text": fg(t.Error), "diagnostic.warn.text": fg(t.Warn), "diagnostic.info.text": fg(t.Info), "diagnostic.hint.text": fg(t.Hint),
		// A diagnostic's note after its line, in its colour on a tint of it.
		"diagnostic.error.note": on(fg(t.Error), Mix(t.Error, t.Bg, 0.85)), "diagnostic.warn.note": on(fg(t.Warn), Mix(t.Warn, t.Bg, 0.85)),
		"diagnostic.info.note": on(fg(t.Info), Mix(t.Info, t.Bg, 0.85)), "diagnostic.hint.note": on(fg(t.Hint), Mix(t.Hint, t.Bg, 0.85)),
		"menu.selected": on(base, t.Raised), "inlay": on(fg(t.Muted), t.Surface),
		// The references to the symbol under the cursor (B461), and a row of
		// code lenses above their line (B462).
		"reference": on(base, t.Selection), "lens": fg(t.Muted),
		"jump.match": on(base, t.Selection), "jump.label": bold(on(fg(t.Bg), t.Cmd)), "finder.dir": fg(t.Muted), "finder.file": bold(base), "snippet.placeholder": on(base, t.Selection),
		// Text a language server linked, which typing changes together (B473).
		"lsp.linked": on(base, t.Surface),
		// The letters of an item that match what was typed.
		"match": bold(fg(t.Normal)),
		// What the :s being typed replaces, and what with.
		"sub.old": strike(fg(t.Error)), "sub.new": fg(t.Insert),
		// Text just yanked, put, undone or redone.
		"flash": on(fg(t.Bg), t.Cmd),
		// A hunk's lines, as git's index has them and as they are.
		"diff.minus": fg(t.Error), "diff.plus": fg(t.Insert),
		// Diff mode's added and changed lines, the text changed in them, and
		// the rows that stand for lines another buffer has (B520).
		"diff.add": on(base, Mix(t.Insert, t.Bg, 0.8)), "diff.change": on(base, Mix(t.Normal, t.Bg, 0.85)),
		"diff.text": on(base, Mix(t.Normal, t.Bg, 0.6)), "diff.delete": on(fg(Mix(t.Error, t.Bg, 0.4)), Mix(t.Error, t.Bg, 0.85)),
		// The key hints: keys, groups of longer ones, counts, and the keys a
		// mapping types.
		"hints.key": bold(fg(t.Normal)), "hints.group": bold(fg(t.Keyword)),
		"hints.muted": fg(t.Muted), "hints.typed": fg(t.String),
		// A language server's state in the :lsp panel.
		"lsp.running": fg(t.Insert), "lsp.starting": fg(t.Warn), "lsp.error": fg(t.Error),
		// What a proposal would replace.
		"proposal": strike(fg(t.Faint)), "proposal.stale": strike(fg(t.Error)),
		// The line a debug session stopped on (B262).
		"debug.line": on(base, t.Selection),
		// The styles a rendered markdown file draws in: each heading level's
		// ground and icon, code blocks' ground, and the marks shown in place
		// of the text's (B298).
		"markdown.h1": on(fg(t.Function), Mix(t.Function, t.Bg, 0.82)), "markdown.h2": on(fg(t.Keyword), Mix(t.Keyword, t.Bg, 0.82)),
		"markdown.h3": on(fg(t.String), Mix(t.String, t.Bg, 0.82)), "markdown.h4": on(fg(t.Type), Mix(t.Type, t.Bg, 0.82)),
		"markdown.h5": on(fg(t.Number), Mix(t.Number, t.Bg, 0.82)), "markdown.h6": on(fg(t.Hint), Mix(t.Hint, t.Bg, 0.82)),
		"markdown.code": on(base, t.Surface), "markdown.bullet": fg(t.Normal), "markdown.quote": fg(t.Muted), "markdown.rule": fg(t.Faint),
		"markdown.checked": fg(t.Insert), "markdown.unchecked": fg(t.Muted), "markdown.table": fg(t.Muted),
	}
	l.Underlines = map[string]RGB{
		"diagnostic.error": t.Error, "diagnostic.warn": t.Warn, "diagnostic.info": t.Info, "diagnostic.hint": t.Hint,
		"spell.bad": t.Hint,
	}
	l.Signs = map[string]RGB{
		"diagnostic.error": t.Error, "diagnostic.warn": t.Warn, "diagnostic.info": t.Info, "diagnostic.hint": t.Hint,
		"git.add": t.Insert, "git.change": t.Normal, "git.delete": t.Error,
		"debug.break": t.Error, "debug.pending": t.Muted, "debug.stopped": t.Warn, "fold.column": t.Faint,
		"icon.azure": t.Function, "icon.blue": t.Normal, "icon.cyan": t.Hint, "icon.green": t.String,
		"icon.grey": t.Muted, "icon.orange": t.Number, "icon.purple": t.Keyword, "icon.red": t.Error,
		"icon.yellow": t.Warn,
	}
	return l
}

// Restyle puts the styles modules defined in l, over its own, each colour
// through fade (B345).
func (l Look) Restyle(own map[string]wire.Style, fade func(RGB) RGB) {
	for name, st := range own {
		out := l.Base
		if rgb, ok := st.Fg.RGB(); ok {
			out.Fg = fade(RGB(rgb))
		}
		if rgb, ok := st.Bg.RGB(); ok {
			out.Bg = fade(RGB(rgb))
		}
		out.Bold, out.Italic, out.Underline, out.Strike = st.Bold, st.Italic, st.Underline, st.Strike
		l.Names[name] = out
	}
}
