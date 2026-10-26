// Package markdown draws a markdown file as it reads, with render on:
// headings on their level's ground without their #s, code blocks on a
// ground without their fences, bullets, checkboxes, quote bars and rules,
// and links and emphasis without their marks. The cursor's line shows its
// text as it is (B298).
package markdown

import (
	"context"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/clipperhouse/displaywidth"

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
(thematic_break) @rule
(pipe_table) @table
[(pipe_table_header) (pipe_table_row)] @row
(pipe_table_delimiter_row) @delimiter`,
	"markdown_inline": `
[(emphasis_delimiter) (code_span_delimiter)] @hide
(inline_link ["[" "]" "(" ")"] @hide)
(inline_link [(link_destination) (link_title)] @hide)
(image ["!" "[" "]" "(" ")"] @hide)
(image (link_title) @hide)
(image (link_destination) @picture)`,
}

// bullets are what a list's marker shows as, by how deep the list is.
var bullets = []string{"•", "◦", "▪"}

// headings are what a heading's #s show as with icons on, by its level.
var headings = []string{"󰲡 ", "󰲣 ", "󰲥 ", "󰲧 ", "󰲩 ", "󰲫 "}

// redraw is how long after a change the module draws again, so typing
// parses the file at most once each redraw, not once each key.
const redraw = 50 * time.Millisecond

// Module is markdown. It keeps what it shows on each buffer.
type Module struct {
	mu     sync.Mutex
	shown  map[string][]int  // by buffer, the ids of its decorations
	off    map[string]bool   // by buffer, render was off when it last drew
	due    map[uint32]string // by timer, the buffer it draws again
	timers uint32
	ids    int
}

// New returns markdown.
func New() *Module {
	return &Module{shown: map[string][]int{}, off: map[string]bool{}, due: map[uint32]string{}}
}

func (m *Module) Name() string { return "markdown" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "buffer.closed", "window.moved", "option.set", "config.reloaded"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Option, Name: "enabled", Default: "true", Help: "markdown files draw as they read, but for the cursor's line", Window: true}}
}

// Init draws the buffer open at the start.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.draw(s), nil
}

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event draws a buffer as it opens, again when render turns on or off,
// and redraw after a change, once for however many changes come meanwhile.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	switch event, detail, _ := strings.Cut(name, " "); event {
	case "buffer.closed":
		maps.DeleteFunc(m.due, func(_ uint32, b string) bool { return b == buf })
		delete(m.shown, buf)
		delete(m.off, buf)
		return api.Result{}, nil
	case "option.set", "config.reloaded":
		// render or icons changed.
		if detail == "markdown.enabled" || detail == "icons" || event == "config.reloaded" {
			return m.draw(s), nil
		}
		return api.Result{}, nil
	case "window.moved":
		if on, _ := s.Option("markdown.enabled"); m.off[buf] == (on == "true") {
			return m.draw(s), nil
		}
		return api.Result{}, nil
	case "buffer.changed":
		if s.About["filetype"] != "markdown" && len(m.shown[buf]) == 0 {
			return api.Result{}, nil
		}
		for _, b := range m.due {
			if b == buf {
				return api.Result{}, nil
			}
		}
		m.timers++
		m.due[m.timers] = buf
		return api.Result{Timers: []api.Timer{{ID: m.timers, After: redraw}}}, nil
	}
	return m.draw(s), nil
}

// Callback draws the buffer a change's timer is about.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf, ok := m.due[id]
	delete(m.due, id)
	if !ok || buf != s.About["buffer"] {
		return api.Result{}, nil
	}
	return m.draw(s), nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }

// draw puts what the buffer s is about shows in place of what it showed.
func (m *Module) draw(s api.Scope) api.Result {
	buf, t := s.About["buffer"], s.Text
	res := api.Result{Undecorate: m.shown[buf]}
	delete(m.shown, buf)
	on, _ := s.Option("markdown.enabled")
	m.off[buf] = on != "true"
	if m.off[buf] || s.About["filetype"] != "markdown" {
		return res
	}
	spans, err := s.Query(queries)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	icons, _ := s.Option("icons")
	add := func(d api.Decoration) {
		m.ids++
		d.ID = m.ids
		res.Decorate = append(res.Decorate, d)
		m.shown[buf] = append(m.shown[buf], m.ids)
	}
	// replaced is every replacement, for a table's cells to count their
	// widths by.
	var replaced []replacement
	show := func(start, end int, text, style string) {
		add(api.Decoration{Start: start, End: end, Style: style, Note: text, Replace: true})
		replaced = append(replaced, replacement{start, end, text})
	}
	var lists, tables, rows []api.Span
	for _, sp := range spans {
		switch sp.Name {
		case "list":
			lists = append(lists, sp)
		case "table":
			tables = append(tables, sp)
		case "row", "delimiter":
			rows = append(rows, sp)
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
		case "picture":
			// The path hides here, since Query gives a span one capture
			// name and this one is picture. An image on disk or at an http
			// or https address shows below its line. A relative path is
			// beside the markdown file, and one in <> may have spaces
			// (B303, B308, B595).
			show(sp.Start, sp.End, "", "")
			src := t.Slice(sp.Start, sp.End)
			if strings.HasPrefix(src, "<") && strings.HasSuffix(src, ">") {
				src = src[1 : len(src)-1]
			}
			switch {
			case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
				add(api.Decoration{Start: sp.Start, End: sp.Start, Image: src})
			case !strings.Contains(src, "://"):
				if !filepath.IsAbs(src) {
					src = filepath.Join(filepath.Dir(s.About["path"]), src)
				}
				add(api.Decoration{Start: sp.Start, End: sp.Start, Image: src})
			}
		case "rule":
			// Its range ends after its line's break.
			end := sp.Start + len(strings.TrimRight(t.Slice(sp.Start, sp.End), "\n"))
			show(sp.Start, end, strings.Repeat("─", end-sp.Start), "markdown.rule")
		}
	}
	// A table's column widths count the marks the loop put in its cells.
	for _, tb := range tables {
		var in []api.Span
		for _, r := range rows {
			if r.Start >= tb.Start && r.End <= tb.End {
				in = append(in, r)
			}
		}
		table(t, in, replaced, add)
	}
	return res
}

// replacement is text shown in place of [start, end).
type replacement struct {
	start, end int
	text       string
}

// table shows a table's pipes as │ and its delimiter row as ├─┼─┤, with
// ┌─┬─┐ above it and └─┴─┘ below it, unless the cursor is in it. The pipes
// take blanks so the columns
// line up. A cell's blanks go after it, before it in a column the
// delimiter row aligns right with -:, and on both sides in one it centres
// with :-:. A table with a row that does not start with a pipe stays as it
// is.
func table(t api.Text, rows []api.Span, replaced []replacement, add func(api.Decoration)) {
	show := func(start, end int, text, style string) {
		add(api.Decoration{Start: start, End: end, Style: style, Note: text, Replace: true})
	}
	pipes := make([][]int, len(rows))
	var widths []int
	for i, r := range rows {
		line := t.Slice(r.Start, r.End)
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			return
		}
		for j := range len(line) {
			if line[j] == '|' && (j == 0 || line[j-1] != '\\') {
				pipes[i] = append(pipes[i], r.Start+j)
			}
		}
		for j := 1; r.Name == "row" && j < len(pipes[i]); j++ {
			if j > len(widths) {
				widths = append(widths, 0)
			}
			widths[j-1] = max(widths[j-1], width(t, pipes[i][j-1]+1, pipes[i][j], replaced))
		}
	}
	aligns := make([]string, len(widths))
	// pads returns the blanks before and after cell j of row i.
	pads := func(i, j int) (int, int) {
		if j < 0 || j+1 >= len(pipes[i]) || j >= len(widths) {
			return 0, 0
		}
		pad := max(widths[j]-width(t, pipes[i][j]+1, pipes[i][j+1], replaced), 0)
		switch aligns[j] {
		case "right":
			return pad, 0
		case "centre":
			return pad / 2, pad - pad/2
		}
		return 0, pad
	}
	for i, r := range rows {
		for j := 0; r.Name == "delimiter" && j+1 < len(pipes[i]) && j < len(aligns); j++ {
			switch cell := strings.TrimSpace(t.Slice(pipes[i][j]+1, pipes[i][j+1])); {
			case strings.HasPrefix(cell, ":") && strings.HasSuffix(cell, ":") && len(cell) > 1:
				aligns[j] = "centre"
			case strings.HasSuffix(cell, ":"):
				aligns[j] = "right"
			}
		}
	}
	for i, r := range rows {
		ps := pipes[i]
		for j, p := range ps {
			if r.Name == "delimiter" {
				edge := "┼"
				switch j {
				case 0:
					edge = "├"
				case len(ps) - 1:
					edge = "┤"
				}
				show(p, p+1, edge, "markdown.table")
				if j+1 < len(ps) && j < len(widths) {
					show(p+1, ps[j+1], strings.Repeat("─", widths[j]), "markdown.table")
				}
				continue
			}
			_, after := pads(i, j-1)
			before, _ := pads(i, j)
			show(p, p+1, strings.Repeat(" ", after)+"│"+strings.Repeat(" ", before), "markdown.table")
		}
	}
	if len(rows) == 0 || len(pipes[0]) == 0 {
		return
	}
	// The borders start where the first row's first pipe shows.
	first, last := pipes[0][0], rows[len(rows)-1].Start
	lead := strings.Repeat(" ", width(t, t.LineStart(t.LineOf(first)), first, replaced))
	border := func(left, mid, right string) string {
		cells := make([]string, len(widths))
		for i, w := range widths {
			cells[i] = strings.Repeat("─", w)
		}
		return lead + left + strings.Join(cells, mid) + right
	}
	add(api.Decoration{Start: first, End: first, Style: "markdown.table", Lines: []string{border("┌", "┬", "┐")}, Above: true})
	add(api.Decoration{Start: last, End: last, Style: "markdown.table", Lines: []string{border("└", "┴", "┘")}})
	// With the cursor in it, the whole table shows its text as it is.
	add(api.Decoration{Start: first, End: last, Block: true})
}

// width is how many columns [start, end) of t takes as it shows, with the
// replacements in it.
func width(t api.Text, start, end int, replaced []replacement) int {
	w := displaywidth.String(t.Slice(start, end))
	for _, r := range replaced {
		if r.start >= start && r.end <= end {
			w += displaywidth.String(r.text) - displaywidth.String(t.Slice(r.start, r.end))
		}
	}
	return w
}
