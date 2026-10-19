// Package timeline is a buffer's timeline log on disk. It is the kernel's
// Store, which keeps every node as it is made, so the timeline outlives
// the process (B33, B34, B198).
package timeline

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/fsys"
	"github.com/y3owk1n/fude/internal/trace"
)

// record is one line of a log. The log starts with a base record, then
// holds a node record for every timeline node as it is made, a save record
// at every write, and a close record when the file is closed cleanly.
type record struct {
	Kind string        `json:"kind"`           // base, node, save or close
	Node *kernel.Node  `json:"node,omitempty"` // for node
	Head kernel.NodeID `json:"head"`           // for save and close
	Hash string        `json:"hash,omitempty"` // of the text, for base and save
}

// Log is one buffer's log. It is the kernel's Store, so the kernel writes
// every node to it as the node is made.
type Log struct {
	path     string
	f        *os.File
	err      error  // the first write that failed, which stops the log
	saved    record // the last base or save record, which the next open matches
	reported bool
}

// Cap is the size over which a clean close cuts a log, as opening the file
// reads the whole log (N26).
const Cap = 1 << 20

// Loaded is what a log holds for text that still matches it.
type Loaded struct {
	Nodes   []kernel.Node
	Head    kernel.NodeID   // the node the text is at
	Saves   []kernel.NodeID // the head at each write
	Unsaved kernel.NodeID   // the newest node made after the last write, if the file was not closed cleanly
}

// Open reads the log at path for a file whose text is text. When the log's
// last write, or its start, matches the text, it returns the timeline.
// Otherwise it sets the log aside and starts a new one from the text.
func Open(path, text string) (*Log, Loaded, error) {
	l := &Log{path: path}
	sum := hash(text)
	at := time.Now()
	loaded, ok, err := read(path, sum)
	if err != nil {
		return nil, Loaded{}, err
	}
	if !ok {
		switch err := os.Rename(path, path+"."+strconv.FormatInt(time.Now().Unix(), 10)); {
		case err == nil:
			trace.Record("file", fmt.Sprintf("set %q aside, as the text no longer matches it", path), 0)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, Loaded{}, fmt.Errorf("set aside timeline log %s: %w", path, err)
		}
		l.write(record{Kind: "base", Hash: sum})
		return l, Loaded{}, nil
	}
	trace.Record("file", fmt.Sprintf("read %q, %d nodes", path, len(loaded.Nodes)), time.Since(at))
	l.saved = record{Kind: "base", Head: loaded.Head, Hash: sum}
	if len(loaded.Saves) > 0 {
		l.saved.Kind = "save"
	}
	return l, loaded, nil
}

