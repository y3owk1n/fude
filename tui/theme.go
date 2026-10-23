package tui

import (
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/y3owk1n/fude/wire"
)

// theme is the colours the terminal client draws with, in true colour, as
// the theme setting picks them (D33).
type theme struct {
	bg, surface, raised, border color.Color // grounds, from the text's to a float's edge
	text, muted, faint          color.Color // text, from the most to the least prominent
	cursorLine, selection       color.Color
	search                      color.Color // search matches' ground
	normal, insert, visual, cmd color.Color // each mode's colour
	err, warn, info, hint       color.Color
	keyword, str, function, typ color.Color
	number, comment, punct      color.Color
}

// themes are the themes by the names the theme setting takes.
var themes = map[string]theme{
	"dark": {
		bg: color.NewHexColor(0x14161b), surface: color.NewHexColor(0x1a1d24), raised: color.NewHexColor(0x232733), border: color.NewHexColor(0x2c313c),
		text: color.NewHexColor(0xd5d9e0), muted: color.NewHexColor(0x8f97a5), faint: color.NewHexColor(0x4b5263),
		cursorLine: color.NewHexColor(0x1b1e25), selection: color.NewHexColor(0x2b3a55), search: color.NewHexColor(0xe3b341),
		normal: color.NewHexColor(0x7aa7f5), insert: color.NewHexColor(0x86c58a), visual: color.NewHexColor(0xc49bf2), cmd: color.NewHexColor(0xe5a56b),
		err: color.NewHexColor(0xe8787a), warn: color.NewHexColor(0xe3b341), info: color.NewHexColor(0x7aa7f5), hint: color.NewHexColor(0x6cc8bd),
		keyword: color.NewHexColor(0xc49bf2), str: color.NewHexColor(0xa6d189), function: color.NewHexColor(0x7ab3f0), typ: color.NewHexColor(0x6cc8bd),
		number: color.NewHexColor(0xe5a56b), comment: color.NewHexColor(0x6a7180), punct: color.NewHexColor(0x8f97a5),
	},
	"light": {
		bg: color.NewHexColor(0xf7f7f5), surface: color.NewHexColor(0xffffff), raised: color.NewHexColor(0xeceef1), border: color.NewHexColor(0xd6d9df),
		text: color.NewHexColor(0x1f2328), muted: color.NewHexColor(0x5b6270), faint: color.NewHexColor(0x9aa1ad),
		cursorLine: color.NewHexColor(0xeef0f3), selection: color.NewHexColor(0xcfe0fb), search: color.NewHexColor(0xa66a00),
		normal: color.NewHexColor(0x2f6fd6), insert: color.NewHexColor(0x2e8540), visual: color.NewHexColor(0x8a4fd6), cmd: color.NewHexColor(0xb85f12),
		err: color.NewHexColor(0xc9363b), warn: color.NewHexColor(0xa66a00), info: color.NewHexColor(0x2f6fd6), hint: color.NewHexColor(0x1d8a7e),
		keyword: color.NewHexColor(0x8a4fd6), str: color.NewHexColor(0x2e7d3a), function: color.NewHexColor(0x2f6fd6), typ: color.NewHexColor(0x1d7a70),
		number: color.NewHexColor(0xa85a10), comment: color.NewHexColor(0x7b8290), punct: color.NewHexColor(0x5b6270),
	},
}

// fromBase16 is the theme a base16 scheme's colours, base00 to base0F, make,
// by the roles base16's styling guide gives them (B146).
func fromBase16(b [16]int32) theme {
	c := func(i int) color.Color { return color.NewHexColor(b[i]) }
	return theme{
		bg: c(0x0), surface: c(0x1), raised: c(0x2), border: c(0x2),
		text: c(0x5), muted: c(0x4), faint: c(0x3),
		cursorLine: c(0x1), selection: c(0x2), search: c(0xA),
		normal: c(0xD), insert: c(0xB), visual: c(0xE), cmd: c(0x9),
		err: c(0x8), warn: c(0xA), info: c(0xD), hint: c(0xC),
		keyword: c(0xE), str: c(0xB), function: c(0xD), typ: c(0xA),
		number: c(0x9), comment: c(0x3), punct: c(0x4),
	}
}

