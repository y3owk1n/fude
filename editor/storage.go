package editor

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/y3owk1n/fude/internal/keep"
)

// Store is a place fude keeps files between runs, for fude storage (B383).
type Store struct {
	Name  string
	Path  string
	Holds string // what is in it
	Loses string // what deleting it loses, or "" when fude makes it again or never reads it
}

// Stores returns the places fude keeps files between runs, which may not
// exist yet.
func Stores() ([]Store, error) {
	state, err1 := keep.Dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	data, err2 := keep.Dir("XDG_DATA_HOME", filepath.Join(".local", "share"))
	cache, err3 := keep.Dir("XDG_CACHE_HOME", ".cache")
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	state = filepath.Join(state, "fude")
	return []Store{
		{
			"timeline", filepath.Join(state, "timeline"), "each file's undo history and the changes a crash left",
			"undo past where each file's next session starts, and :recover after a crash",
		},
		{
			"sessions", filepath.Join(state, "sessions"), "the files and windows fude reopens in each folder",
			"the files and windows fude would reopen in each folder",
		},
		{"pins", filepath.Join(state, "pins"), "the files :pin keeps a key away, by folder", "every folder's pins"},
		{"recent", filepath.Join(state, "recent"), "the files the start screen lists as recent", "the list of recent files"},
		{
			"memory", filepath.Join(state, "memory"), "the registers, file marks, command and search histories and last search, as fude last quit",
			"the registers, file marks and histories from before",
		},
		// fude makes these again, or never reads them back, so fude storage
		// clean deletes them without --force.
		{"log", filepath.Join(state, "log"), "what plugins log and language servers print on stderr", ""},
		{"grammars", filepath.Join(data, "fude", "grammars"), "the tree-sitter grammars fude built, which it builds again", ""},
		{"plugins", filepath.Join(cache, "fude", "wasm"), "plugins compiled for this machine, which fude compiles again", ""},
	}, nil
}
