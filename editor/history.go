package editor

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
)

// record is one line of a timeline log. The log starts with a base record,
// then holds a node record for every timeline node as it is made, a save
// record at every write, and a close record when the file is closed cleanly.
type record struct {
	Kind string        `json:"kind"`           // base, node, save or close
	Node *kernel.Node  `json:"node,omitempty"` // for node
	Head kernel.NodeID `json:"head"`           // for save and close
	Hash string        `json:"hash,omitempty"` // of the text, for base and save
}

// history is a buffer's timeline log on disk. It is the kernel's Store, so
// the kernel writes every node to it as the node is made.
type history struct {
	path     string
	f        *os.File
	err      error // the first write that failed, which stops the log
	reported bool
}

// historyPath returns where the timeline log of the file at path lives.
func historyPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("timeline of %s: %w", path, err)
	}
	dir, err := xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", fmt.Errorf("timeline of %s: %w", path, err)
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(dir, "fude", "timeline", hex.EncodeToString(sum[:16])+".log"), nil
}

func hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Append writes a node to the log.
func (h *history) Append(n kernel.Node) { h.write(record{Kind: "node", Node: &n}) }

func (h *history) write(r record) {
	if h.err != nil {
		return
	}
	line, err := json.Marshal(r)
	if err == nil && h.f == nil {
		if err = os.MkdirAll(filepath.Dir(h.path), 0o700); err == nil {
			h.f, err = os.OpenFile(h.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		}
	}
	if err == nil {
		_, err = h.f.Write(append(line, '\n'))
	}
	if err != nil {
		h.err = fmt.Errorf("timeline log %s: %w", h.path, err)
	}
}

// close records a clean close at head and releases the file.
func (h *history) close(head kernel.NodeID) {
	h.write(record{Kind: "close", Head: head})
	if h.f != nil {
		if err := h.f.Close(); err != nil && h.err == nil {
			h.err = fmt.Errorf("timeline log %s: %w", h.path, err)
		}
		h.f = nil
	}
}

// failed returns a write error the first time it is asked, so the editor
// reports it once.
func (h *history) failed() error {
	if h.err == nil || h.reported {
		return nil
	}
	h.reported = true
	return h.err
}

// loaded is what a log holds for text that still matches it.
type loaded struct {
	nodes   []kernel.Node
	head    kernel.NodeID   // the node the text is at
	saves   []kernel.NodeID // the head at each write
	unsaved kernel.NodeID   // the newest node made after the last write, if the file was not closed cleanly
}

// openHistory reads the log of the file at path. When the log's last write,
// or its start, matches text, it returns the timeline. Otherwise it sets the
// log aside and starts a new one from text.
func openHistory(path, text string) (*history, loaded, error) {
	p, err := historyPath(path)
	if err != nil {
		return nil, loaded{}, err
	}
	h := &history{path: p}
	l, ok, err := readHistory(p, hash(text))
	if err != nil {
		return nil, loaded{}, err
	}
	if !ok {
		if err := os.Rename(p, p+"."+strconv.FormatInt(time.Now().Unix(), 10)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, loaded{}, fmt.Errorf("set aside timeline log %s: %w", p, err)
		}
		h.write(record{Kind: "base", Hash: hash(text)})
		return h, loaded{}, nil
	}
	return h, l, nil
}

// readHistory parses the log at p, and reports whether its last write, or
// its start, has the text hash want.
func readHistory(p, want string) (loaded, bool, error) {
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return loaded{}, false, nil
	}
	if err != nil {
		return loaded{}, false, fmt.Errorf("read timeline log %s: %w", p, err)
	}
	defer f.Close()
	var l loaded
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
			l.head, l.unsaved = r.Head, kernel.NodeID{}
			if r.Kind == "save" {
				l.saves = append(l.saves, r.Head)
			}
		case "node":
			l.nodes = append(l.nodes, *r.Node)
			l.unsaved, closed = r.Node.ID, false
		case "close":
			closed = true
		}
	}
	if err := s.Err(); err != nil {
		return loaded{}, false, fmt.Errorf("read timeline log %s: %w", p, err)
	}
	if closed {
		l.unsaved = kernel.NodeID{}
	}
	return l, match, nil
}
