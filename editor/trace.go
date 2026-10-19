package editor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/plugin"
)

// record traces what e did. Unless e is the local session, the text starts
// with e's origin, so a tail tells attached sessions apart.
func (e *Session) record(src, text string, took time.Duration) {
	if e.origin != "user" {
		text = string(e.origin) + ": " + text
	}
	trace.Record(src, text, took)
}

// kind is the trace source of p: plugin for a wasm plugin, else module.
func kind(p api.Module) string {
	if _, ok := p.(*plugin.Plugin); ok {
		return "plugin"
	}
	return "module"
}

// Tail prints what the fude listening at socket does, as fude tail does
// (B174): the events it kept, then each one as it happens, until it quits.
// Each is a line of text, or of JSON with asJSON.
func Tail(w io.Writer, socket string, asJSON bool) error {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return fmt.Errorf("tail: %w", err)
	}
	defer c.Close()
	if _, err := io.WriteString(c, `{"id":1,"method":"tail"}`+"\n"); err != nil {
		return fmt.Errorf("tail %s: %w", socket, err)
	}
	in := bufio.NewScanner(c)
	in.Buffer(nil, 1<<26)
	last := 0
	for in.Scan() {
		var line struct {
			Method string      `json:"method"`
			Params trace.Event `json:"params"`
		}
		if err := json.Unmarshal(in.Bytes(), &line); err != nil {
			return fmt.Errorf("tail %s: %w", socket, err)
		}
		// The rest are the reply to tail and notices of changes.
		if line.Method != "event" {
			continue
		}
		ev := line.Params
		if asJSON {
			b, err := json.Marshal(ev)
			if err != nil {
				return fmt.Errorf("tail %s: %w", socket, err)
			}
			fmt.Fprintf(w, "%s\n", b)
			continue
		}
		if last > 0 && ev.Seq > last+1 {
			fmt.Fprintf(w, "missed %d events\n", ev.Seq-last-1)
		}
		last = ev.Seq
		took := ""
		if ev.Took > 0 {
			took = fmt.Sprintf(" (%.3fms)", float64(ev.Took)/float64(time.Millisecond))
		}
		fmt.Fprintf(w, "%s %-6s %s%s\n", ev.At.Local().Format("15:04:05.000"), ev.Src, ev.Text, took)
	}
	if err := in.Err(); err != nil {
		return fmt.Errorf("tail %s: %w", socket, err)
	}
	return nil
}

// FindSocket returns the socket setting, or else the socket of the one
// fude that answers in the directory fude listens in without it.
func FindSocket() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if cfg.Socket != "" {
		return cfg.Socket, nil
	}
	paths, err := filepath.Glob(filepath.Join(socketDir(), "fude.*.sock"))
	if err != nil {
		return "", fmt.Errorf("find fude: %w", err)
	}
	var live []string
	for _, p := range paths {
		if c, err := net.Dial("unix", p); err == nil {
			c.Close()
			live = append(live, p)
		}
	}
	switch len(live) {
	case 0:
		return "", errors.New("no fude is running")
	case 1:
		return live[0], nil
	}
	return "", fmt.Errorf("%d fudes are running, so name one with --socket: %s", len(live), strings.Join(live, " "))
}
