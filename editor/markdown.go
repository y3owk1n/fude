package editor

import (
	"strings"

	"github.com/y3owk1n/fude/syntax"
)

// markdown renders the markdown language servers write: fenced code in its
// language's colours, and inline code, bold, italic, headings and links with
// their markers gone. A line of --- stays, spanned as markup.rule for the
// client to draw across. It returns the text and the spans that style it.
func (e *Editor) markdown(src string) (string, []syntax.Span) {
	var b strings.Builder
	var spans []syntax.Span
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if lang, ok := strings.CutPrefix(l, "```"); ok {
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
				code = append(code, lines[i])
			}
			text := strings.Join(code, "\n")
			hl, err := syntax.Highlight(strings.TrimSpace(lang), e.queries, text)
			if err != nil {
				logLine(err.Error())
			}
			for _, s := range hl {
				s.Start, s.End = s.Start+b.Len(), s.End+b.Len()
				spans = append(spans, s)
			}
			b.WriteString(text + "\n")
			continue
		}
		if t := strings.TrimSpace(l); t == "---" || t == "***" || t == "___" {
			spans = append(spans, syntax.Span{Start: b.Len(), End: b.Len() + 3, Name: "markup.rule"})
			b.WriteString("---\n")
			continue
		}
		start := b.Len()
		text, heading := strings.CutPrefix(strings.TrimLeft(l, "#"), " ")
		if heading = heading && strings.HasPrefix(l, "#"); heading {
			l = text
		}
		spans = inline(&b, spans, l)
		if heading {
			spans = append(spans, syntax.Span{Start: start, End: b.Len(), Name: "markup.heading"})
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), spans
}

// inline writes l to b without its inline markers, and returns spans with
// the styles they stood for added.
func inline(b *strings.Builder, spans []syntax.Span, l string) []syntax.Span {
	styled := func(text, name string) {
		spans = append(spans, syntax.Span{Start: b.Len(), End: b.Len() + len(text), Name: name})
		b.WriteString(text)
	}
	for i := 0; i < len(l); i++ {
		rest := l[i:]
		switch {
		case rest[0] == '\\' && len(rest) > 1 && strings.ContainsRune("\\`*_{}[]()#+-.!|<>~", rune(rest[1])):
			b.WriteByte(rest[1])
			i++
			continue
		case rest[0] == '`':
			if j := strings.IndexByte(rest[1:], '`'); j >= 0 {
				styled(rest[1:1+j], "markup.raw")
				i += j + 1
				continue
			}
		case strings.HasPrefix(rest, "**") || strings.HasPrefix(rest, "__"):
			if j := strings.Index(rest[2:], rest[:2]); j > 0 {
				styled(rest[2:2+j], "markup.strong")
				i += j + 3
				continue
			}
		case rest[0] == '*' || rest[0] == '_' && (i == 0 || !isWord(l[i-1])):
			if j := strings.IndexByte(rest[1:], rest[0]); j > 0 && (rest[0] == '*' || 2+j >= len(rest) || !isWord(rest[2+j])) {
				styled(rest[1:1+j], "markup.italic")
				i += j + 1
				continue
			}
		case rest[0] == '[':
			if close := strings.Index(rest, "]("); close > 0 {
				if end := strings.IndexByte(rest[close:], ')'); end > 0 {
					// A link shows its text, which may have markup of its own.
					start := b.Len()
					spans = inline(b, spans, rest[1:close])
					spans = append(spans, syntax.Span{Start: start, End: b.Len(), Name: "markup.link"})
					i += close + end
					continue
				}
			}
		}
		b.WriteByte(rest[0])
	}
	return spans
}

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
