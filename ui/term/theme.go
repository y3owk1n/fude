package term

import (
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

// theme is the colours the terminal client draws with, in true colour, as
// the theme setting picks them (D33).
type theme struct {
	bg, surface, raised, border color.Color // grounds, from the text's to a float's edge
	text, muted, faint          color.Color // text, from the most to the least prominent
	cursorLine, selection       color.Color
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
		cursorLine: color.NewHexColor(0x1b1e25), selection: color.NewHexColor(0x2b3a55),
		normal: color.NewHexColor(0x7aa7f5), insert: color.NewHexColor(0x86c58a), visual: color.NewHexColor(0xc49bf2), cmd: color.NewHexColor(0xe5a56b),
		err: color.NewHexColor(0xe8787a), warn: color.NewHexColor(0xe3b341), info: color.NewHexColor(0x7aa7f5), hint: color.NewHexColor(0x6cc8bd),
		keyword: color.NewHexColor(0xc49bf2), str: color.NewHexColor(0xa6d189), function: color.NewHexColor(0x7ab3f0), typ: color.NewHexColor(0x6cc8bd),
		number: color.NewHexColor(0xe5a56b), comment: color.NewHexColor(0x6a7180), punct: color.NewHexColor(0x8f97a5),
	},
	"light": {
		bg: color.NewHexColor(0xf7f7f5), surface: color.NewHexColor(0xffffff), raised: color.NewHexColor(0xeceef1), border: color.NewHexColor(0xd6d9df),
		text: color.NewHexColor(0x1f2328), muted: color.NewHexColor(0x5b6270), faint: color.NewHexColor(0x9aa1ad),
		cursorLine: color.NewHexColor(0xeef0f3), selection: color.NewHexColor(0xcfe0fb),
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
		cursorLine: c(0x1), selection: c(0x2),
		normal: c(0xD), insert: c(0xB), visual: c(0xE), cmd: c(0x9),
		err: c(0x8), warn: c(0xA), info: c(0xD), hint: c(0xC),
		keyword: c(0xE), str: c(0xB), function: c(0xD), typ: c(0xA),
		number: c(0x9), comment: c(0x3), punct: c(0x4),
	}
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
	// the names mini.icons gives them.
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
)

func init() { useTheme(themes["dark"]) }

// useTheme draws with t from then on.
func useTheme(t theme) {
	lit, dimmed = build(t), build(tint(t))
	apply(lit)
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
		cursorLine: g(t.cursorLine), selection: g(t.selection),
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
		// Markdown in floats, such as hover.
		"markup.strong": base.Bold(true), "markup.italic": base.Italic(true), "markup.heading": base.Bold(true),
		"markup.raw": fg(t.typ), "markup.link": base.Underline(true), "markup.rule": fg(t.faint),
		"menu.selected": base.Background(t.raised),
		// Text just yanked, put, undone or redone.
		"flash": base.Background(t.cmd).Foreground(t.bg),
		// A hunk's lines, as git's index has them and as they are.
		"diff.minus": fg(t.err), "diff.plus": fg(t.insert),
		// What a proposal would replace.
		"proposal": fg(t.faint).StrikeThrough(true), "proposal.stale": fg(t.err).StrikeThrough(true),
	}
	s.underlines = map[string]color.Color{
		"diagnostic.error": t.err, "diagnostic.warn": t.warn, "diagnostic.info": t.info, "diagnostic.hint": t.hint,
	}
	s.signColours = map[string]color.Color{
		"diagnostic.error": t.err, "diagnostic.warn": t.warn, "diagnostic.info": t.info, "diagnostic.hint": t.hint,
		"git.add": t.insert, "git.change": t.normal, "git.delete": t.err,
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
