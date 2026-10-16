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
)

// traced records what as an event of src that took since at, with err when
// it failed.
func traced(src, what string, at time.Time, err error) {
	if err != nil {
		what += ": " + err.Error()
	}
	api.Trace(src, what, time.Since(at))
}

// Tail prints what the fude listening at socket does, as fude --tail does
// (B174): the events it kept, then each one as it happens, until it quits.
// Each is a line of text, or of JSON with asJSON. Without a socket it
// watches the fude at the socket setting, or else the one fude listening in
// XDG_RUNTIME_DIR or the temporary directory.
func Tail(w io.Writer, socket string, asJSON bool) error {
	if socket == "" {
		var err error
		if socket, err = findSocket(); err != nil {
			return err
		}
	}
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
			Method string    `json:"method"`
			Params api.Event `json:"params"`
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

// findSocket returns the socket setting, or else the socket of the one
// fude that answers in the directory fude listens in without it.
func findSocket() (string, error) {
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
	return "", fmt.Errorf("%d fudes are running, so name the socket of one: %s", len(live), strings.Join(live, " "))
}