// hash is the SHA-256 of text, in hex.
func hash(text string) string {
	// A chunk at a time, as converting a long text whole would copy it.
	h, buf := sha256.New(), make([]byte, 64<<10)
	for len(text) > 0 {
		n := copy(buf, text)
		h.Write(buf[:n])
		text = text[n:]
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Append writes a node to the log.
func (l *Log) Append(n kernel.Node) { l.write(record{Kind: "node", Node: &n}) }

// Saved records a write of text at head.
func (l *Log) Saved(head kernel.NodeID, text string) {
	l.write(record{Kind: "save", Head: head, Hash: hash(text)})
}

func (l *Log) write(r record) {
	if l.err != nil {
		return
	}
	if r.Kind == "base" || r.Kind == "save" {
		l.saved = r
	}
	line, err := json.Marshal(r)
	if err == nil && l.f == nil {
		if err = os.MkdirAll(filepath.Dir(l.path), 0o700); err == nil {
			l.f, err = fsys.Append(l.path, 0o600)
		}
	}
	if err == nil {
		_, err = l.f.Write(append(line, '\n'))
	}
	if err != nil {
		l.err = fmt.Errorf("timeline log %s: %w", l.path, err)
	}
}

// Close records a clean close at t's head and releases the file. With cut,
// a log over Cap whose text is at its last save is then cut to the newest
// nodes on the way to the head that fit in Cap (B198).
func (l *Log) Close(t *kernel.Buffer, saves []kernel.NodeID, cut bool) {
	head := t.Head()
	l.end(head)
	if !cut || l.err != nil || head != l.saved.Head {
		return
	}
	info, err := os.Stat(l.path)
	if err != nil {
		l.err = fmt.Errorf("timeline log %s: %w", l.path, err)
		return
	}
	if info.Size() > Cap {
		at := time.Now()
		l.err = l.cut(t.Nodes(), head, saves)
		trace.Since("file", fmt.Sprintf("cut %q, %d bytes", l.path, info.Size()), at, l.err)
	}
}

// end writes the close record at head and closes the file.
func (l *Log) end(head kernel.NodeID) {
	l.write(record{Kind: "close", Head: head})
	if l.f != nil {
		if err := l.f.Close(); err != nil && l.err == nil {
			l.err = fmt.Errorf("timeline log %s: %w", l.path, err)
		}
		l.f = nil
	}
}

// cut rewrites the log with the nodes on the way to head, from the newest,
// until the next one would take it past Cap. The oldest node kept starts
// from the log's start. It drops older nodes and the branches off that way.
func (l *Log) cut(nodes []kernel.Node, head kernel.NodeID, saves []kernel.NodeID) error {
	byID := make(map[kernel.NodeID]kernel.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var kept []kernel.Node // newest first
	keep := map[kernel.NodeID]bool{}
	for id, size := head, 0; id != (kernel.NodeID{}); {
		n := byID[id]
		line, err := json.Marshal(record{Kind: "node", Node: &n})
		if err != nil {
			return fmt.Errorf("cut timeline log %s: %w", l.path, err)
		}
		// The head always stays, so undo reverts the newest change.
		if size += len(line); size > Cap && len(kept) > 0 {
			break
		}
		kept, keep[id] = append(kept, n), true
		id = n.Parent
	}
	if len(kept) > 0 {
		kept[len(kept)-1].Parent = kernel.NodeID{}
	}
	cut := &Log{path: l.path + ".new"}
	if err := os.Remove(cut.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cut timeline log %s: %w", l.path, err)
	}
	cut.write(record{Kind: "base"})
	for i := len(kept) - 1; i >= 0; i-- {
		cut.write(record{Kind: "node", Node: &kept[i]})
	}
	// The last save is l.saved, which carries the hash the next open matches.
	if l.saved.Kind == "save" {
		saves = saves[:len(saves)-1]
	}
	for _, s := range saves {
		if keep[s] {
			cut.write(record{Kind: "save", Head: s})
		}
	}
	cut.write(l.saved)
	cut.end(head)
	if cut.err != nil {
		return cut.err
	}
	if err := os.Rename(cut.path, l.path); err != nil {
		return fmt.Errorf("cut timeline log %s: %w", l.path, err)
	}
	return nil
}

// Failed returns a write error the first time it is asked, so the caller
// reports it once.
func (l *Log) Failed() error {
	if l.err == nil || l.reported {
		return nil
	}
	l.reported = true
	return l.err
}

// read parses the log at path, and reports whether its last write, or its
// start, has the text hash want.
func read(path, want string) (Loaded, bool, error) {
	f, err := fsys.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Loaded{}, false, nil
	}
	if err != nil {
		return Loaded{}, false, fmt.Errorf("read timeline log %s: %w", path, err)
	}
	defer f.Close()
	var l Loaded
	match, closed := false, false
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<30)
	for s.Scan() {
		var r record
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			// A line cut short by a crash ends the log.
			break
		}
		switch r.Kind {
		case "base", "save":
			match, closed = r.Hash == want, false
			l.Head, l.Unsaved = r.Head, kernel.NodeID{}
			if r.Kind == "save" {
				l.Saves = append(l.Saves, r.Head)
			}
		case "node":
			l.Nodes = append(l.Nodes, *r.Node)
			l.Unsaved, closed = r.Node.ID, false
		case "close":
			closed = true
		}
	}
	if err := s.Err(); err != nil {
		return Loaded{}, false, fmt.Errorf("read timeline log %s: %w", path, err)
	}
	if closed {
		l.Unsaved = kernel.NodeID{}
	}
	return l, match, nil
}
