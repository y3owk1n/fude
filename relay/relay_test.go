package relay_test

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/y3owk1n/fude/relay"
)

// B251, bytes a guest sends as soon as it joins reach the host, even when
// they come with the relay's line that a guest took the place.
func TestB251PassesEarlyBytes(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go relay.Serve(l)
	p, err := relay.Offer(l.Addr().String(), "room")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g, err := relay.Join(l.Addr().String(), "room")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if _, err := g.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	// The bytes reach the host before it reads the relay's line.
	time.Sleep(50 * time.Millisecond)
	c, err := p.Take()
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "hello" {
		t.Errorf("the host read %q and %v, want hello", got, err)
	}
}
