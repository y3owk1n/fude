// Package relay passes a share's connections between a host and its
// guests when neither can reach the other (D41). Each side dials the
// relay and names a room. The relay copies bytes between them and never
// reads the TLS inside.
package relay

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Port is the relay's port when an address names none (B251).
const Port = "7418"

// hello is the first line a connection sends the relay.
type hello struct {
	Room string `json:"room"`
	Role string `json:"role"` // "share" or "join"
}

// reply is a line the relay sends, empty when all is well.
type reply struct {
	Error string `json:"error,omitempty"`
}

// conn is a connection whose first lines came through r, which may hold
// the bytes after them.
type conn struct {
	net.Conn
	r *bufio.Reader
}

func (c conn) Read(p []byte) (int, error) { return c.r.Read(p) }

// WithPort returns addr with the relay's port when it names none.
func WithPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return net.JoinHostPort(addr, Port)
	}
	return addr
}

// Serve relays the connections l accepts until it closes, then closes
// every place still held.
func Serve(l net.Listener) error {
	var mu sync.Mutex
	places := map[string]conn{}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range places {
			c.Close()
		}
	}()
	for {
		raw, err := l.Accept()
		if err != nil {
			return err
		}
		go func() {
			c := conn{raw, bufio.NewReader(raw)}
			raw.SetReadDeadline(time.Now().Add(10 * time.Second))
			var h hello
			line, err := c.r.ReadSlice('\n')
			if err != nil || json.Unmarshal(line, &h) != nil || h.Room == "" {
				raw.Close()
				return
			}
			raw.SetReadDeadline(time.Time{})
			mu.Lock()
			held, ok := places[h.Room]
			switch {
			case h.Role == "share":
				// A later share takes the place, as a host that restarted would.
				if ok {
					held.Close()
				}
				places[h.Room] = c
				mu.Unlock()
				send(c, reply{})
				return
			case h.Role == "join" && ok:
				delete(places, h.Room)
				mu.Unlock()
			default:
				mu.Unlock()
				send(c, reply{Error: "no one shares in that room"})
				raw.Close()
				return
			}
			if send(held, reply{}) != nil || send(c, reply{}) != nil {
				held.Close()
				raw.Close()
				return
			}
			pass(held, c)
		}()
	}
}

// send writes r as one line to c.
func send(c net.Conn, r reply) error {
	return json.NewEncoder(c).Encode(r)
}

// pass copies bytes between a and b until either closes, then closes both.
func pass(a, b net.Conn) {
	done := make(chan struct{}, 2)
	for _, p := range [][2]net.Conn{{a, b}, {b, a}} {
		go func() {
			io.Copy(p[0], p[1])
			done <- struct{}{}
		}()
	}
	<-done
	a.Close()
	b.Close()
	<-done
}

// dial connects to the relay at addr, says hello in room as role, and
// reads its answer.
func dial(addr, room, role string) (conn, error) {
	raw, err := net.Dial("tcp", WithPort(addr))
	if err != nil {
		return conn{}, err
	}
	c := conn{raw, bufio.NewReader(raw)}
	if err := json.NewEncoder(raw).Encode(hello{room, role}); err != nil {
		raw.Close()
		return conn{}, err
	}
	if err := answer(c); err != nil {
		raw.Close()
		return conn{}, err
	}
	return c, nil
}

// answer reads one line of the relay's, and returns the error it names.
func answer(c conn) error {
	line, err := c.r.ReadSlice('\n')
	if err != nil {
		return fmt.Errorf("the relay closed: %w", err)
	}
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		return fmt.Errorf("the relay answered %q", line)
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

// Place is a host's place in a room, which one guest takes.
type Place struct{ c conn }

// Offer holds a place in room at the relay at addr.
func Offer(addr, room string) (*Place, error) {
	c, err := dial(addr, room, "share")
	if err != nil {
		return nil, err
	}
	return &Place{c}, nil
}

// Take waits until a guest takes the place, and returns the connection
// to it.
func (p *Place) Take() (net.Conn, error) {
	if err := answer(p.c); err != nil {
		return nil, err
	}
	return p.c, nil
}

// Close gives the place up.
func (p *Place) Close() error { return p.c.Close() }

// Join returns a connection to the host holding a place in room at the
// relay at addr.
func Join(addr, room string) (net.Conn, error) {
	return dial(addr, room, "join")
}
