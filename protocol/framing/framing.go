// Package framing reads and writes messages framed by a Content-Length
// header, as LSP and DAP frame them, and logs what a server writes to
// stderr a line at a time.
package framing

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Read reads one message body after its headers.
func Read(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			if n, err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
		}
	}
	if n < 0 {
		return nil, errors.New("a message with no Content-Length")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(r, body)
	return body, err
}

// Write writes body as one message with its Content-Length header.
func Write(w io.Writer, body []byte) error {
	_, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

// Lines logs what a server writes to stderr, a line at a time after its
// name. A line over 64KB is logged in parts.
type Lines struct {
	Log  func(string)
	Name string
	rest []byte
}

func (l *Lines) Write(p []byte) (int, error) {
	l.rest = append(l.rest, p...)
	for {
		i := bytes.IndexByte(l.rest, '\n')
		if i < 0 && len(l.rest) < 64<<10 {
			return len(p), nil
		}
		if i < 0 {
			i = len(l.rest)
		}
		// A server on Windows may end its lines with \r\n.
		l.Log(l.Name + ": " + string(bytes.TrimSuffix(l.rest[:i], []byte("\r"))))
		l.rest = l.rest[min(i+1, len(l.rest)):]
	}
}

// Flush logs a last line that has no line break.
func (l *Lines) Flush() {
	if len(l.rest) > 0 {
		l.Log(l.Name + ": " + string(l.rest))
		l.rest = nil
	}
}
