package editor

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// spot is a place in a file that a server named.
type spot struct {
	path string
	pos  lsp.Position
	enc  string // the encoding pos counts in
}

// lineAt returns the text of the line sp is on, from its buffer, or from
// the file when no buffer has it.
func (e *Session) lineAt(sp spot) string {
	text := ""
	switch i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(sp.path) }); {
	case e.outside(sp.path) != nil:
	case i >= 0:
		t := e.bufs[i].text.Snapshot()
		text = t.Slice(0, t.Len())
	default:
		if data, err := os.ReadFile(sp.path); err == nil {
			text = string(data)
		}
	}
	if lines := strings.Split(text, "\n"); sp.pos.Line < len(lines) {
		return strings.TrimSpace(lines[sp.pos.Line])
	}
	return ""
}

// showList opens a window of lines, each naming a spot, where <CR> jumps to the
// spot on the cursor's line in another window.
func (e *Session) showList(title string, lines []string, spots []spot) {
	e.scratch(title, strings.Join(lines, "\n"))
	e.win.buf.spots = spots
	e.lastList = &places{e.win.buf, title, lines, spots, -1}
}

// places is the last list of places a window showed, which ]q, :cnext and
// the others go through after its window closes (B194, N25).
type places struct {
	buf   *buffer
	title string
	lines []string
	spots []spot
	at    int // the place last gone to, -1 before the first
}

// stepPlace goes to the place of the last list that to gives, from the
// place last gone to and the count of places, and says which it is.
func (e *Session) stepPlace(to func(at, n int) int) {
	l := e.lastList
	if l == nil {
		e.message = "no list of places"
		return
	}
	at := to(l.at, len(l.spots))
	if at < 0 || at >= len(l.spots) {
		e.message = "no more places"
		return
	}
	l.at = at
	e.leaveList()
	e.visit(l.spots[at])
	e.message = fmt.Sprintf("(%d of %d) %s", at+1, len(l.spots), strings.TrimSpace(l.lines[at]))
}

// leaveList moves to a window that does not show a list of places, when
// there is one, so going to a place keeps the list in view.
func (e *Session) leaveList() {
	if e.win.buf.spots == nil {
		return
	}
	for _, w := range e.root.windows() {
		if w.buf.spots == nil {
			e.win = w
			return
		}
	}
}

// openList shows the last list of places in a window of its own, the one
// that shows it already or a new one, with the cursor on the place last
// gone to (B194).
func (e *Session) openList() {
	l := e.lastList
	if l == nil {
		e.message = "no list of places"
		return
	}
	if i := slices.IndexFunc(e.root.windows(), func(w *window) bool { return w.buf == l.buf }); i >= 0 {
		e.win = e.root.windows()[i]
	} else {
		at := l.at
		e.showList(l.title, l.lines, l.spots)
		e.lastList.at = at
	}
	t := e.Text()
	e.setCursor(t.LineStart(max(e.lastList.at, 0)))
}

// closeList closes the windows that show the last list of places.
func (e *Session) closeList() {
	for _, w := range e.root.windows() {
		if e.lastList != nil && w.buf == e.lastList.buf {
			if err := e.closeWindow(w); err != nil {
				e.message = err.Error()
				return
			}
		}
	}
}

// listKeys are a list window's own keys, which [keymap.list] changes
// (B142).
var listKeys = map[string]string{"<CR>": "<Plug>(list.open)", "q": ":close<CR>"}

// openSpot jumps to the place on the cursor's line of a list window, such as
// grr's, in another window, keeping the list.
func (e *Session) openSpot() {
	b := e.win.buf
	if b.spots == nil {
		e.message = "not in a list"
		return
	}
	line := e.Text().LineOf(e.Cursor())
	if e.lastList != nil && e.lastList.buf == b {
		e.lastList.at = line
	}
	sp := b.spots[line]
	e.leaveList()
	e.visit(sp)
}

// visit jumps to sp, opening its file when no buffer has it.
func (e *Session) visit(sp spot) {
	// Moving to another buffer is a jump of its own.
	if e.win.buf.is(sp.path) {
		e.pushJump(e.Cursor())
	} else {
		e.edit(sp.path)
	}
	if e.win.buf.is(sp.path) {
		e.setCursor(lsp.Offset(e.Text(), sp.pos, sp.enc))
		e.reveal()
	}
}

// cdo runs cmd at each place of the last list, or with perFile once in
// each of its files, as :cdo and :cfdo do (B257). Marks keep the places on
// their text as cmd edits around them.
func (e *Session) cdo(cmd string, perFile bool) {
	l := e.lastList
	switch {
	case l == nil:
		e.message = "no list of places"
		return
	case cmd == "":
		e.message = "run what? :cdo cmd"
		return
	}
	type stop struct {
		b    *buffer
		mark kernel.MarkID
	}
	var stops []stop
	files := map[string]bool{}
	for _, sp := range l.spots {
		if perFile && files[sp.path] {
			continue
		}
		e.leaveList()
		// A place that visit refuses, such as one outside a guest's
		// project, is skipped.
		if e.visit(sp); !e.win.buf.is(sp.path) {
			continue
		}
		files[sp.path] = true
		stops = append(stops, stop{e.win.buf, e.win.buf.text.SetMark(e.Cursor(), kernel.Right)})
	}
	for _, st := range stops {
		e.show(st.b)
		e.setCursor(st.b.text.Mark(st.mark))
		st.b.text.DeleteMark(st.mark)
		if err := e.vim.Ex(cmd); err != nil {
			e.message = err.Error()
			return
		}
	}
	if perFile {
		e.message = fmt.Sprintf("ran :%s in %d files", cmd, len(files))
		return
	}
	e.message = fmt.Sprintf("ran :%s at %d places in %d files", cmd, len(stops), len(files))
}

// showPlaces makes the places a module gave the last list, in a window of
// its own when it asks. An item with no text shows its line (B230, B292).
func (e *Session) showPlaces(pl *api.Places) {
	var lines []string
	var spots []spot
	for _, item := range pl.Items {
		path, line, col, text := grepSpot(item)
		place := fmt.Sprintf("%s:%d:%d:", path, line, col)
		if pl.File != "" {
			parts := strings.SplitN(item, ":", 3)
			if len(parts) < 3 {
				continue
			}
			line, _ = strconv.Atoi(parts[0])
			col, _ = strconv.Atoi(parts[1])
			path, text, place = pl.File, parts[2], fmt.Sprintf("%d:%d:", line, col)
		}
		sp := spot{path, lsp.Position{Line: max(line-1, 0), Character: max(col-1, 0)}, cmp.Or(pl.Encoding, "utf-8")}
		if strings.TrimSpace(text) == "" {
			text = e.lineAt(sp)
		}
		lines, spots = append(lines, place+" "+text), append(spots, sp)
	}
	if pl.Show {
		e.showList("["+pl.Title+"]", lines, spots)
		return
	}
	e.lastList = &places{title: "[" + pl.Title + "]", lines: lines, spots: spots, at: -1}
}
