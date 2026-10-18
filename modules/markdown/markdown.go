// Package markdown draws a markdown file as it reads, with render on:
// headings on their level's ground without their #s, code blocks on a
// ground without their fences, bullets, checkboxes, quote bars and rules,
// and links and emphasis without their marks. The cursor's line shows its
// text as it is (B298).
package markdown

import (
	"context"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// queries find what the module draws, one for markdown's block grammar and
// one for its inline grammar.
var queries = map[string]string{
	"markdown": `
(atx_heading (atx_h1_marker) @h1) (atx_heading (atx_h2_marker) @h2) (atx_heading (atx_h3_marker) @h3)
(atx_heading (atx_h4_marker) @h4) (atx_heading (atx_h5_marker) @h5) (atx_heading (atx_h6_marker) @h6)
(fenced_code_block) @code
(fenced_code_block_delimiter) @hide
(list) @list
[(list_marker_minus) (list_marker_plus) (list_marker_star)] @bullet
(task_list_marker_unchecked) @unchecked
(task_list_marker_checked) @checked
[(block_quote_marker) (block_continuation)] @quote
(thematic_break) @rule`,
	"markdown_inline": `
[(emphasis_delimiter) (code_span_delimiter)] @hide
(inline_link ["[" "]" "(" ")"] @hide)
(inline_link [(link_destination) (link_title)] @hide)
(image ["!" "[" "]" "(" ")"] @hide)
(image [(link_destination) (link_title)] @hide)`,
}

// bullets are what a list's marker shows as, by how deep the list is.
var bullets = []string{"•", "◦", "▪"}

// headings are what a heading's #s show as with icons on, by its level.
var headings = []string{"󰲡 ", "󰲣 ", "󰲥 ", "󰲧 ", "󰲩 ", "󰲫 "}

// Module is markdown. It keeps what it shows on each buffer.
type Module struct {
	mu    sync.Mutex
	shown map[string][]int // by buffer, the ids of its decorations
	off   map[string]bool  // by buffer, render was off when it last drew
	ids   int
}

// New returns markdown.
func New() *Module { return &Module{shown: map[string][]int{}, off: map[string]bool{}} }

func (m *Module) Name() string { return "markdown" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "view.moved"}
}

func (m *Module) Registered() []api.Registration { return nil }

// Init draws the buffer open at the start.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.draw(s), nil
}

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event draws a buffer as it opens and changes, and again when render
// turns on or off.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event, _, _ := strings.Cut(name, " "); event == "view.moved" {
		if on, _ := s.Option("render"); m.off[s.About["buffer"]] == (on == "true") {
			return m.draw(s), nil
		}
		return api.Result{}, nil
	}
	return m.draw(s), nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }

// draw puts what the buffer s is about shows in place of what it showed.
func (m *Module) draw(s api.Scope) api.Result {
	buf, t := s.About["buffer"], s.Text
	res := api.Result{Undecorate: m.shown[buf]}
	delete(m.shown, buf)
	on, _ := s.Option("render")
	m.off[buf] = on != "true"
	if m.off[buf] || s.About["filetype"] != "markdown" {
		return res
	}
	spans, err := s.Query(queries)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	icons, _ := s.Option("icons")
	add := func(d api.Decoration) {
		m.ids++
		d.ID = m.ids
		res.Decorate = append(res.Decorate, d)
		m.shown[buf] = append(m.shown[buf], m.ids)
	}
	show := func(start, end int, text, style string) {
		add(api.Decoration{Start: start, End: end, Style: style, Note: text, Replace: true})
	}
	var lists []api.Span
	for _, sp := range spans {
		if sp.Name == "list" {
			lists = append(lists, sp)
		}
	}
	for _, sp := range spans {
		switch sp.Name {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			style, end, icon := "markdown."+sp.Name, sp.End, ""
			if t.Slice(end, min(end+1, t.Len())) == " " {
				end++
			}
			if icons == "true" {
				icon = headings[sp.Name[1]-'1']
			}
			show(sp.Start, end, icon, style)
			add(api.Decoration{Start: sp.Start, End: sp.Start, Style: style, Line: true})
		case "code":
			// Its range ends after its last line's break.
			add(api.Decoration{Start: sp.Start, End: max(sp.End-1, sp.Start), Style: "markdown.code", Line: true})
		case "hide":
			show(sp.Start, sp.End, "", "")
		case "bullet":
			depth := 0
			for _, l := range lists {
				if l.Start <= sp.Start && sp.Start < l.End {
					depth++
				}
			}
			show(sp.Start, sp.Start+1, bullets[max(depth-1, 0)%len(bullets)], "markdown.bullet")
		case "unchecked":
			show(sp.Start, sp.End, "☐", "markdown.unchecked")
		case "checked":
			show(sp.Start, sp.End, "☑", "markdown.checked")
		case "quote":
			for i, r := range t.Slice(sp.Start, sp.End) {
				if r == '>' {
					show(sp.Start+i, sp.Start+i+1, "▌", "markdown.quote")
				}
			}
		case "rule":
			// Its range ends after its line's break.
			end := sp.Start + len(strings.TrimRight(t.Slice(sp.Start, sp.End), "\n"))
			show(sp.Start, end, strings.Repeat("─", end-sp.Start), "markdown.rule")
		}
	}
	return res
}