// with returns t with the colours over sets in place of its own (B294).
func (t theme) with(over wire.Colors) theme {
	for to, hex := range map[*color.Color]wire.Hex{
		&t.bg: over.Bg, &t.surface: over.Surface, &t.raised: over.Raised, &t.border: over.Border,
		&t.text: over.Text, &t.muted: over.Muted, &t.faint: over.Faint,
		&t.cursorLine: over.CursorLine, &t.selection: over.Selection, &t.search: over.Search,
		&t.normal: over.Normal, &t.insert: over.Insert, &t.visual: over.Visual, &t.cmd: over.Command,
		&t.err: over.Error, &t.warn: over.Warn, &t.info: over.Info, &t.hint: over.Hint,
		&t.keyword: over.Keyword, &t.str: over.String, &t.function: over.Function, &t.typ: over.Type,
		&t.number: over.Number, &t.comment: over.Comment, &t.punct: over.Punctuation,
	} {
		if rgb, ok := hex.RGB(); ok {
			*to = color.NewHexColor(rgb)
		}
	}
	return t
}

// styles are what drawing takes from a theme.
type styles struct {
	th   theme
	base tcell.Style // the text's colours on the ground, which every style starts from
	// colours are the styles of highlight captures. A capture with no entry
	// takes the one for its name without its last part, so function.builtin
	// falls back to function.
	colours map[string]tcell.Style
	// underlines are the colours diagnostics underline their text in, over its
	// other colours.
	underlines map[string]color.Color
	// signColours are the colours of signs and icons: diagnostics' as they
	// underline, git's in its add, change and delete colours, and icons' by
	// colour name.
	signColours map[string]color.Color
}

// The styles drawing takes now, which apply sets, from the theme in use and
// its tint under a modal box.
var (
	th          theme
	base        tcell.Style
	colours     map[string]tcell.Style
	underlines  map[string]color.Color
	signColours map[string]color.Color
	lit, dimmed styles
	// boxed is lit on dimmed's ground, for what draws over the windows
	// dimmed under a modal box (B151).
	boxed styles
)

func init() { useTheme(themes["dark"], nil) }

// useTheme draws with t, and the styles modules defined over its own,
// from then on.
func useTheme(t theme, own map[string]wire.Style) {
	lit, dimmed = build(t), build(tint(t))
	ground := t
	ground.bg = dimmed.th.bg
	boxed = build(ground)
	same := func(c color.Color) color.Color { return c }
	restyle(lit, own, same)
	restyle(boxed, own, same)
	restyle(dimmed, own, func(c color.Color) color.Color { return mix(c, dimmed.th.bg, 0.55) })
	apply(lit)
}

// restyle puts the styles modules defined in s, over its own, each colour
// through fade (B345).
func restyle(s styles, own map[string]wire.Style, fade func(color.Color) color.Color) {
	for name, st := range own {
		out := s.base
		if rgb, ok := st.Fg.RGB(); ok {
			out = out.Foreground(fade(color.NewHexColor(rgb)))
		}
		if rgb, ok := st.Bg.RGB(); ok {
			out = out.Background(fade(color.NewHexColor(rgb)))
		}
		s.colours[name] = out.Bold(st.Bold).Italic(st.Italic).Underline(st.Underline).StrikeThrough(st.Strike)
	}
}

// apply makes drawing take s.
func apply(s styles) {
	th, base, colours, underlines, signColours = s.th, s.base, s.colours, s.underlines, s.signColours
}

