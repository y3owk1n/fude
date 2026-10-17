package editor

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
)

// debugKeys are the debug panel's own keys: <CR> acts on the frame or the
// variable of its line, and q closes it (B264).
var debugKeys = map[string]string{"<CR>": "<Plug>(debug.open)", "q": ":close<CR>"}

// variable is a scope or a variable of the chosen frame, as scopes and
// variables answer them. One with members has a reference to them.
type variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Ref   int    `json:"variablesReference"`
}

// panelRow is a line of the panel, and what <CR> on it acts on: a frame,
// the members of a variable, or neither.
type panelRow struct {
	text  string
	frame int // -1 for none
	ref   int // 0 for none
}

// debugPanel shows the panel of the session's frames, variables and
// output, as :debug panel does (B264).
func (e *Session) debugPanel() {
	d := e.debug
	if d == nil {
		e.message = "no debug session runs"
		return
	}
	if !slices.Contains(e.bufs, d.panel) {
		e.scratch("[debug]", "", nil, nil)
		d.panel = e.win.buf
	}
	e.drawPanel(d)
	if d.thread != 0 {
		e.fetchScopes(d)
	}
}

// fetchScopes asks for the chosen frame's scopes, the first of which shows
// open, and then for the members of each that shows open.
func (e *Session) fetchScopes(d *debugging) {
	if d.frame >= len(d.frames) || !slices.Contains(e.bufs, d.panel) {
		return
	}
	frame := d.frames[d.frame].ID
	d.client.Request("scopes", map[string]any{"frameId": frame}, func(body json.RawMessage, err error) {
		var r struct {
			Scopes []variable `json:"scopes"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		if e.debug != d || d.frame >= len(d.frames) || d.frames[d.frame].ID != frame {
			return
		}
		if err != nil {
			e.message = "the frame's scopes: " + err.Error()
			return
		}
		d.scopes = r.Scopes
		if len(d.scopes) > 0 && len(d.open) == 0 {
			d.open[d.scopes[0].Ref] = true
		}
		for ref := range d.open {
			e.fetchMembers(d, ref)
		}
		e.drawPanel(d)
	})
}

// fetchMembers asks for the members of the variable or scope ref names.
func (e *Session) fetchMembers(d *debugging, ref int) {
	d.client.Request("variables", map[string]any{"variablesReference": ref}, func(body json.RawMessage, err error) {
		var r struct {
			Variables []variable `json:"variables"`
		}
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		if e.debug != d {
			return
		}
		if err != nil {
			e.message = "the variables: " + err.Error()
			return
		}
		d.members[ref] = r.Variables
		e.drawPanel(d)
	})
}

// drawPanel writes the panel's text from d: the frames, the chosen one
// marked, the chosen frame's scopes and variables, open ones with their
// members under them, and the program's output.
func (e *Session) drawPanel(d *debugging) {
	b := d.panel
	if !slices.Contains(e.bufs, b) {
		return
	}
	rows := []panelRow{{"Frames", -1, 0}}
	for i, f := range d.frames {
		mark := "  "
		if i == d.frame {
			mark = "→ "
		}
		rows = append(rows, panelRow{fmt.Sprintf("%s%s  %s:%d", mark, f.Name, filepath.Base(f.Source.Path), f.Line), i, 0})
	}
	if d.thread == 0 {
		rows = append(rows, panelRow{"  not stopped", -1, 0})
	}
	rows = append(rows, panelRow{"Variables", -1, 0})
	var add func(vs []variable, depth int)
	add = func(vs []variable, depth int) {
		for _, v := range vs {
			sign := "  "
			if v.Ref > 0 {
				sign = map[bool]string{true: "▾ ", false: "▸ "}[d.open[v.Ref]]
			}
			text := strings.Repeat("  ", depth) + sign + v.Name
			if value, _, _ := strings.Cut(v.Value, "\n"); value != "" {
				text += " = " + value
			}
			rows = append(rows, panelRow{text, -1, v.Ref})
			if v.Ref > 0 && d.open[v.Ref] {
				add(d.members[v.Ref], depth+1)
			}
		}
	}
	add(d.scopes, 0)
	rows = append(rows, panelRow{"Output", -1, 0})
	for _, l := range d.output {
		rows = append(rows, panelRow{"  " + l, -1, 0})
	}
	d.rows = rows
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r.text
	}
	text, t := strings.Join(lines, "\n"), b.text.Snapshot()
	old := t.Slice(0, t.Len())
	if old == text {
		return
	}
	// Only what changed is replaced, so the cursor keeps its line.
	if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Edits: []kernel.Edit{replace(old, text)}}); err != nil {
		e.message = err.Error()
		return
	}
	// fude writes the panel, so it is never modified.
	b.saved = b.text.Head()
}

// debugOpen acts on the panel's line under the cursor: a frame is chosen
// and shows its line, and a variable with members shows or hides them
// (B264).
func (e *Session) debugOpen() {
	d := e.debug
	if d == nil || e.win.buf != d.panel {
		return
	}
	line := e.Text().LineOf(e.Cursor())
	if line >= len(d.rows) {
		return
	}
	switch r := d.rows[line]; {
	case r.frame >= 0:
		d.frame, d.scopes = r.frame, nil
		clear(d.open)
		clear(d.members)
		e.showFrame(d)
		e.fetchScopes(d)
		e.drawPanel(d)
	case r.ref > 0:
		d.open[r.ref] = !d.open[r.ref]
		if d.open[r.ref] && d.members[r.ref] == nil {
			e.fetchMembers(d, r.ref)
		}
		e.drawPanel(d)
	}
}

// debugOutput keeps the last lines the program wrote, for the panel.
func (e *Session) debugOutput(d *debugging, body json.RawMessage) {
	var o struct {
		Category string `json:"category"`
		Output   string `json:"output"`
	}
	if err := json.Unmarshal(body, &o); err != nil {
		e.message = "the debug adapter sent a bad output: " + err.Error()
		return
	}
	if o.Category == "telemetry" {
		return
	}
	d.output = append(d.output, strings.Split(strings.TrimSuffix(o.Output, "\n"), "\n")...)
	d.output = d.output[max(len(d.output)-100, 0):]
	e.drawPanel(d)
}
