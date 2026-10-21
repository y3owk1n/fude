package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/vim"
)

// memory is what fude remembers from one run to the next: the registers,
// the file marks, the command and search histories and the last search
// (B454).
type memory struct {
	Registers map[string]vim.Saved `json:"registers"`
	Marks     map[string]filePlace `json:"marks"`
	History   map[string][]string  `json:"history"`
	Search    string               `json:"search"`
	Forward   bool                 `json:"forward"`
}

// filePlace is a place in a file by its line and byte column, as a file
// mark is kept while its file is not open.
type filePlace struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// memoryKept is how many lines of each history fude remembers.
const memoryKept = 1000

// memoryPath is the file the memory is kept in.
func memoryPath() (string, error) {
	dir, err := keep.Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fude", "memory"), nil
}

// remember writes what the session's machine and file marks hold, as fude
// quits.
func (e *Session) remember() error {
	m := memory{Registers: map[string]vim.Saved{}, Marks: map[string]filePlace{}, History: map[string][]string{}}
	for name, r := range e.vim.Registers() {
		if name != '/' && name != '%' && name != ':' && name != '.' {
			m.Registers[string(name)] = r
		}
	}
	for name, f := range e.fileMarks {
		t := f.buf.text.Snapshot()
		pos := f.buf.text.Mark(f.mark)
		line := t.LineOf(pos)
		m.Marks[string(name)] = filePlace{f.buf.file.path, line + 1, pos - t.LineStart(line)}
	}
	for name, f := range e.marksToOpen {
		if _, open := m.Marks[string(name)]; !open {
			m.Marks[string(name)] = f
		}
	}
	for _, prompt := range []string{":", "/"} {
		h := e.vim.History(prompt)
		m.History[prompt] = h[max(len(h)-memoryKept, 0):]
	}
	m.Search, m.Forward = e.vim.Search()
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("remember: %w", err)
	}
	path, err := memoryPath()
	if err != nil {
		return fmt.Errorf("remember: %w", err)
	}
	return keep.Write(path, data)
}

// recall gives the session's machine what fude remembered as it last quit.
// A file mark in a file that is not open waits for a jump to it to open it.
func (e *Session) recall() error {
	path, err := memoryPath()
	if err != nil {
		return fmt.Errorf("recall: %w", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recall: %w", err)
	}
	var m memory
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("recall %s: %w", path, err)
	}
	for name, r := range m.Registers {
		if n := []rune(name); len(n) == 1 {
			if err := e.vim.SetRegister(n[0], r); err != nil {
				return fmt.Errorf("recall register %s: %w", name, err)
			}
		}
	}
	for prompt, lines := range m.History {
		e.vim.SetHistory(prompt, lines)
	}
	if m.Search != "" {
		// The last search comes back without its highlight.
		e.vim.SetSearch(m.Search, m.Forward)
		if err := e.vim.Ex("nohlsearch"); err != nil {
			return fmt.Errorf("recall search: %w", err)
		}
	}
	e.marksToOpen = map[rune]filePlace{}
	for name, f := range m.Marks {
		if n := []rune(name); len(n) == 1 && n[0] >= 'A' && n[0] <= 'Z' {
			e.marksToOpen[n[0]] = f
		}
	}
	return nil
}

// openMark opens the file of a remembered file mark not yet set in a
// buffer, and sets the mark there.
func (e *Session) openMark(name rune) bool {
	f, ok := e.marksToOpen[name]
	if !ok {
		return false
	}
	if err := e.edit(f.Path); err != nil {
		e.message = err.Error()
		return false
	}
	b := e.win.buf
	if !b.is(f.Path) {
		return false
	}
	t := b.text.Snapshot()
	ls := t.LineStart(min(max(f.Line, 1), t.Lines()) - 1)
	pos := min(ls+f.Column, vim.LineEnd(t, ls))
	host{e}.SetMark(name, pos)
	return true
}