// tint is t darkened for what shows under a modal box: its grounds darker,
// and every colour of text faded into the darker ground (B151).
func tint(t theme) theme {
	black := color.NewRGBColor(0, 0, 0)
	g := func(c color.Color) color.Color { return mix(c, black, 0.35) }
	bg := g(t.bg)
	f := func(c color.Color) color.Color { return mix(c, bg, 0.55) }
	return theme{
		bg: bg, surface: g(t.surface), raised: g(t.raised), border: f(t.border),
		text: f(t.text), muted: f(t.muted), faint: f(t.faint),
		cursorLine: g(t.cursorLine), selection: g(t.selection), search: f(t.search),
		normal: f(t.normal), insert: f(t.insert), visual: f(t.visual), cmd: f(t.cmd),
		err: f(t.err), warn: f(t.warn), info: f(t.info), hint: f(t.hint),
		keyword: f(t.keyword), str: f(t.str), function: f(t.function), typ: f(t.typ),
		number: f(t.number), comment: f(t.comment), punct: f(t.punct),
	}
}

func build(t theme) styles {
	base := tcell.StyleDefault.Background(t.bg).Foreground(t.text)
	fg := func(c color.Color) tcell.Style { return base.Foreground(c) }
	s := styles{th: t, base: base}
	s.colours = map[string]tcell.Style{
		"comment": fg(t.comment).Italic(true),
		"keyword": fg(t.keyword),
		"string":  fg(t.str), "escape": fg(t.typ),
		"number": fg(t.number), "constant": fg(t.number),
		"function": fg(t.function), "type": fg(t.typ),
		"punctuation": fg(t.punct), "operator": fg(t.punct),
		// The names other grammars give the same kinds of text.
		"boolean": fg(t.number), "conditional": fg(t.keyword), "repeat": fg(t.keyword), "label": fg(t.keyword),
		"preproc": fg(t.keyword), "tag": fg(t.keyword), "method": fg(t.function), "constructor": fg(t.typ),
		"attribute": fg(t.typ), "property": fg(t.typ), "field": fg(t.typ),
		// The names CSS's grammar gives its at-rules.
		"charset": fg(t.keyword), "import": fg(t.keyword), "keyframes": fg(t.keyword), "media": fg(t.keyword),
		"namespace": fg(t.keyword), "supports": fg(t.keyword),
		// The names markdown's grammar uses.
		"text.title": fg(t.function).Bold(true), "text.strong": base.Bold(true), "text.emphasis": base.Italic(true), "text.literal": fg(t.typ), "text.uri": base.Underline(true), "text.reference": fg(t.function),
		// Markdown in floats, such as hover.
		"markup.strong": base.Bold(true), "markup.italic": base.Italic(true), "markup.heading": base.Bold(true),
		"markup.raw": fg(t.typ), "markup.link": base.Underline(true), "markup.rule": fg(t.faint),
		"diagnostic.error.text": fg(t.err), "diagnostic.warn.text": fg(t.warn), "diagnostic.info.text": fg(t.info), "diagnostic.hint.text": fg(t.hint),
		// A diagnostic's note after its line, in its colour on a tint of it.
		"diagnostic.error.note": fg(t.err).Background(mix(t.err, t.bg, 0.85)), "diagnostic.warn.note": fg(t.warn).Background(mix(t.warn, t.bg, 0.85)),
		"diagnostic.info.note": fg(t.info).Background(mix(t.info, t.bg, 0.85)), "diagnostic.hint.note": fg(t.hint).Background(mix(t.hint, t.bg, 0.85)),
		"menu.selected": base.Background(t.raised), "inlay": fg(t.muted).Background(t.surface),
		// The references to the symbol under the cursor (B461), and a row of
		// code lenses above their line (B462).
		"reference": base.Background(t.selection), "lens": fg(t.muted),
		"jump.match": base.Background(t.selection), "jump.label": base.Background(t.cmd).Foreground(t.bg).Bold(true), "finder.dir": fg(t.muted), "finder.file": base.Bold(true), "snippet.placeholder": base.Background(t.selection),
		// Text a language server linked, which typing changes together (B473).
		"lsp.linked": base.Background(t.surface),
		// The letters of an item that match what was typed.
		"match": fg(t.normal).Bold(true),
		// What the :s being typed replaces, and what with.
		"sub.old": fg(t.err).StrikeThrough(true), "sub.new": fg(t.insert),
		// Text just yanked, put, undone or redone.
		"flash": base.Background(t.cmd).Foreground(t.bg),
		// A hunk's lines, as git's index has them and as they are.
		"diff.minus": fg(t.err), "diff.plus": fg(t.insert),
		// Diff mode's added and changed lines, the text changed in them, and
		// the rows that stand for lines another buffer has (B520).
		"diff.add": base.Background(mix(t.insert, t.bg, 0.8)), "diff.change": base.Background(mix(t.normal, t.bg, 0.85)),
		"diff.text": base.Background(mix(t.normal, t.bg, 0.6)), "diff.delete": fg(mix(t.err, t.bg, 0.4)).Background(mix(t.err, t.bg, 0.85)),
		// The key hints: keys, groups of longer ones, counts, and the keys a
		// mapping types.
		"hints.key": fg(t.normal).Bold(true), "hints.group": fg(t.keyword).Bold(true),
		"hints.muted": fg(t.muted), "hints.typed": fg(t.str),
		// A language server's state in the :lsp panel.
		"lsp.running": fg(t.insert), "lsp.starting": fg(t.warn), "lsp.error": fg(t.err),
		// What a proposal would replace.
		"proposal": fg(t.faint).StrikeThrough(true), "proposal.stale": fg(t.err).StrikeThrough(true),
		// The line a debug session stopped on (B262).
		"debug.line": base.Background(t.selection),
		// The styles a rendered markdown file draws in: each heading level's
		// ground and icon, code blocks' ground, and the marks shown in place
		// of the text's (B298).
		"markdown.h1": fg(t.function).Background(mix(t.function, t.bg, 0.82)), "markdown.h2": fg(t.keyword).Background(mix(t.keyword, t.bg, 0.82)),
		"markdown.h3": fg(t.str).Background(mix(t.str, t.bg, 0.82)), "markdown.h4": fg(t.typ).Background(mix(t.typ, t.bg, 0.82)),
		"markdown.h5": fg(t.number).Background(mix(t.number, t.bg, 0.82)), "markdown.h6": fg(t.hint).Background(mix(t.hint, t.bg, 0.82)),
		"markdown.code": base.Background(t.surface), "markdown.bullet": fg(t.normal), "markdown.quote": fg(t.muted), "markdown.rule": fg(t.faint),
		"markdown.checked": fg(t.insert), "markdown.unchecked": fg(t.muted), "markdown.table": fg(t.muted),
	}
	s.underlines = map[string]color.Color{
		"diagnostic.error": t.err, "diagnostic.warn": t.warn, "diagnostic.info": t.info, "diagnostic.hint": t.hint,
		"spell.bad": t.hint,
	}
	s.signColours = map[string]color.Color{
		"diagnostic.error": t.err, "diagnostic.warn": t.warn, "diagnostic.info": t.info, "diagnostic.hint": t.hint,
		"git.add": t.insert, "git.change": t.normal, "git.delete": t.err,
		"debug.break": t.err, "debug.pending": t.muted, "debug.stopped": t.warn, "fold.column": t.faint,
		"icon.azure": t.function, "icon.blue": t.normal, "icon.cyan": t.hint, "icon.green": t.str,
		"icon.grey": t.muted, "icon.orange": t.number, "icon.purple": t.keyword, "icon.red": t.err,
		"icon.yellow": t.warn,
	}
	return s
}

// mix returns the colour t of the way from a to b, for tints. A colour that
// is not one, such as the terminal's default, stays as it is.
func mix(a, b color.Color, t float64) color.Color {
	if !a.Valid() || !b.Valid() {
		return a
	}
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	at := func(x, y int32) int32 { return x + int32(float64(y-x)*t) }
	return color.NewRGBColor(at(ar, br), at(ag, bg), at(ab, bb))
}
