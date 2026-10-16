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
	err      error  // the first write that failed, which stops the log
	saved    record // the last base or save record, which the next open matches
	reported bool
}

// logCap is the size over which a clean close cuts a timeline log, as
// opening the file reads the whole log (N26).
const logCap = 1 << 20

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
func (h *history) Append(n kernel.Node) { h.write(record{Kind: "node", Node: &n}) }

func (h *history) write(r record) {
	if h.err != nil {
		return
	}
	if r.Kind == "base" || r.Kind == "save" {
		h.saved = r
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

// close records a clean close at t's head and releases the file. With cut,
// a log over logCap whose text is at its last save is then cut to the
// newest nodes on the way to the head that fit in logCap (B198).
func (h *history) close(t *kernel.Buffer, saves []kernel.NodeID, cut bool) {
	head := t.Head()
	h.end(head)
	if !cut || h.err != nil || head != h.saved.Head {
		return
	}
	info, err := os.Stat(h.path)
	if err != nil {
		h.err = fmt.Errorf("timeline log %s: %w", h.path, err)
		return
	}
	if info.Size() > logCap {
		h.err = h.cut(t.Nodes(), head, saves)
	}
}

// end writes the close record at head and closes the file.
func (h *history) end(head kernel.NodeID) {
	h.write(record{Kind: "close", Head: head})
	if h.f != nil {
		if err := h.f.Close(); err != nil && h.err == nil {
			h.err = fmt.Errorf("timeline log %s: %w", h.path, err)
		}
		h.f = nil
	}
}

// cut rewrites the log with the nodes on the way to head, from the newest,
// until the next one would take it past logCap. The oldest node kept starts
// from the log's start. It drops older nodes and the branches off that way.
func (h *history) cut(nodes []kernel.Node, head kernel.NodeID, saves []kernel.NodeID) error {
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
			return fmt.Errorf("cut timeline log %s: %w", h.path, err)
		}
		// The head always stays, so undo reverts the newest change.
		if size += len(line); size > logCap && len(kept) > 0 {
			break
		}
		kept, keep[id] = append(kept, n), true
		id = n.Parent
	}
	if len(kept) > 0 {
		kept[len(kept)-1].Parent = kernel.NodeID{}
	}
	cut := &history{path: h.path + ".new"}
	if err := os.Remove(cut.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cut timeline log %s: %w", h.path, err)
	}
	cut.write(record{Kind: "base"})
	for i := len(kept) - 1; i >= 0; i-- {
		cut.write(record{Kind: "node", Node: &kept[i]})
	}
	// The last save is h.saved, which carries the hash the next open matches.
	if h.saved.Kind == "save" {
		saves = saves[:len(saves)-1]
	}
	for _, s := range saves {
		if keep[s] {
			cut.write(record{Kind: "save", Head: s})
		}
	}
	cut.write(h.saved)
	cut.end(head)
	if cut.err != nil {
		return cut.err
	}
	if err := os.Rename(cut.path, h.path); err != nil {
		return fmt.Errorf("cut timeline log %s: %w", h.path, err)
	}
	return nil
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
	sum := hash(text)
	l, ok, err := readHistory(p, sum)
	if err != nil {
		return nil, loaded{}, err
	}
	if !ok {
		if err := os.Rename(p, p+"."+strconv.FormatInt(time.Now().Unix(), 10)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, loaded{}, fmt.Errorf("set aside timeline log %s: %w", p, err)
		}
		h.write(record{Kind: "base", Hash: sum})
		return h, loaded{}, nil
	}
	h.saved = record{Kind: "base", Head: l.head, Hash: sum}
	if len(l.saves) > 0 {
		h.saved.Kind = "save"
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
