package tui

import (
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	look "github.com/y3owk1n/fude/internal/theme"
	"github.com/y3owk1n/fude/wire"
)

// theme is a theme's colours as tcell takes them (D33).
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

// rgb is c as tcell takes it.
func rgb(c look.RGB) color.Color { return color.NewHexColor(int32(c)) }

// tcellTheme is t in tcell's colours.
func tcellTheme(t look.Theme) theme {
	return theme{
		bg: rgb(t.Bg), surface: rgb(t.Surface), raised: rgb(t.Raised), border: rgb(t.Border),
		text: rgb(t.Text), muted: rgb(t.Muted), faint: rgb(t.Faint),
		cursorLine: rgb(t.CursorLine), selection: rgb(t.Selection), search: rgb(t.Search),
		normal: rgb(t.Normal), insert: rgb(t.Insert), visual: rgb(t.Visual), cmd: rgb(t.Cmd),
		err: rgb(t.Error), warn: rgb(t.Warn), info: rgb(t.Info), hint: rgb(t.Hint),
		keyword: rgb(t.Keyword), str: rgb(t.String), function: rgb(t.Function), typ: rgb(t.Type),
		number: rgb(t.Number), comment: rgb(t.Comment), punct: rgb(t.Punctuation),
	}
}

// styles are a look of internal/theme as tcell draws it.
type styles struct {
	th          theme
	base        tcell.Style
	colours     map[string]tcell.Style
	underlines  map[string]color.Color
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

func init() { useTheme(look.Themes["dark"], nil) }

// useTheme draws with t, and the styles modules defined over its own,
// from then on.
func useTheme(t look.Theme, own map[string]wire.Style) {
	l, d := look.LookOf(t), look.LookOf(t.Tint())
	ground := t
	ground.Bg = d.Theme.Bg
	b := look.LookOf(ground)
	same := func(c look.RGB) look.RGB { return c }
	l.Restyle(own, same)
	b.Restyle(own, same)
	d.Restyle(own, func(c look.RGB) look.RGB { return look.Mix(c, d.Theme.Bg, 0.55) })
	lit, dimmed, boxed = build(l), build(d), build(b)
	apply(lit)
}

// apply makes drawing take s.
func apply(s styles) {
	th, base, colours, underlines, signColours = s.th, s.base, s.colours, s.underlines, s.signColours
}

// build is l as tcell draws it.
func build(l look.Look) styles {
	s := styles{th: tcellTheme(l.Theme), base: style(l.Base), colours: map[string]tcell.Style{}, underlines: map[string]color.Color{}, signColours: map[string]color.Color{}}
	for name, st := range l.Names {
		s.colours[name] = style(st)
	}
	for name, c := range l.Underlines {
		s.underlines[name] = rgb(c)
	}
	for name, c := range l.Signs {
		s.signColours[name] = rgb(c)
	}
	return s
}

// style is s as tcell draws it.
func style(s look.Style) tcell.Style {
	return tcell.StyleDefault.Background(rgb(s.Bg)).Foreground(rgb(s.Fg)).Bold(s.Bold).Italic(s.Italic).Underline(s.Underline).StrikeThrough(s.Strike)
}
