// Package root finds the folder a project's tool runs in, by the files
// that mark it.
package root

import (
	"os"
	"path/filepath"
	"strings"
)

// Marker is a file that marks a root: a name, or with *, ? or [ a pattern
// of names. With Contains the file must hold each of those texts, and with
// Outermost the root is the farthest folder up that holds one, not the
// nearest (B528, B533).
type Marker struct {
	Name      string
	Contains  []string
	Outermost bool
}

// Names are markers of the names given, each in a group of its own.
func Names(names []string) [][]Marker {
	groups := make([][]Marker, len(names))
	for i, n := range names {
		groups[i] = []Marker{{Name: n}}
	}
	return groups
}

// Of returns the root of the file at abs, and whether a marker found it.
// The groups are tried in order. In a group the markers rank the same, so
// the folder nearest the file that one of them finds wins. With none found
// the root is the file's folder (B167, B533).
func Of(abs string, groups [][]Marker) (string, bool) {
	for _, g := range groups {
		best := ""
		for _, m := range g {
			if dir := find(filepath.Dir(abs), "", m); len(dir) > len(best) {
				best = dir
			}
		}
		if best != "" {
			return best, true
		}
	}
	return filepath.Dir(abs), false
}

// Between reports whether one of markers is in a folder from the file at
// abs's up to root, root included (B534).
func Between(abs, root string, markers []Marker) bool {
	for _, m := range markers {
		if find(filepath.Dir(abs), root, m) != "" {
			return true
		}
	}
	return false
}

// find returns the folder from dir up, to stop when it is not "", that m
// marks: the nearest, or with Outermost the farthest. It returns "" when
// none is.
func find(dir, stop string, m Marker) string {
	found := ""
	for {
		if holds(dir, m) {
			if !m.Outermost {
				return dir
			}
			found = dir
		}
		up := filepath.Dir(dir)
		if up == dir || dir == stop {
			return found
		}
		dir = up
	}
}

// holds reports whether dir has a file m names, holding what m asks.
func holds(dir string, m Marker) bool {
	var names []string
	if strings.ContainsAny(m.Name, "*?[") {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if ok, _ := filepath.Match(m.Name, e.Name()); ok {
				names = append(names, e.Name())
			}
		}
	} else if _, err := os.Stat(filepath.Join(dir, m.Name)); err == nil {
		names = []string{m.Name}
	}
	for _, n := range names {
		if contains(filepath.Join(dir, n), m.Contains) {
			return true
		}
	}
	return false
}

// contains reports whether the file at path holds each of texts.
func contains(path string, texts []string) bool {
	if len(texts) == 0 {
		return true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, t := range texts {
		if !strings.Contains(string(b), t) {
			return false
		}
	}
	return true
}
